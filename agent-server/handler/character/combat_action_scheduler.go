package character

import (
	"context"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

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
	targetPosition := target.GetNavmeshPosition()
	if targetPosition.Offset == nil {
		return ErrCombatTargetInvalid
	}
	pending.movementGeneration = h.move(player, targetPosition)
	if pending.movementGeneration == 0 {
		return ErrCombatTargetInvalid
	}
	pending.lastTargetGlobal = targetPosition.GetGlobalCoordinates()
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
