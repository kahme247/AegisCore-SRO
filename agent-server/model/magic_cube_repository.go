package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrMagicCubeUnavailable    = errors.New("magic cube repository is unavailable")
	ErrMagicCubeInvalid        = errors.New("magic cube request is invalid")
	ErrMagicCubeConflict       = errors.New("magic cube state conflicts with persistence")
	ErrMagicCubeOutcomeUnknown = errors.New("magic cube commit outcome is unknown")
)

type MagicCubeMoveResult struct {
	Inventory Inventory
	ItemID    int
}

type MagicCubeRepository interface {
	Push(context.Context, int, uint32, int, byte, byte, uint16) (MagicCubeMoveResult, error)
	Pop(context.Context, int, uint32, int, byte, byte, uint16) (MagicCubeMoveResult, error)
}

type SQLMagicCubeRepository struct{ shard *sql.DB }

func NewMagicCubeRepository(shard *sql.DB) (*SQLMagicCubeRepository, error) {
	if shard == nil {
		return nil, ErrMagicCubeUnavailable
	}
	return &SQLMagicCubeRepository{shard: shard}, nil
}

func (r *SQLMagicCubeRepository) Push(ctx context.Context, characterID int, userID uint32, cubeID int, source, destination byte, amount uint16) (MagicCubeMoveResult, error) {
	return r.move(ctx, characterID, userID, cubeID, source, destination, amount, true)
}

func (r *SQLMagicCubeRepository) Pop(ctx context.Context, characterID int, userID uint32, cubeID int, source, destination byte, amount uint16) (MagicCubeMoveResult, error) {
	return r.move(ctx, characterID, userID, cubeID, source, destination, amount, false)
}

func (r *SQLMagicCubeRepository) move(ctx context.Context, characterID int, userID uint32, cubeID int, source, destination byte, amount uint16, push bool) (MagicCubeMoveResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || cubeID <= 0 || amount == 0 {
		return MagicCubeMoveResult{}, ErrMagicCubeInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return MagicCubeMoveResult{}, fmt.Errorf("begin magic cube move: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var owner uint32
	var inventorySize byte
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER`,`INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner, &inventorySize); err != nil || owner != userID {
		return MagicCubeMoveResult{}, ErrMagicCubeConflict
	}
	normalSlot := source
	if !push {
		normalSlot = destination
	}
	if normalSlot < InventoryEquipmentSlots || normalSlot >= inventorySize || inventorySize > MaxInventoryWireSize {
		return MagicCubeMoveResult{}, ErrMagicCubeInvalid
	}
	var capacity int
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(NULLIF(ref.`Param9`,0),0) FROM `INVENTORY` inv JOIN `ITEM` item ON item.`id`=inv.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=item.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND inv.`FK_ITEM`=? AND ref.`TypeID1`=3 AND ref.`TypeID2`=2 AND ref.`TypeID3`=3 FOR UPDATE", characterID, cubeID).Scan(&capacity); err != nil || capacity <= 0 || capacity > 255 {
		return MagicCubeMoveResult{}, ErrMagicCubeConflict
	}
	if (push && int(destination) >= capacity) || (!push && int(source) >= capacity) {
		return MagicCubeMoveResult{}, ErrMagicCubeInvalid
	}
	for slot := 0; slot < capacity; slot++ {
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO `MAGIC_CUBE_INVENTORY` (`FK_CUBE_ITEM`,`SLOT`,`FK_ITEM`) VALUES (?,?,NULL)", cubeID, slot); err != nil {
			return MagicCubeMoveResult{}, fmt.Errorf("allocate magic cube slots: %w", err)
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT `SLOT`,`FK_ITEM` FROM `MAGIC_CUBE_INVENTORY` WHERE `FK_CUBE_ITEM`=? ORDER BY `SLOT` FOR UPDATE", cubeID)
	if err != nil {
		return MagicCubeMoveResult{}, fmt.Errorf("lock magic cube slots: %w", err)
	}
	lockedSlots := 0
	for rows.Next() {
		var slot byte
		var item sql.NullInt64
		if err = rows.Scan(&slot, &item); err != nil {
			_ = rows.Close()
			return MagicCubeMoveResult{}, err
		}
		lockedSlots++
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return MagicCubeMoveResult{}, err
	}
	if lockedSlots != capacity {
		return MagicCubeMoveResult{}, ErrMagicCubeConflict
	}

	sourceTable, sourceOwner, destinationTable, destinationOwner := "INVENTORY", "FK_CHAR", "MAGIC_CUBE_INVENTORY", "FK_CUBE_ITEM"
	sourceOwnerID, destinationOwnerID := characterID, cubeID
	if !push {
		sourceTable, sourceOwner, destinationTable, destinationOwner = destinationTable, destinationOwner, sourceTable, sourceOwner
		sourceOwnerID, destinationOwnerID = destinationOwnerID, sourceOwnerID
	}
	var itemID, refID, stack, sourceMaxStack int
	query := fmt.Sprintf("SELECT link.`FK_ITEM`,item.`FK_REF_ITEM`,item.`STACK_COUNT`,COALESCE(NULLIF(ref.`Stack_Size`,0),1) FROM `%s` link JOIN `ITEM` item ON item.`id`=link.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=item.`FK_REF_ITEM` WHERE link.`%s`=? AND link.`SLOT`=? FOR UPDATE", sourceTable, sourceOwner)
	if err = tx.QueryRowContext(ctx, query, sourceOwnerID, source).Scan(&itemID, &refID, &stack, &sourceMaxStack); err != nil || stack < int(amount) || (stack != int(amount) && sourceMaxStack <= 1) || (push && itemID == cubeID) {
		return MagicCubeMoveResult{}, ErrMagicCubeConflict
	}
	var destinationItemID, destinationRef, destinationStack, maxStack int
	query = fmt.Sprintf("SELECT link.`FK_ITEM`,item.`FK_REF_ITEM`,item.`STACK_COUNT`,COALESCE(NULLIF(ref.`Stack_Size`,0),1) FROM `%s` link JOIN `ITEM` item ON item.`id`=link.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=item.`FK_REF_ITEM` WHERE link.`%s`=? AND link.`SLOT`=? FOR UPDATE", destinationTable, destinationOwner)
	destinationErr := tx.QueryRowContext(ctx, query, destinationOwnerID, destination).Scan(&destinationItemID, &destinationRef, &destinationStack, &maxStack)
	if destinationErr == nil {
		if destinationRef != refID || destinationStack+int(amount) > maxStack {
			return MagicCubeMoveResult{}, ErrMagicCubeConflict
		}
		result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `DATA`=IF(`DATA`=`STACK_COUNT`,`DATA`+?,`DATA`),`STACK_COUNT`=`STACK_COUNT`+? WHERE `id`=? AND `STACK_COUNT`=?", amount, amount, destinationItemID, destinationStack)
		if updateErr != nil || !singleAffectedRow(result) {
			return MagicCubeMoveResult{}, ErrMagicCubeConflict
		}
		if err = reduceMagicCubeSource(ctx, tx, sourceTable, sourceOwner, sourceOwnerID, source, itemID, stack, int(amount)); err != nil {
			return MagicCubeMoveResult{}, err
		}
		itemID = destinationItemID
	} else if !errors.Is(destinationErr, sql.ErrNoRows) {
		return MagicCubeMoveResult{}, ErrMagicCubeConflict
	} else if stack == int(amount) {
		if err = relinkMagicCubeItem(ctx, tx, sourceTable, sourceOwner, sourceOwnerID, source, destinationTable, destinationOwner, destinationOwnerID, destination, itemID); err != nil {
			return MagicCubeMoveResult{}, err
		}
	} else {
		result, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) SELECT `VARIANCE`,`FK_REF_ITEM`,?,`OPT_LEVEL`,IF(`DATA`=`STACK_COUNT`,?,`DATA`),`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME` FROM `ITEM` WHERE `id`=?", amount, amount, itemID)
		if insertErr != nil {
			return MagicCubeMoveResult{}, insertErr
		}
		newID, idErr := result.LastInsertId()
		if idErr != nil || newID <= 0 {
			return MagicCubeMoveResult{}, ErrMagicCubeConflict
		}
		updateResult, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `DATA`=IF(`DATA`=`STACK_COUNT`,`DATA`-?,`DATA`),`STACK_COUNT`=`STACK_COUNT`-? WHERE `id`=? AND `STACK_COUNT`=?", amount, amount, itemID, stack)
		if updateErr != nil || !singleAffectedRow(updateResult) {
			return MagicCubeMoveResult{}, ErrMagicCubeConflict
		}
		if err = linkMagicCubeDestination(ctx, tx, destinationTable, destinationOwner, destinationOwnerID, destination, int(newID)); err != nil {
			return MagicCubeMoveResult{}, err
		}
		itemID = int(newID)
	}
	if err = tx.Commit(); err != nil {
		return MagicCubeMoveResult{}, errors.Join(ErrMagicCubeOutcomeUnknown, err)
	}
	committed = true
	inventoryRepository, err := NewInventoryRepository(r.shard)
	if err != nil {
		return MagicCubeMoveResult{}, err
	}
	inventory, err := inventoryRepository.GetCharacterInventory(ctx, int64(characterID))
	return MagicCubeMoveResult{Inventory: inventory, ItemID: itemID}, err
}

func reduceMagicCubeSource(ctx context.Context, tx *sql.Tx, table, owner string, ownerID int, slot byte, itemID, stack, amount int) error {
	if stack == amount {
		query := fmt.Sprintf("UPDATE `%s` SET `FK_ITEM`=NULL WHERE `%s`=? AND `SLOT`=? AND `FK_ITEM`=?", table, owner)
		if table == "INVENTORY" {
			query = fmt.Sprintf("DELETE FROM `%s` WHERE `%s`=? AND `SLOT`=? AND `FK_ITEM`=?", table, owner)
		}
		result, err := tx.ExecContext(ctx, query, ownerID, slot, itemID)
		if err != nil || !singleAffectedRow(result) {
			return ErrMagicCubeConflict
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", itemID)
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `DATA`=IF(`DATA`=`STACK_COUNT`,`DATA`-?,`DATA`),`STACK_COUNT`=`STACK_COUNT`-? WHERE `id`=? AND `STACK_COUNT`=?", amount, amount, itemID, stack)
	if err != nil || !singleAffectedRow(result) {
		return ErrMagicCubeConflict
	}
	return nil
}

func relinkMagicCubeItem(ctx context.Context, tx *sql.Tx, sourceTable, sourceOwner string, sourceOwnerID int, source byte, destinationTable, destinationOwner string, destinationOwnerID int, destination byte, itemID int) error {
	if sourceTable == "INVENTORY" {
		if result, err := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", sourceOwnerID, source, itemID); err != nil || !singleAffectedRow(result) {
			return ErrMagicCubeConflict
		}
	} else if result, err := tx.ExecContext(ctx, "UPDATE `MAGIC_CUBE_INVENTORY` SET `FK_ITEM`=NULL WHERE `FK_CUBE_ITEM`=? AND `SLOT`=? AND `FK_ITEM`=?", sourceOwnerID, source, itemID); err != nil || !singleAffectedRow(result) {
		return ErrMagicCubeConflict
	}
	return linkMagicCubeDestination(ctx, tx, destinationTable, destinationOwner, destinationOwnerID, destination, itemID)
}

func linkMagicCubeDestination(ctx context.Context, tx *sql.Tx, table, owner string, ownerID int, slot byte, itemID int) error {
	if table == "INVENTORY" {
		_, err := tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", ownerID, slot, itemID)
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE `MAGIC_CUBE_INVENTORY` SET `FK_ITEM`=? WHERE `FK_CUBE_ITEM`=? AND `SLOT`=? AND `FK_ITEM` IS NULL", itemID, ownerID, slot)
	if err != nil || !singleAffectedRow(result) {
		return ErrMagicCubeConflict
	}
	return nil
}
