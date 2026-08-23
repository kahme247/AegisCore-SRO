package character

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

const (
	actionPhaseStart = byte(1)
	// vSRO 1.88 emits basic targeted attacks as
	// `01 | 04 | targetUID:uint32 | 00` on 0x7074.
	actionTypeAttack       = byte(4)
	actionTypeAttackLegacy = byte(1)
	actionTypeDispel       = byte(5)
	maximumWireDamage      = 0xFFFFFF

	attackPursuitPollInterval  = 25 * time.Millisecond
	attackPursuitArrivalMargin = float32(1)
	attackPursuitRetargetRate  = 200 * time.Millisecond
	attackPursuitRetargetDelta = float32(2)

	skillCastErrorInvalidSkill     = byte(0x03)
	skillCastErrorInvalidMP        = byte(0x04)
	skillCastErrorInvalidEquipment = byte(0x0D)
	skillCastErrorCategory         = byte(0x30)
)

var ErrCombatTargetInvalid = errors.New("combat target is invalid")

type combatWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetObjectByUniqueId(uint32) (model.ISRObject, error)
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

type skillCooldownKey struct {
	playerUID uint32
	skillID   uint32
}

type combatRateProvider interface {
	Current() model.GameplayRates
}

type CombatHandler struct {
	mu             sync.Mutex
	world          combatWorld
	referenceData  *model.ReferenceDataCatalog
	progress       model.CharacterProgressRepository
	quests         model.QuestRuntimeRepository
	vitals         model.CharacterVitalsRepository
	monsterMasks   model.MonsterMaskRepository
	hwan           model.HwanRepository
	levels         *model.LevelTable
	rates          combatRateProvider
	schedule       func(time.Duration, func())
	move           func(*model.Player, navmeshv2.RtNavmeshPosition) uint64
	send           func(*server.Session, network.Packet) error
	broadcast      func(*model.Player, *network.Packet)
	pending        map[uint32]*pendingAttack
	repeating      map[uint32]*pendingAttack
	nextAttack     map[uint32]time.Time
	nextSkill      map[skillCooldownKey]time.Time
	now            func() time.Time
	rollPercent    func(int) bool
	hitRollPercent func(int) bool
	criticalRoll   func(int) bool
	blockRoll      func(int) bool
	rollHwanPoint  func(int) bool
}

func RegisterCombatHandler(target *server.Server, progress model.CharacterProgressRepository, levels *model.LevelTable, rates combatRateProvider, quests ...model.QuestRuntimeRepository) *CombatHandler {
	if target == nil {
		return nil
	}
	world := service.GetWorldServiceInstance()
	handler := &CombatHandler{
		world: world, progress: progress, levels: levels, rates: rates,
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
		send:       sendCombatPacket,
		broadcast:  broadcastCombatPacket,
		pending:    make(map[uint32]*pendingAttack),
		repeating:  make(map[uint32]*pendingAttack),
		nextAttack: make(map[uint32]time.Time),
		nextSkill:  make(map[skillCooldownKey]time.Time),
		now:        time.Now,
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

func (h *CombatHandler) CancelPlayerAttack(session *server.Session, playerUID uint32) {
	if h == nil || playerUID == 0 {
		return
	}
	h.mu.Lock()
	h.cancelPendingAttack(playerUID, false)
	h.cancelRepeatingAttack(playerUID)
	h.mu.Unlock()
	_ = h.sendActionState(session, false)
}

func (h *CombatHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if os.Getenv("SRO_SESSION_TRACE") == "1" {
		logrus.Infof("COMBAT_ACTION_TRACE payload=% X", packet.Payload())
	}
	if packet.Remaining() < 1 {
		return fmt.Errorf("combat action payload length %d, want at least 1", packet.Remaining())
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
			candidateID, readErr := packet.ReadUInt32()
			if readErr != nil {
				return readErr
			}
			terminator, readErr := packet.ReadByte()
			if readErr != nil {
				return readErr
			}
			candidate, exists := h.referenceSkill(candidateID)
			kind := candidate.ExecutionKind()
			if terminator == 0 && exists && (kind == model.SkillExecutionBuff || kind == model.SkillExecutionBarrier || kind == model.SkillExecutionMovement || kind == model.SkillExecutionStatusAttack && !candidate.TargetRequired || kind == model.SkillExecutionCure && !candidate.TargetRequired || kind == model.SkillExecutionAggro && !candidate.TargetRequired || kind == model.SkillExecutionHealing && (candidate.TargetSelf || !candidate.TargetRequired)) {
				player, err = h.resolveCombatPlayer(session)
				if err == nil && playerOwnsSkill(player, candidateID) {
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
			if terminator != 0 || targetUID == 0 {
				return h.sendActionState(session, false)
			}
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
			if !exists || candidate.ExecutionKind() != model.SkillExecutionMovement || regionID < 0 {
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
		if resolveErr != nil || !playerOwnsSkill(player, skill.ID) {
			return h.sendActionState(session, false)
		}
		return h.executeMovementSkill(ctx, session, player, skill, areaTarget)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionBuff {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID) {
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
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validHealingTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeHealing(ctx, session, caster, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionCure {
		caster, resolveErr := h.resolveCombatPlayer(session)
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID) {
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
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID) {
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
		if resolveErr != nil || !playerOwnsSkill(caster, skill.ID) {
			return h.sendActionState(session, false)
		}
		object, resolveErr := h.world.GetObjectByUniqueId(targetUID)
		target, targetIsPlayer := object.(*model.Player)
		if resolveErr != nil || !targetIsPlayer || !validHealingTarget(caster, target, skill) {
			return h.sendActionState(session, false)
		}
		return h.executeTargetedPlayerAggroSkill(ctx, session, caster, target, skill)
	}

	player, target, err := h.resolveAttack(session, targetUID)
	if err != nil {
		_ = h.sendActionState(session, false)
		return nil
	}
	if isSkillCast && !playerOwnsSkill(player, skill.ID) {
		return h.sendActionState(session, false)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionStatusAttack {
		return h.executeTargetedStatusSkill(ctx, session, player, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() == model.SkillExecutionAggro {
		return h.executeTargetedAggroSkill(ctx, session, player, target, skill)
	}
	if isSkillCast && skill.ExecutionKind() != model.SkillExecutionPhysicalAttack && skill.ExecutionKind() != model.SkillExecutionMagicalAttack {
		return h.sendActionState(session, false)
	}
	if isSkillCast {
		if reason, rejected := skillCastRejection(player, skill); rejected {
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

func (h *CombatHandler) cancelSelfBuff(session *server.Session, player *model.Player, skillID uint32) error {
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

func (h *CombatHandler) executeHealing(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionHealing || h.vitals == nil {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(caster, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	if skill.ActionRange > 0 && caster != target && navmeshv2.HorizontalDistance(caster.GetNavmeshPosition(), target.GetNavmeshPosition()) > float32(skill.ActionRange)/10 {
		return h.sendActionState(session, false)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	key := skillCooldownKey{playerUID: caster.GetUniqueID(), skillID: skill.ID}
	if next := h.nextSkill[key]; !next.IsZero() && now.Before(next) {
		return h.sendActionState(session, false)
	}
	casterHP, casterMP := caster.CurrentVitals()
	if skill.ConsumeMP > 0 {
		spent, err := h.vitals.SpendMP(ctx, caster.ID, casterMP, skill.ConsumeMP)
		if err != nil {
			return err
		}
		if !spent || !caster.CommitMP(casterMP, skill.ConsumeMP) {
			return h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		casterMP -= skill.ConsumeMP
	}
	expectedHP, expectedMP, healedHP, healedMP, ok := target.PreviewHealing(skill.HealHP, skill.HealMP)
	if !ok {
		return h.sendActionState(session, false)
	}
	if err := h.vitals.SaveVitals(ctx, target.ID, healedHP, healedMP); err != nil {
		return err
	}
	if !target.CommitHealing(expectedHP, expectedMP, healedHP, healedMP) {
		return h.sendActionState(session, false)
	}
	if h.nextSkill == nil {
		h.nextSkill = make(map[skillCooldownKey]time.Time)
	}
	if skill.ReuseDelay > 0 {
		h.nextSkill[key] = now.Add(time.Duration(skill.ReuseDelay) * time.Millisecond)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, caster.GetUniqueID(), target.GetUniqueID(), castUID)
	end := model.BuildBuffCastEnd(caster.GetUniqueID(), castUID)
	if err := h.emitAttack(caster, &begin); err != nil {
		return err
	}
	if err := h.emitAttack(caster, &end); err != nil {
		return err
	}
	if err := h.sendActionState(session, false); err != nil {
		return err
	}
	targetVitals := model.BuildVitalsUpdatePacket(target.GetUniqueID(), healedHP, healedMP)
	if err := h.sendPlayerPacket(target.Session, targetVitals); err != nil {
		return err
	}
	if caster != target && skill.ConsumeMP > 0 {
		casterVitals := model.BuildVitalsUpdatePacket(caster.GetUniqueID(), casterHP, casterMP)
		return h.sendPlayerPacket(session, casterVitals)
	}
	return nil
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
	return h.beginUtilityCast(session, caster, target.GetUniqueID(), skill, func(time.Time) {
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

func buildServerInventoryItemAdded(slot byte, item model.Item) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(14)
	packet.WriteByte(slot)
	packet.WriteByte(0x3C)
	model.WriteRentInfo(&packet, item)
	packet.WriteUInt32(item.GetRefObjectID())
	model.WriteInventoryItem(&packet, item)
	return packet
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
	delay := time.Duration(skill.ActionPreparingTime+skill.ActionCastingTime) * time.Millisecond
	if delay > 0 && h.schedule != nil {
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
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

func playerOwnsSkill(player *model.Player, skillID uint32) bool {
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
			if weapon.IsBow() {
				ammo, ok := inventory.Items[model.SlotSecondaryWeapon]
				return ok && ammo.StackSize > 0 && ammo.IsArrow()
			}
			if weapon.IsCrossbow() {
				ammo, ok := inventory.Items[model.SlotSecondaryWeapon]
				return ok && ammo.StackSize > 0 && ammo.IsBolt()
			}
			return true
		}
	}
	return false
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
		return skillCastErrorInvalidEquipment, true
	}
	if !playerHasSkillMP(player, skill) {
		return skillCastErrorInvalidMP, true
	}
	return 0, false
}

func (h *CombatHandler) executeSelfBuff(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) error {
	return h.executeBuff(ctx, session, player, player, skill)
}

func (h *CombatHandler) executeBuff(ctx context.Context, session *server.Session, caster, target *model.Player, skill model.RefSkill) error {
	if caster == nil || target == nil || skill.ExecutionKind() != model.SkillExecutionBuff {
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
	if next := h.nextSkill[key]; !next.IsZero() && now.Before(next) {
		return h.sendActionState(session, false)
	}
	if skill.ConsumeMP > 0 {
		if h.vitals == nil {
			return h.sendActionState(session, false)
		}
		hp, mp := caster.CurrentVitals()
		spent, err := h.vitals.SpendMP(ctx, caster.ID, mp, skill.ConsumeMP)
		if err != nil {
			return err
		}
		if !spent || !caster.CommitMP(mp, skill.ConsumeMP) {
			return h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		defer func() {
			packet := model.BuildVitalsUpdatePacket(caster.GetUniqueID(), hp, mp-skill.ConsumeMP)
			_ = h.sendPlayerPacket(session, packet)
		}()
	}
	if h.nextSkill == nil {
		h.nextSkill = make(map[skillCooldownKey]time.Time)
	}
	if skill.ReuseDelay > 0 {
		h.nextSkill[key] = now.Add(time.Duration(skill.ReuseDelay) * time.Millisecond)
	}
	duration := time.Duration(skill.BuffDuration) * time.Millisecond
	token := model.NextActionUID()
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
	replacedToken, committed := target.CommitActiveBuffFrom(skill.ID, token, caster.GetUniqueID(), duration, skill.BuffModifiers)
	if !committed {
		return h.sendActionState(session, false)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, caster.GetUniqueID(), target.GetUniqueID(), castUID)
	end := model.BuildBuffCastEnd(target.GetUniqueID(), castUID)
	if err := h.emitAttack(caster, &begin); err != nil {
		return err
	}
	if err := h.emitAttack(caster, &end); err != nil {
		return err
	}
	if replacedToken != 0 {
		remove := model.BuildBuffRemove(replacedToken)
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
		h.schedule(duration, func() { h.expireBuff(target, token) })
		if skill.RecoveryHP > 0 || skill.RecoveryMP > 0 {
			h.scheduleRecoveryTick(target, skill, token, 5*time.Second)
		}
	}
	return nil
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
	if !player.RemoveActiveBuff(token) {
		return
	}
	packet := model.BuildBuffRemove(token)
	_ = h.emitAttack(player, &packet)
	_ = h.sendPlayerPacket(player.Session, player.BuildStatsUpdatePacket())
}

func (h *CombatHandler) skillAttackReach(player *model.Player, target *model.NPC, skill model.RefSkill) float32 {
	if skill.ActionRange > 0 {
		// Skills author their own cast reach. A long-range bow must not extend a
		// 15 m spell, and a short-range weapon must not shorten one.
		return float32(skill.ActionRange)
	}
	if skill.TransferRange > 0 {
		// Several radial/nuker series omit ACTION_RANGE and carry their authored
		// reach only in the transfer block. Never substitute equipped-weapon reach
		// for such a skill.
		return float32(skill.TransferRange)
	}
	return h.baseAttackReach(player, target)
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
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	playerUID := player.GetUniqueID()
	if next := h.nextAttack[playerUID]; !next.IsZero() && now.Before(next) {
		return h.sendActionState(session, false)
	}
	targetUID := target.GetUniqueID()
	damage := player.AverageAttackPower(false)
	imbue, hasImbue := h.activeWeaponImbue(player)
	if hasImbue {
		damage += weaponImbueDamage(player, imbue)
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
	if next := h.nextSkill[key]; !next.IsZero() && now.Before(next) {
		return h.sendActionState(session, false)
	}
	if skill.ConsumeMP > 0 {
		if h.vitals == nil {
			return h.sendActionState(session, false)
		}
		_, mp := player.CurrentVitals()
		if mp < skill.ConsumeMP {
			return h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		spent, err := h.vitals.SpendMP(ctx, player.ID, mp, skill.ConsumeMP)
		if err != nil {
			return err
		}
		if !spent || !player.CommitMP(mp, skill.ConsumeMP) {
			return h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		defer func() {
			hp, currentMP := player.CurrentVitals()
			packet := model.BuildVitalsUpdatePacket(playerUID, hp, currentMP)
			_ = h.sendPlayerPacket(session, packet)
		}()
	}
	if h.nextSkill == nil {
		h.nextSkill = make(map[skillCooldownKey]time.Time)
	}
	if skill.ReuseDelay > 0 {
		h.nextSkill[key] = now.Add(time.Duration(skill.ReuseDelay) * time.Millisecond)
	}
	impactDelay := time.Duration(skill.ActionPreparingTime+skill.ActionCastingTime) * time.Millisecond
	if impactDelay > 0 && h.schedule != nil {
		if err := h.sendActionState(session, true); err != nil {
			return err
		}
		castUID := model.NextActionUID()
		begin := model.BuildDelayedAttackBegin(skill.ID, playerUID, target.GetUniqueID(), castUID)
		if err := h.emitAttack(player, &begin); err != nil {
			return err
		}
		h.schedule(impactDelay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.resolveDelayedSkillAttack(session, player, target, skill, castUID)
		})
		return nil
	}
	damageHits := make([]uint32, 0, 3)
	damageRecords := make([]model.CombatDamageRecord, 0, 3)
	appliedDamage := 0
	killed := false
	imbue, hasImbue := h.activeWeaponImbue(player)
	for _, strike := range h.skillStrikes(skill) {
		damage := skillStrikeDamage(player, strike)
		if hasImbue {
			damage += weaponImbueDamage(player, imbue)
		}
		result := h.resolvePlayerDamage(player, target, damage, strike.IsMagicalAttack(), strike.AbsoluteDamage > 0, now)
		wireDamage, strikeApplied, strikeKilled := result.wireDamage, result.appliedDamage, result.killed
		damageHits = append(damageHits, uint32(wireDamage))
		damageRecords = append(damageRecords, result.combatRecord(0))
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

func (h *CombatHandler) executeBarrierSkill(ctx context.Context, session *server.Session, player *model.Player, skill model.RefSkill) error {
	if player == nil || skill.ExecutionKind() != model.SkillExecutionBarrier {
		return h.sendActionState(session, false)
	}
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
	}
	token := model.NextActionUID()
	if old, active := player.RemoveActiveBarrier(); active {
		remove := model.BuildBuffRemove(old.Token)
		if err := h.emitAttack(player, &remove); err != nil {
			return err
		}
	}
	if !player.CommitBarrier(model.ActiveBarrier{
		SkillID: skill.ID, Token: token, HitPoints: skill.BarrierHitPoints,
		Absorption: skill.BarrierAbsorption, Physical: skill.BarrierPhysical,
	}) {
		return h.sendActionState(session, false)
	}
	if h.world != nil {
		h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "wall-skill-root", false)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, player.GetUniqueID(), player.GetUniqueID(), castUID)
	end := model.BuildBuffCastEnd(player.GetUniqueID(), castUID)
	add := model.BuildBuffAdd(player.GetUniqueID(), skill.ID, token)
	for _, packet := range []*network.Packet{&begin, &end, &add} {
		if err := h.emitAttack(player, packet); err != nil {
			return err
		}
	}
	if err := h.sendActionState(session, false); err != nil {
		return err
	}
	if h.schedule != nil && skill.BarrierDrainMS > 0 {
		h.scheduleBarrierDrain(player, skill, token)
	}
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
	_, ready, err := h.prepareStatusSkill(ctx, session, player, skill)
	if err != nil || !ready {
		return err
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
			candidateGlobal := candidate.GetGlobalCoordinates()
			scale := distance / candidateDistance
			destination, err = h.world.NewPosFromGlobalCoordinates(math32.NewVector3(
				global.X+(candidateGlobal.X-global.X)*scale,
				global.Y,
				global.Z+(candidateGlobal.Z-global.Z)*scale,
			))
		}
	}
	if err != nil || !h.world.CanMoveDirectly(start, destination) {
		return h.sendActionState(session, false)
	}
	destination.Heading = start.Heading
	h.world.StopMovingCharacter(player, start, "ghost-walk", false)
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildMovementCastBegin(skill.ID, player.GetUniqueID(), player.GetUniqueID(), castUID, destination)
	if err := h.emitAttack(player, &begin); err != nil {
		return err
	}
	finish := func() {
		// The stock client advances Ghost Walk during the authored movement
		// phase. Commit and publish the endpoint only when that phase finishes;
		// completing it beside B070 makes the animation trail behind the player.
		player.SetNavmeshPosition(destination)
		end := model.BuildTeleportCastEnd(player.GetUniqueID(), castUID, destination)
		_ = h.emitAttack(player, &end)
		_ = h.sendActionState(session, false)
	}
	delay := time.Duration(skill.MovementDurationMS) * time.Millisecond
	if delay > 0 && h.schedule != nil {
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			finish()
		})
		return nil
	}
	finish()
	return nil
}

func (h *CombatHandler) resolveDelayedSkillAttack(session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill, castUID uint32) {
	if h == nil || player == nil || target == nil || target.GetLifeState() != model.Alive {
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
	imbue, hasImbue := h.activeWeaponImbue(player)
	for _, strike := range h.skillStrikes(skill) {
		damage := skillStrikeDamage(player, strike)
		if hasImbue {
			damage += weaponImbueDamage(player, imbue)
		}
		result := h.resolvePlayerDamage(player, target, damage, strike.IsMagicalAttack(), strike.AbsoluteDamage > 0, now)
		wireDamage, strikeApplied, strikeKilled := result.wireDamage, result.appliedDamage, result.killed
		damageHits = append(damageHits, uint32(wireDamage))
		damageRecords = append(damageRecords, result.combatRecord(0))
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
	end := model.BuildAttackEndDamageRecords(target.GetUniqueID(), castUID, damageRecords)
	_ = h.emitAttack(player, &end)
	_ = h.applySkillTransfers(context.Background(), session, player, target, skill, now)
	if hasImbue {
		_ = h.applyImbueTransfers(context.Background(), session, player, target, imbue, now)
	}
	_ = h.sendActionState(session, false)
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
	return h.beginStatusCast(session, player, target.GetUniqueID(), skill, func(impactTime time.Time) []model.SkillEffectTarget {
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
	if next := h.nextSkill[key]; !next.IsZero() && now.Before(next) {
		return time.Time{}, false, h.sendActionState(session, false)
	}
	if skill.ConsumeMP > 0 {
		if h.vitals == nil {
			return time.Time{}, false, h.sendActionState(session, false)
		}
		hp, mp := player.CurrentVitals()
		if mp < skill.ConsumeMP {
			return time.Time{}, false, h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		spent, err := h.vitals.SpendMP(ctx, player.ID, mp, skill.ConsumeMP)
		if err != nil {
			return time.Time{}, false, err
		}
		if !spent || !player.CommitMP(mp, skill.ConsumeMP) {
			return time.Time{}, false, h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		packet := model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, mp-skill.ConsumeMP)
		if err := h.sendPlayerPacket(session, packet); err != nil {
			return time.Time{}, false, err
		}
	}
	if h.nextSkill == nil {
		h.nextSkill = make(map[skillCooldownKey]time.Time)
	}
	if skill.ReuseDelay > 0 {
		h.nextSkill[key] = now.Add(time.Duration(skill.ReuseDelay) * time.Millisecond)
	}
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
	delay := time.Duration(skill.ActionPreparingTime+skill.ActionCastingTime) * time.Millisecond
	if delay > 0 && h.schedule != nil {
		h.schedule(delay, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
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

func skillStrikeDamage(player *model.Player, skill model.RefSkill) int {
	if skill.AbsoluteDamage > 0 {
		return skill.AbsoluteDamage
	}
	baseDamage := player.AverageAttackPower(skill.IsMagicalAttack())
	percent := skill.AttackPercent
	if percent <= 0 {
		percent = 100
	}
	damage := (baseDamage + (skill.AttackMin+skill.AttackMax)/2) * percent / 100
	if damage < 1 {
		return 1
	}
	return damage
}

func weaponImbueDamage(player *model.Player, imbue model.RefSkill) int {
	if player == nil || !imbue.IsWeaponImbue() {
		return 0
	}
	return skillStrikeDamage(player, imbue)
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
			ExpiresAt: now.Add(time.Duration(effect.DurationMS) * time.Millisecond),
		}
		if !target.ApplyCombatStatus(status) {
			continue
		}
		if h.world != nil && statusRestrainsMovement(effect.Kind) {
			h.world.StopMovingCharacter(target, target.GetNavmeshPosition(), "combat-status-control", true)
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
		status, active := target.CombatStatus(expected.Kind, current)
		if active && status.Token != expected.Token {
			return
		}
		if active {
			return
		}
		h.publishBadEffectState(player, target, current)
	})
}

func (h *CombatHandler) applySkillTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time) error {
	return h.applyTransfers(ctx, session, player, primary, skill, now, skillStrikeDamage(player, skill))
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
		status, active := target.CombatStatus(expected.Kind, current)
		if !active || status.Token != expected.Token || target.GetLifeState() != model.Alive {
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
		packet := model.BuildAttackBeginWithSkillHits(status.SkillID, player.GetUniqueID(), target.GetUniqueID(), castUID, []uint32{uint32(result.wireDamage)}, result.killed)
		_ = h.emitAttack(player, &packet)
		if result.killed {
			_ = h.awardPlayerKill(context.Background(), session, player, target, target.GetUniqueID())
			return
		}
		h.scheduleStatusDamageTick(session, player, target, status, current.Add(5*time.Second))
	})
}

func (h *CombatHandler) applyImbueTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, imbue model.RefSkill, now time.Time) error {
	return h.applyTransfers(ctx, session, player, primary, imbue, now, weaponImbueDamage(player, imbue))
}

func (h *CombatHandler) applyTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time, damage int) error {
	// REF_SKILL's transfer count is the simultaneous target count, including
	// the primary target. A value of five therefore permits four nearby mobs.
	secondaryCount := skill.TransferCount - 1
	if h == nil || player == nil || primary == nil || secondaryCount <= 0 || skill.TransferRange <= 0 || damage <= 0 {
		return nil
	}
	type candidate struct {
		npc      *model.NPC
		distance float32
	}
	known := player.GetKnownObjectList()
	if known == nil {
		return nil
	}
	radius := float32(skill.TransferRange)
	candidates := make([]candidate, 0, secondaryCount)
	for _, object := range known.GetKnownObjects() {
		npc, ok := object.(*model.NPC)
		if !ok || npc == nil || npc == primary || npc.GetLifeState() != model.Alive || !npc.GetTypeInfo().IsNPCMob() {
			continue
		}
		distance := navmeshv2.HorizontalDistance(primary.GetNavmeshPosition(), npc.GetNavmeshPosition())
		if distance <= radius {
			candidates = append(candidates, candidate{npc: npc, distance: distance})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			return candidates[i].npc.GetUniqueID() < candidates[j].npc.GetUniqueID()
		}
		return candidates[i].distance < candidates[j].distance
	})
	if len(candidates) > secondaryCount {
		candidates = candidates[:secondaryCount]
	}
	for _, candidate := range candidates {
		result := h.resolvePlayerDamage(player, candidate.npc, damage, skill.IsMagicalAttack(), skill.AbsoluteDamage > 0, now)
		effectMask := uint32(0)
		if result.appliedDamage > 0 {
			effectMask = h.applySkillStatuses(session, player, candidate.npc, skill, now)
			if err := h.restoreDamageVitals(ctx, session, player, skill, result.appliedDamage); err != nil {
				return err
			}
		}
		castUID := model.NextActionUID()
		packet := model.BuildAttackBeginWithSkillDamageRecords(skill.ID, player.GetUniqueID(), candidate.npc.GetUniqueID(), castUID, []model.CombatDamageRecord{result.combatRecord(effectMask)})
		if err := h.emitAttack(player, &packet); err != nil {
			return err
		}
		if result.killed {
			if err := h.awardPlayerKill(ctx, session, player, candidate.npc, candidate.npc.GetUniqueID()); err != nil {
				return err
			}
		}
	}
	return nil
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

func skillCooldown(skill model.RefSkill) time.Duration {
	cooldown := time.Duration(skill.ActionDuration) * time.Millisecond
	if reuse := time.Duration(skill.ReuseDelay) * time.Millisecond; reuse > cooldown {
		cooldown = reuse
	}
	if coolTime := time.Duration(skill.CoolTime) * time.Millisecond; coolTime > cooldown {
		cooldown = coolTime
	}
	return cooldown
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
	if !h.playerAttackLands(player, target, now) {
		return playerDamageResult{}
	}
	if !absolute && h.playerAttackBlocked(target) {
		return playerDamageResult{attackState: model.DamageAttackStateBlock}
	}
	critical := !magical && !absolute && h.playerAttackCritical(player)
	return h.applyPlayerDamageResult(player, target, damage, magical, absolute, critical)
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
	now := time.Now()
	if h != nil && h.now != nil {
		now = h.now()
	}
	isMagical := len(damageFlags) > 0 && damageFlags[0]
	isAbsolute := len(damageFlags) > 1 && damageFlags[1]
	isCritical := len(damageFlags) > 2 && damageFlags[2]
	defense := 0
	if !isAbsolute && h != nil && h.referenceData != nil {
		if character, exists := h.referenceData.Character(target.GetRefObjectID()); exists {
			if isMagical {
				defense = character.MagDef
			} else {
				defense = character.PhyDef
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
		damage = max(1, damage-defense)
	}
	if status, active := target.CombatStatus(model.CombatStatusDamageTakenUp, now); active && !isAbsolute {
		damage += damage * status.Power / 100
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
	playerAggro := applied
	if sourceUID, percent, active := player.AggroAbsorber(h.referenceData); active {
		absorbed := applied * percent / 100
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
	if err := sendExperienceUpdate(session, targetUID, reward, skillReward, progress); err != nil {
		return err
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
	if h.quests == nil {
		return nil
	}
	questProgress, err := h.quests.RecordMobKill(ctx, player.ID, target.GetRefObjectID())
	if err != nil {
		return err
	}
	if !questProgress.Changed {
		return nil
	}
	player.CommitQuestMutation(questProgress.Quest)
	update, err := buildQuestStatePacket(questProgress.Quest)
	if err != nil {
		return err
	}
	if h.send != nil {
		return h.send(session, update)
	}
	return session.Send(update)
}

// AwardCOSKill routes a growth-pet kill through the owner's normal progression,
// HWAN, and quest pipeline while retaining the killed mob as the EXP source.
func (h *CombatHandler) AwardCOSKill(ctx context.Context, owner *model.Player, target *model.NPC) error {
	if h == nil || owner == nil || owner.Session == nil || target == nil {
		return ErrCombatTargetInvalid
	}
	return h.awardPlayerKill(ctx, owner.Session, owner, target, target.GetUniqueID())
}

func (h *CombatHandler) baseAttackCooldown(player *model.Player) time.Duration {
	if h == nil || h.referenceData == nil || player == nil {
		return 0
	}
	skill, ok := h.referenceData.Skill(playerBaseAttackSkillID(player))
	if !ok {
		return 0
	}
	return skillCooldown(skill)
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
	standOff := pending.reach - attackPursuitArrivalMargin
	if standOff < 0 {
		standOff = 0
	}
	targetGlobal := target.GetNavmeshPosition().GetGlobalCoordinates()
	destinationGlobal := attackRangePosition(
		player.GetNavmeshPosition().GetGlobalCoordinates(),
		targetGlobal,
		standOff,
	)
	destination, err := h.world.NewPosFromGlobalCoordinates(destinationGlobal)
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

func attackRangePosition(source, target *math32.Vector3, reach float32) *math32.Vector3 {
	if source == nil || target == nil {
		return nil
	}
	delta := target.Clone().Sub(source.Clone())
	delta.Y = 0
	distance := delta.Length()
	if distance <= reach || distance <= 0.0001 {
		return source.Clone()
	}
	delta.Normalize().MultiplyScalar(distance - reach)
	return source.Clone().Add(delta)
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
			// reaches its stale stand-off point. Stop that leg immediately so the
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
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityActionResponse
	if active {
		p.WriteByte(1) // Enter action.
		p.WriteByte(1) // Recurring/active.
	} else {
		p.WriteByte(2) // Exit action.
		p.WriteByte(0) // Not recurring.
	}
	return p
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
