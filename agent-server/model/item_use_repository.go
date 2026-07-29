package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrItemUseRepositoryUnavailable = errors.New("item-use repository is unavailable")
	ErrItemUseInvalidRequest        = errors.New("invalid item-use request")
	ErrItemUseUnsupported           = errors.New("item use is not supported")
	ErrItemUseNoEffect              = errors.New("item use would have no effect")
	ErrItemUseOutcomeUnknown        = errors.New("item-use transaction outcome is unknown")
)

type ItemUseResult struct {
	Inventory  Inventory
	HP         int
	MP         int
	RefItemID  uint32
	ItemType   uint16
	SourceSlot byte
	NewAmount  uint16
}

type ItemUseVitals struct {
	HP, MP       int
	MaxHP, MaxMP int
}

type ItemUseRepository interface {
	UseRecoveryItem(context.Context, int64, byte, uint16, ItemUseVitals) (ItemUseResult, error)
}

type SQLItemUseRepository struct{ shard *sql.DB }

func NewItemUseRepository(shard *sql.DB) (*SQLItemUseRepository, error) {
	if shard == nil {
		return nil, ErrItemUseRepositoryUnavailable
	}
	return &SQLItemUseRepository{shard: shard}, nil
}

func InventoryItemType(ref RefItem) uint16 {
	cash := 0
	if ref.IsCashItem {
		cash = 1
	}
	return uint16(cash + ref.TypeID1*4 + ref.TypeID2*32 + ref.TypeID3*128 + ref.TypeID4*2048)
}

func recoveryValues(ref RefItem, vitals ItemUseVitals) (int, int, bool) {
	if !ref.IsUseable || ref.TypeID1 != 3 || ref.TypeID2 != 3 || ref.TypeID3 != 1 || ref.TypeID4 < 1 || ref.TypeID4 > 3 {
		return vitals.HP, vitals.MP, false
	}
	maxHP := maxInt(vitals.HP, vitals.MaxHP)
	maxMP := maxInt(vitals.MP, vitals.MaxMP)
	newHP := minInt(maxHP, vitals.HP+ref.Param1+(maxHP*ref.Param2/100))
	newMP := minInt(maxMP, vitals.MP+ref.Param3+(maxMP*ref.Param4/100))
	return newHP, newMP, newHP != vitals.HP || newMP != vitals.MP
}

func (r *SQLItemUseRepository) UseRecoveryItem(ctx context.Context, characterID int64, slot byte, requestedType uint16, vitals ItemUseVitals) (result ItemUseResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 || slot < 13 || slot >= BaseInventorySize || vitals.HP < 0 || vitals.MP < 0 || vitals.MaxHP <= 0 || vitals.MaxMP <= 0 {
		return result, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin item-use transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var lockedCharacterID int64
	if err = tx.QueryRowContext(ctx, "SELECT `id` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&lockedCharacterID); err != nil {
		return result, fmt.Errorf("lock item-use character: %w", err)
	}
	var itemID int64
	var refID uint32
	var stack int
	if err = tx.QueryRowContext(ctx, "SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` WHERE i.`FK_CHAR`=? AND i.`SLOT`=? FOR UPDATE", characterID, slot).Scan(&itemID, &refID, &stack); err != nil {
		return result, fmt.Errorf("lock item-use inventory slot: %w", err)
	}
	ref, ok := RefItems[refID]
	if !ok {
		return result, ErrInventoryReferenceItemNotFound
	}
	itemType := InventoryItemType(ref)
	if requestedType != itemType {
		return result, ErrItemUseInvalidRequest
	}
	newHP, newMP, changed := recoveryValues(ref, vitals)
	if ref.TypeID1 != 3 || ref.TypeID2 != 3 || ref.TypeID3 != 1 || ref.TypeID4 < 1 || ref.TypeID4 > 3 || !ref.IsUseable {
		return result, ErrItemUseUnsupported
	}
	if !changed {
		return result, ErrItemUseNoEffect
	}
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `HP`=?,`MP`=? WHERE `id`=?", newHP, newMP, characterID); err != nil {
		return result, fmt.Errorf("persist item-use recovery: %w", err)
	}
	if stack > 1 {
		if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1 WHERE `id`=?", itemID); err != nil {
			return result, fmt.Errorf("decrement used item: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, itemID); err != nil {
			return result, fmt.Errorf("unlink used item: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", itemID); err != nil {
			return result, fmt.Errorf("delete used item: %w", err)
		}
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return ItemUseResult{}, err
	}
	result.HP, result.MP, result.RefItemID, result.ItemType = newHP, newMP, refID, itemType
	result.SourceSlot = slot
	if stack > 1 {
		result.NewAmount = uint16(stack - 1)
	}
	if err = tx.Commit(); err != nil {
		return ItemUseResult{}, fmt.Errorf("%w: %v", ErrItemUseOutcomeUnknown, err)
	}
	return result, nil
}

func loadInventoryInItemUseTransaction(ctx context.Context, tx *sql.Tx, characterID int64) (Inventory, error) {
	rows, err := tx.QueryContext(ctx, SelectCharacterInventory, characterID)
	if err != nil {
		return emptyInventory(), fmt.Errorf("reload item-use inventory: %w", err)
	}
	defer rows.Close()
	inv := emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return emptyInventory(), fmt.Errorf("scan item-use inventory: %w", scanErr)
		}
		inv.Items[slot] = item
	}
	if err = rows.Err(); err != nil {
		return emptyInventory(), fmt.Errorf("iterate item-use inventory: %w", err)
	}
	return inv, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
