package character

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/server"
)

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
