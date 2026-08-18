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
	rarity byte
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
	n.rarity = rarity
	n.combat.mutex.Unlock()
}

func (n *NPC) Rarity() byte {
	if n == nil {
		return 0
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
	statuses   map[CombatStatusKind]CombatStatus
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
)

type CombatStatus struct {
	Kind      CombatStatusKind
	Token     uint32
	SourceUID uint32
	SkillID   uint32
	Power     int
	ExpiresAt time.Time
}

func (n *NPC) ApplyCombatStatus(status CombatStatus) bool {
	if n == nil || status.Kind == 0 || status.SkillID == 0 || status.ExpiresAt.IsZero() {
		return false
	}
	n.combat.mutex.Lock()
	if n.LifeState == Dead || n.combat.hitPoints <= 0 {
		n.combat.mutex.Unlock()
		return false
	}
	if n.combat.statuses == nil {
		n.combat.statuses = make(map[CombatStatusKind]CombatStatus)
	}
	n.combat.statuses[status.Kind] = status
	n.combat.mutex.Unlock()
	n.applyStatusMovementEffect(status.Kind, status.Power, time.Now())
	return true
}

func (n *NPC) CombatStatus(kind CombatStatusKind, now time.Time) (CombatStatus, bool) {
	if n == nil || kind == 0 {
		return CombatStatus{}, false
	}
	n.combat.mutex.Lock()
	status, exists := n.combat.statuses[kind]
	if !exists {
		n.combat.mutex.Unlock()
		return CombatStatus{}, false
	}
	if !status.ExpiresAt.After(now) {
		delete(n.combat.statuses, kind)
		n.combat.mutex.Unlock()
		n.restoreStatusMovementEffect(kind)
		return CombatStatus{}, false
	}
	n.combat.mutex.Unlock()
	return status, true
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
	} else if _, frozen := n.combat.statuses[CombatStatusFreeze]; frozen {
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
	nextAttack     time.Time
	nextWander     time.Time
	nextPursuit    time.Time
	pursuitPending bool
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
	if n.ai.targetUID != uid {
		n.ai.pursuitPending = uid != 0
	}
	n.ai.targetUID = uid
	n.ai.mutex.Unlock()
}

func (n *NPC) ClearAggroTarget(uid uint32) {
	if n == nil {
		return
	}
	n.ai.mutex.Lock()
	if uid == 0 || n.ai.targetUID == uid {
		n.ai.targetUID = 0
		n.ai.pursuitPending = false
	}
	n.ai.mutex.Unlock()
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
