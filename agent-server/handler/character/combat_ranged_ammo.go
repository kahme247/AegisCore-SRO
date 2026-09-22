package character

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

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
