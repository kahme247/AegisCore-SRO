package model

import (
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/sirupsen/logrus"
	"sync"
)

type NPC struct {
	SRObject
	Type   string
	Mutex  *sync.Mutex
	rarity MonsterRarity
	BodyState
	LifeState
	MotionState
	WalkSpeed float32
	RunSpeed  float32
	HwanSpeed float32
	// The configured flags distinguish authored zero speeds (stationary
	// creatures) from legacy NPCs whose reference speeds were never loaded.
	walkSpeedConfigured bool
	runSpeedConfigured  bool
	combat              npcCombatState
	ai                  npcAIState
	spawnArea           *SpawnArea
}

func (n *NPC) SetRarity(rarity byte) {
	if n == nil {
		return
	}
	n.combat.mutex.Lock()
	// Preserve authored extension values. Valid identifies the stock classes
	// AegisCore understands semantically, but the wire byte is forward-compatible.
	n.rarity = MonsterRarity(rarity)
	n.combat.mutex.Unlock()
}

func (n *NPC) Rarity() byte {
	if n == nil {
		return 0
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	return byte(n.rarity)
}

func (n *NPC) MonsterRarity() MonsterRarity {
	if n == nil {
		return MonsterNormal
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	return n.rarity
}

func (n *NPC) BindSpawnArea(area *SpawnArea) {
	if n == nil {
		return
	}
	n.combat.mutex.Lock()
	n.spawnArea = area
	n.combat.mutex.Unlock()

	// Spread idle movement across the wander interval. Starting every spawned
	// mob on the same tick creates a large movement and packet burst.
	n.ai.mutex.Lock()
	n.ai.nextWander = time.Now().Add(3*time.Second + time.Duration(n.GetUniqueID()%8000)*time.Millisecond)
	n.ai.mutex.Unlock()
}

func (n *NPC) QueueRespawn() bool {
	if n == nil {
		return false
	}
	n.combat.mutex.Lock()
	area := n.spawnArea
	n.combat.mutex.Unlock()
	return area != nil && area.MarkNPCDead(n.GetUniqueID())
}

type npcCombatState struct {
	mutex      sync.Mutex
	hitPoints  int
	maxHit     int
	experience uint64
	decay      time.Duration
	statuses   map[CombatStatusKind]map[uint32]CombatStatus
	baseSpeed  *npcBaseSpeed
}

type npcBaseSpeed struct {
	walk float32
	run  float32
}

type CombatStatusKind byte

const (
	CombatStatusShock CombatStatusKind = iota + 1
	CombatStatusBurn
	CombatStatusFreeze
	CombatStatusFrostbite
	CombatStatusParryDown
	CombatStatusHitDown
	CombatStatusPhysicalDefenseDown
	CombatStatusMagicalDefenseDown
	CombatStatusAttackDown
	CombatStatusDamageTakenUp
	CombatStatusStun
	CombatStatusPoison
	CombatStatusZombie
	CombatStatusSleep
	CombatStatusBind
	CombatStatusDull
	CombatStatusFear
	CombatStatusShortSight
	CombatStatusBleed
	CombatStatusPetrify
	CombatStatusDarkness
	CombatStatusDisease
	CombatStatusConfusion
	CombatStatusPanic
	CombatStatusCombustion
	CombatStatusHidden
)

type CombatStatus struct {
	Kind      CombatStatusKind
	Token     uint32
	SourceUID uint32
	SkillID   uint32
	Power     int
	Level     int
	Payload   int
	ExpiresAt time.Time
	Stackable bool
}

func CombatStatusBadEffectBit(kind CombatStatusKind) uint32 {
	switch kind {
	case CombatStatusFreeze:
		return 1 << 0
	case CombatStatusFrostbite:
		return 1 << 1
	case CombatStatusShock:
		return 1 << 2
	case CombatStatusBurn:
		return 1 << 3
	case CombatStatusPoison:
		return 1 << 4
	case CombatStatusZombie:
		return 1 << 5
	case CombatStatusSleep:
		return 1 << 6
	case CombatStatusBind:
		return 1 << 7
	case CombatStatusDull:
		return 1 << 8
	case CombatStatusFear:
		return 1 << 9
	case CombatStatusShortSight:
		return 1 << 10
	case CombatStatusBleed:
		return 1 << 11
	case CombatStatusPetrify:
		return 1 << 12
	case CombatStatusDarkness:
		return 1 << 13
	case CombatStatusStun:
		return 1 << 14
	case CombatStatusDisease:
		return 1 << 15
	case CombatStatusConfusion:
		return 1 << 16
	case CombatStatusPhysicalDefenseDown:
		return 1 << 17 // Decay
	case CombatStatusMagicalDefenseDown:
		return 1 << 18 // Weak
	case CombatStatusAttackDown:
		return 1 << 19 // Impotent
	case CombatStatusDamageTakenUp:
		return 1 << 20 // Division
	case CombatStatusPanic:
		return 1 << 21
	case CombatStatusCombustion:
		return 1 << 22
	case CombatStatusHidden:
		return 1 << 24
	default:
		return 0
	}
}

var combatBadEffectKinds = []CombatStatusKind{
	CombatStatusFreeze, CombatStatusFrostbite, CombatStatusShock, CombatStatusBurn,
	CombatStatusPoison, CombatStatusZombie, CombatStatusSleep, CombatStatusBind,
	CombatStatusDull, CombatStatusFear, CombatStatusShortSight, CombatStatusBleed,
	CombatStatusPetrify, CombatStatusDarkness, CombatStatusStun, CombatStatusDisease,
	CombatStatusConfusion, CombatStatusPhysicalDefenseDown, CombatStatusMagicalDefenseDown,
	CombatStatusAttackDown, CombatStatusDamageTakenUp, CombatStatusPanic,
	CombatStatusCombustion, CombatStatusHidden,
}

// CombatBadEffectState returns the stock client's abnormal-state bitmask and
// level bytes. Levels are present only for effects above Zombie, in ascending
// bit order; among the currently supported states that means Stun.
func (n *NPC) CombatBadEffectState(now time.Time) (uint32, []byte) {
	var mask uint32
	levels := make([]byte, 0, 1)
	for _, kind := range combatBadEffectKinds {
		status, active := n.CombatStatus(kind, now)
		if !active {
			continue
		}
		mask |= CombatStatusBadEffectBit(kind)
		if CombatStatusBadEffectBit(kind) > 1<<5 {
			level := status.Level
			if level < 1 {
				level = status.Power
			}
			if level > 255 {
				level = 255
			}
			levels = append(levels, byte(level))
		}
	}
	return mask, levels
}

func (n *NPC) RemoveCombatStatus(kind CombatStatusKind, token uint32) bool {
	if n == nil || kind == 0 {
		return false
	}
	n.combat.mutex.Lock()
	statuses, exists := n.combat.statuses[kind]
	if !exists || len(statuses) == 0 {
		n.combat.mutex.Unlock()
		return false
	}
	removed := false
	if token == 0 {
		delete(n.combat.statuses, kind)
		removed = true
	} else if _, exists := statuses[token]; exists {
		delete(statuses, token)
		removed = true
		if len(statuses) == 0 {
			delete(n.combat.statuses, kind)
		}
	}
	remaining := len(n.combat.statuses[kind])
	n.combat.mutex.Unlock()
	if removed && remaining == 0 {
		n.restoreStatusMovementEffect(kind)
	}
	return removed
}

func (n *NPC) CureCombatStatuses(mask uint32, maxLevel, count int, now time.Time) int {
	return n.CureCombatStatusesWithCurse(mask, maxLevel, false, 0, 100, count, now, nil)
}

func (n *NPC) CureCombatStatusesWithCurse(statusMask uint32, statusMaxLevel int, cureCurse bool, curseMaxLevel, curseChance, count int, now time.Time, roll func(int) bool) int {
	removed := 0
	for _, kind := range combatBadEffectKinds {
		bit := CombatStatusBadEffectBit(kind)
		if bit == 0 || count > 0 && removed >= count {
			continue
		}
		status, active := n.CombatStatus(kind, now)
		if !active || !combatCureEligible(bit, status.Level, statusMask, statusMaxLevel, cureCurse, curseMaxLevel) {
			continue
		}
		if bit > 1<<5 && roll != nil && !roll(curseChance) {
			continue
		}
		if n.RemoveCombatStatus(kind, status.Token) {
			removed++
		}
	}
	return removed
}

func combatCureEligible(bit uint32, level int, statusMask uint32, statusMaxLevel int, cureCurse bool, curseMaxLevel int) bool {
	if bit <= 1<<5 {
		return statusMask&bit != 0 && (statusMaxLevel <= 0 || level <= statusMaxLevel)
	}
	return cureCurse && (curseMaxLevel <= 0 || level <= curseMaxLevel)
}

func (n *NPC) ApplyCombatStatus(status CombatStatus) bool {
	return n.ApplyCombatStatusAt(status, time.Now())
}

// ApplyCombatStatusAt is the timestamped status-admission form used by
// authoritative combat execution and deterministic tests.
func (n *NPC) ApplyCombatStatusAt(status CombatStatus, now time.Time) bool {
	if n == nil || status.Kind == 0 || status.SkillID == 0 || status.ExpiresAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	n.combat.mutex.Lock()
	if n.LifeState == Dead || n.combat.hitPoints <= 0 {
		n.combat.mutex.Unlock()
		return false
	}
	if n.combat.statuses == nil {
		n.combat.statuses = make(map[CombatStatusKind]map[uint32]CombatStatus)
	}
	// SR_GameServer's tisc record is abnormal type 0x14. Its record has
	// priority 0 and class 2, so 0x004A4270 rejects another active record of
	// the same type instead of stacking or replacing it.
	if status.Kind == CombatStatusDamageTakenUp {
		for _, current := range n.combat.statuses[status.Kind] {
			if current.ExpiresAt.After(now) {
				n.combat.mutex.Unlock()
				return false
			}
		}
	}
	if !status.Stackable {
		delete(n.combat.statuses, status.Kind)
	}
	if n.combat.statuses[status.Kind] == nil {
		n.combat.statuses[status.Kind] = make(map[uint32]CombatStatus)
	}
	n.combat.statuses[status.Kind][status.Token] = status
	n.combat.mutex.Unlock()
	n.applyStatusMovementEffect(status.Kind, status.Power, now)
	return true
}

func (n *NPC) CombatStatus(kind CombatStatusKind, now time.Time) (CombatStatus, bool) {
	if n == nil || kind == 0 {
		return CombatStatus{}, false
	}
	n.combat.mutex.Lock()
	statuses, exists := n.combat.statuses[kind]
	if !exists || len(statuses) == 0 {
		n.combat.mutex.Unlock()
		return CombatStatus{}, false
	}
	selected := CombatStatus{}
	selectedSet := false
	for token, status := range statuses {
		if !status.ExpiresAt.After(now) {
			delete(statuses, token)
			continue
		}
		if !selectedSet || status.Level > selected.Level || status.Level == selected.Level && (status.ExpiresAt.After(selected.ExpiresAt) || status.ExpiresAt.Equal(selected.ExpiresAt) && status.Token > selected.Token) {
			selected = status
			selectedSet = true
		}
	}
	if len(statuses) == 0 {
		delete(n.combat.statuses, kind)
	}
	n.combat.mutex.Unlock()
	if !selectedSet {
		n.restoreStatusMovementEffect(kind)
		return CombatStatus{}, false
	}
	return selected, true
}

func (n *NPC) CombatStatusByToken(kind CombatStatusKind, token uint32, now time.Time) (CombatStatus, bool) {
	if n == nil || kind == 0 || token == 0 {
		return CombatStatus{}, false
	}
	n.combat.mutex.Lock()
	statuses := n.combat.statuses[kind]
	status, exists := statuses[token]
	if exists && !status.ExpiresAt.After(now) {
		delete(statuses, token)
		if len(statuses) == 0 {
			delete(n.combat.statuses, kind)
		}
		exists = false
	}
	remaining := len(n.combat.statuses[kind])
	n.combat.mutex.Unlock()
	if !exists && remaining == 0 {
		n.restoreStatusMovementEffect(kind)
	}
	return status, exists
}

func (n *NPC) applyStatusMovementEffect(kind CombatStatusKind, power int, now time.Time) {
	if n == nil || (kind != CombatStatusFreeze && kind != CombatStatusFrostbite) {
		return
	}
	n.combat.mutex.Lock()
	if n.combat.baseSpeed == nil {
		n.combat.baseSpeed = &npcBaseSpeed{walk: n.GetWalkSpeed(), run: n.GetRunSpeed()}
	}
	base := *n.combat.baseSpeed
	n.combat.mutex.Unlock()
	if kind == CombatStatusFreeze {
		n.WalkSpeed = 0
		n.RunSpeed = 0
		return
	}
	// Frostbite's first parameter is the abnormal-state level used for
	// resistance, not a speed percentage. The vSRO state itself halves speed.
	reduction := 50
	factor := float32(100-reduction) / 100
	n.WalkSpeed = base.walk * factor
	n.RunSpeed = base.run * factor
}

func (n *NPC) restoreStatusMovementEffect(kind CombatStatusKind) {
	if n == nil || (kind != CombatStatusFreeze && kind != CombatStatusFrostbite) {
		return
	}
	n.combat.mutex.Lock()
	base := n.combat.baseSpeed
	if kind == CombatStatusFreeze {
		n.combat.baseSpeed = nil
	} else if frozen := len(n.combat.statuses[CombatStatusFreeze]) > 0; frozen {
		n.combat.mutex.Unlock()
		return
	}
	n.combat.mutex.Unlock()
	if base == nil {
		return
	}
	n.WalkSpeed = base.walk
	n.RunSpeed = base.run
}

type npcAIState struct {
	mutex          sync.Mutex
	level          int
	aggressive     bool
	sightRange     float32
	attackSkillID  uint32
	targetUID      uint32
	threat         map[uint32]int64
	nextAttack     time.Time
	nextWander     time.Time
	nextPursuit    time.Time
	pursuitPending bool
	aggroCandidate uint32
	aggroReadyAt   time.Time
}

func (n *NPC) ConfigureAI(level int, aggressive bool, sightRange int) {
	if n == nil {
		return
	}
	if level < 1 {
		level = 1
	}
	if sightRange <= 0 {
		sightRange = 100
	}
	n.ai.mutex.Lock()
	n.ai.level = level
	n.ai.aggressive = aggressive
	n.ai.sightRange = float32(sightRange)
	n.ai.nextWander = time.Now().Add(3 * time.Second)
	n.ai.mutex.Unlock()
}

func (n *NPC) AIProfile() (level int, aggressive bool, sightRange float32, targetUID uint32) {
	if n == nil {
		return 1, false, 0, 0
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	return n.ai.level, n.ai.aggressive, n.ai.sightRange, n.ai.targetUID
}

// ConfigureAttackSkill binds the immutable character-reference default skill
// used for this NPC's basic attack animation. A zero value means the reference
// row does not define an attack and must not be replaced with a player skill.
func (n *NPC) ConfigureAttackSkill(skillID uint32) {
	if n == nil {
		return
	}
	n.ai.mutex.Lock()
	n.ai.attackSkillID = skillID
	n.ai.mutex.Unlock()
}

func (n *NPC) AttackSkillID() uint32 {
	if n == nil {
		return 0
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	return n.ai.attackSkillID
}

func (n *NPC) SetAggroTarget(uid uint32) {
	if n == nil {
		return
	}
	n.ai.mutex.Lock()
	if uid != 0 {
		if n.ai.threat == nil {
			n.ai.threat = make(map[uint32]int64)
		}
		if n.ai.threat[uid] < 1 {
			n.ai.threat[uid] = 1
		}
	}
	if n.ai.targetUID != uid {
		n.ai.pursuitPending = uid != 0
	}
	n.ai.targetUID = uid
	if uid != 0 {
		n.ai.aggroCandidate = 0
		n.ai.aggroReadyAt = time.Time{}
	}
	n.ai.mutex.Unlock()
}

// ReadyToAcquireAggro implements the ordinary SR_GameServer awareness phase:
// seeing a player starts a reaction timer; it does not instantly commit the
// monster to pursuit. Damage-created threat still calls SetAggroTarget and
// therefore retaliates immediately.
func (n *NPC) ReadyToAcquireAggro(uid uint32, now time.Time, delay time.Duration) bool {
	if n == nil || uid == 0 {
		return false
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	if n.ai.targetUID != 0 {
		return true
	}
	if n.ai.aggroCandidate != uid {
		n.ai.aggroCandidate = uid
		n.ai.aggroReadyAt = now.Add(delay)
		return false
	}
	return !now.Before(n.ai.aggroReadyAt)
}

func (n *NPC) ClearAggroCandidate() {
	if n == nil {
		return
	}
	n.ai.mutex.Lock()
	n.ai.aggroCandidate = 0
	n.ai.aggroReadyAt = time.Time{}
	n.ai.mutex.Unlock()
}

func (n *NPC) ClearAggroTarget(uid uint32) {
	if n == nil {
		return
	}
	n.ai.mutex.Lock()
	if uid == 0 {
		clear(n.ai.threat)
	} else {
		delete(n.ai.threat, uid)
	}
	n.selectHighestThreatLocked()
	n.ai.mutex.Unlock()
}

func (n *NPC) AddAggro(uid uint32, amount int) {
	if n == nil || uid == 0 || amount <= 0 {
		return
	}
	n.ai.mutex.Lock()
	if n.ai.threat == nil {
		n.ai.threat = make(map[uint32]int64)
	}
	n.ai.threat[uid] += int64(amount)
	n.selectHighestThreatLocked()
	n.ai.mutex.Unlock()
}

func (n *NPC) ReduceAggro(uid uint32, amount int) {
	if n == nil || uid == 0 || amount <= 0 {
		return
	}
	n.ai.mutex.Lock()
	remaining := n.ai.threat[uid] - int64(amount)
	if remaining > 0 {
		n.ai.threat[uid] = remaining
	} else {
		delete(n.ai.threat, uid)
	}
	n.selectHighestThreatLocked()
	n.ai.mutex.Unlock()
}

func (n *NPC) AggroValue(uid uint32) int64 {
	if n == nil || uid == 0 {
		return 0
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	return n.ai.threat[uid]
}

func (n *NPC) selectHighestThreatLocked() {
	previous := n.ai.targetUID
	var selected uint32
	var highest int64
	for uid, threat := range n.ai.threat {
		if threat > highest || threat == highest && (selected == 0 || uid < selected) {
			selected, highest = uid, threat
		}
	}
	n.ai.targetUID = selected
	if selected != previous {
		n.ai.pursuitPending = selected != 0
	}
	if selected == 0 {
		n.ai.pursuitPending = false
	}
}

// ReadyToPursue lets a newly acquired or changed target override an in-flight
// wander immediately. Once a pursuit route is active, it is allowed to finish
// before another route is emitted; replacing an authoritative route every
// refresh interval makes the stock client repeatedly restart interpolation.
func (n *NPC) ReadyToPursue(now time.Time, interval time.Duration, routeActive bool) bool {
	if n == nil {
		return false
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	if !n.ai.pursuitPending && routeActive {
		return false
	}
	if !n.ai.pursuitPending && now.Before(n.ai.nextPursuit) {
		return false
	}
	n.ai.pursuitPending = false
	n.ai.nextPursuit = now.Add(interval)
	return true
}

func (n *NPC) ReadyToAttack(now time.Time, cooldown time.Duration) bool {
	if n == nil {
		return false
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	if now.Before(n.ai.nextAttack) {
		return false
	}
	n.ai.nextAttack = now.Add(cooldown)
	return true
}

func (n *NPC) ReadyToWander(now time.Time, delay time.Duration) bool {
	if n == nil {
		return false
	}
	n.ai.mutex.Lock()
	defer n.ai.mutex.Unlock()
	if now.Before(n.ai.nextWander) {
		return false
	}
	n.ai.nextWander = now.Add(delay)
	return true
}

func (n *NPC) SpawnArea() *SpawnArea {
	if n == nil {
		return nil
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	return n.spawnArea
}

func (n *NPC) SendToKnown(packet *network.Packet) {
	if n == nil || packet == nil || n.GetKnownObjectList() == nil {
		return
	}
	for _, object := range n.GetKnownObjectList().GetKnownObjects() {
		player, ok := object.(*Player)
		if !ok || player == nil || player.GetSession() == nil {
			continue
		}
		knownList := player.GetCharKnownObjectList()
		if knownList == nil || !knownList.ClientKnows(n.GetUniqueID()) {
			continue
		}
		_ = player.GetSession().Send(*packet)
	}
}

// ConfigureCombat initializes a spawned mob's health and XP reward from its
// immutable reference row. Normal NPCs simply receive zero values and cannot
// be treated as combat targets.
func (n *NPC) ConfigureCombat(maxHitPoints int, experience uint64) {
	if n == nil {
		return
	}
	n.combat.mutex.Lock()
	n.combat.maxHit = maxHitPoints
	n.combat.hitPoints = maxHitPoints
	n.combat.experience = experience
	n.combat.mutex.Unlock()
}

func (n *NPC) ConfigureDecayTime(milliseconds int) {
	if n == nil {
		return
	}
	n.combat.mutex.Lock()
	if milliseconds > 0 {
		n.combat.decay = time.Duration(milliseconds) * time.Millisecond
	}
	n.combat.mutex.Unlock()
}

func (n *NPC) DecayDuration() time.Duration {
	if n == nil {
		return 5 * time.Second
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	if n.combat.decay > 0 {
		return n.combat.decay
	}
	return 5 * time.Second
}

// ApplyDamage atomically changes an alive mob's health. It returns the
// effective damage and whether this hit killed the mob. A dead/non-combat NPC
// accepts no further damage, preventing duplicate EXP rewards from repeated
// client packets.
func (n *NPC) ApplyDamage(damage int) (applied int, killed bool) {
	if n == nil || damage <= 0 {
		return 0, false
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	if n.LifeState == Dead || n.combat.hitPoints <= 0 {
		return 0, false
	}
	if damage > n.combat.hitPoints {
		damage = n.combat.hitPoints
	}
	n.combat.hitPoints -= damage
	if n.combat.hitPoints == 0 {
		n.SetLifeState(Dead)
		return damage, true
	}
	return damage, false
}

func (n *NPC) ExperienceReward() uint64 {
	if n == nil {
		return 0
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	return n.combat.experience
}

// CurrentHitPoints returns a locked health snapshot for selection and combat
// response packets.
func (n *NPC) CurrentHitPoints() int {
	if n == nil {
		return 0
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	return n.combat.hitPoints
}

func (n *NPC) GetPosition() Position {
	return n.Position
}

func (n *NPC) SetPosition(position Position) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.Position = position
	logrus.Tracef("changed position of %s to %v", n.Name, n.Position)
}

func (n *NPC) GetType() string {
	return n.Type
}

func (n *NPC) GetUniqueID() uint32 {
	return n.UniqueID
}

func (n *NPC) SetUniqueID(uniqueId uint32) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.UniqueID = uniqueId
}

func (n *NPC) GetBodyState() BodyState {
	return n.BodyState
}

func (n *NPC) GetLifeState() LifeState {
	return n.LifeState
}

func (n *NPC) GetMotionState() MotionState {
	return n.MotionState
}

func (n *NPC) GetWalkSpeed() float32 {
	if n.Mutex == nil {
		if n.walkSpeedConfigured || n.WalkSpeed > 0 {
			return n.WalkSpeed
		}
		return 16
	}
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	if n.walkSpeedConfigured || n.WalkSpeed > 0 {
		return n.WalkSpeed
	}
	return 16
}

func (n *NPC) GetRunSpeed() float32 {
	if n.Mutex == nil {
		if n.runSpeedConfigured || n.RunSpeed > 0 {
			return n.RunSpeed
		}
		return 50
	}
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	if n.runSpeedConfigured || n.RunSpeed > 0 {
		return n.RunSpeed
	}
	return 50
}

func (n *NPC) GetHwanSpeed() float32 {
	if n.Mutex == nil {
		if n.HwanSpeed > 0 {
			return n.HwanSpeed
		}
		return 100
	}
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	if n.HwanSpeed > 0 {
		return n.HwanSpeed
	}
	return 100
}

func (n *NPC) SetBodyState(state BodyState) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.BodyState = state
}

func (n *NPC) SetLifeState(state LifeState) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.LifeState = state
}

func (n *NPC) SetMotionState(state MotionState) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.MotionState = state
}

func (n *NPC) SetName(name string) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.Name = name
}

func (n *NPC) MoveToPosition(position Position) {
	// TODO implement
}

func (n *NPC) UpdatePosition() bool {
	// TODO implement
	return true
}

func (n *NPC) SetWalkSpeed(speed float32) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.WalkSpeed = speed
	n.walkSpeedConfigured = true
}

func (n *NPC) SetRunSpeed(speed float32) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.RunSpeed = speed
	n.runSpeedConfigured = true
}

// ConfigureMovementSpeeds applies the pair authored by the character
// reference. A zero/zero pair is meaningful: plant-like monsters use it to
// declare that they must remain rooted in place.
func (n *NPC) ConfigureMovementSpeeds(walk, run float32) {
	if n == nil {
		return
	}
	if n.Mutex == nil {
		n.WalkSpeed = walk
		n.RunSpeed = run
		n.walkSpeedConfigured = true
		n.runSpeedConfigured = true
		return
	}
	n.Mutex.Lock()
	n.WalkSpeed = walk
	n.RunSpeed = run
	n.walkSpeedConfigured = true
	n.runSpeedConfigured = true
	n.Mutex.Unlock()
}

func (n *NPC) HasStationaryMovementProfile() bool {
	if n == nil {
		return false
	}
	if n.Mutex == nil {
		return n.walkSpeedConfigured && n.runSpeedConfigured && n.WalkSpeed <= 0 && n.RunSpeed <= 0
	}
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	return n.walkSpeedConfigured && n.runSpeedConfigured && n.WalkSpeed <= 0 && n.RunSpeed <= 0
}

func (n *NPC) SetHwanSpeed(speed float32) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.HwanSpeed = speed
}

func (n *NPC) GetHwanLevel() byte { return 0 }

func (n *NPC) GetMovementSpeed() float32 {
	return n.GetRunSpeed()
}

func (n *NPC) StopMovement() {
	n.CancelMovement(n.GetNavmeshPosition(), "legacy-stop")
	n.SetMotionState(NoMotion)
}

func (n *NPC) SendPositionUpdate() {
	packet := BuildPositionUpdatePacket(n)
	n.SendToKnown(&packet)
}
