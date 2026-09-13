package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	lockGroundPickupCharacter = "SELECT `id`,`INVENTORY_SIZE` FROM `SRO_SHARD`.`CHAR` WHERE `id`=? FOR UPDATE"
	lockGroundPickupItem      = "SELECT `ID` FROM `SRO_SHARD`.`ITEM` WHERE `ID`=? FOR UPDATE"
	findGroundPickupOwner     = "SELECT `FK_CHAR` FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_ITEM`=? LIMIT 1 FOR UPDATE"
	lockGroundPickupInventory = "SELECT `SLOT` FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? FOR UPDATE"
	insertGroundPickup        = "INSERT INTO `SRO_SHARD`.`INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)"
	lockGroundGoldCharacter   = "SELECT `id`,`GOLD` FROM `SRO_SHARD`.`CHAR` WHERE `id`=? FOR UPDATE"
	lockGroundGoldItem        = "SELECT `ID`,`FK_REF_ITEM`,`DATA` FROM `SRO_SHARD`.`ITEM` WHERE `ID`=? FOR UPDATE"
	creditGroundGold          = "UPDATE `SRO_SHARD`.`CHAR` SET `GOLD`=? WHERE `id`=?"
	deleteGroundGoldItem      = "DELETE FROM `SRO_SHARD`.`ITEM` WHERE `ID`=?"
	deleteGroundDropInventory = "DELETE FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?"
	insertGroundDropInventory = "INSERT INTO `SRO_SHARD`.`INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)"
)

var (
	ErrGroundItemRepositoryUnavailable = errors.New("ground-item repository is unavailable")
	ErrGroundItemCharacterNotFound     = errors.New("ground-item character was not found")
	ErrGroundItemNotFound              = errors.New("ground item was not found")
	ErrGroundItemAlreadyPickedUp       = errors.New("ground item has already been picked up")
	ErrGroundItemInventoryFull         = errors.New("character inventory is full")
	ErrGroundItemPickupConflict        = errors.New("ground-item pickup conflicts with persisted inventory")
	ErrGroundItemPickupOutcomeUnknown  = errors.New("ground-item pickup commit outcome is unknown")
	ErrGroundItemDropConflict          = errors.New("ground-item drop conflicts with persisted inventory")
	ErrGroundItemDropOutcomeUnknown    = errors.New("ground-item drop commit outcome is unknown")
	ErrGroundItemNotDroppable          = errors.New("ground item is not droppable")
	ErrGroundGoldOverflow              = errors.New("ground-item gold credit overflows character balance")
)

// GroundItemPickupResult is the canonical post-commit inventory state. The
// handler must publish this snapshot only after Pickup returns nil.
type GroundItemPickupResult struct {
	Inventory Inventory
	Slot      byte
}

// GroundItemCOSPickupResult is the committed replacement identity allocated
// for transient cart cargo and its destination slot in the active job COS.
type GroundItemCOSPickupResult struct {
	Item           Item
	Slot           byte
	Scale          byte
	SafeTradeReset bool
}

// GroundItemPickupRepository claims an existing ITEM row for a character by
// inserting its INVENTORY link. Implementations must serialize the character,
// item, and target slot inside one transaction.
type GroundItemPickupRepository interface {
	Pickup(context.Context, int64, int64, byte) (GroundItemPickupResult, error)
}

type GroundTransientPickupRepository interface {
	PickupTransient(context.Context, int64, *Item, byte) (GroundItemPickupResult, error)
	PickupTransientToJobCOS(context.Context, int64, uint64, *Item, byte, byte) (GroundItemCOSPickupResult, error)
}

// GroundGoldPickupResult is the committed balance after a generated gold
// item is converted into character currency. The ground ITEM row is consumed
// in the same transaction as the balance update.
type GroundGoldPickupResult struct {
	Gold   uint64
	Amount uint64
}

// GroundGoldPickupRepository is deliberately separate from normal inventory
// pickup: the binary's gold item virtual credits character currency instead
// of inserting an INVENTORY link.
type GroundGoldPickupRepository interface {
	PickupGold(context.Context, int64, int64) (GroundGoldPickupResult, error)
}

// GroundItemDropResult is the committed post-drop state. Item is retained as
// an ITEM row without an INVENTORY link until the world admits it; Inventory
// is the canonical remaining character snapshot.
type GroundItemDropResult struct {
	Item      *Item
	Inventory Inventory
	Slot      byte
}

// GroundItemDropRepository removes one normal inventory link atomically while
// preserving the ITEM row. Restore is the compensating boundary used when the
// subsequent world admission cannot complete.
type GroundItemDropRepository interface {
	Drop(context.Context, int64, byte) (GroundItemDropResult, error)
	Restore(context.Context, int64, byte, int64) error
}

type SQLGroundItemPickupRepository struct {
	shard *sql.DB
}

var _ GroundItemPickupRepository = (*SQLGroundItemPickupRepository)(nil)

var _ GroundGoldPickupRepository = (*SQLGroundItemPickupRepository)(nil)

var _ GroundItemDropRepository = (*SQLGroundItemPickupRepository)(nil)

func NewGroundItemPickupRepository(shard *sql.DB) (*SQLGroundItemPickupRepository, error) {
	if shard == nil {
		return nil, fmt.Errorf("configure ground-item repository: %w", ErrGroundItemRepositoryUnavailable)
	}
	return &SQLGroundItemPickupRepository{shard: shard}, nil
}

func (r *SQLGroundItemPickupRepository) PickupTransient(ctx context.Context, characterID int64, item *Item, slot byte) (result GroundItemPickupResult, err error) {
	if r == nil || r.shard == nil || characterID <= 0 || item == nil || !item.TransientGroundItem || len(item.CreatorName) > 17 || item.GetRefObjectID() == 0 || item.StackSize <= 0 || item.StackSize > math.MaxUint16 {
		return result, ErrGroundItemPickupConflict
	}
	if slot < InventoryEquipmentSlots || slot >= MaxInventoryWireSize {
		return result, ErrGroundItemPickupConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var lockedCharacter int64
	var capacity byte
	if err = tx.QueryRowContext(ctx, lockGroundPickupCharacter, characterID).Scan(&lockedCharacter, &capacity); err != nil || lockedCharacter != characterID || slot >= capacity {
		return result, ErrGroundItemInventoryFull
	}
	var occupied int
	if scanErr := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", characterID, slot).Scan(&occupied); scanErr == nil || !errors.Is(scanErr, sql.ErrNoRows) {
		return result, ErrGroundItemPickupConflict
	}
	spec := GroundItemSpec{ReferenceItemID: item.GetRefObjectID(), Variance: item.Variance, StackCount: uint16(item.StackSize), OptLevel: item.OptLevel, Data: item.Data, CreatorName: item.CreatorName, Rent: item.RentInfo, MagicOptions: item.MagicOptions}
	persisted, err := materializeGroundItem(ctx, tx, spec)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, insertGroundPickup, characterID, slot, persisted.ID); err != nil {
		return result, err
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	result.Slot = slot
	if err = tx.Commit(); err != nil {
		return GroundItemPickupResult{}, errors.Join(ErrGroundItemPickupOutcomeUnknown, err)
	}
	committed = true
	return result, nil
}

func (r *SQLGroundItemPickupRepository) PickupTransientToJobCOS(ctx context.Context, characterID int64, cosID uint64, item *Item, slot, previousScale byte) (result GroundItemCOSPickupResult, err error) {
	if r == nil || r.shard == nil || characterID <= 0 || cosID == 0 || item == nil || !item.TransientGroundItem || item.CreatorName == "" || len(item.CreatorName) > 17 || item.GetRefObjectID() == 0 || item.StackSize <= 0 || item.StackSize > math.MaxUint16 {
		return result, ErrGroundItemPickupConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var jobType byte
	var alias string
	if err = tx.QueryRowContext(ctx, "SELECT `JOB_TYPE`,COALESCE(`ALIAS`,'') FROM `CHARACTER_JOB` WHERE `FK_CHAR`=? FOR UPDATE", characterID).Scan(&jobType, &alias); err != nil || !SpecialtyPickupAllowed(jobType, alias, item.CreatorName) {
		return result, ErrGroundItemPickupConflict
	}
	var kind byte
	var capacity int
	if err = tx.QueryRowContext(ctx, "SELECT `KIND`,COALESCE(NULLIF(c.`INVENTORY_SIZE`,0),cc.`Inventory_Size`) FROM `CHAR_COS` c JOIN `CHAR_REF_DATA` cc ON cc.`RefObjID`=c.`REF_OBJECT_ID` WHERE c.`ID`=? AND c.`OWNER_CHAR_ID`=? FOR UPDATE", cosID, characterID).Scan(&kind, &capacity); err != nil || kind != byte(COSJobTransport) || capacity <= 0 || capacity > int(MaxInventoryWireSize) || int(slot) >= capacity {
		return result, ErrGroundItemPickupConflict
	}
	rows, err := tx.QueryContext(ctx, "SELECT `SLOT` FROM `COS_INVENTORY` WHERE `COS_ID`=? FOR UPDATE", cosID)
	if err != nil {
		return result, err
	}
	occupied := false
	for rows.Next() {
		var current byte
		if err = rows.Scan(&current); err != nil {
			rows.Close()
			return result, err
		}
		occupied = occupied || current == slot
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if occupied {
		return result, ErrGroundItemPickupConflict
	}
	spec := GroundItemSpec{ReferenceItemID: item.GetRefObjectID(), Variance: item.Variance, StackCount: uint16(item.StackSize), OptLevel: item.OptLevel, Data: item.Data, CreatorName: item.CreatorName, Rent: item.RentInfo, MagicOptions: item.MagicOptions}
	persisted, err := materializeGroundItem(ctx, tx, spec)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `COS_INVENTORY` (`COS_ID`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", cosID, slot, persisted.ID); err != nil {
		return result, err
	}
	score, thresholds, err := safeTradeScaleData(ctx, tx, int(characterID), cosID)
	if err != nil {
		return result, err
	}
	result.Item, result.Slot, result.Scale = *persisted, slot, SafeTradeScale(score, thresholds)
	if SafeTradeScaleRequiresReset(previousScale, result.Scale) {
		reset, resetErr := tx.ExecContext(ctx, "UPDATE `CHARACTER_SAFE_TRADE` SET `STATUS`=0,`LAST_SAFE_TRADE`=CURRENT_TIMESTAMP(6) WHERE `FK_CHAR`=? AND `STATUS`<>0", characterID)
		if resetErr != nil {
			return result, resetErr
		}
		if affected, affectedErr := reset.RowsAffected(); affectedErr != nil {
			return result, affectedErr
		} else if affected > 1 {
			return result, ErrGroundItemPickupConflict
		}
		result.SafeTradeReset = true
	}
	if err = tx.Commit(); err != nil {
		return GroundItemCOSPickupResult{}, errors.Join(ErrGroundItemPickupOutcomeUnknown, err)
	}
	committed = true
	return result, nil
}

// SpecialtyPickupAllowed mirrors SR_GameServer 0x00525DC0: a Trader may
// recover only cargo created under the same job alias, a Thief may take only
// foreign cargo, and Hunter/no-job states cannot collect specialty goods.
func SpecialtyPickupAllowed(jobType byte, alias, creator string) bool {
	if alias == "" || creator == "" {
		return false
	}
	sameCreator := strings.EqualFold(alias, creator)
	switch jobType {
	case JobTrader:
		return sameCreator
	case JobThief:
		return !sameCreator
	default:
		return false
	}
}

func (r *SQLGroundItemPickupRepository) Drop(ctx context.Context, characterID int64, slot byte) (result GroundItemDropResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrGroundItemRepositoryUnavailable
	}
	if characterID <= 0 || slot < InventoryEquipmentSlots || slot >= MaxInventoryWireSize {
		return result, ErrGroundItemDropConflict
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin ground-item drop: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var lockedCharacterID int64
	var capacity byte
	if err = tx.QueryRowContext(ctx, lockGroundPickupCharacter, characterID).Scan(&lockedCharacterID, &capacity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrGroundItemCharacterNotFound
		}
		return result, fmt.Errorf("lock ground-item drop character: %w", err)
	}
	if lockedCharacterID != characterID {
		return result, ErrGroundItemDropConflict
	}
	if capacity == 0 || capacity > MaxInventoryWireSize {
		capacity = MaxInventoryWireSize
	}
	if slot >= capacity {
		return result, ErrGroundItemDropConflict
	}

	row := tx.QueryRowContext(ctx, SelectCharacterInventory+" AND inv.SLOT=? FOR UPDATE", characterID, slot)
	persistedSlot, item, scanErr := scanInventoryItem(row)
	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return result, ErrGroundItemNotFound
		}
		return result, fmt.Errorf("load ground-item drop: %w", scanErr)
	}
	if persistedSlot != slot {
		return result, ErrGroundItemDropConflict
	}
	ref, exists := RefItems[item.GetRefObjectID()]
	if !exists || !ref.IsDropable {
		return result, fmt.Errorf("%w: reference item %d", ErrGroundItemNotDroppable, item.GetRefObjectID())
	}

	deleted, err := tx.ExecContext(ctx, deleteGroundDropInventory, characterID, slot, item.ID)
	if err != nil {
		return result, fmt.Errorf("delete ground-item inventory link: %w", err)
	}
	if rows, rowsErr := deleted.RowsAffected(); rowsErr != nil || rows != 1 {
		if rowsErr != nil {
			return result, fmt.Errorf("verify ground-item inventory deletion: %w", rowsErr)
		}
		return result, ErrGroundItemDropConflict
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	result.Item = &item
	result.Slot = slot
	if err = tx.Commit(); err != nil {
		return GroundItemDropResult{}, errors.Join(ErrGroundItemDropOutcomeUnknown, err)
	}
	committed = true
	return result, nil
}

func (r *SQLGroundItemPickupRepository) Restore(ctx context.Context, characterID int64, slot byte, itemID int64) (err error) {
	if r == nil || r.shard == nil {
		return ErrGroundItemRepositoryUnavailable
	}
	if characterID <= 0 || itemID <= 0 || slot < InventoryEquipmentSlots || slot >= MaxInventoryWireSize {
		return ErrGroundItemDropConflict
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ground-item drop restore: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var lockedCharacterID int64
	var capacity byte
	if err = tx.QueryRowContext(ctx, lockGroundPickupCharacter, characterID).Scan(&lockedCharacterID, &capacity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGroundItemCharacterNotFound
		}
		return fmt.Errorf("lock ground-item restore character: %w", err)
	}
	if lockedCharacterID != characterID {
		return ErrGroundItemDropConflict
	}
	if capacity != 0 && capacity <= MaxInventoryWireSize && slot >= capacity {
		return ErrGroundItemDropConflict
	}
	var lockedItemID int64
	if err = tx.QueryRowContext(ctx, lockGroundPickupItem, itemID).Scan(&lockedItemID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGroundItemNotFound
		}
		return fmt.Errorf("lock ground-item restore item: %w", err)
	}
	if lockedItemID != itemID {
		return ErrGroundItemDropConflict
	}
	var existingItemID int64
	if err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", characterID, slot).Scan(&existingItemID); err == nil {
		return fmt.Errorf("%w: slot %d is occupied", ErrGroundItemDropConflict, slot)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check ground-item restore slot: %w", err)
	}
	if _, err = tx.ExecContext(ctx, insertGroundDropInventory, characterID, slot, itemID); err != nil {
		return fmt.Errorf("restore ground-item inventory link: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return errors.Join(ErrGroundItemDropOutcomeUnknown, err)
	}
	committed = true
	return nil
}

func (r *SQLGroundItemPickupRepository) Pickup(ctx context.Context, characterID, itemID int64, slot byte) (result GroundItemPickupResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrGroundItemRepositoryUnavailable
	}
	if characterID <= 0 || itemID <= 0 {
		return result, ErrGroundItemPickupConflict
	}
	if slot < InventoryEquipmentSlots || slot >= MaxInventoryWireSize {
		return result, fmt.Errorf("%w: slot %d", ErrGroundItemPickupConflict, slot)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin ground-item pickup: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var lockedCharacterID int64
	var capacity byte
	if err = tx.QueryRowContext(ctx, lockGroundPickupCharacter, characterID).Scan(&lockedCharacterID, &capacity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrGroundItemCharacterNotFound
		}
		return result, fmt.Errorf("lock ground-item character: %w", err)
	}
	if lockedCharacterID != characterID {
		return result, ErrGroundItemPickupConflict
	}
	if capacity == 0 || capacity > MaxInventoryWireSize {
		capacity = MaxInventoryWireSize
	}
	if slot >= capacity {
		return result, ErrGroundItemInventoryFull
	}

	var lockedItemID int64
	if err = tx.QueryRowContext(ctx, lockGroundPickupItem, itemID).Scan(&lockedItemID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrGroundItemNotFound
		}
		return result, fmt.Errorf("lock ground item: %w", err)
	}
	if lockedItemID != itemID {
		return result, ErrGroundItemPickupConflict
	}

	var existingOwner int64
	err = tx.QueryRowContext(ctx, findGroundPickupOwner, itemID).Scan(&existingOwner)
	if err == nil {
		return result, fmt.Errorf("%w: character %d", ErrGroundItemAlreadyPickedUp, existingOwner)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("check ground-item ownership: %w", err)
	}

	rows, err := tx.QueryContext(ctx, lockGroundPickupInventory, characterID)
	if err != nil {
		return result, fmt.Errorf("lock ground-item inventory: %w", err)
	}
	occupied := false
	for rows.Next() {
		var persistedSlot int64
		if scanErr := rows.Scan(&persistedSlot); scanErr != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan ground-item inventory: %w", scanErr)
		}
		if persistedSlot == int64(slot) {
			occupied = true
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return result, fmt.Errorf("iterate ground-item inventory: %w", err)
	}
	if err = rows.Close(); err != nil {
		return result, fmt.Errorf("close ground-item inventory: %w", err)
	}
	if occupied {
		return result, fmt.Errorf("%w: slot %d", ErrGroundItemPickupConflict, slot)
	}

	if _, err = tx.ExecContext(ctx, insertGroundPickup, characterID, slot, itemID); err != nil {
		return result, fmt.Errorf("insert ground-item inventory link: %w", err)
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	result.Slot = slot
	if err = tx.Commit(); err != nil {
		return GroundItemPickupResult{}, errors.Join(ErrGroundItemPickupOutcomeUnknown, err)
	}
	committed = true
	return result, nil
}

func (r *SQLGroundItemPickupRepository) PickupGold(ctx context.Context, characterID, itemID int64) (result GroundGoldPickupResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrGroundItemRepositoryUnavailable
	}
	if characterID <= 0 || itemID <= 0 {
		return result, ErrGroundItemPickupConflict
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin ground-gold pickup: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var lockedCharacterID int64
	var currentGold uint64
	if err = tx.QueryRowContext(ctx, lockGroundGoldCharacter, characterID).Scan(&lockedCharacterID, &currentGold); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrGroundItemCharacterNotFound
		}
		return result, fmt.Errorf("lock ground-gold character: %w", err)
	}
	if lockedCharacterID != characterID {
		return result, ErrGroundItemPickupConflict
	}

	var lockedItemID, referenceItemID int64
	var amount uint64
	if err = tx.QueryRowContext(ctx, lockGroundGoldItem, itemID).Scan(&lockedItemID, &referenceItemID, &amount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrGroundItemNotFound
		}
		return result, fmt.Errorf("lock ground-gold item: %w", err)
	}
	if lockedItemID != itemID || referenceItemID < 1 || referenceItemID > 3 || amount == 0 {
		return result, ErrGroundItemPickupConflict
	}
	var existingOwner int64
	err = tx.QueryRowContext(ctx, findGroundPickupOwner, itemID).Scan(&existingOwner)
	if err == nil {
		return result, fmt.Errorf("%w: character %d", ErrGroundItemAlreadyPickedUp, existingOwner)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("check ground-gold ownership: %w", err)
	}
	if amount > ^uint64(0)-currentGold {
		return result, ErrGroundGoldOverflow
	}
	newGold := currentGold + amount
	updated, err := tx.ExecContext(ctx, creditGroundGold, newGold, characterID)
	if err != nil {
		return result, fmt.Errorf("credit ground gold: %w", err)
	}
	if rows, rowsErr := updated.RowsAffected(); rowsErr != nil || rows != 1 {
		if rowsErr != nil {
			return result, fmt.Errorf("verify ground-gold credit: %w", rowsErr)
		}
		return result, ErrGroundItemPickupConflict
	}
	deleted, err := tx.ExecContext(ctx, deleteGroundGoldItem, itemID)
	if err != nil {
		return result, fmt.Errorf("delete credited ground gold item: %w", err)
	}
	if rows, rowsErr := deleted.RowsAffected(); rowsErr != nil || rows != 1 {
		if rowsErr != nil {
			return result, fmt.Errorf("verify credited ground-gold removal: %w", rowsErr)
		}
		return result, ErrGroundItemPickupConflict
	}
	result.Gold, result.Amount = newGold, amount
	if err = tx.Commit(); err != nil {
		return GroundGoldPickupResult{}, errors.Join(ErrGroundItemPickupOutcomeUnknown, err)
	}
	committed = true
	return result, nil
}
