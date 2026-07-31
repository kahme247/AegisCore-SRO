package character

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

const (
	GMKillRewardsFull byte = iota
	GMKillRewardsDropsOnly
	GMKillRewardsNone
)

// ForceKillForGM applies the same atomic NPC death and client combat framing as
// an ordinary lethal player hit, then selects the requested reward boundary.
func (h *CombatHandler) ForceKillForGM(ctx context.Context, player *model.Player, target *model.NPC, rewardMode byte) error {
	if h == nil || player == nil || player.Session == nil || target == nil || target.GetLifeState() != model.Alive {
		return ErrCombatTargetInvalid
	}
	if rewardMode > GMKillRewardsNone {
		return fmt.Errorf("invalid GM kill reward mode %d", rewardMode)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	hitPoints := target.CurrentHitPoints()
	if hitPoints <= 0 {
		return ErrCombatTargetInvalid
	}
	applied, killed := target.ApplyDamage(hitPoints)
	if !killed || applied <= 0 {
		return ErrCombatTargetInvalid
	}
	if h.interruptMob != nil {
		h.interruptMob(target)
	}

	record := model.CombatDamageRecord{
		AttackState: model.DamageAttackStateNormal | model.DamageAttackStateDead,
		DamageState: model.DamageStateNormal,
		Damage:      uint32(applied),
	}
	packet := model.BuildAttackBeginWithSkillDamageRecords(0, player.GetUniqueID(), target.GetUniqueID(), model.NextActionUID(), []model.CombatDamageRecord{record})
	if err := h.emitAttackAndEnd(player.Session, player, &packet); err != nil {
		return err
	}

	switch rewardMode {
	case GMKillRewardsFull:
		return h.awardPlayerKill(ctx, player.Session, player, target, target.GetUniqueID())
	case GMKillRewardsDropsOnly:
		h.dropsMu.RLock()
		drops := h.drops
		h.dropsMu.RUnlock()
		if drops != nil {
			_, err := drops.SpawnForKill(ctx, player, target)
			return err
		}
	}
	return nil
}
