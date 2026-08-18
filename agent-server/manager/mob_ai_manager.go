package manager

import (
	"context"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/g3n/engine/math32"
	log "github.com/sirupsen/logrus"
)

const (
	mobAITickRate        = 50 * time.Millisecond
	mobAttackRange       = float32(25) // Compatibility fallback for incomplete catalogs.
	mobAttackCooldown    = 1500 * time.Millisecond
	mobAttackClearance   = float32(5)
	mobArrivalMargin     = float32(2)
	mobWanderDelay       = 8 * time.Second
	mobPursuitRefresh    = time.Second
	maximumMobWireDamage = 0xFFFFFF
	// B071 must arrive after B070 has created the client action; otherwise the
	// stock client applies HP but discards the hit effect and damage text.
	mobClientActionRegistrationDelay = 100 * time.Millisecond
)

type mobAIWorld interface {
	GetNPCs() []*model.NPC
	GetNPCsKnownToPlayers() []*model.NPC
	RegisterMovingCharacter(model.ICharacter, model.MovementData) model.MovementData
	StopMovingCharacter(model.ICharacter, navmeshv2.RtNavmeshPosition, string, bool) bool
	NewPosFromGlobalCoordinates(*math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
	CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool
}

type MobAIManager struct {
	Manager
	stateMu        sync.Mutex
	referenceData  *model.ReferenceDataCatalog
	vitals         model.CharacterVitalsRepository
	pending        map[uint32]mobPendingAttack
	nextTrace      time.Time
	hitRollPercent func(int) bool
}

func (m *MobAIManager) ConfigureVitals(repository model.CharacterVitalsRepository) {
	if m == nil {
		return
	}
	m.stateMu.Lock()
	m.vitals = repository
	m.stateMu.Unlock()
}

type mobAttackProfile struct {
	reach     float32
	delay     time.Duration
	cooldown  time.Duration
	attackMin int
	attackMax int
	magical   bool
}

type mobPendingAttack struct {
	npc       *model.NPC
	targetUID uint32
	castUID   uint32
	damage    int
	magical   bool
	readyAt   time.Time
}

var mobAIManagerInstance *MobAIManager
var mobAIManagerOnce sync.Once

func GetMobAIManagerInstance() *MobAIManager {
	mobAIManagerOnce.Do(func() {
		mobAIManagerInstance = &MobAIManager{
			pending:        make(map[uint32]mobPendingAttack),
			hitRollPercent: func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance },
		}
		mobAIManagerInstance.Name = "MobAIManager"
		mobAIManagerInstance.initialDelay = time.Second
		mobAIManagerInstance.rate = mobAITickRate
		mobAIManagerInstance.runnerFunc = mobAIManagerInstance.run
	})
	return mobAIManagerInstance
}

// ConfigureReferenceData supplies the immutable character and skill catalogs
// used to derive collision-aware reach, action delay, and cooldown.
func (m *MobAIManager) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) {
	if m == nil {
		return
	}
	m.stateMu.Lock()
	m.referenceData = catalog
	m.stateMu.Unlock()
}

func (m *MobAIManager) run() {
	world := service.GetWorldServiceInstance()
	for m.started {
		select {
		case now := <-m.ticker.C:
			m.tick(world, now)
		}
	}
}

func (m *MobAIManager) tick(world mobAIWorld, now time.Time) {
	if world == nil {
		return
	}
	active := world.GetNPCsKnownToPlayers()
	pending := m.pendingNPCs()
	byUID := make(map[uint32]*model.NPC, len(active)+len(pending))
	for _, npc := range active {
		if npc != nil {
			byUID[npc.GetUniqueID()] = npc
		}
	}
	for _, npc := range pending {
		if npc != nil {
			byUID[npc.GetUniqueID()] = npc
		}
	}
	npcs := make([]*model.NPC, 0, len(byUID))
	for _, npc := range byUID {
		npcs = append(npcs, npc)
	}
	sort.Slice(npcs, func(i, j int) bool { return npcs[i].GetUniqueID() < npcs[j].GetUniqueID() })
	for _, npc := range npcs {
		m.tickNPC(world, npc, now)
	}
	m.traceTickSummary(world, len(active), now)
}

func (m *MobAIManager) traceTickSummary(world mobAIWorld, active int, now time.Time) {
	if !mobAITraceEnabled() || world == nil {
		return
	}
	m.stateMu.Lock()
	if now.Before(m.nextTrace) {
		m.stateMu.Unlock()
		return
	}
	m.nextTrace = now.Add(5 * time.Second)
	m.stateMu.Unlock()

	npcs := world.GetNPCs()
	var mobs, aggressive, skilled, aware, targeted int
	for _, npc := range npcs {
		if npc == nil || !npc.GetTypeInfo().IsNPCMob() || npc.GetLifeState() != model.Alive {
			continue
		}
		mobs++
		_, isAggressive, _, targetUID := npc.AIProfile()
		if isAggressive {
			aggressive++
		}
		if npc.AttackSkillID() != 0 {
			skilled++
		}
		if targetUID != 0 {
			targeted++
		}
		if known := npc.GetKnownObjectList(); known != nil {
			for _, object := range known.GetKnownObjects() {
				if player, ok := object.(*model.Player); ok && player != nil && player.GetLifeState() == model.Alive {
					aware++
					break
				}
			}
		}
	}
	log.Infof("MOB_AI_TRACE summary npcs=%d alive-mobs=%d aggressive=%d skilled=%d aware=%d targeted=%d active=%d", len(npcs), mobs, aggressive, skilled, aware, targeted, active)
}

func (m *MobAIManager) tickNPC(world mobAIWorld, npc *model.NPC, now time.Time) {
	if npc == nil || !npc.GetTypeInfo().IsNPCMob() {
		return
	}
	if npc.GetLifeState() != model.Alive {
		m.clearPendingAttack(npc.GetUniqueID())
		return
	}
	_, frozen := npc.CombatStatus(model.CombatStatusFreeze, now)
	// Frostbite changes movement immediately and restores it through lazy
	// status expiry, so the AI must observe it even on ticks without attacks.
	_, _ = npc.CombatStatus(model.CombatStatusFrostbite, now)
	_, stunned := npc.CombatStatus(model.CombatStatusStun, now)
	if frozen || stunned {
		m.clearPendingAttack(npc.GetUniqueID())
		if npc.GetMotionState() != model.NoMotion {
			reason := "mob-stunned"
			if frozen {
				reason = "mob-frozen"
			}
			world.StopMovingCharacter(npc, npc.GetNavmeshPosition(), reason, true)
		}
		return
	}
	if m.resolvePendingAttack(world, npc, now) {
		return
	}
	level, aggressive, sightRange, targetUID := npc.AIProfile()
	target := m.resolveTarget(world, npc, targetUID, aggressive, sightRange)
	if target == nil {
		if targetUID != 0 && mobAITraceEnabled() {
			log.Infof("MOB_AI_TRACE lost-target npc=%d target=%d position=%s sight=%.2f", npc.GetUniqueID(), targetUID, npc.GetNavmeshPosition().String(), sightRange)
		}
		npc.ClearAggroTarget(0)
		m.wander(world, npc, now)
		return
	}
	npc.SetAggroTarget(target.GetUniqueID())
	distance := navmeshv2.HorizontalDistance(npc.GetNavmeshPosition(), target.GetNavmeshPosition())
	skillID := npc.AttackSkillID()
	profile, profileFound := m.attackProfile(npc, target, skillID)
	attackRange := mobAttackRange
	if profileFound {
		attackRange = profile.reach
	}
	leash := sightRange * 2
	if leash < 200 {
		leash = 200
	}
	if area := npc.SpawnArea(); area != nil && float32(area.Radius) > leash {
		leash = float32(area.Radius)
	}
	if area := npc.SpawnArea(); area != nil && navmeshv2.HorizontalDistance(area.Position, npc.GetNavmeshPosition()) > leash {
		if mobAITraceEnabled() {
			log.Infof("MOB_AI_TRACE leash npc=%d target=%d position=%s home=%s distance=%.2f limit=%.2f", npc.GetUniqueID(), target.GetUniqueID(), npc.GetNavmeshPosition().String(), area.Position.String(), navmeshv2.HorizontalDistance(area.Position, npc.GetNavmeshPosition()), leash)
		}
		npc.ClearAggroTarget(target.GetUniqueID())
		m.moveTo(world, npc, area.Position, now, false)
		return
	}
	if distance > attackRange {
		movement := npc.GetMovementData()
		routeActive := movement != nil && movement.HasDestination
		if npc.ReadyToPursue(now, mobPursuitRefresh, routeActive) {
			if !world.CanMoveDirectly(npc.GetNavmeshPosition(), target.GetNavmeshPosition()) {
				if npc.GetMotionState() != model.NoMotion {
					world.StopMovingCharacter(npc, npc.GetNavmeshPosition(), "mob-pursuit-obstructed", true)
				}
				return
			}
			if destination, ok := m.standOffDestination(world, npc, target, distance, attackRange); ok {
				if mobAITraceEnabled() {
					log.Infof("MOB_AI_TRACE pursue npc=%d target=%d from=%s target-position=%s destination=%s distance=%.2f reach=%.2f", npc.GetUniqueID(), target.GetUniqueID(), npc.GetNavmeshPosition().String(), target.GetNavmeshPosition().String(), destination.String(), distance, attackRange)
				}
				m.moveTo(world, npc, destination, now, true)
			}
		}
		return
	}
	if npc.GetMotionState() != model.NoMotion {
		world.StopMovingCharacter(npc, npc.GetNavmeshPosition(), "mob-in-attack-range", true)
	}
	if !profileFound || !npc.ReadyToAttack(now, profile.cooldown) {
		return
	}
	if skillID == 0 {
		return
	}
	damage := level * 2
	if profile.attackMin > 0 && profile.attackMax >= profile.attackMin {
		defense := target.DerivedStats().PhyDef
		if profile.magical {
			defense = target.DerivedStats().MagDef
		}
		damage = (profile.attackMin+profile.attackMax)/2 - defense
	}
	if damage < 1 {
		damage = 1
	}
	damage = mobDamageAfterAttackDown(npc, damage, now)
	if damage > maximumMobWireDamage {
		damage = maximumMobWireDamage
	}
	castUID := model.NextActionUID()
	if mobAITraceEnabled() {
		log.Infof("MOB_AI_TRACE cast npc=%d target=%d position=%s target-position=%s distance=%.2f reach=%.2f skill=%d cast=%d delay=%s", npc.GetUniqueID(), target.GetUniqueID(), npc.GetNavmeshPosition().String(), target.GetNavmeshPosition().String(), distance, attackRange, skillID, castUID, profile.delay)
	}
	if profile.delay > 0 {
		m.setPendingAttack(npc.GetUniqueID(), mobPendingAttack{
			npc:       npc,
			targetUID: target.GetUniqueID(),
			castUID:   castUID,
			damage:    damage,
			magical:   profile.magical,
			readyAt:   now.Add(profile.delay),
		})
		begin := model.BuildDelayedAttackBegin(skillID, npc.GetUniqueID(), target.GetUniqueID(), castUID)
		npc.SendToKnown(&begin)
		return
	}
	m.applyAttackDamage(world, npc, target, castUID, damage, false, profile.magical)
}

func mobDamageAfterAttackDown(npc *model.NPC, damage int, now time.Time) int {
	if damage < 1 {
		return 1
	}
	if status, active := npc.CombatStatus(model.CombatStatusAttackDown, now); active {
		reduction := min(max(status.Power, 0), 100)
		return max(1, damage*(100-reduction)/100)
	}
	return damage
}

func (m *MobAIManager) attackProfile(npc *model.NPC, target *model.Player, skillID uint32) (mobAttackProfile, bool) {
	if m == nil || npc == nil || target == nil || skillID == 0 {
		return mobAttackProfile{}, false
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	m.stateMu.Unlock()
	if catalog == nil {
		return mobAttackProfile{}, false
	}
	skill, ok := catalog.Skill(skillID)
	if !ok {
		return mobAttackProfile{}, false
	}

	attackRange := mobAttackClearance + float32(skill.ActionRange)
	if character, exists := catalog.Character(npc.GetRefObjectID()); exists && character.BCRadius > 0 {
		attackRange += float32(character.BCRadius)
	}
	if character, exists := catalog.Character(target.GetRefObjectID()); exists && character.BCRadius > 0 {
		attackRange += float32(character.BCRadius)
	}
	if attackRange <= mobAttackClearance {
		attackRange = mobAttackRange
	}

	// The client plays the full action duration independently. Damage belongs at
	// the authored impact point: preparation plus casting time.
	delay := milliseconds(skill.ActionPreparingTime + skill.ActionCastingTime)
	if delay < mobClientActionRegistrationDelay {
		delay = mobClientActionRegistrationDelay
	}
	cooldown := milliseconds(skill.CoolTime)
	if reuse := milliseconds(skill.ReuseDelay); reuse > cooldown {
		cooldown = reuse
	}
	if delay > cooldown {
		cooldown = delay
	}
	if cooldown <= 0 {
		cooldown = mobAttackCooldown
	}
	return mobAttackProfile{
		reach: attackRange, delay: delay, cooldown: cooldown,
		attackMin: skill.AttackMin, attackMax: skill.AttackMax, magical: skill.IsMagicalAttack(),
	}, true
}

func milliseconds(value int) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Millisecond
}

func mobAITraceEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MOB_AI_TRACE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (m *MobAIManager) standOffDestination(world mobAIWorld, npc *model.NPC, target *model.Player, distance, attackRange float32) (navmeshv2.RtNavmeshPosition, bool) {
	if world == nil || npc == nil || target == nil || distance <= attackRange || distance <= 0 {
		return navmeshv2.RtNavmeshPosition{}, false
	}
	start := npc.GetNavmeshPosition().GetGlobalCoordinates()
	end := target.GetNavmeshPosition().GetGlobalCoordinates()
	standOff := attackRange - mobArrivalMargin
	if standOff < 0 {
		standOff = 0
	}
	ratio := (distance - standOff) / distance
	destination := math32.NewVector3(
		start.X+(end.X-start.X)*ratio,
		start.Y,
		start.Z+(end.Z-start.Z)*ratio,
	)
	position, err := world.NewPosFromGlobalCoordinates(destination)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, false
	}
	return position, true
}

func (m *MobAIManager) setPendingAttack(npcUID uint32, pending mobPendingAttack) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.pending == nil {
		m.pending = make(map[uint32]mobPendingAttack)
	}
	m.pending[npcUID] = pending
}

func (m *MobAIManager) pendingNPCs() []*model.NPC {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	npcs := make([]*model.NPC, 0, len(m.pending))
	for _, pending := range m.pending {
		if pending.npc != nil {
			npcs = append(npcs, pending.npc)
		}
	}
	return npcs
}

func (m *MobAIManager) pendingAttack(npcUID uint32, now time.Time) (mobPendingAttack, bool, bool) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	pending, exists := m.pending[npcUID]
	if !exists {
		return mobPendingAttack{}, false, false
	}
	if now.Before(pending.readyAt) {
		return pending, true, false
	}
	delete(m.pending, npcUID)
	return pending, true, true
}

func (m *MobAIManager) clearPendingAttack(npcUID uint32) {
	m.stateMu.Lock()
	delete(m.pending, npcUID)
	m.stateMu.Unlock()
}

func (m *MobAIManager) resolvePendingAttack(world mobAIWorld, npc *model.NPC, now time.Time) bool {
	pending, exists, due := m.pendingAttack(npc.GetUniqueID(), now)
	if !exists {
		return false
	}
	if !due {
		return true
	}
	target := m.knownPlayer(npc, pending.targetUID)
	if target == nil || target.GetLifeState() != model.Alive {
		if mobAITraceEnabled() {
			log.Infof("MOB_AI_TRACE cancel-pending npc=%d target=%d cast=%d target-known=%t", npc.GetUniqueID(), pending.targetUID, pending.castUID, target != nil)
		}
		npc.ClearAggroTarget(pending.targetUID)
		return true
	}
	m.applyAttackDamage(world, npc, target, pending.castUID, pending.damage, true, pending.magical)
	return true
}

func (m *MobAIManager) knownPlayer(npc *model.NPC, targetUID uint32) *model.Player {
	if npc == nil || npc.GetKnownObjectList() == nil {
		return nil
	}
	target, _ := npc.GetKnownObjectList().GetKnownObjects()[targetUID].(*model.Player)
	return target
}

func (m *MobAIManager) applyAttackDamage(world mobAIWorld, npc *model.NPC, target *model.Player, castUID uint32, damage int, delayed bool, magical ...bool) {
	var applied int
	var killed bool
	landed := m.mobAttackLands(npc, target, time.Now())
	if landed {
		if len(magical) > 0 && magical[0] {
			applied, killed = target.ApplyMagicalDamage(damage)
		} else {
			applied, killed = target.ApplyDamage(damage)
		}
	}
	if mobAITraceEnabled() {
		log.Infof("MOB_AI_TRACE damage npc=%d target=%d cast=%d requested=%d applied=%d killed=%t delayed=%t hp=%d", npc.GetUniqueID(), target.GetUniqueID(), castUID, damage, applied, killed, delayed, target.CurrentHP())
	}
	m.stateMu.Lock()
	vitals := m.vitals
	m.stateMu.Unlock()
	if vitals != nil && applied > 0 {
		hp, mp := target.CurrentVitals()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := vitals.SaveVitals(ctx, target.ID, hp, mp)
		cancel()
		if err != nil {
			log.WithError(err).Warnf("persist mob damage for character %d", target.ID)
		}
	}
	var attack network.Packet
	if delayed {
		attack = model.BuildAttackEnd(target.GetUniqueID(), castUID, uint32(applied), killed)
	} else {
		attack = model.BuildAttackBeginWithSkill(npc.AttackSkillID(), npc.GetUniqueID(), target.GetUniqueID(), castUID, uint32(applied), killed)
	}
	npc.SendToKnown(&attack)
	target.SendVitalsUpdate()
	if killed {
		state := model.BuildLifeStateUpdatePacket(target.GetUniqueID(), model.Dead)
		target.SendToSelfAndKnown(&state)
		npc.ClearAggroTarget(target.GetUniqueID())
		world.StopMovingCharacter(target, target.GetNavmeshPosition(), "mob-killed-player", true)
	}
}

func (m *MobAIManager) mobAttackLands(npc *model.NPC, target *model.Player, now time.Time) bool {
	if m == nil || npc == nil || target == nil {
		return true
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	roll := m.hitRollPercent
	m.stateMu.Unlock()
	if catalog == nil {
		return true
	}
	character, exists := catalog.Character(npc.GetRefObjectID())
	if !exists {
		return true
	}
	hit := character.HitRate
	if status, active := npc.CombatStatus(model.CombatStatusHitDown, now); active {
		hit = max(0, hit-status.Power)
	}
	chance := model.CombatHitChance(hit, target.DerivedStats().ParryRate)
	if roll == nil {
		return true
	}
	return roll(chance)
}

func (m *MobAIManager) resolveTarget(world mobAIWorld, npc *model.NPC, targetUID uint32, aggressive bool, sightRange float32) *model.Player {
	known := npc.GetKnownObjectList()
	if known == nil {
		return nil
	}
	objects := known.GetKnownObjects()
	if targetUID != 0 {
		if player, ok := objects[targetUID].(*model.Player); ok && player != nil && player.GetLifeState() == model.Alive {
			if navmeshv2.HorizontalDistance(npc.GetNavmeshPosition(), player.GetNavmeshPosition()) <= sightRange*2 {
				return player
			}
		}
	}
	if !aggressive {
		return nil
	}
	var nearest *model.Player
	nearestDistance := sightRange
	for _, object := range objects {
		player, ok := object.(*model.Player)
		if !ok || player == nil || player.GetLifeState() != model.Alive {
			continue
		}
		m.stateMu.Lock()
		catalog := m.referenceData
		m.stateMu.Unlock()
		if player.HasAggroSuppression(catalog) {
			continue
		}
		distance := navmeshv2.HorizontalDistance(npc.GetNavmeshPosition(), player.GetNavmeshPosition())
		if distance <= nearestDistance {
			nearest = player
			nearestDistance = distance
		}
	}
	return nearest
}

func (m *MobAIManager) moveTo(world mobAIWorld, npc *model.NPC, target navmeshv2.RtNavmeshPosition, now time.Time, replace bool) {
	if npc == nil || npc.HasStationaryMovementProfile() {
		return
	}
	if !replace && npc.GetMotionState() == model.Running {
		return
	}
	start := npc.GetNavmeshPosition()
	movement := world.RegisterMovingCharacter(npc, model.MovementData{
		StartTime: now, UpdateTime: now, TargetPosition: target, HasDestination: true,
	})
	packet := model.BuildMovementUpdatePacket(npc, movement, start)
	npc.SendToKnown(&packet)
}

func (m *MobAIManager) wander(world mobAIWorld, npc *model.NPC, now time.Time) {
	if npc.GetMotionState() != model.NoMotion || !npc.ReadyToWander(now, mobWanderDelay) {
		return
	}
	area := npc.SpawnArea()
	if area == nil || area.GenerateRadius <= 0 {
		return
	}
	home := area.Position.GetGlobalCoordinates()
	radius := area.GenerateRadius
	targetGlobal := math32.NewVector3(
		home.X+float32(rand.Intn(radius*2+1)-radius),
		home.Y,
		home.Z+float32(rand.Intn(radius*2+1)-radius),
	)
	target, err := world.NewPosFromGlobalCoordinates(targetGlobal)
	if err != nil {
		return
	}
	m.moveTo(world, npc, target, now, false)
}
