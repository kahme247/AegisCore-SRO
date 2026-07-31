package manager

import (
	"context"
	"math"
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
	mobAITickRate          = 50 * time.Millisecond
	mobAttackRange         = float32(25) // Compatibility fallback for incomplete catalogs.
	mobAttackCooldown      = 1500 * time.Millisecond
	mobAttackClearance     = float32(5)
	mobArrivalMargin       = float32(2)
	mobWanderDelay         = 8 * time.Second
	mobPursuitRefresh      = time.Second
	mobAggroReactionMin    = 750 * time.Millisecond
	mobAggroReactionJitter = 750 * time.Millisecond
	maximumMobWireDamage   = 0xFFFFFF
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
	NewPosFromRegionCoordinates(int16, *math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
	CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool
}

type MobAIManager struct {
	Manager
	stateMu         sync.Mutex
	referenceData   *model.ReferenceDataCatalog
	vitals          model.CharacterVitalsRepository
	pending         map[uint32]mobPendingAttack
	nextTrace       time.Time
	hitRollPercent  func(int) bool
	criticalRoll    func(int) bool
	blockRoll       func(int) bool
	damageRoll      func() int
	skillRoll       func() int
	now             func() time.Time
	schedule        func(time.Duration, func())
	ownerTargets    func(*model.NPC, *model.Player, model.RefSkill, int) []*model.Player
	interruptPlayer func(*model.Player)
	returnDamage    func(*model.Player, *model.NPC, model.PendingReturnDamage)
	gameplayRates   interface{ Current() model.GameplayRates }
}

func (m *MobAIManager) ConfigureGameplayRates(rates interface{ Current() model.GameplayRates }) {
	if m != nil {
		m.gameplayRates = rates
	}
}
func (m *MobAIManager) gameplayTimings() (attack, wander, aggroMin, aggroMax, pursuit time.Duration) {
	attack, wander, aggroMin, aggroMax, pursuit = mobAttackCooldown, mobWanderDelay, mobAggroReactionMin, mobAggroReactionMin+mobAggroReactionJitter, mobPursuitRefresh
	if m == nil || m.gameplayRates == nil {
		return
	}
	r := m.gameplayRates.Current()
	if r.MobAttackCooldownMS > 0 {
		attack = time.Duration(r.MobAttackCooldownMS) * time.Millisecond
	}
	if r.MobWanderDelayMS > 0 {
		wander = time.Duration(r.MobWanderDelayMS) * time.Millisecond
	}
	if r.MobAggroReactionMinMS > 0 {
		aggroMin = time.Duration(r.MobAggroReactionMinMS) * time.Millisecond
	}
	if r.MobAggroReactionMaxMS >= r.MobAggroReactionMinMS {
		aggroMax = time.Duration(r.MobAggroReactionMaxMS) * time.Millisecond
	}
	if r.MobPursuitRefreshMS > 0 {
		pursuit = time.Duration(r.MobPursuitRefreshMS) * time.Millisecond
	}
	return
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
	reach             float32
	delay             time.Duration
	impact            time.Duration
	lifecycle         time.Duration
	actionTimePercent float64
	cooldown          time.Duration
	attackMin         int
	attackMax         int
	magical           bool
}

type mobPendingAttack struct {
	npc               *model.NPC
	targetUID         uint32
	skillID           uint32
	castUID           uint32
	damage            int
	magical           bool
	readyAt           time.Time
	impact            time.Duration
	lifecycle         time.Duration
	actionTimePercent float64
}

type mobLinkedDamage struct {
	player      *model.Player
	application model.PlayerDamageApplication
	removedBuff uint32
}

var mobAIManagerInstance *MobAIManager
var mobAIManagerOnce sync.Once

func GetMobAIManagerInstance() *MobAIManager {
	mobAIManagerOnce.Do(func() {
		mobAIManagerInstance = &MobAIManager{
			pending:        make(map[uint32]mobPendingAttack),
			hitRollPercent: func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance },
			criticalRoll:   func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance },
			blockRoll:      func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance },
			damageRoll:     func() int { return rand.Intn(32768) },
			skillRoll:      rand.Int,
			now:            time.Now,
			schedule:       func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) },
		}
		mobAIManagerInstance.Name = "MobAIManager"
		mobAIManagerInstance.initialDelay = time.Second
		mobAIManagerInstance.rate = mobAITickRate
		mobAIManagerInstance.runnerFunc = mobAIManagerInstance.run
	})
	return mobAIManagerInstance
}

// ConfigureReferenceData supplies the immutable character and skill catalogs
// used to derive collision-aware reach, action delay, cooldown, and direct
// NPC abnormal-effect descriptors.
func (m *MobAIManager) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) {
	if m == nil {
		return
	}
	m.stateMu.Lock()
	m.referenceData = catalog
	m.stateMu.Unlock()
}

// ConfigureSkillOwnerTargetProvider supplies the native-owned collection used
// by NPC transfer types 5 and 7. Without it, MobAI never widens those skills
// to arbitrary visible players.
func (m *MobAIManager) ConfigureSkillOwnerTargetProvider(provider func(*model.NPC, *model.Player, model.RefSkill, int) []*model.Player) {
	if m == nil {
		return
	}
	m.stateMu.Lock()
	m.ownerTargets = provider
	m.stateMu.Unlock()
}

// ConfigurePlayerAttackInterrupter connects NPC-authored control and forced
// movement effects to the player combat runtime so casts and repeating attacks
// cannot complete after the actor becomes action-incompatible.
func (m *MobAIManager) ConfigurePlayerAttackInterrupter(interrupt func(*model.Player)) {
	if m == nil {
		return
	}
	m.stateMu.Lock()
	m.interruptPlayer = interrupt
	m.stateMu.Unlock()
}

// ConfigureReturnDamageExecutor connects deferred NPC RGMD records to the
// combat handler, which owns NPC kill credit, decay, and packet lifecycle.
func (m *MobAIManager) ConfigureReturnDamageExecutor(executor func(*model.Player, *model.NPC, model.PendingReturnDamage)) {
	if m == nil {
		return
	}
	m.stateMu.Lock()
	m.returnDamage = executor
	m.stateMu.Unlock()
}

func (m *MobAIManager) combatNow() time.Time {
	if m == nil {
		return time.Now()
	}
	m.stateMu.Lock()
	now := m.now
	m.stateMu.Unlock()
	if now == nil {
		return time.Now()
	}
	return now()
}

func (m *MobAIManager) scheduleAfter(delay time.Duration, callback func()) {
	if m == nil || callback == nil {
		return
	}
	m.stateMu.Lock()
	schedule := m.schedule
	m.stateMu.Unlock()
	if schedule == nil {
		time.AfterFunc(max(0, delay), callback)
		return
	}
	schedule(max(0, delay), callback)
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
	_, asleep := npc.CombatStatus(model.CombatStatusSleep, now)
	_, bound := npc.CombatStatus(model.CombatStatusBind, now)
	_, petrified := npc.CombatStatus(model.CombatStatusPetrify, now)
	if asleep && (m.skillBypassesSleepRejection(npc.AttackSkillID()) || m.isExactSealStone(npc)) {
		asleep = false
	}
	if frozen || stunned || asleep || bound || petrified {
		m.clearPendingAttack(npc.GetUniqueID())
		if npc.GetMotionState() != model.NoMotion {
			reason := "mob-stunned"
			if frozen {
				reason = "mob-frozen"
			} else if asleep {
				reason = "mob-asleep"
			} else if bound {
				reason = "mob-bound"
			} else if petrified {
				reason = "mob-petrified"
			}
			world.StopMovingCharacter(npc, npc.GetNavmeshPosition(), reason, true)
		}
		return
	}
	if npc.CombatActionRestricted(now) {
		m.clearPendingAttack(npc.GetUniqueID())
		return
	}
	if m.resolvePendingAttack(world, npc, now) {
		return
	}
	level, aggressive, sightRange, targetUID := npc.AIProfile()
	target := m.resolveTarget(world, npc, targetUID, aggressive, sightRange)
	if target == nil {
		npc.ClearAggroCandidate()
		if targetUID != 0 && mobAITraceEnabled() {
			log.Infof("MOB_AI_TRACE lost-target npc=%d target=%d position=%s sight=%.2f", npc.GetUniqueID(), targetUID, npc.GetNavmeshPosition().String(), sightRange)
		}
		npc.ClearAggroTarget(0)
		m.wander(world, npc, now)
		return
	}
	if targetUID == 0 && npc.SpawnArea() != nil && npc.MonsterRarity().UsesOrdinaryAggro() {
		_, _, aggroMin, aggroMax, _ := m.gameplayTimings()
		jitter := aggroMax - aggroMin
		delay := aggroMin
		if jitter > 0 {
			delay += time.Duration(npc.GetUniqueID()%uint32(jitter/time.Millisecond)) * time.Millisecond
		}
		if !npc.ReadyToAcquireAggro(target.GetUniqueID(), now, delay) {
			return
		}
	}
	npc.SetAggroTarget(target.GetUniqueID())
	distance := navmeshv2.HorizontalDistance(npc.GetNavmeshPosition(), target.GetNavmeshPosition())
	skillID := npc.AttackSkillID()
	profile, profileFound := m.attackProfile(npc, target, skillID, now)
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
		_, _, _, _, pursuitRefresh := m.gameplayTimings()
		if npc.ReadyToPursue(now, pursuitRefresh, routeActive) {
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
		attackRate := 0
		varianceAvailable := false
		m.stateMu.Lock()
		catalog := m.referenceData
		roll := m.damageRoll
		m.stateMu.Unlock()
		if catalog != nil {
			if character, exists := catalog.Character(npc.GetRefObjectID()); exists {
				attackRate = npc.CombatHitRate(character.HitRate, now)
				varianceAvailable = character.HitRate > 0
			}
		}
		var rolls [4]int
		if roll != nil {
			for index := range rolls {
				rolls[index] = roll()
			}
		}
		if varianceAvailable {
			parryRate := target.CombatParryRate(target.DerivedStats().ParryRate, now)
			damage = model.CombatVarianceDamage(profile.attackMin, profile.attackMax, attackRate, parryRate, level, target.Level, rolls)
		} else {
			damage = (profile.attackMin + profile.attackMax) / 2
		}
		defense := target.CombatPhysicalDefense(target.DerivedStats().PhyDef, now)
		if profile.magical {
			defense = target.CombatMagicalDefense(target.DerivedStats().MagDef, now)
		}
		damage -= defense
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
			npc:               npc,
			targetUID:         target.GetUniqueID(),
			skillID:           skillID,
			castUID:           castUID,
			damage:            damage,
			magical:           profile.magical,
			readyAt:           now.Add(profile.delay),
			impact:            profile.impact,
			lifecycle:         profile.lifecycle,
			actionTimePercent: profile.actionTimePercent,
		})
		begin := model.BuildDelayedAttackBegin(skillID, npc.GetUniqueID(), target.GetUniqueID(), castUID)
		npc.SendToKnown(&begin)
		m.selectNextAttackSkill(npc)
		return
	}
	m.applyMobSkillAttack(world, npc, target, castUID, damage, false, skillID, profile.magical)
	m.selectNextAttackSkill(npc)
}

func (m *MobAIManager) selectNextAttackSkill(npc *model.NPC) {
	if m == nil || npc == nil {
		return
	}
	m.stateMu.Lock()
	roll := m.skillRoll
	m.stateMu.Unlock()
	if roll != nil {
		npc.SelectAttackSkill(roll())
	}
}

func (m *MobAIManager) isExactSealStone(npc *model.NPC) bool {
	if m == nil || npc == nil {
		return false
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	m.stateMu.Unlock()
	if catalog == nil {
		return false
	}
	character, exists := catalog.Character(npc.GetRefObjectID())
	return exists && character.CodeName == "MOB_RM_SEALSTONE"
}

func (m *MobAIManager) skillBypassesSleepRejection(skillID uint32) bool {
	if m == nil || skillID == 0 {
		return false
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	m.stateMu.Unlock()
	if catalog == nil {
		return false
	}
	skill, exists := catalog.Skill(skillID)
	if !exists {
		return false
	}
	switch skill.CodeName {
	case "MSKILL_SD_HAROERIS_ATTACK04", "MSKILL_SD_SETH_ATTACK04", "MSKILL_SD_SETH_ATTACK10":
		return true
	default:
		return false
	}
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

func (m *MobAIManager) attackProfile(npc *model.NPC, target *model.Player, skillID uint32, now time.Time) (mobAttackProfile, bool) {
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
	attackRange = npc.CombatAttackRange(attackRange, now)

	// Ordinary damage lands at the authored impact point. Frostbite and Dull
	// each halve action speed; combined states multiply their slowdown.
	actionTimePercent := npc.CombatActionTimePercent(100, now)
	impact := milliseconds(skill.ActionPreparingTime + skill.ActionCastingTime)
	lifecycle := skill.RuntimeImpactDurationAt(actionTimePercent)
	delay := lifecycle
	if actionTimePercent == 100 {
		delay = impact
	}
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
		cooldown, _, _, _, _ = m.gameplayTimings()
	}
	return mobAttackProfile{
		reach: attackRange, delay: delay, impact: impact, lifecycle: lifecycle,
		actionTimePercent: actionTimePercent, cooldown: cooldown,
		attackMin: skill.AttackMin, attackMax: skill.AttackMax, magical: skill.IsMagicalAttack(),
	}, true
}

// ReconcileAttackTiming keeps an in-flight NPC swing aligned when Frostbite
// starts or expires. The pending hit keeps its cast UID and damage; only its
// authoritative completion time changes.
func (m *MobAIManager) ReconcileAttackTiming(npc *model.NPC, now time.Time) {
	if m == nil || npc == nil {
		return
	}
	if now.IsZero() {
		now = m.combatNow()
	}
	nextPercent := npc.CombatActionTimePercent(100, now)
	m.stateMu.Lock()
	pending, exists := m.pending[npc.GetUniqueID()]
	if !exists || pending.actionTimePercent == nextPercent {
		m.stateMu.Unlock()
		return
	}
	previousPercent := pending.actionTimePercent
	if previousPercent <= 0 {
		previousPercent = 100
	}
	remaining := max(time.Duration(0), pending.readyAt.Sub(now))
	remaining = time.Duration(float64(remaining) * nextPercent / previousPercent)
	if previousPercent == 100 && nextPercent > 100 {
		tail := max(time.Duration(0), pending.lifecycle-pending.impact)
		remaining += time.Duration(float64(tail) * nextPercent / 100)
	}
	pending.readyAt = now.Add(remaining)
	pending.actionTimePercent = nextPercent
	m.pending[npc.GetUniqueID()] = pending
	m.stateMu.Unlock()
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
	sourcePosition := npc.GetNavmeshPosition()
	targetPosition := target.GetNavmeshPosition()
	start, end, ok := mobAIMovementCoordinates(sourcePosition, targetPosition)
	if !ok {
		return navmeshv2.RtNavmeshPosition{}, false
	}
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
	position, err := resolveMobAIDestination(world, sourcePosition, destination)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, false
	}
	return position, true
}

func mobAIMovementCoordinates(source, target navmeshv2.RtNavmeshPosition) (*math32.Vector3, *math32.Vector3, bool) {
	sourceDungeon := navmeshv2.IsDungeonRegion(source.Region.ID)
	targetDungeon := navmeshv2.IsDungeonRegion(target.Region.ID)
	if sourceDungeon || targetDungeon {
		if !sourceDungeon || !targetDungeon || source.Region.ID != target.Region.ID || source.Offset == nil || target.Offset == nil {
			return nil, nil, false
		}
		return source.Offset, target.Offset, true
	}
	return source.GetGlobalCoordinates(), target.GetGlobalCoordinates(), true
}

func resolveMobAIDestination(world mobAIWorld, reference navmeshv2.RtNavmeshPosition, coordinates *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	if navmeshv2.IsDungeonRegion(reference.Region.ID) {
		return world.NewPosFromRegionCoordinates(reference.Region.ID, coordinates)
	}
	return world.NewPosFromGlobalCoordinates(coordinates)
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

// InterruptAttack cancels damage committed by an NPC before forced
// displacement. The next AI tick can then pursue its target from the NPC's
// new authoritative position instead of resolving a melee hit at range.
func (m *MobAIManager) InterruptAttack(npc *model.NPC) {
	if m == nil || npc == nil {
		return
	}
	m.clearPendingAttack(npc.GetUniqueID())
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
	m.applyMobSkillAttack(world, npc, target, pending.castUID, pending.damage, true, pending.skillID, pending.magical)
	return true
}

func (m *MobAIManager) knownPlayer(npc *model.NPC, targetUID uint32) *model.Player {
	if npc == nil || npc.GetKnownObjectList() == nil {
		return nil
	}
	target, _ := npc.GetKnownObjectList().GetKnownObjects()[targetUID].(*model.Player)
	return target
}

// applyMobSkillAttack aggregates ordinary discriminator-1 NPC transfers into
// one native-style result packet. Owner-bound types 5/7 intentionally fall
// through to the primary target only until their runtime collections exist.
func (m *MobAIManager) applyMobSkillAttack(world mobAIWorld, npc *model.NPC, primary *model.Player, castUID uint32, damage int, delayed bool, skillID uint32, magical bool) {
	if m == nil || npc == nil || primary == nil {
		return
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	m.stateMu.Unlock()
	if catalog == nil {
		m.applyAttackDamageWithSkill(world, npc, primary, castUID, damage, delayed, skillID, true, magical)
		return
	}
	skill, exists := catalog.Skill(skillID)
	if !exists || skill.TransferCount <= 0 || (skill.TransferRange <= 0 && skill.TransferType != 5 && skill.TransferType != 7) {
		m.applyAttackDamageWithSkill(world, npc, primary, castUID, damage, delayed, skillID, true, magical)
		return
	}
	secondaries := m.mobSkillTransferTargets(npc, primary, skill)
	if len(secondaries) == 0 {
		m.applyAttackDamageWithSkill(world, npc, primary, castUID, damage, delayed, skillID, true, magical)
		return
	}

	allTargets := make([]model.CombatDamageTarget, 0, len(secondaries)+1)
	appendTargets := func(targets []model.CombatDamageTarget) {
		for _, candidate := range targets {
			merged := false
			for index := range allTargets {
				if allTargets[index].TargetUID == candidate.TargetUID {
					allTargets[index].Records = append(allTargets[index].Records, candidate.Records...)
					merged = true
					break
				}
			}
			if !merged {
				allTargets = append(allTargets, candidate)
			}
		}
	}
	appendTargets(m.applyAttackDamageWithSkillResult(world, npc, primary, castUID, damage, delayed, skillID, true, false, magical))
	factor := 100
	for _, secondary := range secondaries {
		factor = mobNextTransferFactor(factor, skill.TransferFalloffPercent)
		appendTargets(m.applyAttackDamageWithSkillResult(world, npc, secondary, castUID, mobScaleTransferAmount(damage, factor), delayed, skillID, true, false, magical))
	}
	allTargets = alignMobDamageTargets(allTargets)
	if len(allTargets) == 0 {
		return
	}
	var attack network.Packet
	if delayed {
		attack = model.BuildAttackEndDamageTargets(castUID, allTargets)
	} else {
		attack = model.BuildAttackBeginWithSkillDamageTargets(skillID, npc.GetUniqueID(), castUID, allTargets)
	}
	npc.SendToKnown(&attack)
	if mobDamageTargetsHaveEffects(allTargets, primary.GetUniqueID()) {
		m.publishNPCBadEffectState(primary, m.combatNow())
	}
	for _, secondary := range secondaries {
		if mobDamageTargetsHaveEffects(allTargets, secondary.GetUniqueID()) {
			m.publishNPCBadEffectState(secondary, m.combatNow())
		}
	}
	primary.SendVitalsUpdate()
	for _, secondary := range secondaries {
		secondary.SendVitalsUpdate()
	}
}

func (m *MobAIManager) transferActorRadius(refObjectID uint32) float32 {
	if m == nil || m.referenceData == nil || refObjectID == 0 {
		return 0
	}
	character, exists := m.referenceData.Character(refObjectID)
	if !exists || character.BCRadius <= 0 {
		return 0
	}
	return float32(character.BCRadius)
}

func (m *MobAIManager) mobSkillTransferTargets(npc *model.NPC, primary *model.Player, skill model.RefSkill) []*model.Player {
	maximum := skill.TransferCount - 1
	if m == nil || npc == nil || primary == nil || maximum <= 0 {
		return nil
	}
	if skill.TransferType == 5 || skill.TransferType == 7 {
		m.stateMu.Lock()
		provider := m.ownerTargets
		m.stateMu.Unlock()
		if provider == nil {
			return nil
		}
		provided := provider(npc, primary, skill, maximum)
		result := make([]*model.Player, 0, min(maximum, len(provided)))
		seen := make(map[uint32]struct{}, len(provided))
		for _, candidate := range provided {
			if candidate == nil || candidate == primary || candidate.GetLifeState() != model.Alive {
				continue
			}
			if _, exists := seen[candidate.GetUniqueID()]; exists {
				continue
			}
			seen[candidate.GetUniqueID()] = struct{}{}
			result = append(result, candidate)
			if len(result) == maximum {
				break
			}
		}
		return result
	}
	if npc.GetKnownObjectList() == nil {
		return nil
	}
	pool := make([]*model.Player, 0)
	for _, object := range npc.GetKnownObjectList().GetKnownObjects() {
		candidate, ok := object.(*model.Player)
		if ok && candidate != nil && candidate != primary && candidate.GetLifeState() == model.Alive {
			pool = append(pool, candidate)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].GetUniqueID() < pool[j].GetUniqueID() })
	radius := float32(skill.TransferRange)
	if skill.TransferType == 6 {
		result := make([]*model.Player, 0, min(maximum, len(pool)))
		current := primary
		for len(result) < maximum {
			selected, selectedIndex, best := (*model.Player)(nil), -1, float32(math.MaxFloat32)
			currentRadius := m.transferActorRadius(current.GetRefObjectID())
			for index, candidate := range pool {
				distance := navmeshv2.HorizontalDistance(current.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				admissionRadius := radius + currentRadius + m.transferActorRadius(candidate.GetRefObjectID())
				if distance <= admissionRadius && (distance < best || distance == best && (selected == nil || candidate.GetUniqueID() < selected.GetUniqueID())) {
					selected, selectedIndex, best = candidate, index, distance
				}
			}
			if selected == nil {
				break
			}
			result = append(result, selected)
			pool = append(pool[:selectedIndex], pool[selectedIndex+1:]...)
			current = selected
		}
		return result
	}
	center := npc.GetNavmeshPosition()
	centerRadius := m.transferActorRadius(npc.GetRefObjectID())
	if skill.TransferType == 2 {
		center = primary.GetNavmeshPosition()
		centerRadius = m.transferActorRadius(primary.GetRefObjectID())
	}
	type candidateMetric struct {
		player *model.Player
		metric float32
	}
	candidates := make([]candidateMetric, 0, len(pool))
	if skill.TransferType == 3 || skill.TransferType == 4 {
		source := npc.GetNavmeshPosition().GetGlobalCoordinates()
		primaryPosition := primary.GetNavmeshPosition().GetGlobalCoordinates()
		dx, dz := primaryPosition.X-source.X, primaryPosition.Z-source.Z
		length := float32(math.Hypot(float64(dx), float64(dz)))
		if length <= 0 {
			return nil
		}
		ux, uz := dx/length, dz/length
		forwardLimit := max(length, float32(max(skill.ActionRange, skill.TransferRange)))
		for _, candidate := range pool {
			position := candidate.GetNavmeshPosition().GetGlobalCoordinates()
			cx, cz := position.X-source.X, position.Z-source.Z
			projection := cx*ux + cz*uz
			candidateRadius := m.transferActorRadius(candidate.GetRefObjectID())
			lateral := float32(math.Abs(float64(cx*uz - cz*ux)))
			if projection >= -candidateRadius && projection <= forwardLimit+candidateRadius && lateral <= radius+candidateRadius {
				candidates = append(candidates, candidateMetric{player: candidate, metric: max(projection, 0)})
			}
		}
	} else {
		for _, candidate := range pool {
			distance := navmeshv2.HorizontalDistance(center, candidate.GetNavmeshPosition())
			admissionRadius := radius + centerRadius + m.transferActorRadius(candidate.GetRefObjectID())
			if distance <= admissionRadius {
				candidates = append(candidates, candidateMetric{player: candidate, metric: distance})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].metric < candidates[j].metric || candidates[i].metric == candidates[j].metric && candidates[i].player.GetUniqueID() < candidates[j].player.GetUniqueID()
	})
	if len(candidates) > maximum {
		candidates = candidates[:maximum]
	}
	result := make([]*model.Player, len(candidates))
	for index := range candidates {
		result[index] = candidates[index].player
	}
	return result
}

func mobNextTransferFactor(current, falloff int) int {
	current = min(max(current, 0), 100)
	falloff = min(max(falloff, 0), 100)
	return int(math.Round(float64(current*(100-falloff)) / 100))
}

func mobScaleTransferAmount(amount, factor int) int {
	if amount <= 0 || factor <= 0 {
		return 0
	}
	return int(math.Round(float64(amount*min(factor, 100)) / 100))
}

func (m *MobAIManager) applyAttackDamage(world mobAIWorld, npc *model.NPC, target *model.Player, castUID uint32, damage int, delayed bool, magical ...bool) {
	m.applyAttackDamageWithSkill(world, npc, target, castUID, damage, delayed, 0, true, magical...)
}

// applyAttackDamageWithSkill keeps the legacy mob damage API available to
// tests and callers while allowing the timed skill path to retain the
// authored REF_SKILL ID. Periodic abnormal ticks deliberately pass
// applyEffects=false so a poison/burn tick cannot re-admit its parent status.
func (m *MobAIManager) applyAttackDamageWithSkill(world mobAIWorld, npc *model.NPC, target *model.Player, castUID uint32, damage int, delayed bool, skillID uint32, applyEffects bool, magical ...bool) {
	_ = m.applyAttackDamageWithSkillResult(world, npc, target, castUID, damage, delayed, skillID, applyEffects, true, magical...)
}

// applyAttackDamageWithSkillResult applies one target's damage/effects and
// returns the target/result records. emit controls only the attack/vitals
// packet emission; state mutation and persistence remain authoritative either
// way so a caller can aggregate native multi-target result blocks.
func (m *MobAIManager) applyAttackDamageWithSkillResult(world mobAIWorld, npc *model.NPC, target *model.Player, castUID uint32, damage int, delayed bool, skillID uint32, applyEffects bool, emit bool, magical ...bool) []model.CombatDamageTarget {
	isMagical := len(magical) > 0 && magical[0]
	record := model.CombatDamageRecord{DamageState: model.DamageStateNormal}
	var records []model.CombatDamageRecord
	var linked mobLinkedDamage
	applied, killed := 0, false
	effectMask := uint32(0)
	if m.mobAttackBlocked(target) {
		record.AttackState = model.DamageAttackStateBlock
	} else {
		critical := !isMagical && m.mobAttackCritical(npc)
		if critical {
			damage = min(damage*2, maximumMobWireDamage)
			record.DamageState = model.DamageStateCritical
		}
		if status, active := target.CombatStatus(model.CombatStatusDamageTakenUp, m.combatNow()); active {
			// SR_GameServer abnormal type 0x14 writes the same tisc magnitude
			// to physical 0xB4 and magical 0xB5 after critical processing.
			damage = model.CombatFinalDamageFactor(damage, status.Power)
		}
		physicalHit, magicalHit := damage, 0
		if isMagical {
			physicalHit, magicalHit = 0, damage
		}
		// 0x005A0B80 admits rgmd before the later linked-damage branch.
		m.admitNPCToPlayerReturnDamage(npc, target, physicalHit, magicalHit, m.combatNow())
		damage, linked = m.interceptLinkedDamage(npc, target, damage, isMagical)
		application := target.ApplyCombatDamage(damage, isMagical)
		record.Damage = uint32(min(application.WireDamage, maximumMobWireDamage))
		applied, killed = application.AppliedDamage, application.Killed
		if applyEffects && !killed {
			effectMask = m.applyNPCSkillStatuses(world, npc, target, skillID, isMagical, m.combatNow())
			record.EffectMask = effectMask
		}
		if applyEffects && applied > 0 && !killed {
			m.applyNPCSkillDisplacement(world, npc, target, skillID, &record)
		}
		if killed {
			record.AttackState |= model.DamageAttackStateDead
		}
		if linked.player != nil && damage <= 0 {
			record.AttackState = model.DamageResultSuppressed
		}
		records = mobPlayerDamageRecords(application, record)
	}
	if len(records) == 0 {
		records = []model.CombatDamageRecord{record}
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
	if vitals != nil && linked.player != nil && linked.application.AppliedDamage > 0 {
		hp, mp := linked.player.CurrentVitals()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := vitals.SaveVitals(ctx, linked.player.ID, hp, mp)
		cancel()
		if err != nil {
			log.WithError(err).Warnf("persist linked mob damage for character %d", linked.player.ID)
		}
	}
	damageTargets := []model.CombatDamageTarget{{TargetUID: target.GetUniqueID(), Records: records}}
	if linked.player != nil {
		linkedRecord := model.CombatDamageRecord{DamageState: record.DamageState, Damage: uint32(min(linked.application.WireDamage, maximumMobWireDamage))}
		if linked.application.Killed {
			linkedRecord.AttackState |= model.DamageAttackStateDead
		}
		linkedRecords := mobPlayerDamageRecords(linked.application, linkedRecord)
		damageTargets = append(damageTargets, model.CombatDamageTarget{TargetUID: linked.player.GetUniqueID(), Records: linkedRecords})
	}
	damageTargets = alignMobDamageTargets(damageTargets)
	var attack network.Packet
	packetSkillID := skillID
	if packetSkillID == 0 {
		packetSkillID = npc.AttackSkillID()
	}
	if delayed {
		attack = model.BuildAttackEndDamageTargets(castUID, damageTargets)
	} else {
		attack = model.BuildAttackBeginWithSkillDamageTargets(packetSkillID, npc.GetUniqueID(), castUID, damageTargets)
	}
	if emit {
		npc.SendToKnown(&attack)
		if effectMask != 0 {
			m.publishNPCBadEffectState(target, m.combatNow())
		}
		target.SendVitalsUpdate()
	}
	if linked.player != nil {
		if emit {
			linked.player.SendVitalsUpdate()
		}
		if linked.removedBuff != 0 {
			removed := model.BuildBuffRemove(linked.removedBuff)
			target.SendToSelfAndKnown(&removed)
			stats := target.BuildStatsUpdatePacket()
			target.SendToSelf(&stats)
		}
		if linked.application.Killed {
			if linked.player.RemoveCharacterMask(0) {
				mask := model.BuildCharacterMaskPacket(linked.player.GetUniqueID(), model.CharacterMask{})
				linked.player.SendToSelfAndKnown(&mask)
			}
			state := model.BuildLifeStateUpdatePacket(linked.player.GetUniqueID(), model.Dead)
			linked.player.SendToSelfAndKnown(&state)
			world.StopMovingCharacter(linked.player, linked.player.GetNavmeshPosition(), "linked-mob-damage-killed-player", true)
		}
	}
	if killed {
		if target.RemoveCharacterMask(0) {
			mask := model.BuildCharacterMaskPacket(target.GetUniqueID(), model.CharacterMask{})
			target.SendToSelfAndKnown(&mask)
		}
		state := model.BuildLifeStateUpdatePacket(target.GetUniqueID(), model.Dead)
		target.SendToSelfAndKnown(&state)
		npc.ClearAggroTarget(target.GetUniqueID())
		world.StopMovingCharacter(target, target.GetNavmeshPosition(), "mob-killed-player", true)
	}
	return damageTargets
}

func (m *MobAIManager) applyNPCSkillDisplacement(world mobAIWorld, npc *model.NPC, target *model.Player, skillID uint32, record *model.CombatDamageRecord) bool {
	if m == nil || world == nil || npc == nil || target == nil || record == nil || skillID == 0 {
		return false
	}
	m.stateMu.Lock()
	catalog, roll, interrupt := m.referenceData, m.hitRollPercent, m.interruptPlayer
	m.stateMu.Unlock()
	if catalog == nil {
		return false
	}
	skill, exists := catalog.Skill(skillID)
	if !exists {
		return false
	}
	distance, chance, kind := float32(0), 0, byte(0)
	if skill.KnockOverKey != 0 {
		distance, chance, kind = 20, skill.KnockOverChance, model.DamageResultKnockOver
	} else if skill.KnockbackDistance > 0 {
		distance, chance, kind = float32(skill.KnockbackDistance), skill.KnockbackChance, model.DamageResultKnockback
	}
	if distance <= 0 || chance > 0 && (roll == nil || !roll(chance)) {
		return false
	}
	sourcePosition := npc.GetNavmeshPosition()
	targetPosition := target.GetNavmeshPosition()
	sourceCoordinates, targetCoordinates, ok := mobAIMovementCoordinates(sourcePosition, targetPosition)
	if !ok {
		return false
	}
	dx, dz := targetCoordinates.X-sourceCoordinates.X, targetCoordinates.Z-sourceCoordinates.Z
	length := float32(math.Hypot(float64(dx), float64(dz)))
	if length <= 0 {
		dx, dz, length = 1, 0, 1
	}
	destination, err := resolveMobAIDestination(world, targetPosition, math32.NewVector3(
		targetCoordinates.X+dx/length*distance,
		targetCoordinates.Y,
		targetCoordinates.Z+dz/length*distance,
	))
	if err != nil || !world.CanMoveDirectly(targetPosition, destination) {
		return false
	}
	world.StopMovingCharacter(target, destination, "mob-skill-displacement", false)
	target.SetNavmeshPosition(destination)
	if interrupt != nil {
		interrupt(target)
	}
	record.AttackState = record.AttackState&model.DamageAttackStateDead | kind
	record.EndpointRegion = uint16(destination.Region.ID)
	record.EndpointX = destination.Offset.X
	record.EndpointY = destination.Offset.Y
	record.EndpointZ = destination.Offset.Z
	return true
}

// applyNPCSkillStatuses applies only the direct target abnormal actors
// represented by REF_SKILL. Owner-list transfer families remain outside this
// path because the native producer enumerates a runtime-owned collection, not
// the observer list.
func (m *MobAIManager) applyNPCSkillStatuses(world mobAIWorld, npc *model.NPC, target *model.Player, skillID uint32, magical bool, now time.Time) uint32 {
	if m == nil || npc == nil || target == nil || target.GetLifeState() != model.Alive || skillID == 0 {
		return 0
	}
	m.stateMu.Lock()
	catalog, roll := m.referenceData, m.hitRollPercent
	m.stateMu.Unlock()
	if catalog == nil {
		return 0
	}
	skill, ok := catalog.Skill(skillID)
	if !ok {
		return 0
	}
	effects := skill.StatusEffects
	if len(effects) == 0 && skill.ShockPower > 0 {
		effects = []model.SkillStatusEffect{{Kind: model.CombatStatusShock, Power: skill.ShockPower, Payload: skill.ShockPower, Chance: skill.ShockChance, DurationMS: skill.ShockDuration}}
	}
	if len(effects) == 0 {
		return 0
	}
	if roll == nil {
		roll = func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance }
	}
	if now.IsZero() {
		now = m.combatNow()
	}
	var badMask uint32
	for _, effect := range effects {
		chance := target.CombatStatusChance(effect.Chance, now)
		if effect.Kind == 0 || chance <= 0 || effect.DurationMS <= 0 || !roll(chance) {
			continue
		}
		status := model.CombatStatus{
			Kind: effect.Kind, Token: model.NextActionUID(), SourceUID: npc.GetUniqueID(), SkillID: skill.ID,
			Power: effect.Power, Level: effect.Level, Payload: effect.Payload,
			ExpiresAt: now.Add(time.Duration(effect.DurationMS) * time.Millisecond), Stackable: skill.LinkedSameGroup,
		}
		if !target.ApplyCombatStatusAt(status, now) {
			continue
		}
		if npcStatusRestrainsMovement(effect.Kind) && world != nil {
			world.StopMovingCharacter(target, target.GetNavmeshPosition(), "mob-skill-control", true)
		}
		if npcStatusRestrainsAction(effect.Kind) {
			m.stateMu.Lock()
			interrupt := m.interruptPlayer
			m.stateMu.Unlock()
			if interrupt != nil {
				interrupt(target)
			}
		}
		if effect.Kind == model.CombatStatusFrostbite || effect.Kind == model.CombatStatusDull {
			target.SendMovementSpeedUpdate()
		}
		if model.CombatStatusRefreshesStats(effect.Kind) {
			target.SendStatsUpdate()
		}
		bit := model.CombatStatusBadEffectBit(effect.Kind)
		if bit != 0 {
			badMask |= bit
		} else {
			packet := model.BuildTimedBuffAdd(target.GetUniqueID(), skill.ID, status.Token, uint32(effect.DurationMS))
			target.SendToSelfAndKnown(&packet)
		}
		m.scheduleNPCStatusExpiry(target, status, bit != 0)
		if effect.Kind == model.CombatStatusBurn || effect.Kind == model.CombatStatusPoison || effect.Kind == model.CombatStatusBleed {
			m.scheduleNPCStatusDamageTick(world, npc, target, status, magical, now.Add(2*time.Second))
		}
	}
	return badMask
}

func (m *MobAIManager) publishNPCBadEffectState(target *model.Player, now time.Time) {
	if target == nil {
		return
	}
	mask, levels, details := target.CombatBadEffectSnapshot(now)
	update := model.BuildBadEffectUpdate(target.GetUniqueID(), mask, levels)
	target.SendToSelfAndKnown(&update)
	snapshot := model.BuildPlayerBadEffectSnapshot(mask, details)
	target.SendToSelf(&snapshot)
}

func mobDamageTargetsHaveEffects(targets []model.CombatDamageTarget, targetUID uint32) bool {
	for _, target := range targets {
		if target.TargetUID != targetUID {
			continue
		}
		for _, record := range target.Records {
			if record.EffectMask != 0 {
				return true
			}
		}
	}
	return false
}

func npcStatusRestrainsMovement(kind model.CombatStatusKind) bool {
	switch kind {
	case model.CombatStatusFreeze, model.CombatStatusStun, model.CombatStatusSleep,
		model.CombatStatusBind, model.CombatStatusPetrify:
		return true
	default:
		return false
	}
}

func npcStatusRestrainsAction(kind model.CombatStatusKind) bool {
	return npcStatusRestrainsMovement(kind) || kind == model.CombatStatusFear
}

func (m *MobAIManager) scheduleNPCStatusExpiry(target *model.Player, expected model.CombatStatus, badEffect bool) {
	if target == nil || expected.ExpiresAt.IsZero() {
		return
	}
	delay := expected.ExpiresAt.Sub(m.combatNow())
	if delay < 0 {
		delay = 0
	}
	m.scheduleAfter(delay, func() {
		now := m.combatNow()
		_, active := target.CombatStatus(expected.Kind, now)
		if active {
			return
		}
		if badEffect {
			m.publishNPCBadEffectState(target, now)
			if expected.Kind == model.CombatStatusFrostbite || expected.Kind == model.CombatStatusDull {
				target.SendMovementSpeedUpdate()
			}
		} else {
			packet := model.BuildBuffRemove(expected.Token)
			target.SendToSelfAndKnown(&packet)
		}
		if model.CombatStatusRefreshesStats(expected.Kind) {
			target.SendStatsUpdate()
		}
	})
}

// scheduleNPCStatusDamageTick follows the stock abnormal actor's two-second
// cadence. The tick is routed through the existing mob damage/result producer
// with status admission disabled, so the parent effect is not reapplied.
func (m *MobAIManager) scheduleNPCStatusDamageTick(world mobAIWorld, npc *model.NPC, target *model.Player, expected model.CombatStatus, magical bool, tickAt time.Time) {
	if m == nil || world == nil || npc == nil || target == nil || !tickAt.Before(expected.ExpiresAt) {
		return
	}
	delay := time.Until(tickAt)
	if delay < 0 {
		delay = 0
	}
	m.scheduleAfter(delay, func() {
		now := m.combatNow()
		status, active := target.CombatStatus(expected.Kind, now)
		if !active || status.Token != expected.Token || target.GetLifeState() != model.Alive {
			return
		}
		amount := status.Payload
		if amount <= 0 {
			amount = status.Power
		}
		if status.Kind == model.CombatStatusPoison {
			amount = min(amount, max(0, target.CurrentHP()-1))
			if amount == 0 {
				m.scheduleNPCStatusDamageTick(world, npc, target, expected, magical, now.Add(2*time.Second))
				return
			}
		}
		m.applyAttackDamageWithSkill(world, npc, target, model.NextActionUID(), max(1, amount), true, status.SkillID, false, magical)
		m.scheduleNPCStatusDamageTick(world, npc, target, expected, magical, now.Add(2*time.Second))
	})
}

func alignMobDamageTargets(targets []model.CombatDamageTarget) []model.CombatDamageTarget {
	maximum := 0
	for _, target := range targets {
		maximum = max(maximum, len(target.Records))
	}
	for index := range targets {
		for len(targets[index].Records) < maximum {
			targets[index].Records = append(targets[index].Records, model.CombatDamageRecord{AttackState: model.DamageResultSuppressed})
		}
	}
	return targets
}

func mobPlayerDamageRecords(application model.PlayerDamageApplication, record model.CombatDamageRecord) []model.CombatDamageRecord {
	if application.BarrierDamage <= 0 {
		return []model.CombatDamageRecord{record}
	}
	barrierState := model.DamageResultBarrier
	if application.BarrierExhausted {
		barrierState |= model.DamageAttackStateDead
	}
	barrierRecord := model.CombatDamageRecord{
		AttackState:      barrierState,
		DamageState:      model.DamageStateNormal,
		Damage:           uint32(min(application.BarrierDamage, maximumMobWireDamage)),
		BarrierRemaining: uint16(min(max(application.BarrierRemaining, 0), 65535)),
		BarrierMaximum:   uint16(min(max(application.BarrierMaximum, 0), 65535)),
	}
	result := []model.CombatDamageRecord{barrierRecord}
	if application.WireDamage > 0 {
		result = append(result, record)
	}
	return result
}

func (m *MobAIManager) interceptLinkedDamage(npc *model.NPC, target *model.Player, damage int, magical bool) (int, mobLinkedDamage) {
	if m == nil || npc == nil || target == nil || damage <= 0 {
		return damage, mobLinkedDamage{}
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	m.stateMu.Unlock()
	if catalog == nil {
		return damage, mobLinkedDamage{}
	}
	buffs := target.ActiveBuffSnapshot()
	sort.Slice(buffs, func(i, j int) bool { return buffs[i].Token < buffs[j].Token })
	now := time.Now()
	for _, buff := range buffs {
		if buff.SourceUID == 0 || buff.SourceUID == target.GetUniqueID() || !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		skill, exists := catalog.Skill(buff.SkillID)
		if !exists {
			continue
		}
		amount := skill.LinkedDamageAmount(damage, magical)
		if amount <= 0 {
			continue
		}
		linked := m.knownPlayer(npc, buff.SourceUID)
		if linked == nil || linked.GetLifeState() != model.Alive {
			continue
		}
		application := linked.ApplyCombatDamage(amount, magical)
		if application.WireDamage == 0 && application.BarrierDamage == 0 {
			continue
		}
		removed, committed := target.CommitActiveBuffUse(buff.Token, skill.LinkedDamageUseLimit)
		if !committed {
			continue
		}
		removedToken := uint32(0)
		if removed {
			removedToken = buff.Token
		}
		return damage - amount, mobLinkedDamage{player: linked, application: application, removedBuff: removedToken}
	}
	return damage, mobLinkedDamage{}
}

func (m *MobAIManager) mobAttackBlocked(target *model.Player) bool {
	if m == nil || target == nil {
		return false
	}
	m.stateMu.Lock()
	roll := m.blockRoll
	m.stateMu.Unlock()
	blockRate := target.DerivedStats().BlockRate
	return roll != nil && blockRate > 0 && roll(blockRate)
}

func (m *MobAIManager) mobAttackCritical(npc *model.NPC) bool {
	if m == nil || npc == nil {
		return false
	}
	m.stateMu.Lock()
	catalog, roll := m.referenceData, m.criticalRoll
	m.stateMu.Unlock()
	if catalog == nil || roll == nil {
		return false
	}
	character, exists := catalog.Character(npc.GetRefObjectID())
	return exists && character.CriticalHitRate > 0 && roll(character.CriticalHitRate)
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
	hit := npc.CombatHitRate(character.HitRate, now)
	parry := target.CombatParryRate(target.DerivedStats().ParryRate, now)
	chance := model.CombatHitChance(hit, parry)
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
	_, wanderDelay, _, _, _ := m.gameplayTimings()
	if npc.GetMotionState() != model.NoMotion || !npc.ReadyToWander(now, wanderDelay) {
		return
	}
	area := npc.SpawnArea()
	if area == nil || area.GenerateRadius <= 0 {
		return
	}
	homePosition := area.Position
	home := homePosition.GetGlobalCoordinates()
	if navmeshv2.IsDungeonRegion(homePosition.Region.ID) {
		if homePosition.Offset == nil {
			return
		}
		home = homePosition.Offset
	}
	radius := area.GenerateRadius
	targetCoordinates := math32.NewVector3(
		home.X+float32(rand.Intn(radius*2+1)-radius),
		home.Y,
		home.Z+float32(rand.Intn(radius*2+1)-radius),
	)
	target, err := resolveMobAIDestination(world, homePosition, targetCoordinates)
	if err != nil {
		return
	}
	m.moveTo(world, npc, target, now, false)
}
