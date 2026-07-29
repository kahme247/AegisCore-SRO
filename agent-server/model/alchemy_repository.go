package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrAlchemyRepositoryUnavailable = errors.New("alchemy repository is unavailable")
	ErrAlchemyInvalidRequest        = errors.New("invalid alchemy request")
	ErrAlchemyConflict              = errors.New("alchemy state conflicts with the request")
	ErrAlchemyUnsupported           = errors.New("alchemy material or target is unsupported")
	ErrAlchemyOutcomeUnknown        = errors.New("alchemy transaction outcome is unknown")
)

const (
	alchemyElixirClassProtector = 16909056
	alchemyElixirClassShield    = 67108864
	alchemyElixirClassAccessory = 83886080
	alchemyElixirClassWeapon    = 100663296
	// Enhancement-only phase one intentionally stops before advanced-elixir
	// and destruction/protection rules become necessary.
	MaxBasicAlchemyOptLevel byte = 5
)

type AlchemyMaterialUpdate struct {
	Slot      byte
	Remaining uint16
}

type AlchemyResult struct {
	Inventory  Inventory
	TargetSlot byte
	Target     Item
	Success    bool
	Materials  []AlchemyMaterialUpdate
}

type AlchemyRepository interface {
	FuseElixir(context.Context, int64, uint32, byte, byte, *byte) (AlchemyResult, error)
}

type SQLAlchemyRepository struct{ shard *sql.DB }

func NewAlchemyRepository(shard *sql.DB) (*SQLAlchemyRepository, error) {
	if shard == nil {
		return nil, ErrAlchemyRepositoryUnavailable
	}
	return &SQLAlchemyRepository{shard: shard}, nil
}

func alchemyTargetClass(ref RefItem) (int, bool) {
	if !ref.IsEquipment() {
		return 0, false
	}
	switch ref.TypeID3 {
	case 6:
		return alchemyElixirClassWeapon, true
	case 4:
		return alchemyElixirClassShield, true
	case 5, 12:
		return alchemyElixirClassAccessory, true
	case 1, 2, 3, 9, 10, 11:
		return alchemyElixirClassProtector, true
	default:
		return 0, false
	}
}

func alchemyDegree(ref RefItem) int {
	if ref.ItemClass < 1 {
		return 0
	}
	return (ref.ItemClass-1)/3 + 1
}

func validateElixirMaterials(target, elixir RefItem, powder *RefItem) error {
	targetClass, ok := alchemyTargetClass(target)
	if !ok || alchemyDegree(target) == 0 {
		return ErrAlchemyUnsupported
	}
	if elixir.TypeID1 != 3 || elixir.TypeID2 != 3 || elixir.TypeID3 != 10 || elixir.TypeID4 != 1 || elixir.Param1 != targetClass {
		return ErrAlchemyUnsupported
	}
	if powder != nil && (powder.TypeID1 != 3 || powder.TypeID2 != 3 || powder.TypeID3 != 10 || powder.TypeID4 != 2 || powder.ItemClass != alchemyDegree(target)) {
		return ErrAlchemyUnsupported
	}
	return nil
}

func (r *SQLAlchemyRepository) FuseElixir(ctx context.Context, characterID int64, userID uint32, targetSlot, elixirSlot byte, powderSlot *byte) (result AlchemyResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrAlchemyRepositoryUnavailable
	}
	if characterID <= 0 || userID == 0 || targetSlot < 13 || elixirSlot < 13 || targetSlot >= BaseInventorySize || elixirSlot >= BaseInventorySize || targetSlot == elixirSlot {
		return result, ErrAlchemyInvalidRequest
	}
	if powderSlot != nil && (*powderSlot < 13 || *powderSlot >= BaseInventorySize || *powderSlot == targetSlot || *powderSlot == elixirSlot) {
		return result, ErrAlchemyInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin alchemy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil {
		return result, fmt.Errorf("lock alchemy character: %w", err)
	}
	if owner != userID {
		return result, ErrAlchemyConflict
	}

	locked, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	target, targetOK := locked.Items[targetSlot]
	elixir, elixirOK := locked.Items[elixirSlot]
	if !targetOK || !elixirOK || target.OptLevel >= MaxBasicAlchemyOptLevel || elixir.StackSize < 1 {
		return result, ErrAlchemyConflict
	}
	targetRef, targetRefOK := RefItems[target.RefObjectID]
	elixirRef, elixirRefOK := RefItems[elixir.RefObjectID]
	if !targetRefOK || !elixirRefOK {
		return result, ErrInventoryReferenceItemNotFound
	}
	var powder *Item
	var powderRef *RefItem
	if powderSlot != nil {
		item, ok := locked.Items[*powderSlot]
		if !ok || item.StackSize < 1 {
			return result, ErrAlchemyConflict
		}
		ref, ok := RefItems[item.RefObjectID]
		if !ok {
			return result, ErrInventoryReferenceItemNotFound
		}
		powder, powderRef = &item, &ref
	}
	if err = validateElixirMaterials(targetRef, elixirRef, powderRef); err != nil {
		return result, err
	}

	if err = consumeAlchemyMaterial(ctx, tx, characterID, elixirSlot, elixir); err != nil {
		return result, err
	}
	result.Materials = append(result.Materials, AlchemyMaterialUpdate{Slot: elixirSlot, Remaining: remainingAlchemyStack(elixir)})
	if powder != nil {
		if err = consumeAlchemyMaterial(ctx, tx, characterID, *powderSlot, *powder); err != nil {
			return result, err
		}
		result.Materials = append(result.Materials, AlchemyMaterialUpdate{Slot: *powderSlot, Remaining: remainingAlchemyStack(*powder)})
	}
	update, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `OPT_LEVEL`=? WHERE `id`=? AND `OPT_LEVEL`=?", target.OptLevel+1, target.ID, target.OptLevel)
	if err != nil {
		return result, fmt.Errorf("update alchemy target: %w", err)
	}
	if affected, affectedErr := update.RowsAffected(); affectedErr != nil || affected != 1 {
		return result, ErrAlchemyConflict
	}

	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AlchemyResult{}, err
	}
	result.Target, targetOK = result.Inventory.Items[targetSlot]
	if !targetOK {
		return AlchemyResult{}, ErrAlchemyConflict
	}
	result.TargetSlot, result.Success = targetSlot, true
	if err = tx.Commit(); err != nil {
		return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}

func remainingAlchemyStack(item Item) uint16 {
	if item.StackSize <= 1 {
		return 0
	}
	remaining := item.StackSize - 1
	if remaining > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(remaining)
}

func loadLockedAlchemyInventory(ctx context.Context, tx *sql.Tx, characterID int64) (Inventory, error) {
	rows, err := tx.QueryContext(ctx, SelectCharacterInventory+" ORDER BY inv.SLOT FOR UPDATE", characterID)
	if err != nil {
		return emptyInventory(), fmt.Errorf("lock alchemy inventory: %w", err)
	}
	defer rows.Close()
	inv := emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return emptyInventory(), fmt.Errorf("scan alchemy inventory: %w", scanErr)
		}
		inv.Items[slot] = item
	}
	if err = rows.Err(); err != nil {
		return emptyInventory(), fmt.Errorf("iterate alchemy inventory: %w", err)
	}
	return inv, nil
}

func consumeAlchemyMaterial(ctx context.Context, tx *sql.Tx, characterID int64, slot byte, item Item) error {
	if item.StackSize > 1 {
		result, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1 WHERE `id`=? AND `STACK_COUNT`=?", item.ID, item.StackSize)
		if err != nil {
			return fmt.Errorf("decrement alchemy material: %w", err)
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ErrAlchemyConflict
		}
		return nil
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, item.ID)
	if err != nil {
		return fmt.Errorf("unlink alchemy material: %w", err)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrAlchemyConflict
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", item.ID); err != nil {
		return fmt.Errorf("delete alchemy material: %w", err)
	}
	return nil
}
