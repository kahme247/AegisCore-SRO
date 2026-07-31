package character

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

const (
	actionPhaseStart = byte(1)
	// vSRO 1.88 emits basic targeted attacks as
	// `01 | 04 | targetUID:uint32 | 00` on 0x7074.
	actionTypeAttack       = byte(4)
	actionTypeAttackLegacy = byte(1)
	maximumWireDamage      = 0xFFFFFF

	attackPursuitPollInterval  = 25 * time.Millisecond
	attackPursuitArrivalMargin = float32(1)

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
}

type skillCooldownKey struct {
	playerUID uint32
	skillID   uint32
}

type combatRateProvider interface {
	Current() model.GameplayRates
}

type CombatHandler struct {
	mu            sync.Mutex
	world         combatWorld
	referenceData *model.ReferenceDataCatalog
	progress      model.CharacterProgressRepository
	vitals        model.CharacterVitalsRepository
	levels        *model.LevelTable
	rates         combatRateProvider
	schedule      func(time.Duration, func())
	move          func(*model.Player, navmeshv2.RtNavmeshPosition) uint64
	send          func(*server.Session, network.Packet) error
	broadcast     func(*model.Player, *network.Packet)
	pending       map[uint32]*pendingAttack
	repeating     map[uint32]*pendingAttack
	nextAttack    map[uint32]time.Time
	nextSkill     map[skillCooldownKey]time.Time
	now           func() time.Time
}

func RegisterCombatHandler(target *server.Server, progress model.CharacterProgressRepository, levels *model.LevelTable, rates combatRateProvider) *CombatHandler {
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
	isSkillCast := false
	isSelfBuff := false
	isSelfHealing := false
	var player *model.Player
	switch actionType {
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
			if terminator == 0 && exists && (kind == model.SkillExecutionBuff || kind == model.SkillExecutionHealing && candidate.TargetSelf) {
				player, err = h.resolveCombatPlayer(session)
				if err == nil && playerOwnsSkill(player, candidateID) {
					skill, isSkillCast = candidate, true
					isSelfBuff = kind == model.SkillExecutionBuff
					isSelfHealing = kind == model.SkillExecutionHealing
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
		default:
			return fmt.Errorf("combat cast payload has %d trailing bytes, want 5 or 9", packet.Remaining())
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

	player, target, err := h.resolveAttack(session, targetUID)
	if err != nil {
		_ = h.sendActionState(session, false)
		return nil
	}
	if isSkillCast && !playerOwnsSkill(player, skill.ID) {
		return h.sendActionState(session, false)
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
	before := target.CurrentHitPoints()
	if isSkillCast {
		err = h.executeSkillAttack(ctx, session, player, target, skill)
	} else {
		err = h.executeAttack(ctx, session, player, target)
	}
	if err != nil {
		return err
	}
	if !isSkillCast && target.GetLifeState() == model.Alive && target.CurrentHitPoints() < before {
		h.startRepeatingAttack(player, session, target)
	}
	return nil
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

func (h *CombatHandler) referenceSkill(skillID uint32) (model.RefSkill, bool) {
	if h == nil || h.referenceData == nil || skillID == 0 {
		return model.RefSkill{}, false
	}
	return h.referenceData.Skill(skillID)
}

func playerOwnsSkill(player *model.Player, skillID uint32) bool {
	for _, skill := range player.SkillSnapshot() {
		if skill.ID == skillID && skill.Enabled == 1 {
			return true
		}
	}
	return false
}

func playerHasSkillMP(player *model.Player, skill model.RefSkill) bool {
	_, mp := player.CurrentVitals()
	return skill.ConsumeMP >= 0 && mp >= skill.ConsumeMP
}

func playerCanCastWithWeapon(player *model.Player, skill model.RefSkill) bool {
	requiredWeapons := [...]byte{skill.RequiredWeapon1, skill.RequiredWeapon2}
	restricted := false
	for _, requiredWeapon := range requiredWeapons {
		if requiredWeapon != 0 && requiredWeapon != 255 {
			restricted = true
			break
		}
	}
	if !restricted {
		return true
	}

	weaponType := byte(1)
	if weapon, equipped := player.GetInventory().Items[model.SlotPrimaryWeapon]; equipped && weapon.GetTypeInfo().IsWeapon() {
		weaponType = byte(weapon.GetTypeInfo().TypeID4)
	}
	for _, requiredWeapon := range requiredWeapons {
		if requiredWeapon != 0 && requiredWeapon != 255 && weaponType == requiredWeapon {
			return true
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
	if player == nil || skill.ExecutionKind() != model.SkillExecutionBuff {
		return h.sendActionState(session, false)
	}
	if reason, rejected := skillCastRejection(player, skill); rejected {
		return h.sendSkillCastError(session, reason)
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	key := skillCooldownKey{playerUID: player.GetUniqueID(), skillID: skill.ID}
	if next := h.nextSkill[key]; !next.IsZero() && now.Before(next) {
		return h.sendActionState(session, false)
	}
	if skill.ConsumeMP > 0 {
		if h.vitals == nil {
			return h.sendActionState(session, false)
		}
		hp, mp := player.CurrentVitals()
		spent, err := h.vitals.SpendMP(ctx, player.ID, mp, skill.ConsumeMP)
		if err != nil {
			return err
		}
		if !spent || !player.CommitMP(mp, skill.ConsumeMP) {
			return h.sendSkillCastError(session, skillCastErrorInvalidMP)
		}
		defer func() {
			packet := model.BuildVitalsUpdatePacket(player.GetUniqueID(), hp, mp-skill.ConsumeMP)
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
	replacedToken, committed := player.CommitActiveBuff(skill.ID, token, duration, skill.BuffModifiers)
	if !committed {
		return h.sendActionState(session, false)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildBuffCastBegin(skill.ID, player.GetUniqueID(), player.GetUniqueID(), castUID)
	end := model.BuildBuffCastEnd(player.GetUniqueID(), castUID)
	if err := h.emitAttack(player, &begin); err != nil {
		return err
	}
	if err := h.emitAttack(player, &end); err != nil {
		return err
	}
	if replacedToken != 0 {
		remove := model.BuildBuffRemove(replacedToken)
		if err := h.emitAttack(player, &remove); err != nil {
			return err
		}
	}
	add := model.BuildBuffAdd(player.GetUniqueID(), skill.ID, token)
	if err := h.emitAttack(player, &add); err != nil {
		return err
	}
	if err := h.sendActionState(session, false); err != nil {
		return err
	}
	if err := h.sendPlayerPacket(session, player.BuildStatsUpdatePacket()); err != nil {
		return err
	}
	if h.schedule != nil {
		h.schedule(duration, func() { h.expireBuff(player, token) })
	}
	return nil
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
	reach := h.baseAttackReach(player, target)
	if skill.ActionRange <= 0 {
		return reach
	}
	skillReach := float32(skill.ActionRange) / 10
	if skillReach > reach {
		return skillReach
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
	if damage < 1 {
		damage = 1
	}
	if damage > maximumWireDamage {
		damage = maximumWireDamage
	}
	cooldown := h.baseAttackCooldown(player)
	if cooldown > 0 {
		if h.nextAttack == nil {
			h.nextAttack = make(map[uint32]time.Time)
		}
		h.nextAttack[playerUID] = now.Add(cooldown)
	}
	applied, killed := h.applyPlayerDamage(player, target, damage)
	if applied == 0 {
		return h.sendActionState(session, false)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	damageHits := []uint32{uint32(applied)}
	if player.BaseAttackHitCount() == 2 {
		first := uint32(applied) / 2
		damageHits = []uint32{first, uint32(applied) - first}
	}
	begin := model.BuildAttackBeginWithSkillHits(player.BaseAttackSkillID(), player.GetUniqueID(), targetUID, castUID, damageHits, killed)
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
		hp, mp := player.CurrentVitals()
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
			packet := model.BuildVitalsUpdatePacket(playerUID, hp, mp-skill.ConsumeMP)
			_ = h.sendPlayerPacket(session, packet)
		}()
	}
	if h.nextSkill == nil {
		h.nextSkill = make(map[skillCooldownKey]time.Time)
	}
	if skill.ReuseDelay > 0 {
		h.nextSkill[key] = now.Add(time.Duration(skill.ReuseDelay) * time.Millisecond)
	}
	damageHits := make([]uint32, 0, 3)
	killed := false
	for _, strike := range h.skillStrikes(skill) {
		applied, strikeKilled := h.applyPlayerDamage(player, target, skillStrikeDamage(player, strike))
		if applied == 0 {
			break
		}
		damageHits = append(damageHits, uint32(applied))
		if strikeKilled {
			killed = true
			break
		}
	}
	if len(damageHits) == 0 {
		return h.sendActionState(session, false)
	}
	if err := h.sendActionState(session, true); err != nil {
		return err
	}
	castUID := model.NextActionUID()
	begin := model.BuildAttackBeginWithSkillHits(skill.ID, playerUID, target.GetUniqueID(), castUID, damageHits, killed)
	if err := h.emitAttackAndEnd(session, player, &begin); err != nil {
		return err
	}
	if !killed {
		return nil
	}
	return h.awardPlayerKill(ctx, session, player, target, target.GetUniqueID())
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
	baseDamage := player.AverageAttackPower(skill.IsMagicalAttack())
	percent := skill.AttackPercent
	if percent <= 0 {
		percent = 100
	}
	damage := (baseDamage + (skill.AttackMin+skill.AttackMax)/2) * percent / 100
	if damage < 1 {
		return 1
	}
	if damage > maximumWireDamage {
		return maximumWireDamage
	}
	return damage
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

func (h *CombatHandler) applyPlayerDamage(player *model.Player, target *model.NPC, damage int) (int, bool) {
	applied, killed := target.ApplyDamage(damage)
	if applied == 0 {
		return 0, false
	}
	// A successful hit establishes reciprocal visibility before aggro so a
	// passive mob can resolve its attacker between visibility ticks.
	if known := target.GetKnownObjectList(); known != nil && !known.Knows(player) {
		known.AddObject(player)
	}
	target.SetAggroTarget(player.GetUniqueID())
	if killed {
		h.world.StopMovingCharacter(target, target.GetNavmeshPosition(), "mob-killed", false)
		target.QueueRespawn()
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, fn func()) { time.AfterFunc(delay, fn) }
		}
		schedule(target.DecayDuration(), func() { h.world.RemoveVisibleObject(target) })
	}
	return applied, killed
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
	progress := player.PreviewExperienceAward(h.levels, reward, skillReward)
	if h.progress == nil {
		return model.ErrCharacterProgressRepositoryUnavailable
	}
	if err := h.progress.SaveProgress(ctx, player.ID, progress); err != nil {
		return err
	}
	player.CommitProgress(progress)
	return sendExperienceUpdate(session, targetUID, reward, skillReward, progress)
}

func (h *CombatHandler) baseAttackCooldown(player *model.Player) time.Duration {
	if h == nil || h.referenceData == nil || player == nil {
		return 0
	}
	skill, ok := h.referenceData.Skill(player.BaseAttackSkillID())
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
	destinationGlobal := attackRangePosition(
		player.GetNavmeshPosition().GetGlobalCoordinates(),
		target.GetNavmeshPosition().GetGlobalCoordinates(),
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
	return nil
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
				executeErr = h.executeSkillAttack(ctx, pending.session, player, target, skill)
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
	if movement == nil {
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
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityUpdateExperience
	p.WriteUInt32(sourceUID)
	p.WriteUInt64(gained)
	p.WriteUInt64(gainedSkillExperience)
	p.WriteByte(0)
	if progress.LevelsGained > 0 {
		p.WriteUInt16(uint16(progress.StatPointsGained))
	}
	return session.Send(p)
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
