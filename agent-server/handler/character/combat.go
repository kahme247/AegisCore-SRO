package character

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

const (
	actionPhaseStart = vsro188.CombatActionPhaseStart
	// vSRO 1.88 emits basic targeted attacks as
	// `01 | 04 | targetUID:uint32 | 00` on 0x7074.
	actionTypeAttack       = vsro188.CombatActionTypeCandidate
	actionTypeAttackLegacy = byte(1)
	actionTypeDispel       = byte(5)
	maximumWireDamage      = 0xFFFFFF

	attackPursuitPollInterval  = 25 * time.Millisecond
	attackPursuitRetargetRate  = 200 * time.Millisecond
	attackPursuitRetargetDelta = float32(2)

	skillCastErrorInvalidSkill     = byte(0x03)
	skillCastErrorInvalidMP        = byte(0x04)
	skillCastErrorInvalidEquipment = byte(0x0D)
	skillCastErrorInvalidAmmo      = byte(0x0E)
	skillCastErrorCategory         = byte(0x30)
)

var ErrCombatTargetInvalid = errors.New("combat target is invalid")

type combatWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetObjectByUniqueId(uint32) (model.ISRObject, error)
	DistanceTo(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) float32
	RemoveVisibleObject(model.ISRObject)
	StopMovingCharacter(model.ICharacter, navmeshv2.RtNavmeshPosition, string, bool) bool
	NewPosFromGlobalCoordinates(*math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
	CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool
}

type pendingAttack struct {
	session            *server.Session
	targetUID          uint32
	skillID            uint32
	reach              float32
	movementGeneration uint64
	lastTargetGlobal   *math32.Vector3
	lastRetarget       time.Time
}

type delayedCast struct {
	token  uint64
	cancel func()
}

type skillCooldownKey struct {
	playerUID uint32
	skillID   uint32
}

type combatRateProvider interface {
	Current() model.GameplayRates
}

type growthPetCombatSupport interface {
	EngageOwnerTarget(*model.Player, *model.NPC) error
	AwardOwnerKillExperience(context.Context, *model.Player, *model.NPC) error
}

type jobProgressionRepository interface {
	AdvanceJob(context.Context, int, uint64, uint64) (model.JobSnapshot, error)
}

type academyExperienceRepository interface {
	AccumulateAcademyExperience(context.Context, int, uint64) ([]model.AcademyBuffState, error)
}

type questCollectionDropRepository interface {
	CollectionDropsForKill(context.Context, int, uint32) ([]model.QuestCollectionDrop, error)
}

type questUniqueDeadRepository interface {
	GrantUniqueDeadMissionItem(context.Context, int, uint32, int16) (model.QuestUniqueDeadGrantResult, bool, error)
}

type questPlayerKillRepository interface {
	RecordPlayerKill(context.Context, int, model.QuestPlayerKillTarget) ([]model.QuestProgressResult, error)
}

type nativeQuestInstanceTeleportSubmitter interface {
	SubmitNativeQuestInstanceTeleport(*model.QuestInstanceRegistry, model.NativeQuestInstanceDeathPlan, *model.Player) error
}

type dimensionHoleEntrySubmitter interface {
	Enter(context.Context, uint32, *model.Player) (service.DimensionHoleAdmission, error)
}

type CombatHandler struct {
	mu               sync.Mutex
	dropsMu          sync.RWMutex
	world            combatWorld
	groundItemPickup func(context.Context, *server.Session, uint32) error
	referenceData    *model.ReferenceDataCatalog
	progress         model.CharacterProgressRepository
	jobProgress      jobProgressionRepository
	academyProgress  academyExperienceRepository
	academyBuffs     func([]model.AcademyBuffState)
	guildWarKill     func(context.Context, int, int, uint32) error
	competitiveKill  func(int, int) error
	fortressDamage   func(context.Context, uint32, int, uint32, time.Time) (model.FortressDamageResult, error)
	fortressKill     func(context.Context, int, int, time.Time) error
	pkKills          model.CharacterPKKillRepository
	quests           model.QuestRuntimeRepository
	vitals           model.CharacterVitalsRepository
	rangedAmmo       model.RangedAmmoRepository
	monsterMasks     model.MonsterMaskRepository
	hwan             model.HwanRepository
	levels           *model.LevelTable
	rates            combatRateProvider
	party            *service.PartyService
	companions       growthPetCombatSupport
	drops            *service.MonsterDropService
	schedule         func(time.Duration, func())
	move             func(*model.Player, navmeshv2.RtNavmeshPosition) uint64
	send             func(*server.Session, network.Packet) error
	broadcast        func(*model.Player, *network.Packet)
	pending          map[uint32]*pendingAttack
	repeating        map[uint32]*pendingAttack
	nextAttack       map[uint32]time.Time
	nextSkill        map[skillCooldownKey]time.Time
	nextCoolTime     map[uint32]time.Time
	delayedCasts     map[uint32]delayedCast
	linkedBuffs      map[uint32]map[uint32]*model.Player
	linkedParents    map[uint32]uint32
	now              func() time.Time
	rollPercent      func(int) bool
	hitRollPercent   func(int) bool
	criticalRoll     func(int) bool
	blockRoll        func(int) bool
	damageRoll       func() int
	rollHwanPoint    func(int) bool
	ownerTargets     func(*model.NPC, model.RefSkill, int) []*model.NPC
	actionTime       func(*model.Player, model.RefSkill) float64
	pvpAdmission     func(*model.Player, *model.Player, model.RefSkill) bool
	longCooldown     func(*model.Player, model.RefSkill, time.Time)
	interruptMob     func(*model.NPC)
	retimeMob        func(*model.NPC, time.Time)
	questInstances   *model.QuestInstanceRegistry
	instanceTeleport nativeQuestInstanceTeleportSubmitter
	dimensionHoles   dimensionHoleEntrySubmitter
}

// ConfigureGroundItemPickup routes the stock client's kind-4 ground-item
// action through the inventory handler's persistence-first pickup boundary.
func (h *CombatHandler) ConfigureGroundItemPickup(pickup func(context.Context, *server.Session, uint32) error) {
	if h != nil {
		h.groundItemPickup = pickup
	}
}

func (h *CombatHandler) ConfigureNativeQuestInstanceDeaths(registry *model.QuestInstanceRegistry, teleport nativeQuestInstanceTeleportSubmitter) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.questInstances = registry
	h.instanceTeleport = teleport
	h.mu.Unlock()
}

// ConfigureDimensionHoleEntry connects the strict structure-interaction
// boundary to admission and loading. Leaving it unset keeps 0x7074 fail-closed;
// startup must not install it until the destination/transfer authority exists.
func (h *CombatHandler) ConfigureDimensionHoleEntry(entry dimensionHoleEntrySubmitter) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.dimensionHoles = entry
	h.mu.Unlock()
}

func (h *CombatHandler) ConfigureJobProgression(repository jobProgressionRepository) {
	if h != nil {
		h.jobProgress = repository
	}
}

func (h *CombatHandler) ConfigureAcademyProgression(repository academyExperienceRepository, publish func([]model.AcademyBuffState)) {
	if h != nil {
		h.academyProgress = repository
		h.academyBuffs = publish
	}
}

func (h *CombatHandler) ConfigureGuildWarScoring(record func(context.Context, int, int, uint32) error) {
	if h != nil {
		h.guildWarKill = record
	}
}

func (h *CombatHandler) ConfigureCompetitiveScoring(record func(int, int) error) {
	if h != nil {
		h.competitiveKill = record
	}
}

func (h *CombatHandler) ConfigureFortressDamage(record func(context.Context, uint32, int, uint32, time.Time) (model.FortressDamageResult, error)) {
	if h != nil {
		h.fortressDamage = record
	}
}

func (h *CombatHandler) ConfigureFortressKillScoring(record func(context.Context, int, int, time.Time) error) {
	if h != nil {
		h.fortressKill = record
	}
}

func (h *CombatHandler) ConfigurePKPenalties(repository model.CharacterPKKillRepository) {
	if h != nil {
		h.pkKills = repository
	}
}

// ConfigureMobAttackInterrupter connects forced NPC displacement to the mob
// AI's pending-cast state. Knockback and knock-over move the authoritative NPC
// position and interrupt an attack that was started at the old position.
func (h *CombatHandler) ConfigureMobAttackInterrupter(interrupt func(*model.NPC)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.interruptMob = interrupt
	h.mu.Unlock()
}

func (h *CombatHandler) ConfigureMobActionTiming(reconcile func(*model.NPC, time.Time)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.retimeMob = reconcile
	h.mu.Unlock()
}

// ConfigureSkillOwnerTargetProvider supplies the runtime-owned collections
// used by native transfer geometry types 5 and 7. Those producers do not walk
// the observer list; they enumerate an execution/owner manager. Keeping this
// provider explicit prevents unsupported systems from accidentally targeting
// arbitrary visible mobs.
func (h *CombatHandler) ConfigureSkillOwnerTargetProvider(provider func(*model.NPC, model.RefSkill, int) []*model.NPC) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.ownerTargets = provider
	h.mu.Unlock()
}

// ConfigureActionTimeProvider supplies the actor-specific action-time
// percentage recovered by a runtime status/equipment system. The native
// arithmetic is already represented by RefSkill; absent a provider, the
// catalog's authored timing remains at 100 percent.
func (h *CombatHandler) ConfigureActionTimeProvider(provider func(*model.Player, model.RefSkill) float64) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.actionTime = provider
	h.mu.Unlock()
}

func (h *CombatHandler) skillActionTimePercent(player *model.Player, skill model.RefSkill) float64 {
	percent := float64(100)
	if h != nil && h.actionTime != nil {
		percent = h.actionTime(player, skill)
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}
	if player != nil {
		if _, frosted := player.CombatStatus(model.CombatStatusFrostbite, h.combatNow()); frosted {
			percent *= 2
		}
	}
	return percent
}

// ConfigurePVPAdmissionProvider supplies the runtime actor-state policy used
// by native TargetGroup_Enemy_P admission (peace/battle, PK/cape, job, and
// related owner relationships). The default remains permissive because those
// virtual-state fields are not currently reconstructed by AegisCore.
func (h *CombatHandler) ConfigurePVPAdmissionProvider(provider func(*model.Player, *model.Player, model.RefSkill) bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.pvpAdmission = provider
	h.mu.Unlock()
}

func (h *CombatHandler) pvpTargetAdmitted(source, target *model.Player, skill model.RefSkill) bool {
	if h == nil || h.pvpAdmission == nil {
		return true
	}
	return h.pvpAdmission(source, target, skill)
}

// ConfigureLongCooldownSink connects the native external coordination path
// used for very long reuse intervals. The local map remains authoritative for
// this process; the sink owns cross-process/service persistence or messaging.
func (h *CombatHandler) ConfigureLongCooldownSink(sink func(*model.Player, model.RefSkill, time.Time)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.longCooldown = sink
	h.mu.Unlock()
}

func RegisterCombatHandler(target *server.Server, progress model.CharacterProgressRepository, levels *model.LevelTable, rates combatRateProvider, quests ...model.QuestRuntimeRepository) *CombatHandler {
	if target == nil {
		return nil
	}
	world := service.GetWorldServiceInstance()
	handler := &CombatHandler{
		world: world, progress: progress, levels: levels, rates: rates,
		party:    service.GetPartyServiceInstance(),
		schedule: func(delay time.Duration, fn func()) { time.AfterFunc(delay, fn) },
		move: func(player *model.Player, destination navmeshv2.RtNavmeshPosition) uint64 {
			currentTime := time.Now()
			start := player.GetNavmeshPosition()
			movement := world.RegisterMovingCharacter(player, model.MovementData{
				StartTime: currentTime, UpdateTime: currentTime, TargetPosition: destination, HasDestination: true,
			})
			packet := model.BuildMovementUpdatePacket(player, movement, start)
			player.SendToSelfAndKnown(&packet)
			return movement.Generation
		},
		send:          sendCombatPacket,
		broadcast:     broadcastCombatPacket,
		pending:       make(map[uint32]*pendingAttack),
		repeating:     make(map[uint32]*pendingAttack),
		nextAttack:    make(map[uint32]time.Time),
		nextSkill:     make(map[skillCooldownKey]time.Time),
		nextCoolTime:  make(map[uint32]time.Time),
		delayedCasts:  make(map[uint32]delayedCast),
		linkedBuffs:   make(map[uint32]map[uint32]*model.Player),
		linkedParents: make(map[uint32]uint32),
		now:           time.Now,
		rollPercent: func(chance int) bool {
			return chance >= 100 || chance > 0 && rand.Intn(100) < chance
		},
		hitRollPercent: func(chance int) bool {
			return chance >= 100 || chance > 0 && rand.Intn(100) < chance
		},
		criticalRoll: func(chance int) bool {
			return chance >= 100 || chance > 0 && rand.Intn(100) < chance
		},
		blockRoll: func(chance int) bool {
			return chance >= 100 || chance > 0 && rand.Intn(100) < chance
		},
		damageRoll: func() int { return rand.Intn(32768) },
		rollHwanPoint: func(chanceBasisPoints int) bool {
			return chanceBasisPoints >= 10000 || chanceBasisPoints > 0 && rand.Intn(10000) < chanceBasisPoints
		},
	}
	if len(quests) != 0 {
		handler.quests = quests[0]
	}
	target.RegisterHandler(opcode.EntityActionRequest, handler.handlePacket)
	return handler
}

func (h *CombatHandler) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.referenceData = catalog
	h.mu.Unlock()
}

func (h *CombatHandler) ConfigureVitals(vitals model.CharacterVitalsRepository) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.vitals = vitals
	h.mu.Unlock()
}

// ConfigureRangedAmmo installs the persistence boundary used to debit the
// equipped arrow or bolt for each committed ranged attack.
func (h *CombatHandler) ConfigureRangedAmmo(repository model.RangedAmmoRepository) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.rangedAmmo = repository
	h.mu.Unlock()
}

func (h *CombatHandler) ConfigureMonsterMasks(repository model.MonsterMaskRepository) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.monsterMasks = repository
	h.mu.Unlock()
}

func (h *CombatHandler) ConfigureHwan(repository model.HwanRepository) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.hwan = repository
	h.mu.Unlock()
}

func (h *CombatHandler) ConfigureCompanions(companions growthPetCombatSupport) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.companions = companions
	h.mu.Unlock()
}

// ConfigureMonsterDrops installs the post-death drop producer. The producer
// owns reference-table selection; CombatHandler only invokes it once after a
// confirmed mob death and keeps drop failures from rolling back EXP/quest
// progress that has already been committed.
func (h *CombatHandler) ConfigureMonsterDrops(drops *service.MonsterDropService) {
	if h == nil {
		return
	}
	h.dropsMu.Lock()
	h.drops = drops
	h.dropsMu.Unlock()
}

func (h *CombatHandler) engageGrowthPet(player *model.Player, target *model.NPC) {
	if h != nil && h.companions != nil {
		_ = h.companions.EngageOwnerTarget(player, target)
	}
}

func (h *CombatHandler) CancelPlayerAttack(session *server.Session, playerUID uint32) {
	if h == nil || playerUID == 0 {
		return
	}
	h.mu.Lock()
	h.interruptPlayerActionLocked(playerUID)
	h.mu.Unlock()
	_ = h.sendActionState(session, false)
}

// InterruptPlayerAction cancels combat work committed at a player's previous
// position. It is used by external forced-movement producers such as MobAI.
func (h *CombatHandler) InterruptPlayerAction(player *model.Player) {
	if h == nil || player == nil || player.GetUniqueID() == 0 {
		return
	}
	h.mu.Lock()
	h.interruptPlayerActionLocked(player.GetUniqueID())
	h.mu.Unlock()
	_ = h.sendActionState(player.Session, false)
}

func (h *CombatHandler) interruptPlayerActionLocked(playerUID uint32) {
	h.cancelDelayedCast(playerUID)
	h.cancelPendingAttack(playerUID, false)
	h.cancelRepeatingAttack(playerUID)
}

func (h *CombatHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if combatTraceEnabled() {
		logrus.Infof("COMBAT_ACTION_TRACE payload=% X", packet.Payload())
	}
	var candidateAction *vsro188.CombatCandidateActionRequest
	var interactionAction *vsro188.EntityActionInteractionRequest
	if packet.Remaining() == 7 {
		if decoded, decodeErr := (vsro188.CombatCandidateActionRequestCodec{}).Decode(packet); decodeErr == nil {
			candidateAction = &decoded
		}
		if decoded, decodeErr := (vsro188.EntityActionInteractionRequestCodec{}).Decode(packet); decodeErr == nil {
			interactionAction = &decoded
		}
		// Some stock clients use a nonzero final context byte on the same kind-4
		// envelope. Accept that variant only when its UID resolves to a live ground
		// item; combat targets remain governed by the strict codec above.
		if candidateAction == nil && interactionAction == nil && h.world != nil {
			payload := packet.Payload()
			if payload[0] == actionPhaseStart && payload[1] == vsro188.EntityActionKindInteraction && payload[6] != 0 &&
				payload[2] != vsro188.EntityActionTargetModeEntity && payload[2] != vsro188.EntityActionTargetModePosition {
				candidateID := binary.LittleEndian.Uint32(payload[2:6])
				if object, lookupErr := h.world.GetObjectByUniqueId(candidateID); lookupErr == nil {
					switch object.(type) {
					case *model.Item, *model.Equipment:
						candidateAction = &vsro188.CombatCandidateActionRequest{CandidateID: candidateID}
						logrus.Infof("GROUND_PICKUP_ACTION_VARIANT uid=%d context=0x%02X", candidateID, payload[6])
					}
				}
			}
		}
	}
	// A ground-item click uses the same seven-byte kind-4 envelope as a basic
	// target action. Route it before the in-flight combat suppression below:
	// a pending cast must not swallow a non-combat pickup command.
	if candidateAction != nil && h.groundItemPickup != nil && h.world != nil {
		object, lookupErr := h.world.GetObjectByUniqueId(candidateAction.CandidateID)
		switch object.(type) {
		case *model.Item, *model.Equipment:
			if lookupErr != nil {
				return h.sendActionState(session, false)
			}
			if pickupErr := h.groundItemPickup(ctx, session, candidateAction.CandidateID); pickupErr != nil {
				return pickupErr
			}
			return nil
		}
	}
	// Stock ground-item clicks use the interaction form
	// `01 | 02 | 01 | targetUID:u32`. Dimension Hole portals share this
	// envelope, so route it to inventory pickup only after the target resolves
	// to a ground item; portal interactions continue through the switch below.
	if interactionAction != nil && h.groundItemPickup != nil && h.world != nil {
		object, lookupErr := h.world.GetObjectByUniqueId(interactionAction.TargetUID)
		if lookupErr == nil {
			switch object.(type) {
			case *model.Item, *model.Equipment:
				if pickupErr := h.groundItemPickup(ctx, session, interactionAction.TargetUID); pickupErr != nil {
					return pickupErr
				}
				return nil
			}
		}
	}
	if packet.Remaining() < 1 {
		return fmt.Errorf("combat action payload length %d, want at least 1", packet.Remaining())
	}
	// Native 0x004AD630 keeps a conflicting start behind the actor-owned
	// action instead of replacing the in-flight engage task. Until the full
	// action queue is represented here, ignore rapid starts so they cannot
	// overwrite the delayed impact token. The phase-only cancel below remains
	// authoritative and still interrupts the current cast.
	payload := packet.Payload()
	if session != nil && payload[0] == actionPhaseStart && (len(payload) < 2 || payload[1] != vsro188.EntityActionKindInteraction) && h.delayedCasts != nil {
		if active, exists := h.delayedCasts[session.UserContext.UniqueID]; exists && active.token != 0 {
			return nil
		}
	}
	phase, err := packet.ReadByte()
	if err != nil {
		return err
	}
	// The stock client sends the cancel/stop form as only the phase byte. The
	// original server accepts it without a response. Trying to read an action
	// byte from this form turns a normal attack stop into a disconnect.
	if phase != actionPhaseStart {
		if packet.Remaining() != 0 {
			return fmt.Errorf("combat cancel payload has %d trailing bytes", packet.Remaining())
		}
		if session != nil {
			h.cancelDelayedCast(session.UserContext.UniqueID)
			h.cancelPendingAttack(session.UserContext.UniqueID, true)
			h.cancelRepeatingAttack(session.UserContext.UniqueID)
			return h.sendActionState(session, false)
		}
		return nil
	}
	if packet.Remaining() < 1 {
		return fmt.Errorf("combat start payload is missing action type")
	}
	actionType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	if actionType == actionTypeAttack || actionType == actionTypeAttackLegacy {
		actingPlayer, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr == nil && actingPlayer.GetMotionState() == model.Sitting {
			actingPlayer.SetMotionState(model.Stand)
			stand := model.BuildMotionStateUpdatePacket(actingPlayer.GetUniqueID(), model.Stand)
			actingPlayer.SendToSelfAndKnown(&stand)
		}
	}
	var targetUID uint32
	var skill model.RefSkill
	var areaTarget *navmeshv2.RtNavmeshPosition
	isSkillCast := false
	isSelfBuff := false
	isSelfHealing := false
	isAreaStatus := false
	isAreaCure := false
	isAreaAggro := false
	isBarrier := false
	var player *model.Player
	switch actionType {
	case vsro188.EntityActionKindInteraction:
		if interactionAction == nil {
			return h.sendActionState(session, false)
		}
		player, portal, resolveErr := h.resolveDimensionHolePortalInteraction(session, interactionAction.TargetUID)
		if resolveErr != nil {
			return h.sendActionState(session, false)
		}
		entry := h.dimensionHoles
		if entry == nil {
			return h.sendActionState(session, false)
		}
		if _, entryErr := entry.Enter(ctx, portal.GetUniqueID(), player); entryErr != nil {
			logrus.WithError(entryErr).WithFields(logrus.Fields{
				"uid": player.GetUniqueID(), "portal_uid": portal.GetUniqueID(),
			}).Debug("rejected Dimension Hole entry")
		}
		// B074 acknowledges the one-shot action boundary; the entry coordinator
		// owns the independent loading exchange and rolls admission back on error.
		return h.sendActionState(session, false)
	case actionTypeDispel:
		if packet.Remaining() != 5 {
			return fmt.Errorf("buff cancellation payload has %d trailing bytes, want 5", packet.Remaining())
		}
		skillID, readErr := packet.ReadUInt32()
		if readErr != nil {
			return readErr
		}
		terminator, readErr := packet.ReadByte()
		if readErr != nil {
			return readErr
		}
		player, readErr = h.resolveCombatPlayer(session)
		if readErr != nil || terminator != 0 {
			return h.sendActionState(session, false)
		}
		return h.cancelSelfBuff(session, player, skillID)
	case actionTypeAttack:
		switch packet.Remaining() {
		case 5:
			// A basic attack and a no-target skill share this five-byte tail.
			if candidateAction == nil {
				return h.sendActionState(session, false)
			}
			candidateID := candidateAction.CandidateID
			candidate, exists := h.referenceSkill(candidateID)
			kind := candidate.ExecutionKind()
			if exists && (kind == model.SkillExecutionBuff || kind == model.SkillExecutionBarrier || kind == model.SkillExecutionMovement || kind == model.SkillExecutionStatusAttack && !candidate.TargetRequired || kind == model.SkillExecutionCure && !candidate.TargetRequired || kind == model.SkillExecutionAggro && !candidate.TargetRequired || kind == model.SkillExecutionHealing && (candidate.TargetSelf || !candidate.TargetRequired)) {
				player, err = h.resolveCombatPlayer(session)
				if err == nil && playerOwnsSkill(player, candidateID, candidate) {
					skill, isSkillCast = candidate, true
					isSelfBuff = kind == model.SkillExecutionBuff
					isSelfHealing = kind == model.SkillExecutionHealing
					isAreaStatus = kind == model.SkillExecutionStatusAttack
					isAreaCure = kind == model.SkillExecutionCure
					isAreaAggro = kind == model.SkillExecutionAggro
					isBarrier = kind == model.SkillExecutionBarrier
					break
				}
			}
			targetUID = candidateID
		case 9:
			// vSRO targeted skill cast: skill ID, entity target marker, target UID.
			skillID, readErr := packet.ReadUInt32()
			if readErr != nil {
				return readErr
			}
			targetType, readErr := packet.ReadByte()
			if readErr != nil {
				return readErr
			}
			targetUID, readErr = packet.ReadUInt32()
			if readErr != nil {
				return readErr
			}
			var exists bool
			skill, exists = h.referenceSkill(skillID)
			if targetType != 1 || targetUID == 0 || !exists {
				return h.sendActionState(session, false)
			}
			isSkillCast = true
		case 19:
			// Position-targeted stock cast: skill, area marker, region, then
			// X/Z/Y int32 offsets. Ghost Walk uses this exact 21-byte packet.
			skillID, readErr := packet.ReadUInt32()
			if readErr != nil {
				return readErr
			}
			targetType, readErr := packet.ReadByte()
			if readErr != nil || targetType != 2 {
				return h.sendActionState(session, false)
			}
			regionID, readErr := packet.ReadInt16()
			if readErr != nil {
				return readErr
			}
			x, xErr := packet.ReadInt32()
			z, zErr := packet.ReadInt32()
			y, yErr := packet.ReadInt32()
			if err := firstError(xErr, zErr, yErr); err != nil {
				return err
			}
			candidate, exists := h.referenceSkill(skillID)
			if !exists || candidate.ExecutionKind() != model.SkillExecutionMovement {
				return h.sendActionState(session, false)
			}
			// The action-position tuple is X, Z(height), Y(map axis).
			// RtNavmeshPosition uses engine X, Y(height), Z(map axis).
			position, positionErr := resolveDestinationPosition(h.world, regionID, math32.NewVector3(float32(x), float32(z), float32(y)))
			if positionErr != nil {
				return h.sendActionState(session, false)
			}
			skill, isSkillCast, areaTarget = candidate, true, &position
		default:
			return h.sendActionState(session, false)
		}
	case actionTypeAttackLegacy:
		if packet.Remaining() != 5 {
			return fmt.Errorf("legacy combat attack payload has %d trailing bytes, want 5", packet.Remaining())
		}
		// Retain the previous emulator form for non-stock clients:
		// `01 | 01 | targeted:bool | targetUID:uint32`.
		targeted, readErr := packet.ReadBool()
		if readErr != nil {
			return readErr
		}
		targetUID, err = packet.ReadUInt32()
		if err != nil {
			return err
		}
		if !targeted || targetUID == 0 {
			return h.sendActionState(session, false)
		}
	default:
		return h.sendActionState(session, false)
	}
	if isSelfBuff {
		return h.executeSelfBuff(ctx, session, player, skill)
	}
	if isSelfHealing {
		return h.executeHealing(ctx, session, player, player, skill)
	}
	if isAreaStatus {
		return h.executeAreaStatusSkill(ctx, session, player, skill)
	}
	if isAreaCure {
		return h.executeAreaCureSkill(ctx, session, player, skill)
	}
	if isAreaAggro {
		return h.executeAreaAggroSkill(ctx, session, player, skill)
	}
	if isBarrier {
		return h.executeBarrierSkill(ctx, session, player, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionMovement {
		player, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(player, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeMovementSkill(ctx, session, player, skill, areaTarget)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionBuff {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		if h.world == nil {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validHealingTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeBuff(ctx, session, caster, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionHealing {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validHealingTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeHealing(ctx, session, caster, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionResurrection {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validResurrectionTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeResurrection(ctx, session, caster, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionCure {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validHealingTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeCureSkill(ctx, session, caster, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionTransform {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsNPC := object.(*model.NPC)
		if resolveErr != nil || !targetIsNPC {
			return h.sendActionState(session, false)
		}
		return h.executeMonsterMaskSkill(ctx, session, caster, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionAggro && !skill.TargetEnemyMob {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validHealingTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeTargetedPlayerAggroSkill(ctx, session, caster, target, skill)
	}

	if isSkillCast && skill.TargetEnemyPVP && skill.ExecutionKind() == model.SkillExecutionStatusAttack {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) || h.world == nil {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		pvpTarget, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil {
			return h.sendActionState(session, false)
		}
		if targetIsPlayer {
			if pvpTarget == caster || pvpTarget.GetLifeState() != model.Alive || !h.pvpTargetAdmitted(caster, pvpTarget, skill) {
				return h.sendActionState(session, false)
			}
			return h.executeTargetedPlayerStatusSkill(ctx, session, caster, pvpTarget, skill)
		}
	}
	if isSkillCast && skill.TargetEnemyPVP && (skill.ExecutionKind() == model.SkillExecutionPhysicalAttack || skill.ExecutionKind() == model.SkillExecutionMagicalAttack) {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID, skill) {
			return h.sendActionState(session, false)
		}
		if h.world == nil {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		pvpTarget, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil {
			return h.sendActionState(session, false)
		}
		if targetIsPlayer {
			if pvpTarget == caster || pvpTarget.GetLifeState() != model.Alive || !h.pvpTargetAdmitted(caster, pvpTarget, skill) {
				return h.sendActionState(session, false)
			}
			return h.executePlayerSkillAttack(ctx, session, caster, pvpTarget, skill)
		}
	}

	player, target, err := h.resolveAttack(session, targetUID)
	if err != nil {
		logrus.WithError(err).Infof("COMBAT_REJECT_TRACE gate=resolve-attack skill=%d target=%d", skill.ID, targetUID)
		_ = h.sendActionState(session, false)
		return nil
	}
	if isSkillCast && !playerOwnsSkill(player, skill.ID, skill) {
		logrus.Infof("COMBAT_REJECT_TRACE gate=skill-ownership skill=%d target=%d", skill.ID, targetUID)
		return h.sendActionState(session, false)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionStatusAttack {
		return h.executeTargetedStatusSkill(ctx, session, player, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionAggro {
		return h.executeTargetedAggroSkill(ctx, session, player, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() != model.SkillExecutionPhysicalAttack && skill.ExecutionKind() != model.SkillExecutionMagicalAttack {
		logrus.Infof("COMBAT_REJECT_TRACE gate=execution-kind skill=%d kind=%d target=%d", skill.ID, skill.ExecutionKind(), targetUID)
		return h.sendActionState(session, false)
	}
	if isSkillCast {
		if reason, rejected := skillCastRejection(player, skill); rejected {
			logrus.Infof("COMBAT_REJECT_TRACE gate=cast-requirements skill=%d reason=0x%02X target=%d", skill.ID, reason, targetUID)
			return h.sendSkillCastError(session, reason)
		}
	}
	reach := h.baseAttackReach(player, target)
	if isSkillCast {
		reach = h.skillAttackReach(player, target, skill)
	} else if maskSkillID, masked := player.CharacterMaskBaseSkill(time.Now()); masked {
		if maskSkill, exists := h.referenceSkill(maskSkillID); exists {
			reach = h.skillAttackReach(player, target, maskSkill)
		}
	}
	if navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()) > reach {
		logrus.Infof("COMBAT_REJECT_TRACE gate=range-pursuit skill=%d target=%d distance=%.3f reach=%.3f", skill.ID, targetUID, navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()), reach)
		h.cancelRepeatingAttack(player.GetUniqueID())
		if err := h.beginAttackPursuit(session, player, target, reach, skill.ID); err != nil {
			_ = h.sendActionState(session, false)
		}
		return nil
	}
	h.cancelPendingAttack(player.GetUniqueID(), true)
	h.cancelRepeatingAttack(player.GetUniqueID())
	if isSkillCast {
		err = h.executeSkillAttack(ctx, session, player, target, skill)
	} else {
		err = h.executeAttack(ctx, session, player, target)
	}
	if err != nil {
		return err
	}
	if !isSkillCast && target.GetLifeState() == model.Alive {
		h.startRepeatingAttack(player, session, target)
	}
	return nil
}

func (h *CombatHandler) executeTargetedPlayerStatusSkill(ctx context.Context, session *server.Session, player, target *model.Player, skill model.RefSkill) error {
	if player == nil || target == nil || !skill.TargetEnemyPVP || skill.ExecutionKind() != model.SkillExecutionStatusAttack || target.GetLifeState() != model.Alive || !h.pvpTargetAdmitted(player, target, skill) {
		return h.sendActionState(session, false)
	}
	if skill.ActionRange > 0 && navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
	}
	h.facePlayerTowardTarget(player, target)
	return h.beginStatusCast(session, player, target.GetUniqueID(), skill, func(impactTime time.Time) []model.SkillEffectTarget {
		if target.GetLifeState() != model.Alive {
			return nil
		}
		mask := h.applyPlayerSkillStatuses(target, player, skill, impactTime)
		if mask == 0 {
			return nil
		}
		return []model.SkillEffectTarget{{TargetUID: target.GetUniqueID(), EffectMask: mask}}
	})
}

func (h *CombatHandler) cancelSelfBuff(session *server.Session, player *model.Player, skillID uint32) error {
	// Cancellation is authorized by the player's persisted skill ownership and
	// active-buff state. It must remain available if the optional reference
	// catalog is absent or being refreshed.
	if player == nil || !playerOwnsSkill(player, skillID) {
		return h.sendActionState(session, false)
	}
	if barrier := player.BarrierSnapshot(); barrier.SkillID == skillID {
		if removed, ok := player.RemoveActiveBarrier(); ok {
			packet := model.BuildBuffRemove(removed.Token)
			if err := h.emitAttack(player, &packet); err != nil {
				return err
			}
			return h.sendActionState(session, false)
		}
	}
	token, removed := player.RemoveActiveBuffBySkill(skillID)
	if !removed {
		return h.sendActionState(session, false)
	}
	h.removeLinkedBuffs(player, token)
	h.unregisterLinkedBuff(token)
	h.syncHiddenBodyState(player)
	h.reconcilePlayerVisibility(player)
	packet := model.BuildBuffRemove(token)
	if err := h.emitAttack(player, &packet); err != nil {
		return err
	}
	return h.sendPlayerPacket(session, player.BuildStatsUpdatePacket())
}

func validHealingTarget(caster, target *model.Player, skill model.RefSkill) bool {
	if caster == nil || target == nil || target.GetLifeState() != model.Alive {
		return false
	}
	if caster == target {
		return skill.TargetSelf || skill.TargetAlly
	}
	party := caster.GetParty()
	return party != nil && party == target.GetParty() && (skill.TargetAlly || skill.TargetParty)
}

func validResurrectionTarget(caster, target *model.Player, skill model.RefSkill) bool {
	if caster == nil || target == nil || caster == target || target.GetLifeState() != model.Dead || target.CurrentHP() != 0 || target.Level > skill.ResurrectLevel {
		return false
	}
	party := caster.GetParty()
	return party != nil && party == target.GetParty() && (skill.TargetAlly || skill.TargetParty)
}

func (h *CombatHandler) executeHealing(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionHealing || h.vitals == nil {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(caster, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	if skill.ActionRange > 0 && caster != target && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	key := skillCooldownKey{playerUID: caster.GetUniqueID(), skillID: skill.ID}
	if !h.skillAvailable(key, now) {
		logrus.Infof("COMBAT_REJECT_TRACE gate=cooldown skill=%d target=%d", skill.ID, target.GetUniqueID())
		return h.sendActionState(session, false)
	}
	if err := h.spendSkillResources(ctx, session, caster, skill); err != nil {
		return err
	}
	h.registerSkillCooldownForPlayer(caster, key, skill, now)
	// Resource consumption and cooldown registration are pre-engage state. The
	// healing mutation itself belongs to the impact producer after the authored
	// preparation/casting interval, just like attack and status skills.
	resourceCost := playerHasSkillResourceCost(caster, skill)
	if caster != target {
		h.facePlayerTowardTarget(caster, target)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, caster.GetUniqueID(), target.GetUniqueID(), castUID)
	if err := h.emitAttack(caster, &begin); err != nil {
		return err
	}
	finish := func() {
		var healedHP, healedMP int
		healed := false
		if target.GetLifeState() == model.Alive {
			maxHP, maxMP := target.MaxVitals()
			hpAmount, mpAmount := skill.RestorationAmounts(maxHP, maxMP)
			expectedHP, expectedMP, nextHP, nextMP, ok := target.PreviewHealing(hpAmount, mpAmount)
			if ok && h.vitals.SaveVitals(context.Background(), target.ID, nextHP, nextMP) == nil && target.CommitHealing(expectedHP, expectedMP, nextHP, nextMP) {
				healedHP, healedMP, healed = nextHP, nextMP, true
			}
		}
		end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
		_ = h.emitAttack(caster, &end)
		_ = h.sendActionState(session, false)
		if healed {
			_ = h.sendPlayerPacket(target.Session, model.BuildVitalsUpdatePacket(target.GetUniqueID(), healedHP, healedMP))
			_ = h.applyHealingTransfers(context.Background(), caster, target, skill)
		}
		if resourceCost && caster != target {
			hp, mp := caster.CurrentVitals()
			_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(caster.GetUniqueID(), hp, mp))
		}
	}
	delay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(caster, skill))
	if delay > 0 && h.schedule != nil {
		delayedToken := h.beginDelayedCast(caster.GetUniqueID(), func() {
			end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
			_ = h.emitAttack(caster, &end)
			_ = h.sendActionState(session, false)
			if resourceCost && caster != target {
				hp, mp := caster.CurrentVitals()
				_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(caster.GetUniqueID(), hp, mp))
			}
		})
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(caster.GetUniqueID(), delayedToken) {
				return
			}
			finish()
		})
		return nil
	}
	finish()
	return nil
}

func (h *CombatHandler) applyHealingTransfers(ctx context.Context, caster, primary *model.Player, skill model.RefSkill) error {
	maximum := skill.TransferCount - 1
	if h == nil || h.vitals == nil || caster == nil || primary == nil || maximum <= 0 || skill.TransferRange <= 0 {
		return nil
	}
	falloff := min(max(skill.TransferFalloffPercent, 0), 100)
	factor := 100 - falloff
	for _, target := range h.healingTransferTargets(caster, primary, skill, maximum) {
		factor = nextTransferFactor(factor, falloff)
		maxHP, maxMP := target.MaxVitals()
		hpAmount, mpAmount := skill.RestorationAmounts(maxHP, maxMP)
		hpAmount = scaleTransferAmount(hpAmount, factor)
		mpAmount = scaleTransferAmount(mpAmount, factor)
		expectedHP, expectedMP, healedHP, healedMP, ok := target.PreviewHealing(hpAmount, mpAmount)
		if !ok {
			continue
		}
		if err := h.vitals.SaveVitals(ctx, target.ID, healedHP, healedMP); err != nil {
			return err
		}
		if !target.CommitHealing(expectedHP, expectedMP, healedHP, healedMP) {
			continue
		}
		packet := model.BuildVitalsUpdatePacket(target.GetUniqueID(), healedHP, healedMP)
		if err := h.sendPlayerPacket(target.Session, packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *CombatHandler) healingTransferTargets(caster, primary *model.Player, skill model.RefSkill, maximum int) []*model.Player {
	if caster == nil || primary == nil || maximum <= 0 || caster.GetKnownObjectList() == nil || skill.TransferType == 5 || skill.TransferType == 7 {
		return nil
	}
	pool := make([]*model.Player, 0)
	seen := map[uint32]struct{}{primary.GetUniqueID(): {}}
	addCandidate := func(candidate *model.Player) {
		if candidate == nil {
			return
		}
		if _, exists := seen[candidate.GetUniqueID()]; exists || !validHealingTarget(caster, candidate, skill) {
			return
		}
		seen[candidate.GetUniqueID()] = struct{}{}
		pool = append(pool, candidate)
	}
	// The native geometry producers treat the caster as an ordinary candidate
	// but admit it only when descriptor mask bit 0x01 is set. This matters for
	// party chain heals whose primary target is another member: the authored
	// mask decides whether the caster can receive a propagated hit as well.
	if skill.TransferTargetMask&1 != 0 {
		addCandidate(caster)
	}
	for _, object := range caster.GetKnownObjectList().GetKnownObjects() {
		candidate, ok := object.(*model.Player)
		if ok {
			addCandidate(candidate)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].GetUniqueID() < pool[j].GetUniqueID() })
	radius := float32(skill.TransferRange)
	if skill.TransferType == 6 {
		result := make([]*model.Player, 0, maximum)
		current := primary
		for len(result) < maximum {
			selected, selectedIndex, best := (*model.Player)(nil), -1, float32(math.MaxFloat32)
			for index, candidate := range pool {
				distance := navmeshv2.HorizontalDistance(current.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				if distance <= radius && (distance < best || distance == best && (selected == nil || candidate.GetUniqueID() < selected.GetUniqueID())) {
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
	center := primary.GetNavmeshPosition()
	if skill.TransferType == 1 {
		center = caster.GetNavmeshPosition()
	}
	candidates := make([]struct {
		player *model.Player
		metric float32
	}, 0, len(pool))
	for _, candidate := range pool {
		distance := navmeshv2.HorizontalDistance(center, candidate.GetNavmeshPosition())
		if distance <= radius {
			candidates = append(candidates, struct {
				player *model.Player
				metric float32
			}{player: candidate, metric: distance})
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

func (h *CombatHandler) executeResurrection(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	resurrection, ok := h.vitals.(model.SkillResurrectionRepository)
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionResurrection || !ok || !validResurrectionTarget(caster, target, skill) {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(caster, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	if skill.ActionRange > 0 && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	now := h.combatNow()
	key := skillCooldownKey{playerUID: caster.GetUniqueID(), skillID: skill.ID}
	if !h.skillAvailable(key, now) {
		return h.sendActionState(session, false)
	}
	_, casterMP := caster.CurrentVitals()
	if skill.ConsumeMP < 0 || casterMP < skill.ConsumeMP {
		return h.sendActionState(session, false)
	}
	h.registerSkillCooldownForPlayer(caster, key, skill, now)
	h.facePlayerTowardTarget(caster, target)
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, caster.GetUniqueID(), target.GetUniqueID(), castUID)
	if err := h.emitAttack(caster, &begin); err != nil {
		return err
	}
	finish := func() error {
		maxHP, maxMP := target.MaxVitals()
		targetHP, targetMP := skill.RestorationAmounts(maxHP, maxMP)
		targetHP = max(1, targetHP)
		_, currentMP := caster.CurrentVitals()
		committed, err := resurrection.CastResurrection(context.Background(), caster.ID, target.ID, currentMP, skill.ConsumeMP, targetHP, targetMP)
		if err != nil {
			end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
			_ = h.emitAttack(caster, &end)
			_ = h.sendActionState(session, false)
			return err
		}
		if err == nil && committed && model.CommitSkillResurrection(caster, target, currentMP, skill.ConsumeMP, targetHP, targetMP) {
			end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
			_ = h.emitAttack(caster, &end)
			_ = h.sendActionState(session, false)
			_ = h.sendPlayerPacket(target.Session, model.BuildVitalsUpdatePacket(target.GetUniqueID(), targetHP, targetMP))
			life := model.BuildLifeStateUpdatePacket(target.GetUniqueID(), model.Alive)
			h.publishCombatPacket(caster, &life)
			if skill.ConsumeMP > 0 {
				casterHP, casterCurrentMP := caster.CurrentVitals()
				_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(caster.GetUniqueID(), casterHP, casterCurrentMP))
			}
			if visibility, supported := h.world.(resurrectionVisibilityReconciler); supported {
				_ = visibility.ReconcileCharacterVisibility(target)
			}
			return nil
		}
		end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
		_ = h.emitAttack(caster, &end)
		_ = h.sendActionState(session, false)
		return nil
	}
	delay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(caster, skill))
	if delay > 0 && h.schedule != nil {
		delayedToken := h.beginDelayedCast(caster.GetUniqueID(), func() {
			end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
			_ = h.emitAttack(caster, &end)
			_ = h.sendActionState(session, false)
		})
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(caster.GetUniqueID(), delayedToken) {
				return
			}
			if err := finish(); err != nil {
				logrus.WithError(err).Warn("delayed resurrection impact failed")
			}
		})
		return nil
	}
	return finish()
}

func (h *CombatHandler) executeCureSkill(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionCure {
		return h.sendActionState(session, false)
	}
	if skill.ActionRange > 0 && caster != target && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, caster, skill)
	if err != nil || !ready {
		return err
	}
	if caster != target {
		h.facePlayerTowardTarget(caster, target)
	}
	return h.beginUtilityCast(session, caster, target.GetUniqueID(), skill, func(now time.Time) {
		h.curePlayer(target, skill, now)
		remaining, levels := target.CombatBadEffectState(now)
		packet := model.BuildBadEffectUpdate(target.GetUniqueID(), remaining, levels)
		h.publishCombatPacket(caster, &packet)
	})
}

func (h *CombatHandler) executeAreaCureSkill(ctx context.Context, session *server.Session, caster *model.Player, skill model.RefSkill) error {
	if caster == nil || skill.ExecutionKind() != model.SkillExecutionCure || skill.TargetRequired {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, caster, skill)
	if err != nil || !ready {
		return err
	}
	return h.beginUtilityCast(session, caster, caster.GetUniqueID(), skill, func(now time.Time) {
		targets := []*model.Player{caster}
		if known := caster.GetKnownObjectList(); known != nil {
			for _, object := range known.GetKnownObjects() {
				target, ok := object.(*model.Player)
				if !ok || target == nil || target == caster || !validHealingTarget(caster, target, skill) {
					continue
				}
				radius := float32(skill.TransferRange)
				if radius <= 0 {
					radius = float32(skill.ActionRange)
				}
				if radius <= 0 || navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) <= radius {
					targets = append(targets, target)
				}
			}
		}
		for _, target := range targets {
			h.curePlayer(target, skill, now)
			remaining, levels := target.CombatBadEffectState(now)
			packet := model.BuildBadEffectUpdate(target.GetUniqueID(), remaining, levels)
			h.publishCombatPacket(caster, &packet)
		}
	})
}

func (h *CombatHandler) curePlayer(target *model.Player, skill model.RefSkill, now time.Time) int {
	if target == nil {
		return 0
	}
	roll := h.rollPercent
	if roll == nil {
		roll = func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance }
	}
	chance := skill.CureCurseChance
	if chance <= 0 {
		chance = 100
	}
	return target.CureCombatStatusesWithCurse(
		skill.CureStatusMask, skill.CureStatusMaxLevel,
		skill.CureCurse, skill.CureCurseMaxLevel, chance,
		skill.CureCount, now, roll,
	)
}

func (h *CombatHandler) executeTargetedAggroSkill(ctx context.Context, session *server.Session, caster *model.Player, target *model.NPC, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionAggro {
		return h.sendActionState(session, false)
	}
	if skill.ActionRange > 0 && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, caster, skill)
	if err != nil || !ready {
		return err
	}
	h.facePlayerTowardTarget(caster, target)
	return h.beginUtilityCast(session, caster, target.GetUniqueID(), skill, func(time.Time) {
		if target.GetLifeState() != model.Alive {
			return
		}
		h.applyAggroSkill(caster, target, skill)
	})
}

func (h *CombatHandler) executeAreaAggroSkill(ctx context.Context, session *server.Session, caster *model.Player, skill model.RefSkill) error {
	if caster == nil || skill.ExecutionKind() != model.SkillExecutionAggro || skill.TargetRequired {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, caster, skill)
	if err != nil || !ready {
		return err
	}
	return h.beginUtilityCast(session, caster, caster.GetUniqueID(), skill, func(time.Time) {
		known := caster.GetKnownObjectList()
		if known == nil {
			return
		}
		radius := float32(skill.TransferRange)
		if radius <= 0 {
			radius = float32(skill.ActionRange)
		}
		for _, object := range known.GetKnownObjects() {
			target, ok := object.(*model.NPC)
			if !ok || target == nil || target.GetLifeState() != model.Alive || !target.GetTypeInfo().IsNPCMob() {
				continue
			}
			if radius <= 0 || navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) <= radius {
				h.applyAggroSkill(caster, target, skill)
			}
		}
	})
}

func (h *CombatHandler) executeTargetedPlayerAggroSkill(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionAggro || skill.AggroReduce <= 0 {
		return h.sendActionState(session, false)
	}
	if skill.ActionRange > 0 && caster != target && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, caster, skill)
	if err != nil || !ready {
		return err
	}
	if caster != target {
		h.facePlayerTowardTarget(caster, target)
	}
	return h.beginUtilityCast(session, caster, target.GetUniqueID(), skill, func(time.Time) {
		known := target.GetKnownObjectList()
		if known == nil {
			return
		}
		for _, object := range known.GetKnownObjects() {
			npc, ok := object.(*model.NPC)
			if ok && npc != nil && npc.GetTypeInfo().IsNPCMob() {
				npc.ReduceAggro(target.GetUniqueID(), skill.AggroReduce)
			}
		}
	})
}

func (h *CombatHandler) executeMonsterMaskSkill(ctx context.Context, session *server.Session, caster *model.Player, target *model.NPC, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionTransform || h.monsterMasks == nil || h.referenceData == nil {
		return h.sendActionState(session, false)
	}
	if target.GetLifeState() != model.Dead || !target.GetTypeInfo().IsNPCMob() {
		return h.sendActionState(session, false)
	}
	character, exists := h.referenceData.Character(target.GetRefObjectID())
	if !exists || character.Level > skill.TransformMaxLevel {
		return h.sendActionState(session, false)
	}
	maskItem, exists := h.referenceData.Item(skill.TransformItemRefID)
	if !exists || maskItem.TypeID1 != 3 || maskItem.TypeID2 != 2 || maskItem.TypeID3 != 2 {
		return h.sendActionState(session, false)
	}
	if skill.ActionRange > 0 && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, caster, skill)
	if err != nil || !ready {
		return err
	}
	h.facePlayerTowardTarget(caster, target)
	return h.beginUtilityCast(session, caster, target.GetUniqueID(), skill, func(time.Time) {
		operationContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		caster.LockEconomy()
		inventory, slot, createErr := h.monsterMasks.CreateMonsterMask(operationContext, int64(caster.ID), skill.TransformItemRefID, target.GetRefObjectID())
		if createErr == nil {
			caster.CommitInventory(inventory)
		}
		caster.UnlockEconomy()
		if createErr != nil {
			logrus.WithError(createErr).Warn("create monster mask essence")
			return
		}
		item, exists := inventory.Items[slot]
		if !exists {
			return
		}
		packet := buildServerInventoryItemAdded(slot, item)
		_ = h.sendPlayerPacket(session, packet)
	})
}

func buildServerInventoryItemAdded(slot byte, _ model.Item) network.Packet {
	// The stock 0x0E success branch is only two scalar bytes after the
	// discriminator. The second value is preserved from the recovered server
	// path without assigning it an unresolved business name.
	return vsro188.BuildItemOperationTwoByteSuccess(vsro188.ItemOperationServerAdd, slot, 0x3C)
}

func (h *CombatHandler) applyAggroSkill(caster *model.Player, target *model.NPC, skill model.RefSkill) {
	if caster == nil || target == nil {
		return
	}
	if skill.AggroIncrease > 0 {
		target.AddAggro(caster.GetUniqueID(), skill.AggroIncrease)
		return
	}
	if skill.AggroReduce > 0 {
		target.ReduceAggro(caster.GetUniqueID(), skill.AggroReduce)
	}
}

func (h *CombatHandler) beginUtilityCast(session *server.Session, caster *model.Player, targetUID uint32, skill model.RefSkill, apply func(time.Time)) error {
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, caster.GetUniqueID(), targetUID, castUID)
	if err := h.emitAttack(caster, &begin); err != nil {
		return err
	}
	finish := func() {
		now := time.Now()
		if h.now != nil {
			now = h.now()
		}
		apply(now)
		end := model.BuildBuffCastEnd(targetUID, castUID)
		_ = h.emitAttack(caster, &end)
		_ = h.sendActionState(session, false)
	}
	delay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(caster, skill))
	if delay > 0 && h.schedule != nil {
		delayedToken := h.beginDelayedCast(caster.GetUniqueID(), func() {
			end := model.BuildBuffCastEnd(targetUID, castUID)
			_ = h.emitAttack(caster, &end)
			_ = h.sendActionState(session, false)
		})
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(caster.GetUniqueID(), delayedToken) {
				return
			}
			finish()
		})
		return nil
	}
	finish()
	return nil
}

func (h *CombatHandler) referenceSkill(skillID uint32) (model.RefSkill, bool) {
	if h == nil || h.referenceData == nil || skillID == 0 {
		return model.RefSkill{}, false
	}
	return h.referenceData.Skill(skillID)
}

func playerOwnsSkill(player *model.Player, skillID uint32, references ...model.RefSkill) bool {
	if player == nil {
		return false
	}
	if _, masked := player.CharacterMaskBaseSkill(time.Now()); masked {
		return player.CharacterMaskSkill(skillID, time.Now())
	}
	for _, skill := range player.SkillSnapshot() {
		if skill.ID == skillID && skill.Enabled == 1 {
			return true
		}
	}
	if len(references) != 0 {
		skill := references[0]
		// NASRUN actions are granted by the equipped Devil Spirit and are not
		// persisted in CHAR_SKILL. The stock skill rows carry the explicit
		// required-item tuple (TypeID3 14, TypeID4 1), so use that tuple as the
		// ownership boundary instead of admitting arbitrary mall skills.
		if strings.Contains(strings.ToUpper(skill.CodeName), "_NASRUN_") && playerHasRequiredSkillItem(player, skill) {
			return true
		}
	}
	return false
}

func playerBaseAttackSkillID(player *model.Player) uint32 {
	if player == nil {
		return 0
	}
	if skillID, masked := player.CharacterMaskBaseSkill(time.Now()); masked {
		return skillID
	}
	return player.BaseAttackSkillID()
}

func playerHasSkillMP(player *model.Player, skill model.RefSkill) bool {
	_, mp := player.CurrentVitals()
	return skill.ConsumeMP >= 0 && mp >= skill.ConsumeMP
}

func playerCanCastWithWeapon(player *model.Player, skill model.RefSkill) bool {
	if player == nil {
		return false
	}
	inventory := player.GetInventory()
	requiredWeapons := [...]byte{skill.RequiredWeapon1, skill.RequiredWeapon2}
	restricted := false
	for _, requiredWeapon := range requiredWeapons {
		if requiredWeapon != 0 && requiredWeapon != 255 {
			restricted = true
			break
		}
	}
	if !restricted && !playerHasRequiredSkillItem(player, skill) {
		return false
	}
	if !restricted {
		return true
	}

	weaponType := byte(0)
	weapon, equipped := inventory.Items[model.SlotPrimaryWeapon]
	if equipped && weapon.GetTypeInfo().IsWeapon() {
		weaponType = byte(weapon.GetTypeInfo().TypeID4)
	}
	for _, requiredWeapon := range requiredWeapons {
		if requiredWeapon != 0 && requiredWeapon != 255 && weaponType == requiredWeapon {
			if !playerHasRequiredSkillItem(player, skill) {
				return false
			}
			// Ranged weapons require their authored secondary item. A bow or
			// crossbow cannot cast while its arrow/bolt slot is empty.
			return playerHasRangedAmmo(player, weapon)
		}
	}
	return false
}

func playerHasRangedAmmo(player *model.Player, weapon model.Item) bool {
	if player == nil {
		return false
	}
	if !weapon.IsBow() && !weapon.IsCrossbow() {
		return true
	}
	ammo, equipped := player.GetInventory().Items[model.SlotSecondaryWeapon]
	if !equipped || ammo.StackSize <= 0 {
		return false
	}
	if weapon.IsBow() {
		return ammo.IsArrow()
	}
	return ammo.IsBolt()
}

// consumeRangedAmmo commits one equipped arrow or bolt and publishes the
// corresponding stock inventory update. Melee attacks deliberately return
// without mutating inventory. A nil repository is retained for isolated
// handler tests; production composition always installs the SQL repository.
func (h *CombatHandler) consumeRangedAmmo(ctx context.Context, session *server.Session, player *model.Player) error {
	if player == nil {
		return h.sendSkillCastError(session, skillCastErrorInvalidAmmo)
	}
	player.LockEconomy()
	defer player.UnlockEconomy()

	inventory := player.GetInventory()
	weapon, weaponEquipped := inventory.Items[model.SlotPrimaryWeapon]
	if !weaponEquipped || (!weapon.IsBow() && !weapon.IsCrossbow()) {
		return nil
	}
	ammo, equipped := inventory.Items[model.SlotSecondaryWeapon]
	if !equipped || ammo.StackSize < 1 || (weapon.IsBow() && !ammo.IsArrow()) || (weapon.IsCrossbow() && !ammo.IsBolt()) {
		return h.sendSkillCastError(session, skillCastErrorInvalidAmmo)
	}

	remaining := ammo.StackSize - 1
	if h.rangedAmmo != nil {
		persisted, err := h.rangedAmmo.ConsumeEquippedRangedAmmo(ctx, int64(player.ID), int64(ammo.ID))
		if err != nil {
			if !errors.Is(err, model.ErrRangedAmmoUnavailable) {
				logrus.WithError(err).Warn("consume equipped ranged ammunition")
			}
			return h.sendSkillCastError(session, skillCastErrorInvalidAmmo)
		}
		remaining = int(persisted)
	}
	if remaining == 0 {
		delete(inventory.Items, model.SlotSecondaryWeapon)
	} else {
		ammo.StackSize = remaining
		inventory.Items[model.SlotSecondaryWeapon] = ammo
	}
	player.CommitInventory(inventory)

	var mutation network.Packet
	if remaining == 0 {
		mutation = vsro188.BuildItemOperationServerDeleteSuccess(model.SlotSecondaryWeapon)
	} else {
		mutation = vsro188.BuildInventoryItemQuantityUpdate(model.SlotSecondaryWeapon, uint16(remaining))
	}
	if err := h.sendPlayerPacket(session, mutation); err != nil {
		return err
	}
	return h.sendPlayerPacket(session, vsro188.BuildInventoryAmmoUpdate(uint16(remaining)))
}

func playerCanBaseAttack(player *model.Player) bool {
	if player == nil {
		return false
	}
	weapon, equipped := player.GetInventory().Items[model.SlotPrimaryWeapon]
	return !equipped || !weapon.GetTypeInfo().IsWeapon() || playerHasRangedAmmo(player, weapon)
}

func playerMissingSkillAmmo(player *model.Player, skill model.RefSkill) bool {
	if player == nil {
		return false
	}
	weapon, equipped := player.GetInventory().Items[model.SlotPrimaryWeapon]
	if !equipped || (!weapon.IsBow() && !weapon.IsCrossbow()) || playerHasRangedAmmo(player, weapon) {
		return false
	}
	weaponType := byte(weapon.GetTypeInfo().TypeID4)
	restricted := false
	for _, required := range [...]byte{skill.RequiredWeapon1, skill.RequiredWeapon2} {
		if required == 0 || required == 255 {
			continue
		}
		restricted = true
		if required == weaponType {
			return true
		}
	}
	return !restricted
}

func playerHasRequiredSkillItem(player *model.Player, skill model.RefSkill) bool {
	if len(skill.RequiredItems) == 0 {
		return true
	}
	if player == nil {
		return false
	}
	inventory := player.GetInventory()
	for _, slot := range []byte{model.SlotPrimaryWeapon, model.SlotSecondaryWeapon} {
		item, equipped := inventory.Items[slot]
		if !equipped {
			continue
		}
		for _, required := range skill.RequiredItems {
			typeInfo := item.GetTypeInfo()
			if typeInfo.TypeID3 == required.TypeID3 && typeInfo.TypeID4 == required.TypeID4 {
				return true
			}
		}
	}
	avatar := player.GetAvatarInventory()
	if devil, equipped := avatar.Items[4]; equipped && devil.IsActiveDevilSpirit(time.Now()) {
		for _, required := range skill.RequiredItems {
			typeInfo := devil.GetTypeInfo()
			if typeInfo.TypeID3 == required.TypeID3 && typeInfo.TypeID4 == required.TypeID4 {
				return true
			}
		}
	}
	return false
}

func playerMeetsSkillCastRequirements(player *model.Player, skill model.RefSkill) bool {
	if player == nil || player.BaseStats.Str < skill.RequiredStr || player.BaseStats.Int < skill.RequiredInt {
		return false
	}
	return playerCanCastWithWeapon(player, skill)
}

func skillCastRejection(player *model.Player, skill model.RefSkill) (byte, bool) {
	if player == nil || skill.ConsumeMP < 0 || player.BaseStats.Str < skill.RequiredStr || player.BaseStats.Int < skill.RequiredInt {
		return skillCastErrorInvalidSkill, true
	}
	if !playerCanCastWithWeapon(player, skill) {
		if playerMissingSkillAmmo(player, skill) {
			return skillCastErrorInvalidAmmo, true
		}
		return skillCastErrorInvalidEquipment, true
	}
	if !playerHasSkillMP(player, skill) {
		return skillCastErrorInvalidMP, true
	}
	return 0, false
}

// spendSkillResources applies the complete REF_SKILL consume tuple for a
// normal attack cast. HP is debited through the same compare-and-debit
// repository contract as MP and is never allowed to kill the caster.
func (h *CombatHandler) spendSkillResources(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) error {
	if h == nil || player == nil {
		if h == nil {
			return ErrCombatTargetInvalid
		}
		return h.sendSkillCastError(session, skillCastErrorInvalidSkill)
	}
	hp, mp := player.CurrentVitals()
	originalHP, originalMP := hp, mp
	maxHP, maxMP := player.MaxVitals()
	hpCost, mpCost := skill.ResourceCosts(maxHP, maxMP)
	if hpCost == 0 && mpCost == 0 {
		return nil
	}
	if h.vitals == nil || hp <= hpCost || mp < mpCost {
		return h.sendSkillCastError(session, skillCastErrorInvalidMP)
	}
	rollback := func(cause error) error {
		currentHP, currentMP := player.CurrentVitals()
		var rollbackErr error
		if currentHP != originalHP || currentMP != originalMP {
			if !player.CommitHealing(currentHP, currentMP, originalHP, originalMP) {
				rollbackErr = ErrCombatTargetInvalid
			}
		}
		if player.ID > 0 {
			if err := h.vitals.SaveVitals(ctx, player.ID, originalHP, originalMP); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		return errors.Join(cause, rollbackErr)
	}
	if hpCost > 0 {
		spent, err := h.vitals.SpendHP(ctx, player.ID, hp, hpCost)
		if err != nil {
			return rollback(err)
		}
		if !spent || !player.CommitHP(hp, hpCost) {
			return rollback(h.sendSkillCastError(session, skillCastErrorInvalidMP))
		}
		hp -= hpCost
	}
	if mpCost > 0 {
		spent, err := h.vitals.SpendMP(ctx, player.ID, mp, mpCost)
		if err != nil {
			return rollback(err)
		}
		if !spent || !player.CommitMP(mp, mpCost) {
			return rollback(h.sendSkillCastError(session, skillCastErrorInvalidMP))
		}
		mp -= mpCost
	}
	return nil
}

func playerHasSkillResourceCost(player *model.Player, skill model.RefSkill) bool {
	if player == nil {
		return false
	}
	maxHP, maxMP := player.MaxVitals()
	hp, mp := skill.ResourceCosts(maxHP, maxMP)
	return hp > 0 || mp > 0
}

func (h *CombatHandler) executeSelfBuff(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) error {
	return h.executeBuff(ctx, session, player, player, skill)
}

func (h *CombatHandler) executeBuff(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionBuff {
		return h.sendActionState(session, false)
	}
	if caster != target && skill.ActionRange > 0 && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(caster, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	key := skillCooldownKey{playerUID: caster.GetUniqueID(), skillID: skill.ID}
	if !h.skillAvailable(key, now) {
		return h.sendActionState(session, false)
	}
	if err := h.spendSkillResources(ctx, session, caster, skill); err != nil {
		return err
	}
	h.registerSkillCooldownForPlayer(caster, key, skill, now)
	resourceCost := playerHasSkillResourceCost(caster, skill)
	if caster != target {
		h.facePlayerTowardTarget(caster, target)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, caster.GetUniqueID(), target.GetUniqueID(), castUID)
	if err := h.emitAttack(caster, &begin); err != nil {
		return err
	}
	finish := func() {
		if err := h.applyBuffImpact(session, caster, target, skill, castUID, h.combatNow()); err != nil {
			logrus.WithError(err).Warn("delayed buff impact failed")
		}
		if resourceCost {
			hp, mp := caster.CurrentVitals()
			_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(caster.GetUniqueID(), hp, mp))
		}
	}
	delay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(caster, skill))
	if delay > 0 && h.schedule != nil {
		delayedToken := h.beginDelayedCast(caster.GetUniqueID(), func() {
			end := model.BuildBuffCastEnd(target.GetUniqueID(), castUID)
			_ = h.emitAttack(caster, &end)
			_ = h.sendActionState(session, false)
			if resourceCost {
				hp, mp := caster.CurrentVitals()
				_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(caster.GetUniqueID(), hp, mp))
			}
		})
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(caster.GetUniqueID(), delayedToken) {
				return
			}
			finish()
		})
		return nil
	}
	finish()
	return nil
}

func (h *CombatHandler) applyBuffImpact(session *server.Session, caster, target *model.Player, skill model.RefSkill, castUID uint32, now time.Time) error {
	if h == nil || caster == nil || target == nil {
		return ErrCombatTargetInvalid
	}
	if target.GetLifeState() != model.Alive {
		end := model.BuildBuffCastEnd(target.GetUniqueID(), castUID)
		_ = h.emitAttack(caster, &end)
		return h.sendActionState(session, false)
	}
	duration := time.Duration(skill.BuffDuration) * time.Millisecond
	token := model.NextActionUID()
	admissionReplacements, admitted := h.activeBuffAdmission(target, skill)
	if !admitted {
		end := model.BuildBuffCastEnd(target.GetUniqueID(), castUID)
		_ = h.emitAttack(caster, &end)
		return h.sendActionState(session, false)
	}
	if skill.IsWeaponImbue() {
		if old, active := h.activeWeaponImbue(target); active && old.ID != skill.ID {
			if oldToken, removed := target.RemoveActiveBuffBySkill(old.ID); removed {
				remove := model.BuildBuffRemove(oldToken)
				if err := h.emitAttack(caster, &remove); err != nil {
					return err
				}
			}
		}
	}
	for _, oldToken := range admissionReplacements {
		h.removeLinkedBuffs(caster, oldToken)
		h.unregisterLinkedBuff(oldToken)
		target.RemoveActiveBuff(oldToken)
	}
	replacedToken, committed := target.CommitActiveBuffAt(skill.ID, token, caster.GetUniqueID(), duration, skill.BuffModifiers, now)
	if !committed {
		end := model.BuildBuffCastEnd(target.GetUniqueID(), castUID)
		_ = h.emitAttack(caster, &end)
		return h.sendActionState(session, false)
	}
	if skill.RecoveryStatusMask != 0 {
		target.RegisterActiveRecoveryStatus(token, model.RecoveryStatusDescriptor{
			SkillID: skill.ID, Mask: skill.RecoveryStatusMask,
			Value: skill.RecoveryStatusValue, Grade: skill.RecoveryStatusGrade,
		})
	}
	h.syncHiddenBodyState(target)
	if skill.HideLevel > 0 || skill.DetectionLevel > 0 || skill.DamageTypeFamily == 6 {
		h.reconcilePlayerVisibility(target)
	}
	end := model.BuildBuffCastEnd(target.GetUniqueID(), castUID)
	if err := h.emitAttack(caster, &end); err != nil {
		return err
	}
	if replacedToken != 0 {
		h.removeLinkedBuffs(caster, replacedToken)
		h.unregisterLinkedBuff(replacedToken)
		remove := model.BuildBuffRemove(replacedToken)
		if err := h.emitAttack(caster, &remove); err != nil {
			return err
		}
	}
	for _, oldToken := range admissionReplacements {
		remove := model.BuildBuffRemove(oldToken)
		if err := h.emitAttack(caster, &remove); err != nil {
			return err
		}
	}
	add := model.BuildBuffAdd(target.GetUniqueID(), skill.ID, token)
	if err := h.emitAttack(caster, &add); err != nil {
		return err
	}
	if err := h.sendActionState(session, false); err != nil {
		return err
	}
	if err := h.sendPlayerPacket(target.Session, target.BuildStatsUpdatePacket()); err != nil {
		return err
	}
	if h.schedule != nil {
		if duration > 0 {
			h.schedule(duration, func() { h.expireBuff(target, token) })
		}
		// The binary's tick dispatcher gives dura precedence: an effect with an
		// authored hard lifetime never enters the recurring onff debit branch.
		if duration == 0 && skill.UpkeepIntervalMS > 0 {
			h.scheduleBuffUpkeep(target, skill, token)
		}
		if skill.RecoveryHP > 0 || skill.RecoveryMP > 0 {
			h.scheduleRecoveryTick(target, skill, token, 5*time.Second)
		}
	}
	if err := h.propagateReferenceBuff(caster, target, skill, token, duration, now); err != nil {
		return err
	}
	return nil
}

func (h *CombatHandler) propagateReferenceBuff(caster, primary *model.Player, skill model.RefSkill, parentToken uint32, duration time.Duration, nowArgs ...time.Time) error {
	// `dtt` detect skills reuse discriminator 2 for their detection-area actor;
	// it is not the parasite rule that clones the active buff onto candidates.
	if h == nil || caster == nil || skill.DetectionLevel > 0 || !skill.HasReferenceDescriptor(2) || skill.ReferenceRadius <= 0 || caster.GetKnownObjectList() == nil {
		return nil
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	if len(nowArgs) > 0 && !nowArgs[0].IsZero() {
		now = nowArgs[0]
	}
	type propagationCandidate struct {
		uid    uint32
		player *model.Player
	}
	candidates := make([]propagationCandidate, 0)
	position := caster.GetNavmeshPosition()
	for uid, object := range caster.GetKnownObjectList().GetKnownObjects() {
		candidate, ok := object.(*model.Player)
		if !ok || candidate == nil || candidate == caster || candidate == primary || candidate.GetLifeState() != model.Alive {
			continue
		}
		candidatePosition := candidate.GetNavmeshPosition()
		if candidatePosition.Region.ID != position.Region.ID || navmeshv2.HorizontalDistance(position, candidatePosition) > float32(skill.ReferenceRadius) {
			continue
		}
		candidates = append(candidates, propagationCandidate{uid: uid, player: candidate})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].uid < candidates[j].uid })
	for _, entry := range candidates {
		replacements, admitted := h.activeBuffAdmission(entry.player, skill)
		if !admitted {
			continue
		}
		for _, oldToken := range replacements {
			h.removeLinkedBuffs(caster, oldToken)
			h.unregisterLinkedBuff(oldToken)
			entry.player.RemoveActiveBuff(oldToken)
			remove := model.BuildBuffRemove(oldToken)
			if err := h.emitAttack(caster, &remove); err != nil {
				return err
			}
		}
		token := model.NextActionUID()
		replacedToken, committed := entry.player.CommitLinkedActiveBuffAt(skill.ID, token, caster.GetUniqueID(), parentToken, duration, skill.BuffModifiers, now)
		if !committed {
			continue
		}
		if skill.RecoveryStatusMask != 0 {
			entry.player.RegisterActiveRecoveryStatus(token, model.RecoveryStatusDescriptor{
				SkillID: skill.ID, Mask: skill.RecoveryStatusMask,
				Value: skill.RecoveryStatusValue, Grade: skill.RecoveryStatusGrade,
			})
		}
		if replacedToken != 0 {
			h.removeLinkedBuffs(caster, replacedToken)
			h.unregisterLinkedBuff(replacedToken)
			remove := model.BuildBuffRemove(replacedToken)
			if err := h.emitAttack(caster, &remove); err != nil {
				return err
			}
		}
		h.registerLinkedBuff(parentToken, token, entry.player)
		add := model.BuildBuffAdd(entry.uid, skill.ID, token)
		if err := h.emitAttack(caster, &add); err != nil {
			return err
		}
		if err := h.sendPlayerPacket(entry.player.Session, entry.player.BuildStatsUpdatePacket()); err != nil {
			return err
		}
		if h.schedule != nil {
			if duration > 0 {
				h.schedule(duration, func() { h.expireBuff(entry.player, token) })
			} else if skill.UpkeepIntervalMS > 0 {
				h.scheduleBuffUpkeep(entry.player, skill, token)
			}
			if skill.RecoveryHP > 0 || skill.RecoveryMP > 0 {
				h.scheduleRecoveryTick(entry.player, skill, token, 5*time.Second)
			}
		}
	}
	return nil
}

// activeBuffAdmission implements the binary's reference-driven class-3
// active-effect precedence. Match mode 1 uses the ordinary overlap indexes;
// other effects use dttp family/rank and then same-group skill level.
func (h *CombatHandler) activeBuffAdmission(target *model.Player, incoming model.RefSkill) ([]uint32, bool) {
	if target == nil {
		return nil, false
	}
	existingBuffs := target.ActiveBuffSnapshot()
	if incoming.MatchMode == 1 {
		for _, buff := range existingBuffs {
			existing, exists := h.referenceSkill(buff.SkillID)
			if !exists {
				continue
			}
			if incoming.SecondaryOverlap != 0 && (incoming.SecondaryOverlap == existing.SecondaryOverlap || incoming.SecondaryOverlap == existing.ActionOverlap) {
				return nil, false
			}
			if incoming.ActionOverlap != 0 && (incoming.ActionOverlap == existing.ActionOverlap || incoming.ActionOverlap == existing.SecondaryOverlap) {
				return nil, false
			}
		}
		return nil, true
	}

	replacements := make([]uint32, 0, 1)
	for _, buff := range existingBuffs {
		existing, exists := h.referenceSkill(buff.SkillID)
		if !exists || existing.ID == incoming.ID {
			continue
		}
		if incoming.DamageTypeFamily != 0 && incoming.DamageTypeFamily == existing.DamageTypeFamily {
			if incoming.DamageTypePrecedence < existing.DamageTypePrecedence {
				return nil, false
			}
			replacements = append(replacements, buff.Token)
			continue
		}
		if incoming.GroupID == 0 || incoming.GroupID != existing.GroupID {
			continue
		}
		if incoming.BasicLevel < existing.BasicLevel {
			return nil, false
		}
		// lks2 suppresses ordinary same-group replacement. A refr discriminator-2
		// actor first attempts linked-owner redirection in the binary, but falls
		// through to this ordinary replacement when no live link resolves.
		if incoming.LinkedSameGroup {
			continue
		}
		replacements = append(replacements, buff.Token)
	}
	return replacements, true
}

func (h *CombatHandler) scheduleBuffUpkeep(player *model.Player, skill model.RefSkill, token uint32) {
	if h == nil || h.schedule == nil || player == nil || token == 0 || skill.UpkeepIntervalMS <= 0 {
		return
	}
	delay := time.Duration(skill.UpkeepIntervalMS) * time.Millisecond
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		active := false
		for _, buff := range player.ActiveBuffSnapshot() {
			if buff.Token == token {
				active = true
				break
			}
		}
		if !active {
			return
		}
		_, mp := player.CurrentVitals()
		spent := skill.UpkeepMPCost <= 0
		if !spent && h.vitals != nil && mp >= skill.UpkeepMPCost {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			var err error
			spent, err = h.vitals.SpendMP(ctx, player.ID, mp, skill.UpkeepMPCost)
			cancel()
			spent = err == nil && spent && player.CommitMP(mp, skill.UpkeepMPCost)
		}
		if !spent {
			h.removeBuff(player, token)
			return
		}
		hp, currentMP := player.CurrentVitals()
		_ = h.sendPlayerPacket(player.Session, model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, currentMP))
		h.scheduleBuffUpkeep(player, skill, token)
	})
}

func (h *CombatHandler) removeBuff(player *model.Player, token uint32) {
	h.removeLinkedBuffs(player, token)
	h.unregisterLinkedBuff(token)
	affectsVisibility := h.activeBuffAffectsVisibility(player, token)
	if !player.RemoveActiveBuff(token) {
		return
	}
	h.syncHiddenBodyState(player)
	if affectsVisibility {
		h.reconcilePlayerVisibility(player)
	}
	remove := model.BuildBuffRemove(token)
	_ = h.emitAttack(player, &remove)
	_ = h.sendPlayerPacket(player.Session, player.BuildStatsUpdatePacket())
}

func (h *CombatHandler) registerLinkedBuff(parentToken, childToken uint32, owner *model.Player) {
	if h == nil || parentToken == 0 || childToken == 0 || owner == nil {
		return
	}
	if h.linkedBuffs == nil {
		h.linkedBuffs = make(map[uint32]map[uint32]*model.Player)
	}
	if h.linkedParents == nil {
		h.linkedParents = make(map[uint32]uint32)
	}
	if h.linkedBuffs[parentToken] == nil {
		h.linkedBuffs[parentToken] = make(map[uint32]*model.Player)
	}
	h.linkedBuffs[parentToken][childToken] = owner
	h.linkedParents[childToken] = parentToken
}

func (h *CombatHandler) unregisterLinkedBuff(token uint32) {
	if h == nil || token == 0 || h.linkedParents == nil {
		return
	}
	parentToken, exists := h.linkedParents[token]
	if !exists {
		return
	}
	delete(h.linkedParents, token)
	delete(h.linkedBuffs[parentToken], token)
	if len(h.linkedBuffs[parentToken]) == 0 {
		delete(h.linkedBuffs, parentToken)
	}
}

func (h *CombatHandler) removeLinkedBuffs(source *model.Player, parentToken uint32) {
	h.removeLinkedBuffsSeen(source, parentToken, make(map[uint32]bool))
}

// removeLinkedBuffsSeen tears down the owner graph depth-first. Native linked
// actors are allowed to own further linked actors; the visited set protects
// malformed/runtime-provided graphs from recursive cycles.
func (h *CombatHandler) removeLinkedBuffsSeen(source *model.Player, parentToken uint32, visited map[uint32]bool) {
	if h == nil || parentToken == 0 || h.linkedBuffs == nil {
		return
	}
	if visited == nil {
		visited = make(map[uint32]bool)
	}
	if visited[parentToken] {
		return
	}
	visited[parentToken] = true
	children := h.linkedBuffs[parentToken]
	delete(h.linkedBuffs, parentToken)
	for childToken, owner := range children {
		delete(h.linkedParents, childToken)
		h.removeLinkedBuffsSeen(owner, childToken, visited)
		affectsVisibility := h.activeBuffAffectsVisibility(owner, childToken)
		if owner == nil || !owner.RemoveActiveBuff(childToken) {
			continue
		}
		h.syncHiddenBodyState(owner)
		if affectsVisibility {
			h.reconcilePlayerVisibility(owner)
		}
		removed := model.BuildBuffRemove(childToken)
		_ = h.emitAttack(source, &removed)
		_ = h.sendPlayerPacket(owner.Session, owner.BuildStatsUpdatePacket())
	}
}

func (h *CombatHandler) syncHiddenBodyState(player *model.Player) {
	if h == nil || player == nil || h.referenceData == nil {
		return
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	desired, _, hidden := player.HiddenBodyState(h.referenceData, now)
	current := player.GetBodyState()
	if !hidden {
		if current != model.Stealth && current != model.Invisible {
			return
		}
		desired = model.Normal
	}
	if current == desired {
		return
	}
	player.SetBodyState(desired)
	packet := model.BuildBodyStateUpdatePacket(player.GetUniqueID(), desired)
	_ = h.emitAttack(player, &packet)
	h.reconcilePlayerVisibility(player)
}

func (h *CombatHandler) reconcilePlayerVisibility(player *model.Player) {
	if h == nil || player == nil {
		return
	}
	if visibility, ok := h.world.(interface {
		ReconcileCharacterVisibility(model.ICharacter) []service.VisibilityDelta
	}); ok {
		visibility.ReconcileCharacterVisibility(player)
	}
}

func (h *CombatHandler) scheduleRecoveryTick(player *model.Player, skill model.RefSkill, token uint32, delay time.Duration) {
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		active := false
		for _, buff := range player.ActiveBuffSnapshot() {
			if buff.Token == token {
				active = true
				break
			}
		}
		if !active {
			return
		}
		hp, mp, healedHP, healedMP, ok := player.PreviewHealing(skill.RecoveryHP, skill.RecoveryMP)
		if ok && (healedHP != hp || healedMP != mp) && h.vitals != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := h.vitals.SaveVitals(ctx, player.ID, healedHP, healedMP)
			cancel()
			if err == nil && player.CommitHealing(hp, mp, healedHP, healedMP) {
				_ = h.sendPlayerPacket(player.Session, model.BuildVitalsUpdatePacket(player.GetUniqueID(), healedHP, healedMP))
			}
		}
		h.scheduleRecoveryTick(player, skill, token, delay)
	})
}

func (h *CombatHandler) expireBuff(player *model.Player, token uint32) {
	if h == nil || player == nil || token == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeBuff(player, token)
}

func (h *CombatHandler) activeBuffAffectsVisibility(player *model.Player, token uint32) bool {
	if h == nil || player == nil || token == 0 || h.referenceData == nil {
		return false
	}
	for _, buff := range player.ActiveBuffSnapshot() {
		if buff.Token != token {
			continue
		}
		skill, exists := h.referenceSkill(buff.SkillID)
		return exists && (skill.HideLevel > 0 || skill.DetectionLevel > 0 || skill.DamageTypeFamily == 6)
	}
	return false
}

func (h *CombatHandler) skillAttackReach(player *model.Player, target *model.NPC, skill model.RefSkill) float32 {
	reach := h.baseAttackReach(player, target)
	if skill.ActionRange > 0 {
		// ACTION_RANGE is an authored addition to the actor's normal SkillRange,
		// not an absolute replacement for it. In particular, many bow attacks
		// carry only a small positive addition and still depend on the equipped
		// bow's ITEMDATA range. Treating that value as absolute makes the player
		// pursue a mob into melee range before casting.
		return reach + float32(skill.ActionRange)
	}
	if skill.TransferRange > 0 {
		// Several radial/nuker series omit ACTION_RANGE and carry their authored
		// addition only in the transfer block.
		return reach + float32(skill.TransferRange)
	}
	return reach
}

func (h *CombatHandler) baseAttackReach(player *model.Player, target *model.NPC) float32 {
	reach := player.BaseAttackRange()
	if h == nil || h.referenceData == nil {
		return reach
	}
	if character, exists := h.referenceData.Character(player.GetRefObjectID()); exists && character.BCRadius > 0 {
		reach += float32(character.BCRadius)
	}
	if character, exists := h.referenceData.Character(target.GetRefObjectID()); exists && character.BCRadius > 0 {
		reach += float32(character.BCRadius)
	}
	return reach
}

func (h *CombatHandler) executeAttack(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC) error {
	if !playerCanBaseAttack(player) {
		return h.sendSkillCastError(session, skillCastErrorInvalidAmmo)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	playerUID := player.GetUniqueID()
	if next := h.nextAttack[playerUID]; !next.IsZero() && now.Before(next) {
		return h.sendActionState(session, false)
	}
	if err := h.consumeRangedAmmo(ctx, session, player); err != nil {
		return err
	}
	h.engageGrowthPet(player, target)
	targetUID := target.GetUniqueID()
	damage := h.playerVarianceDamage(player, target, false, 0, 0)
	imbue, hasImbue := h.activeWeaponImbue(player)
	if hasImbue {
		damage += h.weaponImbueDamage(player, target, imbue)
	}
	if damage < 1 {
		damage = 1
	}
	cooldown := h.baseAttackCooldown(player)
	if cooldown > 0 {
		if h.nextAttack == nil {
			h.nextAttack = make(map[uint32]time.Time)
		}
		h.nextAttack[playerUID] = now.Add(cooldown)
	}
	result := h.resolvePlayerDamage(player, target, damage, false, false, now)
	wireDamage, killed := result.wireDamage, result.killed
	landed := result.appliedDamage > 0
	effectMask := uint32(0)
	if landed && hasImbue {
		effectMask |= h.applyImbueStatuses(session, player, target, imbue, now)
	}
	h.facePlayerTowardTarget(player, target)
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	records := []model.CombatDamageRecord{result.combatRecord(0)}
	if player.BaseAttackHitCount() == 2 {
		first := uint32(wireDamage) / 2
		firstRecord, secondRecord := result.combatRecord(0), result.combatRecord(effectMask)
		firstRecord.Damage, secondRecord.Damage = first, uint32(wireDamage)-first
		firstRecord.AttackState &^= model.DamageAttackStateDead
		records = []model.CombatDamageRecord{firstRecord, secondRecord}
	} else {
		records[0].EffectMask = effectMask
	}
	begin := model.BuildAttackBeginWithSkillDamageRecords(playerBaseAttackSkillID(player), player.GetUniqueID(), targetUID, castUID, records)
	keepActive := !killed && h.schedule != nil && h.baseAttackCooldown(player) > 0
	var emitErr error
	if keepActive {
		emitErr = h.emitAttack(player, &begin)
	} else if killed && h.actionRecoveryDuration(player, h.baseAttackSkill(player)) > 0 && h.schedule != nil {
		emitErr = h.emitAttack(player, &begin)
		h.finishActionAfter(session, h.actionRecoveryDuration(player, h.baseAttackSkill(player)))
	} else {
		emitErr = h.emitAttackAndEnd(session, player, &begin)
	}
	if emitErr != nil {
		return emitErr
	}
	if landed && hasImbue {
		if err := h.applyImbueTransfers(ctx, session, player, target, imbue, now); err != nil {
			return err
		}
	}
	if !killed {
		return nil
	}
	return h.awardPlayerKill(ctx, session, player, target, targetUID)
}

func (h *CombatHandler) facePlayerTowardTarget(player *model.Player, target model.ICharacter) {
	if h == nil || player == nil || target == nil {
		return
	}
	position := player.GetNavmeshPosition()
	targetPosition := target.GetNavmeshPosition()
	if position.Offset == nil || targetPosition.Offset == nil {
		return
	}
	from := position.GetGlobalCoordinates()
	to := targetPosition.GetGlobalCoordinates()
	dx, dz := to.X-from.X, to.Z-from.Z
	if dx == 0 && dz == 0 {
		return
	}
	position.Heading = model.RadiansToHeading(math32.Atan2(dz, dx))
	player.SetNavmeshPosition(position)
	turn := model.BuildTurnUpdatePacket(player)
	h.publishCombatPacket(player, &turn)
}

func (h *CombatHandler) executeSkillAttack(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill) error {
	if reason, rejected := skillCastRejection(player, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	playerUID := player.GetUniqueID()
	key := skillCooldownKey{playerUID: playerUID, skillID: skill.ID}
	if !h.skillAvailable(key, now) {
		return h.sendActionState(session, false)
	}
	if err := h.consumeRangedAmmo(ctx, session, player); err != nil {
		return err
	}
	if err := h.spendSkillResources(ctx, session, player, skill); err != nil {
		return err
	}
	if playerHasSkillResourceCost(player, skill) {
		defer func() {
			hp, mp := player.CurrentVitals()
			_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, mp))
		}()
	}
	h.registerSkillCooldownForPlayer(player, key, skill, now)
	h.engageGrowthPet(player, target)
	h.facePlayerTowardTarget(player, target)
	impactDelay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(player, skill))
	if impactDelay > 0 && h.schedule != nil {
		if err := h.sendActionState(session, true); err != nil {
			return err
		}
		castUID := model.NextActionUID()
		begin := model.BuildDelayedAttackBegin(skill.ID, playerUID, target.GetUniqueID(), castUID)
		if err := h.emitAttack(player, &begin); err != nil {
			return err
		}
		delayedToken := h.beginDelayedCast(playerUID, func() {
			end := model.BuildAttackEndDamageRecords(target.GetUniqueID(), castUID, nil)
			_ = h.emitAttack(player, &end)
			_ = h.sendActionState(session, false)
		})
		h.schedule(impactDelay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(playerUID, delayedToken) {
				return
			}
			h.resolveDelayedSkillAttack(session, player, target, skill, castUID)
		})
		return nil
	}
	damageHits := make([]uint32, 0, 3)
	damageRecords := make([]model.CombatDamageRecord, 0, 3)
	appliedDamage := 0
	killed := false
	displaced := false
	imbue, hasImbue := h.activeWeaponImbue(player)
	for _, strike := range h.skillStrikes(skill) {
		damage := h.skillStrikeDamage(player, target, strike)
		if hasImbue {
			damage += h.weaponImbueDamage(player, target, imbue)
		}
		result := h.resolvePlayerDamageWithAggro(player, target, damage, strike.IsMagicalAttack(), strike.AbsoluteDamage > 0, now, strike.AggroIncrease, strike.AggroIncreasePercent, strike.AttackPercent)
		wireDamage, strikeApplied, strikeKilled := result.wireDamage, result.appliedDamage, result.killed
		damageHits = append(damageHits, uint32(wireDamage))
		record := result.combatRecord(0)
		if strikeApplied > 0 && !strikeKilled && !displaced {
			displaced = h.applySkillDisplacement(player, target, skill, &record)
		}
		damageRecords = append(damageRecords, record)
		appliedDamage += strikeApplied
		if strikeKilled {
			killed = true
			break
		}
	}
	landed := false
	for _, damage := range damageHits {
		landed = landed || damage > 0
	}
	effectMask := uint32(0)
	if landed && hasImbue {
		effectMask |= h.applyImbueStatuses(session, player, target, imbue, now)
	}
	if landed {
		effectMask |= h.applySkillStatuses(session, player, target, skill, now)
	}
	if len(damageRecords) != 0 {
		damageRecords[len(damageRecords)-1].EffectMask = effectMask
	}
	if err := h.restoreDamageVitals(ctx, session, player, skill, appliedDamage); err != nil {
		return err
	}
	if len(damageHits) == 0 {
		return h.sendActionState(session, false)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildAttackBeginWithSkillDamageRecords(skill.ID, playerUID, target.GetUniqueID(), castUID, damageRecords)
	if err := h.emitAttackAndEnd(session, player, &begin); err != nil {
		return err
	}
	if err := h.applySkillTransfers(ctx, session, player, target, skill, now); err != nil {
		return err
	}
	if hasImbue {
		if err := h.applyImbueTransfers(ctx, session, player, target, imbue, now); err != nil {
			return err
		}
	}
	if !killed {
		return nil
	}
	return h.awardPlayerKill(ctx, session, player, target, target.GetUniqueID())
}

// executePlayerSkillAttack is the player-target counterpart of the ordinary
// NPC skill path. The reference catalog marks these skills with
// TargetGroup_Enemy_P; they use the same cast/cooldown/MP lifecycle and hit
// record framing, but resolve defense, parry, block, and vitals from the
// target's live Player state rather than CHAR_REF_DATA.
func (h *CombatHandler) executePlayerSkillAttack(ctx context.Context, session *server.Session, player, target *model.Player, skill model.RefSkill) error {
	if player == nil || target == nil || !skill.TargetEnemyPVP || !h.pvpTargetAdmitted(player, target, skill) {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(player, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	now := h.combatNow()
	key := skillCooldownKey{playerUID: player.GetUniqueID(), skillID: skill.ID}
	if !h.skillAvailable(key, now) {
		return h.sendActionState(session, false)
	}
	if skill.ActionRange > 0 && navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange) {
		return h.sendActionState(session, false)
	}
	if err := h.consumeRangedAmmo(ctx, session, player); err != nil {
		return err
	}
	if err := h.spendSkillResources(ctx, session, player, skill); err != nil {
		return err
	}
	if playerHasSkillResourceCost(player, skill) {
		defer func() {
			hp, mp := player.CurrentVitals()
			_ = h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, mp))
		}()
	}
	h.registerSkillCooldownForPlayer(player, key, skill, now)
	h.facePlayerTowardTarget(player, target)
	impactDelay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(player, skill))
	if impactDelay > 0 && h.schedule != nil {
		if err := h.sendActionState(session, true); err != nil {
			return err
		}
		castUID := model.NextActionUID()
		begin := model.BuildDelayedAttackBegin(skill.ID, player.GetUniqueID(), target.GetUniqueID(), castUID)
		if err := h.emitAttack(player, &begin); err != nil {
			return err
		}
		delayedToken := h.beginDelayedCast(player.GetUniqueID(), func() {
			end := model.BuildAttackEndDamageRecords(target.GetUniqueID(), castUID, nil)
			_ = h.emitAttack(player, &end)
			_ = h.sendActionState(session, false)
		})
		h.schedule(impactDelay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(player.GetUniqueID(), delayedToken) {
				return
			}
			_ = h.resolveDelayedPlayerSkillAttack(ctx, session, player, target, skill, castUID)
		})
		return nil
	}
	return h.resolvePlayerSkillAttackImpact(ctx, session, player, target, skill, now, model.NextActionUID(), false)
}

func (h *CombatHandler) resolveDelayedPlayerSkillAttack(ctx context.Context, session *server.Session, player, target *model.Player, skill model.RefSkill, castUID uint32) error {
	if target == nil || target.GetLifeState() != model.Alive {
		if h != nil && player != nil && target != nil {
			end := model.BuildAttackEndDamageRecords(target.GetUniqueID(), castUID, nil)
			_ = h.emitAttack(player, &end)
		}
		return h.sendActionState(session, false)
	}
	return h.resolvePlayerSkillAttackImpact(ctx, session, player, target, skill, h.combatNow(), castUID, true)
}

func (h *CombatHandler) resolvePlayerSkillAttackImpact(ctx context.Context, session *server.Session, player, target *model.Player, skill model.RefSkill, now time.Time, castUID uint32, delayed bool) error {
	attackState := model.DamageAttackStateNormal
	damageState := model.DamageStateNormal
	damage := 0
	if skill.AbsoluteDamage <= 0 && h.playerAttackBlockedAgainstPlayer(target) {
		attackState = model.DamageAttackStateBlock
	} else {
		for _, strike := range h.skillStrikes(skill) {
			hit := h.playerSkillDamageAgainstPlayerAt(player, target, strike, now)
			if strike.AbsoluteDamage <= 0 && h.playerAttackCritical(player) {
				hit *= 2
				damageState = model.DamageStateCritical
			}
			hit = h.mitigatePlayerSkillDamage(target, hit, strike.IsMagicalAttack(), strike.AbsoluteDamage > 0, now, strike.AttackPercent)
			hit += hit * player.ActiveCombatDamagePercent() / 100
			damage += hit
		}
	}
	damage, linkedTarget, linkedApplication, linkedBuff := h.interceptPlayerLinkedDamage(player, target, damage, skill.IsMagicalAttack())
	application := target.ApplyCombatDamage(damage, skill.IsMagicalAttack())
	if application.AppliedDamage > 0 && h.vitals != nil && target.ID > 0 {
		hp, mp := target.CurrentVitals()
		if err := h.vitals.SaveVitals(ctx, target.ID, hp, mp); err != nil {
			return err
		}
	}
	if err := h.restoreDamageVitals(ctx, session, player, skill, application.AppliedDamage); err != nil {
		return err
	}
	if application.BarrierDamage > 0 && application.AppliedDamage == 0 {
		attackState = model.DamageResultBarrier
	}
	if application.Killed {
		attackState |= model.DamageAttackStateDead
	}
	effectMask := uint32(0)
	if application.AppliedDamage > 0 {
		effectMask = h.applyPlayerSkillStatuses(target, player, skill, now)
	}
	record := model.CombatDamageRecord{AttackState: attackState, DamageState: damageState, Damage: uint32(min(application.WireDamage, maximumWireDamage)), EffectMask: effectMask, BarrierRemaining: uint16(max(0, application.BarrierRemaining)), BarrierMaximum: uint16(max(0, application.BarrierMaximum))}
	linkedRecord := model.CombatDamageRecord{}
	if linkedTarget != nil {
		linkedRecord = model.CombatDamageRecord{DamageState: damageState, Damage: uint32(min(linkedApplication.WireDamage, maximumWireDamage))}
		if linkedApplication.Killed {
			linkedRecord.AttackState |= model.DamageAttackStateDead
		}
		if linkedApplication.BarrierDamage > 0 && linkedApplication.AppliedDamage == 0 {
			linkedRecord.AttackState = model.DamageResultBarrier
		}
		if linkedApplication.AppliedDamage > 0 {
			linkedTarget.SendVitalsUpdate()
		}
		if linkedApplication.Killed {
			life := model.BuildLifeStateUpdatePacket(linkedTarget.GetUniqueID(), model.Dead)
			linkedTarget.SendToSelfAndKnown(&life)
			if err := h.recordPlayerKill(ctx, player, linkedTarget); err != nil {
				return err
			}
		}
		if linkedBuff != 0 {
			removed := model.BuildBuffRemove(linkedBuff)
			linkedTarget.SendToSelfAndKnown(&removed)
		}
		if linkedApplication.AppliedDamage > 0 && h.vitals != nil && linkedTarget.ID > 0 {
			hp, mp := linkedTarget.CurrentVitals()
			if err := h.vitals.SaveVitals(ctx, linkedTarget.ID, hp, mp); err != nil {
				return err
			}
		}
	}
	if application.AppliedDamage > 0 && !application.Killed {
		h.applyPlayerSkillDisplacement(player, target, skill, &record)
	}
	if delayed {
		targets := []model.CombatDamageTarget{{TargetUID: target.GetUniqueID(), Records: []model.CombatDamageRecord{record}}}
		if linkedTarget != nil {
			targets = append(targets, model.CombatDamageTarget{TargetUID: linkedTarget.GetUniqueID(), Records: []model.CombatDamageRecord{linkedRecord}})
		}
		end := model.BuildAttackEndDamageTargets(castUID, targets)
		if err := h.emitAttack(player, &end); err != nil {
			return err
		}
		if err := h.sendActionState(session, false); err != nil {
			return err
		}
	} else {
		if err := h.sendActionState(session, true); err != nil {
			return err
		}
		targets := []model.CombatDamageTarget{{TargetUID: target.GetUniqueID(), Records: []model.CombatDamageRecord{record}}}
		if linkedTarget != nil {
			targets = append(targets, model.CombatDamageTarget{TargetUID: linkedTarget.GetUniqueID(), Records: []model.CombatDamageRecord{linkedRecord}})
		}
		begin := model.BuildAttackBeginWithSkillDamageTargets(skill.ID, player.GetUniqueID(), castUID, targets)
		if err := h.emitAttackAndEnd(session, player, &begin); err != nil {
			return err
		}
	}
	if target.Session != nil {
		target.SendVitalsUpdate()
		if application.Killed {
			life := model.BuildLifeStateUpdatePacket(target.GetUniqueID(), model.Dead)
			h.publishCombatPacket(player, &life)
		}
	}
	if !application.Killed {
		return h.applyPlayerPVPTransfers(ctx, session, player, target, skill, now)
	}
	return h.recordPlayerKill(ctx, player, target)
}

func (h *CombatHandler) applyPlayerPVPTransfers(ctx context.Context, session *server.Session, source, primary *model.Player, skill model.RefSkill, now time.Time) error {
	maximum := skill.TransferCount - 1
	if source == nil || primary == nil || maximum <= 0 || skill.TransferRange <= 0 || skill.TransferType == 5 || skill.TransferType == 7 || source.GetKnownObjectList() == nil {
		return nil
	}
	pool := make([]*model.Player, 0)
	for _, object := range source.GetKnownObjectList().GetKnownObjects() {
		candidate, ok := object.(*model.Player)
		if !ok || candidate == nil || candidate == source || candidate == primary || candidate.GetLifeState() != model.Alive || !h.pvpTargetAdmitted(source, candidate, skill) {
			continue
		}
		pool = append(pool, candidate)
	}
	radius := float32(skill.TransferRange)
	if skill.TransferType == 6 {
		ordered := make([]*model.Player, 0, maximum)
		current := primary
		for len(ordered) < maximum {
			bestIndex, bestDistance := -1, float32(math.MaxFloat32)
			for index, candidate := range pool {
				distance := navmeshv2.HorizontalDistance(current.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				if distance <= radius && (distance < bestDistance || distance == bestDistance && (bestIndex < 0 || candidate.GetUniqueID() < pool[bestIndex].GetUniqueID())) {
					bestIndex, bestDistance = index, distance
				}
			}
			if bestIndex < 0 {
				break
			}
			candidate := pool[bestIndex]
			ordered = append(ordered, candidate)
			pool = append(pool[:bestIndex], pool[bestIndex+1:]...)
			current = candidate
		}
		pool = ordered
	} else {
		if skill.TransferType == 3 || skill.TransferType == 4 {
			sourceGlobal := source.GetNavmeshPosition().GetGlobalCoordinates()
			primaryGlobal := primary.GetNavmeshPosition().GetGlobalCoordinates()
			dx, dz := primaryGlobal.X-sourceGlobal.X, primaryGlobal.Z-sourceGlobal.Z
			length := float32(math.Hypot(float64(dx), float64(dz)))
			if length <= 0 {
				return nil
			}
			ux, uz := dx/length, dz/length
			forwardLimit := max(length, max(float32(skill.ActionRange), radius))
			filtered := pool[:0]
			for _, candidate := range pool {
				global := candidate.GetNavmeshPosition().GetGlobalCoordinates()
				cx, cz := global.X-sourceGlobal.X, global.Z-sourceGlobal.Z
				projection := cx*ux + cz*uz
				lateral := float32(math.Abs(float64(cx*uz - cz*ux)))
				if projection >= 0 && projection <= forwardLimit && lateral <= radius {
					filtered = append(filtered, candidate)
				}
			}
			pool = filtered
			sort.SliceStable(pool, func(i, j int) bool {
				left := navmeshv2.HorizontalDistance(source.GetNavmeshPosition(), pool[i].GetNavmeshPosition())
				right := navmeshv2.HorizontalDistance(source.GetNavmeshPosition(), pool[j].GetNavmeshPosition())
				return left < right || left == right && pool[i].GetUniqueID() < pool[j].GetUniqueID()
			})
		} else {
			center := primary.GetNavmeshPosition()
			if skill.TransferType == 1 {
				center = source.GetNavmeshPosition()
			}
			filtered := pool[:0]
			for _, candidate := range pool {
				if navmeshv2.HorizontalDistance(center, candidate.GetNavmeshPosition()) <= radius {
					filtered = append(filtered, candidate)
				}
			}
			pool = filtered
			sort.Slice(pool, func(i, j int) bool {
				di := navmeshv2.HorizontalDistance(center, pool[i].GetNavmeshPosition())
				dj := navmeshv2.HorizontalDistance(center, pool[j].GetNavmeshPosition())
				return di < dj || di == dj && pool[i].GetUniqueID() < pool[j].GetUniqueID()
			})
		}
		if len(pool) > maximum {
			pool = pool[:maximum]
		}
	}
	falloff := min(max(skill.TransferFalloffPercent, 0), 100)
	factor := 100 - falloff
	for _, candidate := range pool {
		factor = nextTransferFactor(factor, falloff)
		if err := h.applyPlayerPVPTransfer(ctx, session, source, candidate, skill, now, factor); err != nil {
			return err
		}
	}
	return nil
}

// interceptPlayerLinkedDamage mirrors the native linked-owner branch for a
// live player target. The source UID is authoritative: only a player present
// in the attacker's known-object graph can receive the redirected hit, and the
// owning buff's use limit is committed only after the redirected application
// has produced a wire-visible result.
func (h *CombatHandler) interceptPlayerLinkedDamage(source, target *model.Player, damage int, magical bool) (int, *model.Player, model.PlayerDamageApplication, uint32) {
	if h == nil || source == nil || target == nil || damage <= 0 || h.referenceData == nil {
		return damage, nil, model.PlayerDamageApplication{}, 0
	}
	players := make(map[uint32]*model.Player)
	if known := source.GetKnownObjectList(); known != nil {
		for _, object := range known.GetKnownObjects() {
			if candidate, ok := object.(*model.Player); ok && candidate != nil {
				players[candidate.GetUniqueID()] = candidate
			}
		}
	}
	now := h.combatNow()
	buffs := target.ActiveBuffSnapshot()
	sort.Slice(buffs, func(i, j int) bool { return buffs[i].Token < buffs[j].Token })
	for _, buff := range buffs {
		if buff.SourceUID == 0 || buff.SourceUID == target.GetUniqueID() || (!buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now)) {
			continue
		}
		skill, exists := h.referenceData.Skill(buff.SkillID)
		if !exists {
			continue
		}
		amount := skill.LinkedDamageAmount(damage, magical)
		linked := players[buff.SourceUID]
		if amount <= 0 || linked == nil || linked == source || linked.GetLifeState() != model.Alive {
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
		if removed {
			return damage - amount, linked, application, buff.Token
		}
		return damage - amount, linked, application, 0
	}
	return damage, nil, model.PlayerDamageApplication{}, 0
}

func (h *CombatHandler) applyPlayerPVPTransfer(ctx context.Context, session *server.Session, source, target *model.Player, skill model.RefSkill, now time.Time, factor int) error {
	if source == nil || target == nil || target.GetLifeState() != model.Alive {
		return nil
	}
	damage := 0
	blocked := skill.AbsoluteDamage <= 0 && h.playerAttackBlockedAgainstPlayer(target)
	critical := false
	if !blocked {
		for _, strike := range h.skillStrikes(skill) {
			hit := h.playerSkillDamageAgainstPlayerAt(source, target, strike, now)
			hit = scaleTransferAmount(hit, factor)
			if strike.AbsoluteDamage <= 0 && h.playerAttackCritical(source) {
				hit *= 2
				critical = true
			}
			hit = h.mitigatePlayerSkillDamage(target, hit, strike.IsMagicalAttack(), strike.AbsoluteDamage > 0, now, strike.AttackPercent)
			hit += hit * source.ActiveCombatDamagePercent() / 100
			damage += hit
		}
	}
	damage, linkedTarget, linkedApplication, linkedBuff := h.interceptPlayerLinkedDamage(source, target, damage, skill.IsMagicalAttack())
	application := target.ApplyCombatDamage(damage, skill.IsMagicalAttack())
	if application.AppliedDamage > 0 && h.vitals != nil && target.ID > 0 {
		hp, mp := target.CurrentVitals()
		if err := h.vitals.SaveVitals(ctx, target.ID, hp, mp); err != nil {
			return err
		}
	}
	if err := h.restoreDamageVitals(ctx, session, source, skill, application.AppliedDamage); err != nil {
		return err
	}
	state := model.DamageAttackStateNormal
	if blocked {
		state = model.DamageAttackStateBlock
	}
	if application.BarrierDamage > 0 && application.AppliedDamage == 0 {
		state = model.DamageResultBarrier
	}
	if application.Killed {
		state |= model.DamageAttackStateDead
	}
	effectMask := uint32(0)
	if application.AppliedDamage > 0 {
		effectMask = h.applyPlayerSkillStatuses(target, source, skill, now)
	}
	damageState := model.DamageStateNormal
	if critical {
		damageState = model.DamageStateCritical
	}
	record := model.CombatDamageRecord{AttackState: state, DamageState: damageState, Damage: uint32(min(application.WireDamage, maximumWireDamage)), EffectMask: effectMask, BarrierRemaining: uint16(max(0, application.BarrierRemaining)), BarrierMaximum: uint16(max(0, application.BarrierMaximum))}
	if application.AppliedDamage > 0 && !application.Killed {
		h.applyPlayerSkillDisplacement(source, target, skill, &record)
	}
	targets := []model.CombatDamageTarget{{TargetUID: target.GetUniqueID(), Records: []model.CombatDamageRecord{record}}}
	if linkedTarget != nil {
		linkedRecord := model.CombatDamageRecord{DamageState: damageState, Damage: uint32(min(linkedApplication.WireDamage, maximumWireDamage))}
		if linkedApplication.Killed {
			linkedRecord.AttackState |= model.DamageAttackStateDead
		}
		if linkedApplication.BarrierDamage > 0 && linkedApplication.AppliedDamage == 0 {
			linkedRecord.AttackState = model.DamageResultBarrier
		}
		targets = append(targets, model.CombatDamageTarget{TargetUID: linkedTarget.GetUniqueID(), Records: []model.CombatDamageRecord{linkedRecord}})
		if linkedApplication.AppliedDamage > 0 && h.vitals != nil && linkedTarget.ID > 0 {
			hp, mp := linkedTarget.CurrentVitals()
			if err := h.vitals.SaveVitals(ctx, linkedTarget.ID, hp, mp); err != nil {
				return err
			}
		}
		linkedTarget.SendVitalsUpdate()
		if linkedBuff != 0 {
			removed := model.BuildBuffRemove(linkedBuff)
			linkedTarget.SendToSelfAndKnown(&removed)
		}
		if linkedApplication.Killed {
			life := model.BuildLifeStateUpdatePacket(linkedTarget.GetUniqueID(), model.Dead)
			linkedTarget.SendToSelfAndKnown(&life)
			if err := h.recordPlayerKill(ctx, source, linkedTarget); err != nil {
				return err
			}
		}
	}
	castUID := model.NextActionUID()
	packet := model.BuildAttackBeginWithSkillDamageTargets(skill.ID, source.GetUniqueID(), castUID, targets)
	if err := h.emitAttack(source, &packet); err != nil {
		return err
	}
	target.SendVitalsUpdate()
	if application.Killed {
		life := model.BuildLifeStateUpdatePacket(target.GetUniqueID(), model.Dead)
		h.publishCombatPacket(source, &life)
		return h.recordPlayerKill(ctx, source, target)
	}
	return nil
}

func (h *CombatHandler) playerSkillDamageAgainstPlayer(player, target *model.Player, skill model.RefSkill) int {
	return h.playerSkillDamageAgainstPlayerAt(player, target, skill, h.combatNow())
}

func (h *CombatHandler) playerSkillDamageAgainstPlayerAt(player, target *model.Player, skill model.RefSkill, now time.Time) int {
	if skill.AbsoluteDamage > 0 {
		return skill.AbsoluteDamage
	}
	stats := player.DerivedStats()
	minimum, maximum := stats.PhyAttackMin+skill.AttackMin, stats.PhyAttackMax+skill.AttackMax
	if skill.IsMagicalAttack() {
		minimum, maximum = stats.MagAttackMin+skill.AttackMin, stats.MagAttackMax+skill.AttackMax
	}
	damage := max(1, model.CombatVarianceDamage(minimum, maximum, stats.HitRate, target.DerivedStats().ParryRate, player.Level, target.Level, h.combatDamageRolls()))
	if status, active := player.CombatStatus(model.CombatStatusAttackDown, now); active {
		reduction := min(max(status.Power, 0), 100)
		damage = max(1, damage*(100-reduction)/100)
	}
	return damage
}

func (h *CombatHandler) mitigatePlayerSkillDamage(target *model.Player, damage int, magical, absolute bool, now time.Time, attackPercent int) int {
	if damage <= 0 {
		return 0
	}
	if absolute {
		return damage
	}
	stats := target.DerivedStats()
	defense := stats.PhyDef
	absorption := stats.PhyAbsorb
	statusKind := model.CombatStatusPhysicalDefenseDown
	if magical {
		defense, statusKind = stats.MagDef, model.CombatStatusMagicalDefenseDown
		absorption = stats.MagAbsorb
	}
	if status, active := target.CombatStatus(statusKind, now); active {
		defense = max(0, defense-status.Power)
	}
	damage = max(1, int(model.CombatMitigatedDamage(float32(damage), float32(defense), float32(absorption))))
	if attackPercent <= 0 {
		attackPercent = 100
	}
	damage = damage * attackPercent / 100
	if status, active := target.CombatStatus(model.CombatStatusDamageTakenUp, now); active {
		damage = model.CombatFinalDamageFactor(damage, status.Power)
	}
	return damage
}

func (h *CombatHandler) playerAttackBlockedAgainstPlayer(target *model.Player) bool {
	if h == nil || target == nil || h.blockRoll == nil {
		return false
	}
	return target.DerivedStats().BlockRate > 0 && h.blockRoll(target.DerivedStats().BlockRate)
}

func (h *CombatHandler) applyPlayerSkillStatuses(target, source *model.Player, skill model.RefSkill, now time.Time) uint32 {
	if target == nil || source == nil || len(skill.StatusEffects) == 0 {
		return 0
	}
	roll := h.rollPercent
	if roll == nil {
		roll = func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance }
	}
	var badMask uint32
	for _, effect := range skill.StatusEffects {
		if effect.Kind == 0 || effect.Chance <= 0 || effect.DurationMS <= 0 || !roll(effect.Chance) {
			continue
		}
		status := model.CombatStatus{Kind: effect.Kind, Token: model.NextActionUID(), SourceUID: source.GetUniqueID(), SkillID: skill.ID, Power: effect.Power, Level: effect.Level, Payload: effect.Payload, ExpiresAt: now.Add(time.Duration(effect.DurationMS) * time.Millisecond), Stackable: skill.LinkedSameGroup}
		if !target.ApplyCombatStatusAt(status, now) {
			continue
		}
		if h.world != nil && statusRestrainsMovement(effect.Kind) {
			h.world.StopMovingCharacter(target, target.GetNavmeshPosition(), "combat-status-control", true)
			h.interruptPlayerActionLocked(target.GetUniqueID())
			_ = h.sendActionState(target.Session, false)
		}
		if bit := model.CombatStatusBadEffectBit(effect.Kind); bit != 0 {
			badMask |= bit
			h.schedulePlayerBadEffectExpiration(target, status)
		} else {
			packet := model.BuildTimedBuffAdd(target.GetUniqueID(), skill.ID, status.Token, uint32(effect.DurationMS))
			target.SendToSelfAndKnown(&packet)
		}
		if effect.Kind == model.CombatStatusBurn || effect.Kind == model.CombatStatusPoison || effect.Kind == model.CombatStatusBleed {
			h.schedulePlayerStatusDamageTick(source, target, status, now.Add(5*time.Second))
		}
	}
	if badMask != 0 {
		mask, levels := target.CombatBadEffectState(now)
		packet := model.BuildBadEffectUpdate(target.GetUniqueID(), mask, levels)
		target.SendToSelfAndKnown(&packet)
	}
	return badMask
}

// schedulePlayerStatusDamageTick applies the native five-second abnormal
// damage cadence to player targets. The token check prevents a replaced
// same-kind status from inheriting an older actor's ticks.
func (h *CombatHandler) schedulePlayerStatusDamageTick(source, target *model.Player, expected model.CombatStatus, tickAt time.Time) {
	if h == nil || h.schedule == nil || source == nil || target == nil || !tickAt.Before(expected.ExpiresAt) {
		return
	}
	delay := tickAt.Sub(h.combatNow())
	if delay < 0 {
		delay = 0
	}
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		now := h.combatNow()
		status, active := target.CombatStatus(expected.Kind, now)
		if !active || status.Token != expected.Token || target.GetLifeState() != model.Alive {
			return
		}
		amount := status.Payload
		if amount <= 0 {
			amount = status.Power
		}
		if status, active := target.CombatStatus(model.CombatStatusDamageTakenUp, now); active {
			amount = model.CombatFinalDamageFactor(max(1, amount), status.Power)
		}
		amount, linkedTarget, linkedApplication, linkedBuff := h.interceptPlayerLinkedDamage(source, target, max(1, amount), false)
		application := target.ApplyCombatDamage(amount, false)
		if application.WireDamage == 0 && application.BarrierDamage == 0 {
			return
		}
		if application.AppliedDamage > 0 && h.vitals != nil && target.ID > 0 {
			hp, mp := target.CurrentVitals()
			if err := h.vitals.SaveVitals(context.Background(), target.ID, hp, mp); err != nil {
				return
			}
		}
		linkedRecord := model.CombatDamageRecord{}
		if linkedTarget != nil {
			if linkedApplication.AppliedDamage > 0 && h.vitals != nil && linkedTarget.ID > 0 {
				hp, mp := linkedTarget.CurrentVitals()
				if err := h.vitals.SaveVitals(context.Background(), linkedTarget.ID, hp, mp); err != nil {
					return
				}
			}
			linkedRecord = model.CombatDamageRecord{DamageState: model.DamageStateNormal, Damage: uint32(min(linkedApplication.WireDamage, maximumWireDamage))}
			if linkedApplication.BarrierDamage > 0 && linkedApplication.AppliedDamage == 0 {
				linkedRecord.AttackState = model.DamageResultBarrier
				linkedRecord.BarrierRemaining = uint16(max(0, linkedApplication.BarrierRemaining))
				linkedRecord.BarrierMaximum = uint16(max(0, linkedApplication.BarrierMaximum))
			}
			if linkedApplication.Killed {
				linkedRecord.AttackState |= model.DamageAttackStateDead
			}
			linkedTarget.SendVitalsUpdate()
			if linkedBuff != 0 {
				removed := model.BuildBuffRemove(linkedBuff)
				linkedTarget.SendToSelfAndKnown(&removed)
			}
			if linkedApplication.Killed {
				life := model.BuildLifeStateUpdatePacket(linkedTarget.GetUniqueID(), model.Dead)
				linkedTarget.SendToSelfAndKnown(&life)
				if err := h.recordPlayerKill(context.Background(), source, linkedTarget); err != nil {
					logrus.WithError(err).Warn("linked player-kill quest progress failed")
				}
			}
		}
		castUID := model.NextActionUID()
		record := model.CombatDamageRecord{DamageState: model.DamageStateNormal, Damage: uint32(min(application.WireDamage, maximumWireDamage))}
		if application.BarrierDamage > 0 && application.AppliedDamage == 0 {
			record.AttackState = model.DamageResultBarrier
			record.BarrierRemaining = uint16(max(0, application.BarrierRemaining))
			record.BarrierMaximum = uint16(max(0, application.BarrierMaximum))
		}
		if application.Killed {
			record.AttackState |= model.DamageAttackStateDead
		}
		targets := []model.CombatDamageTarget{{TargetUID: target.GetUniqueID(), Records: []model.CombatDamageRecord{record}}}
		if linkedTarget != nil {
			targets = append(targets, model.CombatDamageTarget{TargetUID: linkedTarget.GetUniqueID(), Records: []model.CombatDamageRecord{linkedRecord}})
		}
		packet := model.BuildAttackBeginWithSkillDamageTargets(status.SkillID, source.GetUniqueID(), castUID, targets)
		h.publishCombatPacket(source, &packet)
		target.SendVitalsUpdate()
		if application.Killed {
			life := model.BuildLifeStateUpdatePacket(target.GetUniqueID(), model.Dead)
			h.publishCombatPacket(source, &life)
			if err := h.recordPlayerKill(context.Background(), source, target); err != nil {
				logrus.WithError(err).Warn("player-kill quest progress failed")
			}
			return
		}
		h.schedulePlayerStatusDamageTick(source, target, status, now.Add(5*time.Second))
	})
}

func (h *CombatHandler) recordPlayerKill(ctx context.Context, killer, target *model.Player) error {
	if h == nil || killer == nil || target == nil || killer == target {
		return nil
	}
	if h.guildWarKill != nil {
		if err := h.guildWarKill(ctx, killer.ID, target.ID, uint32(max(target.Level, 1))); err != nil {
			return err
		}
	}
	competitiveScored := false
	if h.competitiveKill != nil {
		if err := h.competitiveKill(killer.ID, target.ID); err != nil && !errors.Is(err, service.ErrCompetitiveMatchNotFound) {
			return err
		} else if err == nil {
			competitiveScored = true
		}
	}
	fortressScored := false
	if h.fortressKill != nil {
		err := h.fortressKill(ctx, killer.ID, target.ID, time.Now().UTC())
		if err == nil {
			fortressScored = true
		} else if !errors.Is(err, model.ErrSocialNotFound) && !errors.Is(err, model.ErrSocialAccessDenied) && !errors.Is(err, model.ErrSocialConflict) {
			return err
		}
	}
	if h.pkKills != nil && !competitiveScored && !fortressScored {
		state, err := h.pkKills.RecordKill(ctx, killer.ID)
		if err != nil {
			return err
		}
		killer.CommitPKCounters(state)
		for _, packet := range []network.Packet{
			vsro188.BuildPKPenaltyUpdate(state.PKPenaltyPoint),
			vsro188.BuildPKDailyUpdate(state.DailyPK),
			vsro188.BuildPKLevelUpdate(state.TotalPK),
		} {
			if err = h.sendPlayerPacket(killer.GetSession(), packet); err != nil {
				return err
			}
		}
	}
	repository, ok := h.quests.(questPlayerKillRepository)
	if !ok {
		return nil
	}
	targetJob := target.GetJob()
	if !targetJob.Active() {
		return nil
	}
	progress, err := repository.RecordPlayerKill(ctx, killer.ID, model.QuestPlayerKillTarget{
		Level: target.Level, JobType: targetJob.Type, RegionID: target.GetNavmeshPosition().Region.ID,
	})
	if err != nil {
		return err
	}
	for _, result := range progress {
		if !result.Changed {
			continue
		}
		killer.CommitQuestMutation(result.Quest)
		packet, buildErr := buildQuestStatePacket(result.Quest)
		if buildErr != nil {
			return buildErr
		}
		if err = h.sendPlayerPacket(killer.GetSession(), packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *CombatHandler) schedulePlayerBadEffectExpiration(target *model.Player, expected model.CombatStatus) {
	if h == nil || h.schedule == nil || target == nil {
		return
	}
	delay := expected.ExpiresAt.Sub(h.combatNow())
	if delay < 0 {
		delay = 0
	}
	h.schedule(delay, func() {
		now := h.combatNow()
		current, active := target.CombatStatus(expected.Kind, now)
		if active && current.Token == expected.Token {
			return
		}
		mask, levels := target.CombatBadEffectState(now)
		packet := model.BuildBadEffectUpdate(target.GetUniqueID(), mask, levels)
		target.SendToSelfAndKnown(&packet)
	})
}

func (h *CombatHandler) executeBarrierSkill(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) error {
	if player == nil || skill.ExecutionKind() != model.SkillExecutionBarrier {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
	}
	token := model.NextActionUID()
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, player.GetUniqueID(), player.GetUniqueID(), castUID)
	if err := h.emitAttack(player, &begin); err != nil {
		return err
	}
	finish := func() {
		if old, active := player.RemoveActiveBarrier(); active {
			remove := model.BuildBuffRemove(old.Token)
			_ = h.emitAttack(player, &remove)
		}
		if !player.CommitBarrier(model.ActiveBarrier{
			SkillID: skill.ID, Token: token, HitPoints: skill.BarrierHitPoints,
			Absorption: skill.BarrierAbsorption, Physical: skill.BarrierPhysical,
		}) {
			end := model.BuildBuffCastEnd(player.GetUniqueID(), castUID)
			_ = h.emitAttack(player, &end)
			_ = h.sendActionState(session, false)
			return
		}
		if h.world != nil {
			h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "wall-skill-root", false)
		}
		end := model.BuildBuffCastEnd(player.GetUniqueID(), castUID)
		add := model.BuildBuffAdd(player.GetUniqueID(), skill.ID, token)
		_ = h.emitAttack(player, &end)
		_ = h.emitAttack(player, &add)
		_ = h.sendActionState(session, false)
		if h.schedule != nil && skill.BarrierDrainMS > 0 {
			h.scheduleBarrierDrain(player, skill, token)
		}
	}
	delay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(player, skill))
	if delay > 0 && h.schedule != nil {
		delayedToken := h.beginDelayedCast(player.GetUniqueID(), func() {
			end := model.BuildBuffCastEnd(player.GetUniqueID(), castUID)
			_ = h.emitAttack(player, &end)
			_ = h.sendActionState(session, false)
		})
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(player.GetUniqueID(), delayedToken) {
				return
			}
			finish()
		})
		return nil
	}
	finish()
	return nil
}

func (h *CombatHandler) scheduleBarrierDrain(player *model.Player, skill model.RefSkill, token uint32) {
	delay := time.Duration(skill.BarrierDrainMS) * time.Millisecond
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		barrier := player.BarrierSnapshot()
		if barrier.Token != token {
			return
		}
		if barrier.HitPoints <= 0 {
			if player.RemoveBarrier(token) {
				remove := model.BuildBuffRemove(token)
				_ = h.emitAttack(player, &remove)
			}
			return
		}
		_, mp := player.CurrentVitals()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		spent, err := h.vitals.SpendMP(ctx, player.ID, mp, skill.BarrierMPCost)
		cancel()
		if err != nil || !spent || !player.CommitMP(mp, skill.BarrierMPCost) {
			if player.RemoveBarrier(token) {
				remove := model.BuildBuffRemove(token)
				_ = h.emitAttack(player, &remove)
			}
			return
		}
		hp, currentMP := player.CurrentVitals()
		_ = h.sendPlayerPacket(player.Session, model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, currentMP))
		h.scheduleBarrierDrain(player, skill, token)
	})
}

func (h *CombatHandler) executeMovementSkill(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill, requested ...*navmeshv2.RtNavmeshPosition) error {
	if player == nil || skill.ExecutionKind() != model.SkillExecutionMovement || h.world == nil {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(player, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	start := player.GetNavmeshPosition()
	global := start.GetGlobalCoordinates()
	distance := float32(skill.MovementDistance)
	radians := model.HeadingToRadians(start.Heading)
	destination, err := h.world.NewPosFromGlobalCoordinates(math32.NewVector3(
		global.X+float32(math.Cos(float64(radians)))*distance,
		global.Y,
		global.Z+float32(math.Sin(float64(radians)))*distance,
	))
	if len(requested) > 0 && requested[0] != nil {
		candidate := *requested[0]
		candidateDistance := navmeshv2.HorizontalDistance(start, candidate)
		if candidateDistance <= distance+1 {
			destination = candidate
			err = nil
		} else if candidateDistance > 0 {
			// TestIn clamps the requested direction to the skill-authored
			// distance. Falling back to the actor heading here can land at a
			// different point when the client cursor is beyond maximum range.
			scale := distance / candidateDistance
			if navmeshv2.IsDungeonRegion(start.Region.ID) && candidate.Region.ID == start.Region.ID {
				destination, err = resolveDestinationPosition(h.world, start.Region.ID, math32.NewVector3(
					start.Offset.X+(candidate.Offset.X-start.Offset.X)*scale,
					start.Offset.Y,
					start.Offset.Z+(candidate.Offset.Z-start.Offset.Z)*scale,
				))
			} else {
				candidateGlobal := candidate.GetGlobalCoordinates()
				destination, err = h.world.NewPosFromGlobalCoordinates(math32.NewVector3(
					global.X+(candidateGlobal.X-global.X)*scale,
					global.Y,
					global.Z+(candidateGlobal.Z-global.Z)*scale,
				))
			}
		}
	}
	if err != nil || !h.world.CanMoveDirectly(start, destination) {
		return h.sendActionState(session, false)
	}
	destination.Heading = start.Heading
	// Geometry and direct-path admission are non-mutating pre-engage checks.
	// Only after they pass may the authoritative resource/cooldown transaction
	// run, matching the native validator's ordering.
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
	}
	h.world.StopMovingCharacter(player, start, "ghost-walk", false)
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildMovementCastBegin(skill.ID, player.GetUniqueID(), player.GetUniqueID(), castUID, destination)
	if err := h.emitAttack(player, &begin); err != nil {
		return err
	}
	// Teleport skills are instantaneous. The descriptor duration is animation
	// metadata; delaying the world commit leaves the client frozen at origin.
	player.SetNavmeshPosition(destination)
	end := model.BuildBuffCastEnd(player.GetUniqueID(), castUID)
	if err := h.emitAttack(player, &end); err != nil {
		return err
	}
	return h.sendActionState(session, false)
}

func (h *CombatHandler) resolveDelayedSkillAttack(session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill, castUID uint32) {
	if h == nil || player == nil || target == nil || target.GetLifeState() != model.Alive {
		if h != nil && player != nil && target != nil {
			end := model.BuildAttackEndDamageRecords(target.GetUniqueID(), castUID, nil)
			_ = h.emitAttack(player, &end)
		}
		_ = h.sendActionState(session, false)
		return
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	damageHits := make([]uint32, 0, 3)
	damageRecords := make([]model.CombatDamageRecord, 0, 3)
	appliedDamage := 0
	killed := false
	displaced := false
	imbue, hasImbue := h.activeWeaponImbue(player)
	for _, strike := range h.skillStrikes(skill) {
		damage := h.skillStrikeDamage(player, target, strike)
		if hasImbue {
			damage += h.weaponImbueDamage(player, target, imbue)
		}
		result := h.resolvePlayerDamageWithAggro(player, target, damage, strike.IsMagicalAttack(), strike.AbsoluteDamage > 0, now, strike.AggroIncrease, strike.AggroIncreasePercent, strike.AttackPercent)
		wireDamage, strikeApplied, strikeKilled := result.wireDamage, result.appliedDamage, result.killed
		damageHits = append(damageHits, uint32(wireDamage))
		record := result.combatRecord(0)
		if strikeApplied > 0 && !strikeKilled && !displaced {
			displaced = h.applySkillDisplacement(player, target, skill, &record)
		}
		damageRecords = append(damageRecords, record)
		appliedDamage += strikeApplied
		if strikeKilled {
			killed = true
			break
		}
	}
	if len(damageHits) == 0 {
		_ = h.sendActionState(session, false)
		return
	}
	landed := false
	for _, damage := range damageHits {
		landed = landed || damage > 0
	}
	effectMask := uint32(0)
	if landed && hasImbue {
		effectMask |= h.applyImbueStatuses(session, player, target, imbue, now)
	}
	if landed {
		effectMask |= h.applySkillStatuses(session, player, target, skill, now)
	}
	if len(damageRecords) != 0 {
		damageRecords[len(damageRecords)-1].EffectMask = effectMask
	}
	_ = h.restoreDamageVitals(context.Background(), session, player, skill, appliedDamage)
	targets := []model.CombatDamageTarget{{TargetUID: target.GetUniqueID(), Records: damageRecords}}
	transferTargets, _ := h.resolveTransfers(context.Background(), session, player, target, skill, now, h.skillStrikeDamage(player, target, skill))
	targets = mergeCombatDamageTargets(targets, transferTargets)
	if hasImbue {
		imbueTargets, _ := h.resolveTransfers(context.Background(), session, player, target, imbue, now, h.weaponImbueDamage(player, target, imbue))
		targets = mergeCombatDamageTargets(targets, imbueTargets)
	}
	end := model.BuildAttackEndDamageTargets(castUID, targets)
	_ = h.emitAttack(player, &end)
	impactDelay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(player, skill))
	recovery := h.skillComboImpactDuration(player, skill) - impactDelay
	h.finishActionAfter(session, max(recovery, 0))
	if killed {
		_ = h.awardPlayerKill(context.Background(), session, player, target, target.GetUniqueID())
	}
}

func (h *CombatHandler) executeTargetedStatusSkill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill) error {
	if player == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionStatusAttack {
		return h.sendActionState(session, false)
	}
	reach := h.skillAttackReach(player, target, skill)
	if navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()) > reach {
		if reason, rejected := skillCastRejection(player, skill); rejected {
			return h.sendSkillCastError(session, reason)
		}
		if err := h.beginAttackPursuit(session, player, target, reach, skill.ID); err != nil {
			return h.sendActionState(session, false)
		}
		return nil
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
	}
	h.facePlayerTowardTarget(player, target)
	return h.beginStatusCast(session, player, target.GetUniqueID(), skill, func(impactTime time.Time) []model.SkillEffectTarget {
		if target.GetLifeState() != model.Alive {
			return nil
		}
		h.applyStatusImpact(ctx, session, player, target, skill)
		mask := h.applySkillStatuses(session, player, target, skill, impactTime)
		if mask == 0 {
			return nil
		}
		return []model.SkillEffectTarget{{TargetUID: target.GetUniqueID(), EffectMask: mask}}
	})
}

func (h *CombatHandler) executeAreaStatusSkill(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) error {
	if player == nil || skill.ExecutionKind() != model.SkillExecutionStatusAttack || skill.TargetRequired {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
	}
	return h.beginStatusCast(session, player, player.GetUniqueID(), skill, func(impactTime time.Time) []model.SkillEffectTarget {
		type candidate struct {
			npc      *model.NPC
			distance float32
		}
		known := player.GetKnownObjectList()
		if known == nil {
			return nil
		}
		radius := float32(skill.TransferRange)
		if radius <= 0 {
			radius = float32(skill.ActionRange)
		}
		candidates := make([]candidate, 0)
		for _, object := range known.GetKnownObjects() {
			if skill.TargetEnemyPVP {
				continue
			}
			npc, ok := object.(*model.NPC)
			if !ok || npc == nil || npc.GetLifeState() != model.Alive || !npc.GetTypeInfo().IsNPCMob() {
				continue
			}
			distance := navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), npc.GetNavmeshPosition())
			if radius > 0 && distance <= radius {
				candidates = append(candidates, candidate{npc: npc, distance: distance})
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].distance < candidates[j].distance })
		if skill.TransferCount > 0 && len(candidates) > skill.TransferCount {
			candidates = candidates[:skill.TransferCount]
		}
		targets := make([]model.SkillEffectTarget, 0, len(candidates))
		for _, candidate := range candidates {
			h.applyStatusImpact(ctx, session, player, candidate.npc, skill)
			mask := h.applySkillStatuses(session, player, candidate.npc, skill, impactTime)
			if mask != 0 {
				targets = append(targets, model.SkillEffectTarget{TargetUID: candidate.npc.GetUniqueID(), EffectMask: mask})
			}
		}
		if skill.TargetEnemyPVP {
			playerCandidates := make([]*model.Player, 0)
			for _, object := range known.GetKnownObjects() {
				candidate, ok := object.(*model.Player)
				if !ok || candidate == nil || candidate == player || candidate.GetLifeState() != model.Alive || !h.pvpTargetAdmitted(player, candidate, skill) {
					continue
				}
				distance := navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				if radius <= 0 || distance <= radius {
					playerCandidates = append(playerCandidates, candidate)
				}
			}
			sort.Slice(playerCandidates, func(i, j int) bool {
				di := navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), playerCandidates[i].GetNavmeshPosition())
				dj := navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), playerCandidates[j].GetNavmeshPosition())
				return di < dj || di == dj && playerCandidates[i].GetUniqueID() < playerCandidates[j].GetUniqueID()
			})
			remaining := len(playerCandidates)
			if skill.TransferCount > 0 {
				remaining = min(remaining, max(0, skill.TransferCount-len(targets)))
			}
			for _, candidate := range playerCandidates[:remaining] {
				mask := h.applyPlayerSkillStatuses(candidate, player, skill, impactTime)
				if mask != 0 {
					targets = append(targets, model.SkillEffectTarget{TargetUID: candidate.GetUniqueID(), EffectMask: mask})
				}
			}
		}
		return targets
	})
}

func (h *CombatHandler) applyStatusImpact(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill) {
	elementalImpact := false
	for _, effect := range skill.StatusEffects {
		if effect.Kind == model.CombatStatusFreeze || effect.Kind == model.CombatStatusFrostbite {
			elementalImpact = true
			break
		}
	}
	// `tant` accompanies cold status metadata; it is not direct spell damage.
	// Cold Wave/Frost Nova damage is therefore zero unless a distinct status
	// mechanic itself owns damage-over-time (for example Burn).
	_ = elementalImpact
}

func (h *CombatHandler) prepareStatusSkill(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) (time.Time, bool, error) {
	if reason, rejected := skillCastRejection(player, skill); rejected {
		return time.Time{}, false, h.sendSkillCastError(session, reason)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	key := skillCooldownKey{playerUID: player.GetUniqueID(), skillID: skill.ID}
	if !h.skillAvailable(key, now) {
		return time.Time{}, false, h.sendActionState(session, false)
	}
	if err := h.consumeRangedAmmo(ctx, session, player); err != nil {
		return time.Time{}, false, err
	}
	if err := h.spendSkillResources(ctx, session, player, skill); err != nil {
		return time.Time{}, false, err
	}
	if playerHasSkillResourceCost(player, skill) {
		hp, mp := player.CurrentVitals()
		if err := h.sendPlayerPacket(session, model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, mp)); err != nil {
			return time.Time{}, false, err
		}
	}
	h.registerSkillCooldownForPlayer(player, key, skill, now)
	return now, true, nil
}

func (h *CombatHandler) beginStatusCast(session *server.Session, player *model.Player, animationTarget uint32, skill model.RefSkill, apply func(time.Time) []model.SkillEffectTarget) error {
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildDelayedAttackBegin(skill.ID, player.GetUniqueID(), animationTarget, castUID)
	if err := h.emitAttack(player, &begin); err != nil {
		return err
	}
	finish := func() {
		impactTime := time.Now()
		if h.now != nil {
			impactTime = h.now()
		}
		targets := apply(impactTime)
		end := model.BuildStatusAttackEnd(animationTarget, castUID, targets)
		_ = h.emitAttack(player, &end)
		_ = h.sendActionState(session, false)
	}
	delay := skill.RuntimeCastingDurationAt(h.skillActionTimePercent(player, skill))
	if delay > 0 && h.schedule != nil {
		delayedToken := h.beginDelayedCast(player.GetUniqueID(), func() {
			end := model.BuildStatusAttackEnd(animationTarget, castUID, nil)
			_ = h.emitAttack(player, &end)
			_ = h.sendActionState(session, false)
		})
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !h.consumeDelayedCast(player.GetUniqueID(), delayedToken) {
				return
			}
			finish()
		})
		return nil
	}
	finish()
	return nil
}

func (h *CombatHandler) skillStrikes(skill model.RefSkill) []model.RefSkill {
	strikes := []model.RefSkill{skill}
	seen := map[uint32]bool{skill.ID: true}
	for len(strikes) < 16 {
		nextID := strikes[len(strikes)-1].BasicChainCode
		if nextID == 0 || seen[nextID] {
			break
		}
		next, exists := h.referenceSkill(nextID)
		if !exists {
			break
		}
		seen[nextID] = true
		strikes = append(strikes, next)
	}
	return strikes
}

func (h *CombatHandler) skillComboImpactDuration(player *model.Player, skill model.RefSkill) time.Duration {
	var duration time.Duration
	for _, phase := range h.skillStrikes(skill) {
		// Resolve action time per phase so chained skills can carry distinct
		// speed policy and future phase schedulers do not inherit phase one's
		// stale timing.
		duration += phase.RuntimeImpactDurationAt(h.skillActionTimePercent(player, phase))
	}
	return duration
}

func (h *CombatHandler) skillStrikeDamage(player *model.Player, target *model.NPC, skill model.RefSkill) int {
	if skill.AbsoluteDamage > 0 {
		return skill.AbsoluteDamage
	}
	damage := h.playerVarianceDamage(player, target, skill.IsMagicalAttack(), skill.AttackMin, skill.AttackMax)
	if damage < 1 {
		return 1
	}
	return damage
}

func (h *CombatHandler) weaponImbueDamage(player *model.Player, target *model.NPC, imbue model.RefSkill) int {
	if player == nil || !imbue.IsWeaponImbue() {
		return 0
	}
	return h.skillStrikeDamage(player, target, imbue)
}

func (h *CombatHandler) playerVarianceDamage(player *model.Player, target *model.NPC, magical bool, skillMin, skillMax int) int {
	if player == nil {
		return 0
	}
	stats := player.DerivedStats()
	minimum, maximum := stats.PhyAttackMin, stats.PhyAttackMax
	if magical {
		minimum, maximum = stats.MagAttackMin, stats.MagAttackMax
	}
	minimum += skillMin
	maximum += skillMax
	targetLevel, parry := 0, 0
	varianceAvailable := false
	if target != nil && h != nil && h.referenceData != nil {
		if character, exists := h.referenceData.Character(target.GetRefObjectID()); exists {
			targetLevel, parry = character.Level, character.EvasionRate
			varianceAvailable = true
		}
	}
	if !varianceAvailable {
		return (minimum + maximum) / 2
	}
	return model.CombatVarianceDamage(minimum, maximum, stats.HitRate, parry, player.Level, targetLevel, h.combatDamageRolls())
}

func (h *CombatHandler) combatDamageRolls() (rolls [4]int) {
	for index := range rolls {
		if h != nil && h.damageRoll != nil {
			rolls[index] = h.damageRoll()
		}
	}
	return rolls
}

func (h *CombatHandler) activeWeaponImbue(player *model.Player) (model.RefSkill, bool) {
	if h == nil || player == nil {
		return model.RefSkill{}, false
	}
	var selected model.ActiveBuff
	for _, buff := range player.ActiveBuffSnapshot() {
		skill, exists := h.referenceSkill(buff.SkillID)
		if !exists || !skill.IsWeaponImbue() || buff.Token <= selected.Token {
			continue
		}
		selected = buff
	}
	if selected.SkillID == 0 {
		return model.RefSkill{}, false
	}
	return h.referenceSkill(selected.SkillID)
}

func (h *CombatHandler) applyImbueStatuses(session *server.Session, player *model.Player, target *model.NPC, imbue model.RefSkill, now time.Time) uint32 {
	return h.applySkillStatuses(session, player, target, imbue, now)
}

func (h *CombatHandler) applySkillStatuses(session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill, now time.Time) uint32 {
	if h == nil || player == nil || target == nil {
		return 0
	}
	effects := skill.StatusEffects
	if len(effects) == 0 && skill.ShockPower > 0 {
		effects = []model.SkillStatusEffect{{Kind: model.CombatStatusShock, Power: skill.ShockPower, Chance: skill.ShockChance, DurationMS: skill.ShockDuration}}
	}
	roll := h.rollPercent
	if roll == nil {
		roll = func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance }
	}
	appliedBadEffects := uint32(0)
	for _, effect := range effects {
		if effect.Kind == 0 || effect.Chance <= 0 || effect.DurationMS <= 0 || !roll(effect.Chance) {
			continue
		}
		status := model.CombatStatus{
			Kind: effect.Kind, Token: model.NextActionUID(), SourceUID: player.GetUniqueID(), SkillID: skill.ID,
			Power: effect.Power, Level: effect.Level, Payload: effect.Payload,
			ExpiresAt: now.Add(time.Duration(effect.DurationMS) * time.Millisecond), Stackable: skill.LinkedSameGroup,
		}
		if !target.ApplyCombatStatusAt(status, now) {
			continue
		}
		if h.world != nil && statusRestrainsMovement(effect.Kind) {
			h.world.StopMovingCharacter(target, target.GetNavmeshPosition(), "combat-status-control", true)
		}
		if effect.Kind == model.CombatStatusFrostbite && h.retimeMob != nil {
			h.retimeMob(target, now)
		}
		badEffect := model.CombatStatusBadEffectBit(effect.Kind)
		if badEffect != 0 {
			appliedBadEffects |= badEffect
			h.scheduleBadEffectExpiration(player, target, status)
		} else {
			// Skill-authored stat debuffs (for example Vital Spot defense down)
			// remain B0BD buffs. Elemental abnormal states use 0x3057 below.
			packet := model.BuildTimedBuffAdd(target.GetUniqueID(), skill.ID, status.Token, uint32(effect.DurationMS))
			h.publishCombatPacket(player, &packet)
		}
		if effect.Kind == model.CombatStatusBurn || effect.Kind == model.CombatStatusPoison || effect.Kind == model.CombatStatusBleed {
			h.scheduleStatusDamageTick(session, player, target, status, now.Add(5*time.Second))
		}
	}
	if appliedBadEffects != 0 {
		h.publishBadEffectState(player, target, now)
	}
	return appliedBadEffects
}

func statusRestrainsMovement(kind model.CombatStatusKind) bool {
	switch kind {
	case model.CombatStatusFreeze, model.CombatStatusStun, model.CombatStatusSleep,
		model.CombatStatusBind, model.CombatStatusPetrify:
		return true
	default:
		return false
	}
}

func (h *CombatHandler) publishCombatPacket(player *model.Player, packet *network.Packet) {
	if h == nil || player == nil || packet == nil {
		return
	}
	if h.broadcast != nil {
		h.broadcast(player, packet)
		return
	}
	player.SendToSelfAndKnown(packet)
}

func (h *CombatHandler) publishBadEffectState(player *model.Player, target *model.NPC, now time.Time) {
	if target == nil {
		return
	}
	mask, levels := target.CombatBadEffectState(now)
	packet := model.BuildBadEffectUpdate(target.GetUniqueID(), mask, levels)
	h.publishCombatPacket(player, &packet)
}

func (h *CombatHandler) scheduleBadEffectExpiration(player *model.Player, target *model.NPC, expected model.CombatStatus) {
	if h == nil || h.schedule == nil || player == nil || target == nil {
		return
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	delay := expected.ExpiresAt.Sub(now)
	if delay < 0 {
		delay = 0
	}
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		current := time.Now()
		if h.now != nil {
			current = h.now()
		}
		_, active := target.CombatStatusByToken(expected.Kind, expected.Token, current)
		if active {
			return
		}
		h.publishBadEffectState(player, target, current)
		if expected.Kind == model.CombatStatusFrostbite && h.retimeMob != nil {
			h.retimeMob(target, current)
		}
	})
}

func (h *CombatHandler) applySkillTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time) error {
	return h.applyTransfers(ctx, session, player, primary, skill, now, h.skillStrikeDamage(player, primary, skill))
}

func (h *CombatHandler) scheduleStatusDamageTick(session *server.Session, player *model.Player, target *model.NPC, expected model.CombatStatus, tickAt time.Time) {
	if h == nil || h.schedule == nil || player == nil || target == nil || !tickAt.Before(expected.ExpiresAt) {
		return
	}
	delay := time.Until(tickAt)
	if h.now != nil {
		delay = tickAt.Sub(h.now())
	}
	if delay < 0 {
		delay = 0
	}
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		current := time.Now()
		if h.now != nil {
			current = h.now()
		}
		status, active := target.CombatStatusByToken(expected.Kind, expected.Token, current)
		if !active || target.GetLifeState() != model.Alive {
			return
		}
		payload := status.Payload
		if payload <= 0 {
			payload = status.Power
		}
		result := h.applyPlayerDamageResult(player, target, max(1, payload))
		if result.appliedDamage == 0 {
			return
		}
		castUID := model.NextActionUID()
		// Periodic abnormal damage is target-owned. Reusing the original
		// player's UID makes the stock client replay that player's attack
		// animation every five seconds, even after knockback moved the target.
		packet := model.BuildAttackBeginWithSkillHits(status.SkillID, target.GetUniqueID(), target.GetUniqueID(), castUID, []uint32{uint32(result.wireDamage)}, result.killed)
		_ = h.emitAttack(player, &packet)
		if result.killed {
			_ = h.awardPlayerKill(context.Background(), session, player, target, target.GetUniqueID())
			return
		}
		h.scheduleStatusDamageTick(session, player, target, status, current.Add(5*time.Second))
	})
}

func (h *CombatHandler) applyImbueTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, imbue model.RefSkill, now time.Time) error {
	return h.applyTransfers(ctx, session, player, primary, imbue, now, h.weaponImbueDamage(player, primary, imbue))
}

func (h *CombatHandler) applyTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time, damage int) error {
	targets, err := h.resolveTransfers(ctx, session, player, primary, skill, now, damage)
	if err != nil {
		return err
	}
	for _, target := range targets {
		castUID := model.NextActionUID()
		packet := model.BuildAttackBeginWithSkillDamageRecords(skill.ID, player.GetUniqueID(), target.TargetUID, castUID, target.Records)
		if err := h.emitAttack(player, &packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *CombatHandler) resolveTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time, damage int) ([]model.CombatDamageTarget, error) {
	// REF_SKILL's transfer count is the simultaneous target count, including
	// the primary target. A value of five therefore permits four nearby mobs.
	secondaryCount := skill.TransferCount - 1
	if h == nil || player == nil || primary == nil || secondaryCount <= 0 || skill.TransferRange <= 0 || damage <= 0 {
		return nil, nil
	}
	candidates := h.transferTargets(player, primary, skill, secondaryCount)
	targets := make([]model.CombatDamageTarget, 0, len(candidates))
	falloff := min(max(skill.TransferFalloffPercent, 0), 100)
	// Each native geometry producer initializes its byte accumulator to
	// 100-falloff, accounting for the primary hit, and multiplies it once more
	// immediately before appending the first transferred target.
	factor := 100 - falloff
	for _, candidate := range candidates {
		factor = nextTransferFactor(factor, falloff)
		transferDamage := max(1, int(math.Round(float64(damage)*float64(factor)/100)))
		result := h.resolvePlayerDamageWithAggro(player, candidate, transferDamage, skill.IsMagicalAttack(), skill.AbsoluteDamage > 0, now, skill.AggroIncrease, skill.AggroIncreasePercent, skill.AttackPercent)
		effectMask := uint32(0)
		if result.appliedDamage > 0 {
			effectMask = h.applySkillStatuses(session, player, candidate, skill, now)
			if err := h.restoreDamageVitals(ctx, session, player, skill, result.appliedDamage); err != nil {
				return nil, err
			}
		}
		record := result.combatRecord(effectMask)
		if result.appliedDamage > 0 && !result.killed {
			h.applySkillDisplacement(player, candidate, skill, &record)
		}
		targets = append(targets, model.CombatDamageTarget{TargetUID: candidate.GetUniqueID(), Records: []model.CombatDamageRecord{record}})
		if result.killed {
			if err := h.awardPlayerKill(ctx, session, player, candidate, candidate.GetUniqueID()); err != nil {
				return nil, err
			}
		}
	}
	return targets, nil
}

func mergeCombatDamageTargets(targets, additions []model.CombatDamageTarget) []model.CombatDamageTarget {
	for _, addition := range additions {
		merged := false
		for index := range targets {
			if targets[index].TargetUID == addition.TargetUID {
				targets[index].Records = append(targets[index].Records, addition.Records...)
				merged = true
				break
			}
		}
		if !merged {
			targets = append(targets, addition)
		}
	}
	return targets
}

func nextTransferFactor(current, falloff int) int {
	current = min(max(current, 0), 100)
	falloff = min(max(falloff, 0), 100)
	return int(math.Round(float64(current*(100-falloff)) / 100))
}

func scaleTransferAmount(amount, factor int) int {
	if amount <= 0 || factor <= 0 {
		return 0
	}
	return int(math.Round(float64(amount*min(factor, 100)) / 100))
}

type skillTransferCandidate struct {
	npc    *model.NPC
	metric float32
}

func (h *CombatHandler) transferTargets(player *model.Player, primary *model.NPC, skill model.RefSkill, maximum int) []*model.NPC {
	if player == nil || primary == nil || maximum <= 0 || player.GetKnownObjectList() == nil {
		return nil
	}
	// Native types 5 and 7 enumerate owner/runtime-group collections. They must
	// not silently fall back to arbitrary visible monsters. A configured owner
	// provider is the only supported source for those candidates.
	if skill.TransferType == 5 || skill.TransferType == 7 {
		if h == nil || h.ownerTargets == nil {
			return nil
		}
		provided := h.ownerTargets(primary, skill, maximum)
		result := make([]*model.NPC, 0, min(maximum, len(provided)))
		seen := make(map[uint32]struct{}, len(provided))
		for _, candidate := range provided {
			if candidate == nil || candidate == primary || candidate.GetLifeState() != model.Alive || !candidate.GetTypeInfo().IsNPCMob() {
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
	pool := make([]*model.NPC, 0)
	for _, object := range player.GetKnownObjectList().GetKnownObjects() {
		npc, ok := object.(*model.NPC)
		if ok && npc != nil && npc != primary && npc.GetLifeState() == model.Alive && npc.GetTypeInfo().IsNPCMob() {
			pool = append(pool, npc)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].GetUniqueID() < pool[j].GetUniqueID() })
	radius := float32(skill.TransferRange)
	if skill.TransferType == 6 {
		result := make([]*model.NPC, 0, maximum)
		current := primary
		for len(result) < maximum {
			selected, selectedIndex, best := (*model.NPC)(nil), -1, float32(math.MaxFloat32)
			for index, candidate := range pool {
				distance := navmeshv2.HorizontalDistance(current.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				if distance <= radius && (distance < best || distance == best && (selected == nil || candidate.GetUniqueID() < selected.GetUniqueID())) {
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

	center := primary.GetNavmeshPosition()
	if skill.TransferType == 1 {
		center = player.GetNavmeshPosition()
	}
	candidates := make([]skillTransferCandidate, 0, len(pool))
	if skill.TransferType == 3 || skill.TransferType == 4 {
		sourceGlobal := player.GetNavmeshPosition().GetGlobalCoordinates()
		primaryGlobal := primary.GetNavmeshPosition().GetGlobalCoordinates()
		dx, dz := primaryGlobal.X-sourceGlobal.X, primaryGlobal.Z-sourceGlobal.Z
		length := float32(math.Hypot(float64(dx), float64(dz)))
		if length <= 0 {
			return nil
		}
		ux, uz := dx/length, dz/length
		forwardLimit := max(length, float32(max(skill.ActionRange, skill.TransferRange)))
		for _, candidate := range pool {
			global := candidate.GetNavmeshPosition().GetGlobalCoordinates()
			cx, cz := global.X-sourceGlobal.X, global.Z-sourceGlobal.Z
			projection := cx*ux + cz*uz
			lateral := float32(math.Abs(float64(cx*uz - cz*ux)))
			if projection >= 0 && projection <= forwardLimit && lateral <= radius {
				candidates = append(candidates, skillTransferCandidate{npc: candidate, metric: projection})
			}
		}
	} else {
		for _, candidate := range pool {
			distance := navmeshv2.HorizontalDistance(center, candidate.GetNavmeshPosition())
			if distance <= radius {
				candidates = append(candidates, skillTransferCandidate{npc: candidate, metric: distance})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].metric < candidates[j].metric || candidates[i].metric == candidates[j].metric && candidates[i].npc.GetUniqueID() < candidates[j].npc.GetUniqueID()
	})
	if len(candidates) > maximum {
		candidates = candidates[:maximum]
	}
	result := make([]*model.NPC, len(candidates))
	for index := range candidates {
		result[index] = candidates[index].npc
	}
	return result
}

func (h *CombatHandler) applySkillDisplacement(source *model.Player, target *model.NPC, skill model.RefSkill, record *model.CombatDamageRecord) bool {
	if h == nil || h.world == nil || source == nil || target == nil || record == nil {
		return false
	}
	distance := float32(0)
	kind := byte(0)
	if skill.KnockOverKey != 0 {
		// SR_GameServer's fixed knock-over scalar at 0x00B45AC8 is the
		// little-endian IEEE-754 double 20.0.
		distance, kind = 20, model.DamageResultKnockOver
	} else if skill.KnockbackDistance != 0 {
		distance, kind = float32(skill.KnockbackDistance), model.DamageResultKnockback
	}
	if distance <= 0 {
		return false
	}
	chance := skill.KnockbackChance
	if kind == model.DamageResultKnockOver {
		chance = skill.KnockOverChance
	}
	if chance > 0 {
		if h.rollPercent == nil || !h.rollPercent(chance) {
			return false
		}
	}
	sourceGlobal := source.GetNavmeshPosition().GetGlobalCoordinates()
	targetPosition := target.GetNavmeshPosition()
	targetGlobal := targetPosition.GetGlobalCoordinates()
	dx, dz := targetGlobal.X-sourceGlobal.X, targetGlobal.Z-sourceGlobal.Z
	length := float32(math.Hypot(float64(dx), float64(dz)))
	if length <= 0 {
		radians := model.HeadingToRadians(source.GetNavmeshPosition().Heading)
		dx, dz, length = float32(math.Cos(float64(radians))), float32(math.Sin(float64(radians))), 1
	}
	destination, err := h.world.NewPosFromGlobalCoordinates(math32.NewVector3(
		targetGlobal.X+dx/length*distance,
		targetGlobal.Y,
		targetGlobal.Z+dz/length*distance,
	))
	if err != nil || !h.world.CanMoveDirectly(targetPosition, destination) {
		return false
	}
	h.world.StopMovingCharacter(target, destination, "skill-displacement", false)
	target.SetNavmeshPosition(destination)
	// Combat execution owns h.mu, including delayed impact callbacks. Taking
	// the same mutex again here deadlocks displacement skills before B071.
	interruptMob := h.interruptMob
	if interruptMob != nil {
		interruptMob(target)
	}
	record.AttackState = record.AttackState&model.DamageAttackStateDead | kind
	record.EndpointRegion = uint16(destination.Region.ID)
	record.EndpointX = destination.Offset.X
	record.EndpointY = destination.Offset.Y
	record.EndpointZ = destination.Offset.Z
	return true
}

func (h *CombatHandler) applyPlayerSkillDisplacement(source, target *model.Player, skill model.RefSkill, record *model.CombatDamageRecord) bool {
	if h == nil || h.world == nil || source == nil || target == nil || record == nil {
		return false
	}
	distance := float32(0)
	kind := byte(0)
	if skill.KnockOverKey != 0 {
		distance, kind = 20, model.DamageResultKnockOver
	} else if skill.KnockbackDistance != 0 {
		distance, kind = float32(skill.KnockbackDistance), model.DamageResultKnockback
	}
	if distance <= 0 {
		return false
	}
	chance := skill.KnockbackChance
	if kind == model.DamageResultKnockOver {
		chance = skill.KnockOverChance
	}
	if chance > 0 {
		if h.rollPercent == nil || !h.rollPercent(chance) {
			return false
		}
	}
	sourceGlobal := source.GetNavmeshPosition().GetGlobalCoordinates()
	targetPosition := target.GetNavmeshPosition()
	targetGlobal := targetPosition.GetGlobalCoordinates()
	dx, dz := targetGlobal.X-sourceGlobal.X, targetGlobal.Z-sourceGlobal.Z
	length := float32(math.Hypot(float64(dx), float64(dz)))
	if length <= 0 {
		radians := model.HeadingToRadians(source.GetNavmeshPosition().Heading)
		dx, dz, length = float32(math.Cos(float64(radians))), float32(math.Sin(float64(radians))), 1
	}
	destination, err := h.world.NewPosFromGlobalCoordinates(math32.NewVector3(targetGlobal.X+dx/length*distance, targetGlobal.Y, targetGlobal.Z+dz/length*distance))
	if err != nil || !h.world.CanMoveDirectly(targetPosition, destination) {
		return false
	}
	h.world.StopMovingCharacter(target, destination, "skill-displacement", false)
	target.SetNavmeshPosition(destination)
	h.interruptPlayerActionLocked(target.GetUniqueID())
	_ = h.sendActionState(target.Session, false)
	record.AttackState = record.AttackState&model.DamageAttackStateDead | kind
	record.EndpointRegion = uint16(destination.Region.ID)
	record.EndpointX = destination.Offset.X
	record.EndpointY = destination.Offset.Y
	record.EndpointZ = destination.Offset.Z
	return true
}

func (h *CombatHandler) restoreDamageVitals(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill, appliedDamage int) error {
	if h == nil || h.vitals == nil || player == nil || appliedDamage <= 0 || skill.DamageToHPPercent <= 0 && skill.DamageToMPPercent <= 0 {
		return nil
	}
	hpAmount := appliedDamage * max(0, skill.DamageToHPPercent) / 100
	mpAmount := appliedDamage * max(0, skill.DamageToMPPercent) / 100
	expectedHP, expectedMP, healedHP, healedMP, ok := player.PreviewHealing(hpAmount, mpAmount)
	if !ok || healedHP == expectedHP && healedMP == expectedMP {
		return nil
	}
	if err := h.vitals.SaveVitals(ctx, player.ID, healedHP, healedMP); err != nil {
		return err
	}
	if !player.CommitHealing(expectedHP, expectedMP, healedHP, healedMP) {
		return nil
	}
	packet := model.BuildVitalsUpdatePacket(player.GetUniqueID(), healedHP, healedMP)
	return h.sendPlayerPacket(session, packet)
}

func (h *CombatHandler) sendPlayerPacket(session *server.Session, packet network.Packet) error {
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return sendCombatPacket(session, packet)
}

func skillCooldownAt(skill model.RefSkill, actionTimePercent float64) time.Duration {
	lifecycle := skill.RuntimeCastingDurationAt(actionTimePercent) + time.Duration(max(0, skill.ActionDuration))*time.Millisecond
	reuse := time.Duration(max(0, skill.ReuseDelay)) * time.Millisecond
	if reuse > lifecycle {
		return reuse
	}
	return lifecycle
}

func skillCooldown(skill model.RefSkill) time.Duration {
	return skillCooldownAt(skill, 100)
}

func (h *CombatHandler) skillAvailable(key skillCooldownKey, now time.Time) bool {
	if h == nil {
		return false
	}
	if next := h.nextSkill[key]; !next.IsZero() && now.Before(next) {
		return false
	}
	if next := h.nextCoolTime[key.playerUID]; !next.IsZero() && now.Before(next) {
		return false
	}
	return true
}

// registerSkillCooldown preserves the fixed 100% timing helper used by
// package-level combat tests and non-player callers.
func (h *CombatHandler) registerSkillCooldown(key skillCooldownKey, skill model.RefSkill, now time.Time) {
	h.registerSkillCooldownForPlayer(nil, key, skill, now)
}

func (h *CombatHandler) registerSkillCooldownForPlayer(player *model.Player, key skillCooldownKey, skill model.RefSkill, now time.Time) {
	if h == nil {
		return
	}
	if h.nextSkill == nil {
		h.nextSkill = make(map[skillCooldownKey]time.Time)
	}
	if reuse := skill.ReuseDuration(h.skillActionTimePercent(player, skill)); reuse > 0 {
		h.nextSkill[key] = now.Add(reuse)
		if reuse >= 300*time.Second && h.longCooldown != nil {
			h.longCooldown(player, skill, now.Add(reuse))
		}
	}
	if h.nextCoolTime == nil {
		h.nextCoolTime = make(map[uint32]time.Time)
	}
	if coolTime := skill.CoolTimeDuration(); coolTime > 0 {
		h.nextCoolTime[key.playerUID] = now.Add(coolTime)
	}
}

func (h *CombatHandler) applyPlayerDamage(player *model.Player, target *model.NPC, damage int, damageFlags ...bool) (int, bool) {
	result := h.applyPlayerDamageResult(player, target, damage, damageFlags...)
	return result.wireDamage, result.killed
}

type playerDamageResult struct {
	wireDamage    int
	appliedDamage int
	killed        bool
	attackState   byte
	damageState   byte
}

func (r playerDamageResult) combatRecord(effectMask uint32) model.CombatDamageRecord {
	attackState := r.attackState
	if r.killed {
		attackState |= model.DamageAttackStateDead
	}
	damageState := r.damageState
	if damageState == 0 {
		damageState = model.DamageStateNormal
	}
	return model.CombatDamageRecord{
		AttackState: attackState,
		DamageState: damageState,
		Damage:      uint32(r.wireDamage),
		EffectMask:  effectMask,
	}
}

func (h *CombatHandler) resolvePlayerDamage(player *model.Player, target *model.NPC, damage int, magical, absolute bool, now time.Time) playerDamageResult {
	return h.resolvePlayerDamageWithAggro(player, target, damage, magical, absolute, now, 0, 0)
}

func (h *CombatHandler) resolvePlayerDamageWithAggro(player *model.Player, target *model.NPC, damage int, magical, absolute bool, now time.Time, aggroFlat, aggroPercent int, attackPercent ...int) playerDamageResult {
	if h.sourceUsesStateOnlyResult(player, now) {
		return playerDamageResult{attackState: model.DamageResultSourceState}
	}
	if targetUsesStateOnlyResult(target) {
		if target != nil && target.TypeInfo.IsSiegeStruct() && h.fortressDamage != nil && damage > 0 {
			result, err := h.fortressDamage(context.Background(), target.GetUniqueID(), player.ID, uint32(damage), now)
			if err != nil {
				return playerDamageResult{attackState: model.DamageAttackStateBlock}
			}
			update, buildErr := vsro188.BuildSiegeStructureStateUpdate(uint32(result.Structure.FortressID), result.Structure.UniqueID, result.Structure.RefEventID, result.Structure.State, result.Structure.Heart, "")
			if buildErr == nil && h.broadcast != nil {
				h.broadcast(player, &update)
			}
		}
		return playerDamageResult{attackState: model.DamageResultTargetState}
	}
	if !absolute && h.playerAttackBlocked(target) {
		return playerDamageResult{attackState: model.DamageAttackStateBlock}
	}
	critical := !magical && !absolute && h.playerAttackCritical(player)
	percent := 100
	if len(attackPercent) != 0 && attackPercent[0] > 0 {
		percent = attackPercent[0]
	}
	return h.applyPlayerDamageResultWithAggroPercent(player, target, damage, aggroFlat, aggroPercent, percent, magical, absolute, critical)
}

// targetUsesStateOnlyResult reproduces the two permanent packed-TypeID
// predicates selected by SR_GameServer's hit-result producer. The packed
// layouts decode to NPC siege object subtype 2 and every NPC siege structure.
func targetUsesStateOnlyResult(target *model.NPC) bool {
	if target == nil {
		return false
	}
	typeInfo := target.TypeInfo
	return typeInfo.IsSiegeStruct() || typeInfo.IsSiegeObject() && typeInfo.TypeID4 == 2
}

func (h *CombatHandler) sourceUsesStateOnlyResult(player *model.Player, now time.Time) bool {
	if h == nil || player == nil {
		return false
	}
	for _, buff := range player.ActiveBuffSnapshot() {
		if !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		skill, exists := h.referenceSkill(buff.SkillID)
		if exists && skill.MatchMode == 1 {
			return true
		}
	}
	return false
}

func (h *CombatHandler) playerAttackBlocked(target *model.NPC) bool {
	if h == nil || target == nil || h.referenceData == nil || h.blockRoll == nil {
		return false
	}
	character, exists := h.referenceData.Character(target.GetRefObjectID())
	return exists && character.BlockRate > 0 && h.blockRoll(character.BlockRate)
}

func (h *CombatHandler) playerAttackCritical(player *model.Player) bool {
	if h == nil || player == nil || h.criticalRoll == nil {
		return false
	}
	criticalRate := player.DerivedStats().CriticalRate
	return criticalRate > 0 && h.criticalRoll(criticalRate)
}

func (h *CombatHandler) applyPlayerDamageResult(player *model.Player, target *model.NPC, damage int, damageFlags ...bool) playerDamageResult {
	return h.applyPlayerDamageResultWithAggro(player, target, damage, 0, 0, damageFlags...)
}

func (h *CombatHandler) applyPlayerDamageResultWithAggro(player *model.Player, target *model.NPC, damage, aggroFlat, aggroPercent int, damageFlags ...bool) playerDamageResult {
	return h.applyPlayerDamageResultWithAggroPercent(player, target, damage, aggroFlat, aggroPercent, 100, damageFlags...)
}

func (h *CombatHandler) applyPlayerDamageResultWithAggroPercent(player *model.Player, target *model.NPC, damage, aggroFlat, aggroPercent, attackPercent int, damageFlags ...bool) playerDamageResult {
	now := time.Now()
	if h != nil && h.now != nil {
		now = h.now()
	}
	isMagical := len(damageFlags) > 0 && damageFlags[0]
	isAbsolute := len(damageFlags) > 1 && damageFlags[1]
	isCritical := len(damageFlags) > 2 && damageFlags[2]
	defense := 0
	absorption := 0
	if !isAbsolute && h != nil && h.referenceData != nil {
		if character, exists := h.referenceData.Character(target.GetRefObjectID()); exists {
			if isMagical {
				defense = character.MagDef
				absorption = character.MagAbsorbRate
			} else {
				defense = character.PhyDef
				absorption = character.PhyAbsorbRate
			}
		}
	}
	defenseStatus := model.CombatStatusPhysicalDefenseDown
	if isMagical {
		defenseStatus = model.CombatStatusMagicalDefenseDown
	}
	if !isAbsolute {
		if status, active := target.CombatStatus(defenseStatus, now); active {
			defense = max(0, defense-status.Power)
		}
		damage = max(1, int(model.CombatMitigatedDamage(float32(damage), float32(defense), float32(absorption))))
		damage = damage * attackPercent / 100
		// SR_GameServer 0x0040E450/0x0040E830 consumes urd through stat
		// slots 0x80-0x83 after mitigation, not in the attack range that
		// feeds variance.
		damage += damage * player.ActiveCombatDamagePercent() / 100
	}
	if status, active := target.CombatStatus(model.CombatStatusDamageTakenUp, now); active && !isAbsolute {
		// tisc abnormal type 0x14 dispatches through 0x004A50B0. The handler
		// writes the same authored magnitude, with modifier type 2 and owner
		// key 5, to physical slot 0xB4 and magical slot 0xB5.
		damage = model.CombatFinalDamageFactor(damage, status.Power)
	}
	if isCritical {
		damage *= 2
	}
	applied, killed := target.ApplyDamage(damage)
	if applied == 0 {
		return playerDamageResult{}
	}
	// A successful hit establishes reciprocal visibility before aggro so a
	// passive mob can resolve its attacker between visibility ticks.
	if known := target.GetKnownObjectList(); known != nil && !known.Knows(player) {
		known.AddObject(player)
	}
	// SR_GameServer 0x0058E5F0: 2tnt threat is
	// round(base * (1 + percent/100)) + flat.
	playerAggro := model.CombatThreat(applied, aggroFlat, aggroPercent)
	if sourceUID, percent, active := player.AggroAbsorber(h.referenceData); active {
		absorbed := playerAggro * percent / 100
		playerAggro -= absorbed
		target.AddAggro(sourceUID, absorbed)
	}
	target.AddAggro(player.GetUniqueID(), playerAggro)
	if killed {
		h.world.StopMovingCharacter(target, target.GetNavmeshPosition(), "mob-killed", false)
		target.QueueRespawn()
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, fn func()) { time.AfterFunc(delay, fn) }
		}
		schedule(target.DecayDuration(), func() { h.world.RemoveVisibleObject(target) })
	}
	damageState := model.DamageStateNormal
	if isCritical {
		damageState = model.DamageStateCritical
	}
	return playerDamageResult{
		wireDamage:    min(damage, maximumWireDamage),
		appliedDamage: applied,
		killed:        killed,
		damageState:   damageState,
	}
}

func (h *CombatHandler) playerAttackLands(player *model.Player, target *model.NPC, now time.Time) bool {
	if h == nil || player == nil || target == nil || h.referenceData == nil {
		return true
	}
	character, exists := h.referenceData.Character(target.GetRefObjectID())
	if !exists {
		return true
	}
	parry := character.EvasionRate
	if status, active := target.CombatStatus(model.CombatStatusParryDown, now); active {
		parry = max(0, parry-status.Power)
	}
	if _, active := target.CombatStatus(model.CombatStatusShock, now); active {
		// Shock is a parry impairment, not a stun. Its state level controls
		// resistance; the applied state halves the remaining parry rate.
		parry /= 2
	}
	chance := model.CombatHitChance(player.DerivedStats().HitRate, parry)
	if h.hitRollPercent == nil {
		return true
	}
	return h.hitRollPercent(chance)
}

func (h *CombatHandler) awardPlayerKill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, targetUID uint32) error {
	if h != nil && h.party != nil {
		recipients := h.party.ExperienceRecipients(player.GetUniqueID())
		if len(recipients) > 1 {
			return h.awardPartyKill(ctx, player, target, targetUID, recipients)
		}
	}
	return h.awardKill(ctx, session, player, target, targetUID, true)
}

func (h *CombatHandler) awardPartyKill(ctx context.Context, killer *model.Player, target *model.NPC, targetUID uint32, recipients []*model.Player) error {
	if h == nil || target == nil || len(recipients) == 0 {
		return ErrCombatTargetInvalid
	}
	if h.rates == nil {
		return fmt.Errorf("gameplay progression rates are unavailable")
	}
	if h.progress == nil {
		return model.ErrCharacterProgressRepositoryUnavailable
	}

	baseReward := target.ExperienceReward()
	rates := h.rates.Current()
	partyExperienceRate := partyExperienceRate(rates)
	experienceShares := service.SharedRewardShares(scaleCombatReward(baseReward, int(partyExperienceRate)), len(recipients))
	skillShares := service.SharedRewardShares(scaleCombatReward(baseReward, int(rates.SkillExperienceRatePercent)), len(recipients))
	jobRecipients := combatJobRecipients(killer, recipients)
	if !h.isJobExperienceTarget(target) {
		jobRecipients = nil
	}
	totalJobLevel := 0
	for _, recipient := range jobRecipients {
		totalJobLevel += recipient.Level
	}
	for index, recipient := range recipients {
		if recipient == nil || recipient.GetSession() == nil {
			continue
		}
		experienceBonus, skillExperienceBonus := recipient.ItemServiceRates(time.Now().UTC())
		reward := scaleCombatReward(experienceShares[index], 100+int(experienceBonus))
		skillReward := scaleCombatReward(skillShares[index], 100+int(skillExperienceBonus))
		progress := recipient.PreviewExperienceAward(h.levels, reward, skillReward)
		if err := h.progress.SaveProgress(ctx, recipient.ID, progress); err != nil {
			return err
		}
		recipient.CommitProgress(progress)
		h.tryAccumulateAcademyExperience(ctx, recipient, reward)
		if err := sendExperienceUpdate(recipient.GetSession(), targetUID, reward, skillReward, progress); err != nil {
			return err
		}
		publishLevelUpAnimation(recipient, progress)
		if slices.Contains(jobRecipients, recipient) {
			base := model.CombatJobExperience(h.levels, recipient.Level, h.combatTargetLevel(target), recipient.GetJob().Type)
			jobReward := model.PartyCombatJobExperience(base, recipient.Level, totalJobLevel, len(jobRecipients))
			h.tryAwardCombatJobExperience(ctx, recipient, jobReward, targetUID)
		}
	}

	// Kill-side effects remain owned by the player whose attack resolved the
	// death. Quest and HWAN records are character-specific and must not be
	// duplicated merely because EXP was shared.
	return h.finishKill(ctx, killer.GetSession(), killer, target, targetUID, true)
}

func partyExperienceRate(rates model.GameplayRates) uint32 {
	if rates.PartyExperienceRatePercent != 0 {
		return rates.PartyExperienceRatePercent
	}
	// Keep injected legacy rate providers safe until they expose the binary's
	// separate party setting. Production rates are loaded from the migrated
	// GAMEPLAY_PROGRESSION_RATES column above.
	return rates.ExperienceRatePercent
}

func (h *CombatHandler) awardKill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, targetUID uint32, shareGrowthPetExperience bool) error {
	baseReward := target.ExperienceReward()
	if h.rates == nil {
		return fmt.Errorf("gameplay progression rates are unavailable")
	}
	rates := h.rates.Current()
	experienceRate := int(rates.ExperienceRatePercent)
	skillExperienceRate := int(rates.SkillExperienceRatePercent)
	reward := scaleCombatReward(baseReward, experienceRate)
	skillReward := scaleCombatReward(baseReward, skillExperienceRate)
	experienceBonus, skillExperienceBonus := player.ItemServiceRates(time.Now().UTC())
	reward = scaleCombatReward(reward, 100+int(experienceBonus))
	skillReward = scaleCombatReward(skillReward, 100+int(skillExperienceBonus))
	progress := player.PreviewExperienceAward(h.levels, reward, skillReward)
	if h.progress == nil {
		return model.ErrCharacterProgressRepositoryUnavailable
	}
	if err := h.progress.SaveProgress(ctx, player.ID, progress); err != nil {
		return err
	}
	player.CommitProgress(progress)
	h.tryAccumulateAcademyExperience(ctx, player, reward)
	if err := sendExperienceUpdate(session, targetUID, reward, skillReward, progress); err != nil {
		return err
	}
	publishLevelUpAnimation(player, progress)
	if h.isJobExperienceTarget(target) {
		h.tryAwardCombatJobExperience(ctx, player, model.CombatJobExperience(h.levels, player.Level, h.combatTargetLevel(target), player.GetJob().Type), targetUID)
	}
	return h.finishKill(ctx, session, player, target, targetUID, shareGrowthPetExperience)
}

func (h *CombatHandler) tryAccumulateAcademyExperience(ctx context.Context, player *model.Player, amount uint64) {
	if h == nil || h.academyProgress == nil || player == nil || amount == 0 {
		return
	}
	states, err := h.academyProgress.AccumulateAcademyExperience(ctx, player.ID, amount)
	if err != nil {
		logrus.WithError(err).WithField("character_id", player.ID).Warn("academy experience accumulation failed")
		return
	}
	if len(states) != 0 && h.academyBuffs != nil {
		h.academyBuffs(states)
	}
}

func (h *CombatHandler) isJobExperienceTarget(target *model.NPC) bool {
	if h == nil || h.referenceData == nil || target == nil {
		return false
	}
	ref, exists := h.referenceData.Character(target.GetRefObjectID())
	if !exists {
		return false
	}
	code := strings.ToUpper(strings.TrimSpace(ref.CodeName))
	return strings.HasPrefix(code, "MOB_THIEF_NPC_") || strings.HasPrefix(code, "MOB_HUNTER_NPC_")
}

func (h *CombatHandler) combatTargetLevel(target *model.NPC) int {
	if h == nil || h.referenceData == nil || target == nil {
		return 0
	}
	character, exists := h.referenceData.Character(target.GetRefObjectID())
	if !exists {
		return 0
	}
	return character.Level
}

func combatJobRecipients(killer *model.Player, recipients []*model.Player) []*model.Player {
	if killer == nil || !killer.GetJob().Active() {
		return nil
	}
	killerJob := killer.GetJob().Type
	result := make([]*model.Player, 0, len(recipients))
	for _, recipient := range recipients {
		if recipient == nil || recipient.GetSession() == nil || !recipient.GetJob().Active() {
			continue
		}
		if killerJob == model.PlayerJobHunter && recipient.GetJob().Type != model.PlayerJobHunter {
			continue
		}
		result = append(result, recipient)
	}
	return result
}

func (h *CombatHandler) awardCombatJobExperience(ctx context.Context, player *model.Player, amount uint64) error {
	if h == nil || h.jobProgress == nil || player == nil || amount == 0 || !player.GetJob().Active() {
		return nil
	}
	snapshot, err := h.jobProgress.AdvanceJob(ctx, player.ID, amount, 0)
	if err != nil {
		return err
	}
	player.SetJob(model.PlayerJob{Type: snapshot.Type, Alias: snapshot.Alias, Level: snapshot.Level, Experience: snapshot.Experience})
	packet, err := (vsro188.JobUpdateCodec{}).Encode(vsro188.JobState{Type: snapshot.Type, Level: snapshot.Level, Experience: uint32(min(snapshot.Experience, math.MaxUint32))})
	if err != nil {
		return err
	}
	return h.sendPlayerPacket(player.GetSession(), packet)
}

// tryAwardCombatJobExperience preserves the stock update boundary: normal
// character EXP and the rest of the kill lifecycle have independent durable
// owners and must not be rolled back or skipped after a job update fails.
func (h *CombatHandler) tryAwardCombatJobExperience(ctx context.Context, player *model.Player, amount uint64, targetUID uint32) {
	if err := h.awardCombatJobExperience(ctx, player, amount); err != nil {
		characterID := 0
		if player != nil {
			characterID = player.ID
		}
		logrus.WithError(err).WithFields(logrus.Fields{"character_id": characterID, "target_uid": targetUID, "job_experience": amount}).Warn("combat job experience award failed")
	}
}

func (h *CombatHandler) finishKill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, targetUID uint32, shareGrowthPetExperience bool) error {
	if err := h.handleNativeQuestInstanceDeath(player, target); err != nil {
		// The native OnDead callback reports action failure to its quest engine;
		// it does not resurrect the already-dead mob or roll back ordinary kill
		// rewards. Keep that boundary here as well.
		logrus.WithError(err).WithField("target_uid", targetUID).Warn("native quest instance teleport submission failed")
	}
	if shareGrowthPetExperience && h.companions != nil {
		if err := h.companions.AwardOwnerKillExperience(ctx, player, target); err != nil {
			logrus.Warnf("growth pet EXP award failed owner=%d target=%d: %v", player.ID, targetUID, err)
		}
	}
	if h.hwan != nil && h.referenceData != nil {
		character, exists := h.referenceData.Character(target.GetRefObjectID())
		if exists {
			reward := model.HwanRewardForMonsterKill(player.Level, character.Level, target.MonsterRarity())
			roll := h.rollHwanPoint
			if roll == nil {
				roll = func(chanceBasisPoints int) bool {
					return chanceBasisPoints >= 10000 || chanceBasisPoints > 0 && rand.Intn(10000) < chanceBasisPoints
				}
			}
			if roll(reward.ChanceBasisPoints) {
				snapshot, changed, err := h.hwan.AwardPoints(ctx, player.ID, reward.Points)
				if err != nil {
					return err
				}
				player.CommitHwanSnapshot(snapshot)
				if changed {
					points := model.BuildHwanPointUpdatePacket(snapshot.Points, targetUID)
					if err := h.sendPlayerPacket(session, points); err != nil {
						return err
					}
				}
			}
		}
	}
	h.dropsMu.RLock()
	drops := h.drops
	h.dropsMu.RUnlock()
	if drops != nil {
		if _, err := drops.SpawnForKill(ctx, player, target); err != nil {
			logrus.Warnf("monster drop materialization failed target=%d: %v", targetUID, err)
		}
		if resolver, ok := h.quests.(questCollectionDropRepository); ok {
			questDrops, err := resolver.CollectionDropsForKill(ctx, player.ID, target.GetRefObjectID())
			if err != nil {
				return err
			}
			for _, questDrop := range questDrops {
				if _, _, spawnErr := drops.SpawnQuestCollectionItem(ctx, player, target, questDrop); spawnErr != nil {
					logrus.Warnf("quest collection drop materialization failed quest=%d target=%d: %v", questDrop.QuestID, targetUID, spawnErr)
				}
			}
		}
	}
	if h.quests == nil {
		return nil
	}
	if err := h.recordUniqueDead(ctx, session, player, target.GetRefObjectID(), targetUID); err != nil {
		return err
	}
	questProgress, err := h.quests.RecordMobKill(ctx, player.ID, target.GetRefObjectID(), target.MonsterRarity())
	if err != nil {
		return err
	}
	for _, progress := range questProgress {
		if !progress.Changed {
			continue
		}
		player.CommitQuestMutation(progress.Quest)
		update, buildErr := buildQuestStatePacket(progress.Quest)
		if buildErr != nil {
			return buildErr
		}
		if h.send != nil {
			if sendErr := h.send(session, update); sendErr != nil {
				return sendErr
			}
		} else if sendErr := session.Send(update); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

func (h *CombatHandler) handleNativeQuestInstanceDeath(player *model.Player, target *model.NPC) error {
	if h == nil || player == nil || target == nil || h.questInstances == nil || h.instanceTeleport == nil {
		return nil
	}
	scope := model.GameWorldScope(player.GetGameWorldID())
	if !scope.ValidInstance() || target.GetGameWorldID() != uint32(scope) {
		return nil
	}
	instance, err := h.questInstances.Instance(scope)
	if err != nil {
		return err
	}
	variables, err := h.questInstances.Variables(scope)
	if err != nil {
		return err
	}
	elapsed := variables[model.NativeQuestInstanceElapsedTimeKey(instance.QuestID)]
	stagePlan, stageMatched, err := h.questInstances.PrepareNativeBaekryeongStageDeath(scope, target.GetName(), elapsed)
	if err != nil {
		return err
	}
	if stageMatched {
		_, err = h.questInstances.CommitNativeBaekryeongStageDeath(stagePlan)
		return err
	}
	plan, matched, err := h.questInstances.PrepareNativeQuestInstanceDeath(scope, instance.QuestID, target.GetName(), elapsed)
	if err != nil || !matched {
		return err
	}
	return h.instanceTeleport.SubmitNativeQuestInstanceTeleport(h.questInstances, plan, player)
}

func (h *CombatHandler) recordUniqueDead(ctx context.Context, session *server.Session, player *model.Player, monsterRefObjectID, targetUID uint32) error {
	uniqueDead, ok := h.quests.(questUniqueDeadRepository)
	if !ok || player == nil {
		return nil
	}
	result, granted, grantErr := uniqueDead.GrantUniqueDeadMissionItem(ctx, player.ID, monsterRefObjectID, player.GetNavmeshPosition().Region.ID)
	if grantErr != nil {
		if !errors.Is(grantErr, model.ErrQuestInventoryFull) {
			return grantErr
		}
		logrus.WithFields(logrus.Fields{"character_id": player.ID, "target_uid": targetUID}).Info("UNIQUE_DEAD quest item grant skipped: inventory full")
	} else if granted {
		player.CommitQuestItemGrant(model.QuestItemGrantResult{Quest: result.Quest, Inventory: result.Inventory, Added: result.Added})
		if result.RemovedSlot != nil {
			if sendErr := h.sendPlayerPacket(session, buildQuestItemRemoved(*result.RemovedSlot)); sendErr != nil {
				return sendErr
			}
			if *result.RemovedSlot != result.Added.Slot {
				if remaining, exists := result.Inventory.Items[*result.RemovedSlot]; exists {
					if sendErr := h.sendPlayerPacket(session, buildQuestItemAdded(model.QuestItemMutation{Slot: *result.RemovedSlot, Item: remaining})); sendErr != nil {
						return sendErr
					}
				}
			}
		}
		if sendErr := h.sendPlayerPacket(session, buildQuestItemAdded(result.Added)); sendErr != nil {
			return sendErr
		}
		update, buildErr := buildQuestStatePacket(result.Quest)
		if buildErr != nil {
			return buildErr
		}
		if sendErr := h.sendPlayerPacket(session, update); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

// AwardCOSKill routes a growth-pet kill through the owner's normal progression,
// HWAN, and quest pipeline while retaining the killed mob as the EXP source.
func (h *CombatHandler) AwardCOSKill(ctx context.Context, owner *model.Player, target *model.NPC) error {
	if h == nil || owner == nil || owner.Session == nil || target == nil {
		return ErrCombatTargetInvalid
	}
	return h.awardKill(ctx, owner.Session, owner, target, target.GetUniqueID(), false)
}

func (h *CombatHandler) baseAttackCooldown(player *model.Player) time.Duration {
	if h == nil || h.referenceData == nil || player == nil {
		return 0
	}
	skill, ok := h.referenceData.Skill(playerBaseAttackSkillID(player))
	if !ok {
		return 0
	}
	return skillCooldownAt(skill, h.skillActionTimePercent(player, skill))
}

func (h *CombatHandler) baseAttackSkill(player *model.Player) model.RefSkill {
	if h == nil || h.referenceData == nil || player == nil {
		return model.RefSkill{}
	}
	skill, _ := h.referenceData.Skill(playerBaseAttackSkillID(player))
	return skill
}

func (h *CombatHandler) actionRecoveryDuration(player *model.Player, skill model.RefSkill) time.Duration {
	if skill.ActionDuration <= 0 {
		return 0
	}
	percent := h.skillActionTimePercent(player, skill)
	milliseconds := int(math.Round(percent * float64(skill.ActionDuration) / 100))
	return time.Duration(max(0, milliseconds)) * time.Millisecond
}

func (h *CombatHandler) finishActionAfter(session *server.Session, delay time.Duration) {
	if delay <= 0 || h == nil || h.schedule == nil {
		_ = h.sendActionState(session, false)
		return
	}
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		_ = h.sendActionState(session, false)
	})
}

func (h *CombatHandler) resolveAttack(session *server.Session, targetUID uint32) (*model.Player, *model.NPC, error) {
	player, err := h.resolveCombatPlayer(session)
	if err != nil {
		return nil, nil, ErrCombatTargetInvalid
	}
	object, err := h.world.GetObjectByUniqueId(targetUID)
	if err != nil {
		return nil, nil, ErrCombatTargetInvalid
	}
	target, ok := object.(*model.NPC)
	if !ok || target == nil || !target.GetTypeInfo().IsNPCMob() || target.GetLifeState() != model.Alive {
		return nil, nil, ErrCombatTargetInvalid
	}
	known := player.GetKnownObjectList()
	if known == nil || known.GetKnownObjects()[targetUID] != target {
		return nil, nil, ErrCombatTargetInvalid
	}
	return player, target, nil
}

func (h *CombatHandler) resolveDimensionHolePortalInteraction(session *server.Session, targetUID uint32) (*model.Player, *model.DimensionHolePortal, error) {
	player, err := h.resolveCombatPlayer(session)
	if err != nil || targetUID == 0 || session.UserContext.CharacterID <= 0 || player.ID != session.UserContext.CharacterID || player.GetUniqueID() != session.UserContext.UniqueID {
		return nil, nil, ErrCombatTargetInvalid
	}
	object, err := h.world.GetObjectByUniqueId(targetUID)
	portal, isPortal := object.(*model.DimensionHolePortal)
	if err != nil || !isPortal || !model.IsDimensionHolePortalObject(portal) || portal.GetUniqueID() != targetUID || portal.GetGameWorldID() != player.GetGameWorldID() {
		return nil, nil, ErrCombatTargetInvalid
	}
	known := player.GetKnownObjectList()
	if known == nil {
		return nil, nil, ErrCombatTargetInvalid
	}
	knownPortal, knownAsPortal := known.GetKnownObjects()[targetUID].(*model.DimensionHolePortal)
	if !knownAsPortal || knownPortal != portal {
		return nil, nil, ErrCombatTargetInvalid
	}
	playerPosition := player.GetNavmeshPosition()
	portalPosition := portal.GetNavmeshPosition()
	if playerPosition.Offset == nil || portalPosition.Offset == nil {
		return nil, nil, ErrCombatTargetInvalid
	}
	distance := h.world.DistanceTo(playerPosition, portalPosition)
	if math.IsNaN(float64(distance)) || math.IsInf(float64(distance), 0) || distance < 0 || distance > float32(model.RegionHeight) {
		return nil, nil, ErrCombatTargetInvalid
	}
	return player, portal, nil
}

func (h *CombatHandler) resolveCombatPlayer(session *server.Session) (*model.Player, error) {
	if session == nil || session.UserContext.UniqueID == 0 || h.world == nil {
		return nil, ErrCombatTargetInvalid
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.GetLifeState() != model.Alive {
		return nil, ErrCombatTargetInvalid
	}
	return player, nil
}

func (h *CombatHandler) beginAttackPursuit(session *server.Session, player *model.Player, target *model.NPC, reach float32, skillID uint32) error {
	playerUID := player.GetUniqueID()
	if h.pending == nil {
		h.pending = make(map[uint32]*pendingAttack)
	}
	if current := h.pending[playerUID]; current != nil && current.session == session && current.targetUID == target.GetUniqueID() {
		// The original MoveToTarget task updates its destination when the same
		// moving target is requested again. Replacing the movement generation is
		// safe because the existing poll owns this same pending task.
		current.skillID = skillID
		current.reach = reach
		if err := h.restartAttackPursuit(player, target, current); err != nil {
			h.cancelPendingAttack(playerUID, true)
			return err
		}
		return nil
	}
	h.cancelPendingAttack(playerUID, true)

	pending := &pendingAttack{
		session: session, targetUID: target.GetUniqueID(), skillID: skillID, reach: reach,
	}
	if err := h.restartAttackPursuit(player, target, pending); err != nil {
		return err
	}
	h.pending[playerUID] = pending
	h.scheduleAttackPursuit(playerUID, pending)
	return nil
}

func (h *CombatHandler) restartAttackPursuit(player *model.Player, target *model.NPC, pending *pendingAttack) error {
	if h.world == nil || h.move == nil {
		return ErrCombatTargetInvalid
	}
	if !h.world.CanMoveDirectly(player.GetNavmeshPosition(), target.GetNavmeshPosition()) {
		return ErrCombatTargetInvalid
	}
	targetGlobal := target.GetNavmeshPosition().GetGlobalCoordinates()
	// Pursue the target's position instead of a stand-off point derived from
	// its latest snapshot. A moving target can leave that stand-off point
	// behind before the next retarget, which makes the player arrive, stop, and
	// start another movement leg. The pursuit poll stops this longer leg as soon
	// as the live target enters attack range.
	destination, err := h.world.NewPosFromGlobalCoordinates(targetGlobal)
	if err != nil {
		return err
	}
	pending.movementGeneration = h.move(player, destination)
	if pending.movementGeneration == 0 {
		return ErrCombatTargetInvalid
	}
	pending.lastTargetGlobal = targetGlobal.Clone()
	pending.lastRetarget = h.combatNow()
	return nil
}

func (h *CombatHandler) combatNow() time.Time {
	if h != nil && h.now != nil {
		return h.now()
	}
	return time.Now()
}

// beginDelayedCast reserves the actor's one in-flight delayed skill slot.
// Native action state is actor-owned, so a newer cast or explicit cancel must
// invalidate the older impact callback before it can apply damage/effects.
func (h *CombatHandler) beginDelayedCast(playerUID uint32, cancel ...func()) uint64 {
	if h == nil || playerUID == 0 {
		return 0
	}
	if h.delayedCasts == nil {
		h.delayedCasts = make(map[uint32]delayedCast)
	}
	token := uint64(model.NextActionUID())
	entry := delayedCast{token: token}
	if len(cancel) > 0 {
		entry.cancel = cancel[0]
	}
	// A rapid follow-up skill replaces the actor-owned cast slot. Finish the
	// superseded cast before installing the new token; otherwise its scheduled
	// callback is correctly ignored, but its cast-end/action-state cleanup is
	// lost and the stock client remains stuck in the cancelled action.
	if previous, exists := h.delayedCasts[playerUID]; exists && previous.cancel != nil {
		previous.cancel()
	}
	h.delayedCasts[playerUID] = entry
	return token
}

func (h *CombatHandler) consumeDelayedCast(playerUID uint32, token uint64) bool {
	if h == nil || playerUID == 0 || token == 0 || h.delayedCasts == nil {
		return false
	}
	entry, exists := h.delayedCasts[playerUID]
	if !exists || entry.token != token {
		return false
	}
	delete(h.delayedCasts, playerUID)
	return true
}

func (h *CombatHandler) cancelDelayedCast(playerUID uint32) {
	if h == nil || playerUID == 0 || h.delayedCasts == nil {
		return
	}
	entry, exists := h.delayedCasts[playerUID]
	delete(h.delayedCasts, playerUID)
	if exists && entry.cancel != nil {
		entry.cancel()
	}
}

func (h *CombatHandler) attackPursuitNeedsRetarget(target *model.NPC, pending *pendingAttack) bool {
	if target == nil || pending == nil || pending.lastTargetGlobal == nil {
		return true
	}
	if h.combatNow().Sub(pending.lastRetarget) < attackPursuitRetargetRate {
		return false
	}
	targetGlobal := target.GetNavmeshPosition().GetGlobalCoordinates()
	dx := targetGlobal.X - pending.lastTargetGlobal.X
	dz := targetGlobal.Z - pending.lastTargetGlobal.Z
	return dx*dx+dz*dz >= attackPursuitRetargetDelta*attackPursuitRetargetDelta
}

func (h *CombatHandler) scheduleAttackPursuit(playerUID uint32, pending *pendingAttack) {
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, fn func()) { time.AfterFunc(delay, fn) }
	}
	schedule(attackPursuitPollInterval, func() { h.continueAttackPursuit(playerUID, pending) })
}

func (h *CombatHandler) continueAttackPursuit(playerUID uint32, pending *pendingAttack) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil || h.pending[playerUID] != pending {
		return
	}
	player, target, err := h.resolveAttack(pending.session, pending.targetUID)
	if err != nil {
		delete(h.pending, playerUID)
		_ = h.sendActionState(pending.session, false)
		return
	}
	movement := player.GetMovementData()
	if movement != nil && movement.Generation != pending.movementGeneration {
		// A client movement command supersedes the server-created pursuit.
		h.cancelPendingAttack(playerUID, false)
		_ = h.sendActionState(pending.session, false)
		return
	}
	if navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()) <= pending.reach {
		if movement != nil && target.GetMovementData() != nil {
			// A live target can enter weapon range before the last pursuit leg
			// reaches its stale destination. Stop that leg immediately so the
			// player attacks instead of running past or behind the target.
			h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "combat-target-in-range", true)
			movement = nil
		}
		if movement != nil {
			// Let the pursuit movement publish its final position before starting
			// the attack animation, avoiding an overlapping stop-and-hit snap.
			h.scheduleAttackPursuit(playerUID, pending)
			return
		}
		h.cancelPendingAttack(playerUID, false)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var executeErr error
		if pending.skillID != 0 {
			skill, exists := h.referenceSkill(pending.skillID)
			if !exists || !playerOwnsSkill(player, pending.skillID) {
				executeErr = ErrCombatTargetInvalid
			} else {
				switch skill.ExecutionKind() {
				case model.SkillExecutionStatusAttack:
					executeErr = h.executeTargetedStatusSkill(ctx, pending.session, player, target, skill)
				case model.SkillExecutionPhysicalAttack, model.SkillExecutionMagicalAttack:
					executeErr = h.executeSkillAttack(ctx, pending.session, player, target, skill)
				default:
					executeErr = ErrCombatTargetInvalid
				}
			}
		} else {
			executeErr = h.executeAttack(ctx, pending.session, player, target)
		}
		if executeErr != nil {
			logrus.Warnf("queued attack failed player=%d target=%d skill=%d: %v", playerUID, pending.targetUID, pending.skillID, executeErr)
		} else if pending.skillID == 0 && target.GetLifeState() == model.Alive {
			h.startRepeatingAttack(player, pending.session, target)
		}
		return
	}
	if movement == nil || h.attackPursuitNeedsRetarget(target, pending) {
		if err := h.restartAttackPursuit(player, target, pending); err != nil {
			h.cancelPendingAttack(playerUID, false)
			_ = h.sendActionState(pending.session, false)
			return
		}
	}
	h.scheduleAttackPursuit(playerUID, pending)
}

func (h *CombatHandler) startRepeatingAttack(player *model.Player, session *server.Session, target *model.NPC) {
	if h == nil || h.schedule == nil || player == nil || session == nil || target == nil {
		return
	}
	cooldown := h.baseAttackCooldown(player)
	if cooldown <= 0 {
		return
	}
	h.startRepeatingAttackAfter(player, session, target, cooldown)
}

func (h *CombatHandler) startRepeatingAttackAfter(player *model.Player, session *server.Session, target *model.NPC, delay time.Duration) {
	if h == nil || h.schedule == nil || player == nil || session == nil || target == nil {
		return
	}
	if h.repeating == nil {
		h.repeating = make(map[uint32]*pendingAttack)
	}
	playerUID := player.GetUniqueID()
	intent := &pendingAttack{
		session:            session,
		targetUID:          target.GetUniqueID(),
		reach:              h.baseAttackReach(player, target),
		movementGeneration: player.GetMovementGeneration(),
	}
	h.repeating[playerUID] = intent
	h.schedule(delay, func() { h.continueRepeatingAttack(playerUID, intent) })
}

func (h *CombatHandler) continueRepeatingAttack(playerUID uint32, intent *pendingAttack) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.repeating == nil || h.repeating[playerUID] != intent {
		return
	}
	player, target, err := h.resolveAttack(intent.session, intent.targetUID)
	if err != nil || player.GetMovementGeneration() != intent.movementGeneration {
		delete(h.repeating, playerUID)
		_ = h.sendActionState(intent.session, false)
		return
	}
	reach := h.baseAttackReach(player, target)
	if navmeshv2.HorizontalDistance(player.GetNavmeshPosition(), target.GetNavmeshPosition()) > reach {
		delete(h.repeating, playerUID)
		if err := h.beginAttackPursuit(intent.session, player, target, reach, 0); err != nil {
			_ = h.sendActionState(intent.session, false)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = h.executeAttack(ctx, intent.session, player, target)
	cancel()
	if err != nil || target.GetLifeState() != model.Alive {
		delete(h.repeating, playerUID)
		return
	}
	h.startRepeatingAttack(player, intent.session, target)
}

func (h *CombatHandler) cancelRepeatingAttack(playerUID uint32) {
	if h == nil || h.repeating == nil {
		return
	}
	delete(h.repeating, playerUID)
}

func (h *CombatHandler) cancelPendingAttack(playerUID uint32, stopMovement bool) {
	if h.pending == nil {
		return
	}
	pending := h.pending[playerUID]
	if pending == nil {
		return
	}
	delete(h.pending, playerUID)
	if !stopMovement || h.world == nil {
		return
	}
	player, err := h.world.GetPlayerByUniqueId(playerUID)
	if err != nil || player == nil {
		return
	}
	movement := player.GetMovementData()
	if movement != nil && movement.Generation == pending.movementGeneration {
		h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "combat-pursuit-stop", true)
	}
}

func (h *CombatHandler) sendActionState(session *server.Session, active bool) error {
	packet := buildActionStateResponse(active)
	if combatTraceEnabled() {
		logrus.Infof("COMBAT_PACKET_TRACE opcode=0x%04X active=%t payload=% X", packet.MessageID, active, packet.Payload())
	}
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return sendCombatPacket(session, packet)
}

func (h *CombatHandler) sendSkillCastError(session *server.Session, reason byte) error {
	return h.sendPlayerPacket(session, buildSkillCastErrorResponse(reason))
}

// emitAttackAndEnd mirrors the original task completion order. B074 INIT
// makes the client enter its action state; every resolved basic hit must later
// emit B074 END or the stock client remains stuck and sends no next attack.
func (h *CombatHandler) emitAttackAndEnd(session *server.Session, player *model.Player, attack *network.Packet) error {
	if err := h.emitAttack(player, attack); err != nil {
		return err
	}
	return h.sendActionState(session, false)
}

func (h *CombatHandler) emitAttack(player *model.Player, attack *network.Packet) error {
	if attack != nil && combatTraceEnabled() {
		logrus.Infof("COMBAT_PACKET_TRACE opcode=0x%04X payload=% X", attack.MessageID, attack.Payload())
	}
	if h != nil && h.broadcast != nil {
		h.broadcast(player, attack)
	} else {
		broadcastCombatPacket(player, attack)
	}
	return nil
}

func sendCombatPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return fmt.Errorf("combat session is unavailable")
	}
	return session.Send(packet)
}

func broadcastCombatPacket(player *model.Player, packet *network.Packet) {
	if player != nil && packet != nil {
		player.SendToSelfAndKnown(packet)
	}
}

func buildActionStateResponse(active bool) network.Packet {
	packet, _ := (vsro188.CombatActionStateResponseCodec{}).Encode(vsro188.CombatActionStateResponse{Active: active})
	return packet
}

func combatTraceEnabled() bool {
	return os.Getenv("SRO_COMBAT_TRACE") == "1"
}

func buildSkillCastErrorResponse(reason byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastBegin
	p.WriteByte(2)
	p.WriteByte(reason)
	p.WriteByte(skillCastErrorCategory)
	return p
}

func buildAttackBegin(sourceUID, targetUID, castUID, damage uint32, killed bool) network.Packet {
	return model.BuildAttackBegin(sourceUID, targetUID, castUID, damage, killed)
}

func buildAttackEnd(targetUID, castUID, damage uint32, killed bool) network.Packet {
	return model.BuildAttackEnd(targetUID, castUID, damage, killed)
}

func writeDamageBlock(p *network.Packet, targetUID, damage uint32, killed bool) {
	model.WriteDamageBlock(p, targetUID, damage, killed)
}

func sendExperienceUpdate(session *server.Session, sourceUID uint32, gained, gainedSkillExperience uint64, progress model.ProgressionResult) error {
	return session.Send(buildExperienceUpdate(sourceUID, gained, gainedSkillExperience, progress))
}

func publishLevelUpAnimation(player *model.Player, progress model.ProgressionResult) {
	if player == nil || progress.LevelsGained <= 0 {
		return
	}
	packet := model.BuildLevelUpAnimationPacket(player.GetUniqueID())
	player.SendToSelfAndKnown(&packet)
}

func buildExperienceUpdate(sourceUID uint32, gained, gainedSkillExperience uint64, progress model.ProgressionResult) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityUpdateExperience
	p.WriteUInt32(sourceUID)
	p.WriteUInt64(gained)
	p.WriteUInt64(gainedSkillExperience)
	p.WriteByte(0)
	if progress.LevelsGained > 0 {
		p.WriteUInt16(uint16(progress.StatPointsGained))
	}
	return p
}

func scaleCombatReward(base uint64, percent int) uint64 {
	if percent < 0 {
		percent = 100
	}
	if percent == 0 || base == 0 {
		return 0
	}
	if base > math.MaxUint64/uint64(percent) {
		return math.MaxUint64
	}
	return base * uint64(percent) / 100
}
