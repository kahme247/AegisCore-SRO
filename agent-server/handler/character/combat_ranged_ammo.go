package character

import (
	"context"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/server"
)

// consumeRangedAmmo validates the equipped arrow or bolt for ranged attacks.
// Ammunition is infinite, so a valid stack is never persisted, decremented, or
// announced to the client. Melee attacks deliberately return without mutation.
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

	return nil
}
