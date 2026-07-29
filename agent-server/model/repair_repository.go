package model

import (
	"context"
	"errors"
	"math"
)

var (
	ErrRepairConflict         = errors.New("repair state conflicts with the request")
	ErrRepairInsufficientGold = errors.New("insufficient gold for repair")
	ErrRepairOutcomeUnknown   = errors.New("repair transaction outcome is unknown")
)

type RepairResult struct {
	Gold       uint64
	Inventory  Inventory
	Durability map[byte]uint32
}

type RepairRepository interface {
	Repair(context.Context, int64, uint32, *byte) (RepairResult, error)
}

// Repair restores one slot, or all repairable equipment when slot is nil.
// Character ownership, gold, inventory rows, and item rows are locked in one
// transaction so concurrent shop/storage/repair requests serialize safely.
func (r *SQLEconomyRepository) Repair(ctx context.Context, characterID int64, userID uint32, slot *byte) (RepairResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || (slot != nil && *slot >= BaseInventorySize) {
		return RepairResult{}, ErrRepairConflict
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return RepairResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	owner, gold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return RepairResult{}, ErrRepairConflict
	}
	query := "SELECT inv.`SLOT`,it.`id`,it.`VARIANCE`,it.`DATA`,d.`CostRepair`,d.`Dur_L`,d.`Dur_U`,d.`TypeID_3` FROM `INVENTORY` inv JOIN `ITEM` it ON it.`id`=inv.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=it.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND d.`CanRepair`<>0"
	args := []any{characterID}
	if slot != nil {
		query += " AND inv.`SLOT`=?"
		args = append(args, *slot)
	}
	query += " FOR UPDATE"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return RepairResult{}, err
	}
	type repairItem struct {
		slot             byte
		id               int64
		current, maximum uint32
		cost             uint64
	}
	items := make([]repairItem, 0, 13)
	var total uint64
	found := false
	for rows.Next() {
		var item repairItem
		var variance uint64
		var unitCost uint64
		var lower, upper float64
		var typeID3 int
		if err = rows.Scan(&item.slot, &item.id, &variance, &item.current, &unitCost, &lower, &upper, &typeID3); err != nil {
			_ = rows.Close()
			return RepairResult{}, err
		}
		found = true
		item.maximum = maximumItemDurability(lower, upper, variance, typeID3)
		if item.maximum == 0 {
			_ = rows.Close()
			return RepairResult{}, ErrRepairConflict
		}
		if item.maximum <= item.current {
			continue
		}
		damage := uint64(item.maximum - item.current)
		if unitCost != 0 && damage > math.MaxUint64/unitCost {
			_ = rows.Close()
			return RepairResult{}, ErrRepairConflict
		}
		item.cost = damage * unitCost
		if total > math.MaxUint64-item.cost {
			_ = rows.Close()
			return RepairResult{}, ErrRepairConflict
		}
		total += item.cost
		items = append(items, item)
	}
	if err = rows.Close(); err != nil {
		return RepairResult{}, err
	}
	if err = rows.Err(); err != nil {
		return RepairResult{}, err
	}
	if slot != nil && !found {
		return RepairResult{}, ErrRepairConflict
	}
	if gold < total {
		return RepairResult{}, ErrRepairInsufficientGold
	}
	for _, item := range items {
		result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `DATA`=? WHERE `id`=? AND `DATA`=?", item.maximum, item.id, item.current)
		if updateErr != nil {
			return RepairResult{}, updateErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return RepairResult{}, ErrRepairConflict
		}
	}
	gold -= total
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID); err != nil {
		return RepairResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return RepairResult{}, errors.Join(ErrRepairOutcomeUnknown, err)
	}
	committed = true
	inventory, err := r.loadInventory(ctx, characterID)
	if err != nil {
		return RepairResult{}, errors.Join(ErrRepairOutcomeUnknown, err)
	}
	durability := make(map[byte]uint32, len(items))
	for _, item := range items {
		durability[item.slot] = item.maximum
	}
	return RepairResult{Gold: gold, Inventory: inventory, Durability: durability}, nil
}

func maximumItemDurability(lower, upper float64, variance uint64, typeID3 int) uint32 {
	if lower <= 0 || upper < lower || math.IsNaN(lower) || math.IsNaN(upper) {
		return 0
	}
	shift := uint(25) // armor and shields store six five-bit variance fields.
	if typeID3 == TypeID3Weapon {
		shift = 30
	}
	value := lower + (upper-lower)*float64((variance>>shift)&VarianceStatMask)/float64(VarianceStatMask)
	if value >= float64(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(value)
}
