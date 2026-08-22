package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const AvatarInventorySize byte = 5

var (
	ErrAvatarRepositoryUnavailable = errors.New("avatar inventory repository is unavailable")
	ErrAvatarSlotOutOfRange        = errors.New("avatar inventory slot is out of range")
	ErrAvatarItemTypeInvalid       = errors.New("item cannot be equipped in the avatar slot")
	ErrDevilSpiritInactive         = errors.New("devil spirit is not active")
	ErrDevilSpiritDurationEmpty    = errors.New("devil spirit has no remaining duration")
	ErrAvatarInventoryFull         = errors.New("normal inventory has no free slot for avatar item")
)

const selectAvatarInventory = "SELECT inv.SLOT,inv.FK_ITEM,it.VARIANCE,it.FK_REF_ITEM,it.STACK_COUNT,it.OPT_LEVEL,it.DATA,it.RENT_TYPE,it.RENT_CAN_DELETE,it.RENT_CAN_RECHARGE,it.RENT_PERIOD_BEGIN_TIME,it.RENT_PERIOD_END_TIME,it.RENT_METER_RATE_TIME,it.RENT_PACKING_TIME,0,0,0,'',0,NULL,0,0,COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',mo.SLOT,'type',mo.TYPE,'value',mo.VALUE,'paramType',COALESCE(ro.PARAM_TYPE,0))) FROM `SRO_SHARD`.`ITEM_MAGIC_OPTION` mo LEFT JOIN `SRO_SHARD`.`REF_MAGIC_OPTION` ro ON ro.ID=mo.TYPE WHERE mo.FK_ITEM=it.ID),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `SRO_SHARD`.`ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=1),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `SRO_SHARD`.`ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=2),JSON_ARRAY()),0,0,0 FROM `SRO_SHARD`.`AVATAR_INVENTORY` inv INNER JOIN `SRO_SHARD`.`ITEM` it ON it.ID=inv.FK_ITEM WHERE inv.FK_CHAR=? ORDER BY inv.SLOT"

type AvatarInventoryRepository interface {
	GetCharacterAvatarInventory(context.Context, int64) (Inventory, error)
	TransferAvatarItem(context.Context, int64, bool, byte, byte) (AvatarTransferResult, error)
	ActivateDevilSpirit(context.Context, int64, byte) (DevilSpiritActivation, error)
	ExpireDevilSpirit(context.Context, int64, int, time.Time) (DevilSpiritExpiration, error)
}

type AvatarTransferResult struct {
	Inventory       Inventory
	AvatarInventory Inventory
}

type DevilSpiritActivation struct {
	Inventory Inventory
	Item      Item
	Slot      byte
	ExpiresAt time.Time
}

type DevilSpiritExpiration struct {
	AvatarTransferResult
	Expired       bool
	WasEquipped   bool
	AvatarSlot    byte
	InventorySlot byte
	Item          Item
}

type SQLAvatarInventoryRepository struct{ shard *sql.DB }

func NewAvatarInventoryRepository(shard *sql.DB) (*SQLAvatarInventoryRepository, error) {
	if shard == nil {
		return nil, ErrAvatarRepositoryUnavailable
	}
	return &SQLAvatarInventoryRepository{shard: shard}, nil
}

func (r *SQLAvatarInventoryRepository) TransferAvatarItem(ctx context.Context, characterID int64, equip bool, sourceSlot, targetSlot byte) (result AvatarTransferResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrAvatarRepositoryUnavailable
	}
	if characterID <= 0 || sourceSlot == targetSlot {
		return result, ErrAlchemyInvalidRequest
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if equip {
		if sourceSlot < 13 || targetSlot >= AvatarInventorySize {
			return result, ErrAvatarSlotOutOfRange
		}
		var itemID int
		var refID, data, periodEnd uint32
		if err = tx.QueryRowContext(ctx, "SELECT inv.FK_ITEM,it.FK_REF_ITEM,it.DATA,it.RENT_PERIOD_END_TIME FROM `INVENTORY` inv JOIN `ITEM` it ON it.id=inv.FK_ITEM WHERE inv.FK_CHAR=? AND inv.SLOT=? FOR UPDATE", characterID, sourceSlot).Scan(&itemID, &refID, &data, &periodEnd); err != nil {
			return result, ErrAvatarItemTypeInvalid
		}
		ref, ok := RefItems[refID]
		want, valid := AvatarSlotFor(ref)
		if !ok || !valid || want != targetSlot {
			return result, ErrAvatarItemTypeInvalid
		}
		if ref.IsDevilSpirit() && (data == 0 || periodEnd <= uint32(time.Now().Unix())) {
			return result, ErrDevilSpiritInactive
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, sourceSlot, itemID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `AVATAR_INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, targetSlot, itemID); err != nil {
			return result, err
		}
	} else {
		if sourceSlot >= AvatarInventorySize || targetSlot < 13 || targetSlot >= BaseInventorySize {
			return result, ErrAvatarSlotOutOfRange
		}
		var itemID int
		if err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `AVATAR_INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", characterID, sourceSlot).Scan(&itemID); err != nil {
			return result, ErrAvatarItemTypeInvalid
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `AVATAR_INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, sourceSlot, itemID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, targetSlot, itemID); err != nil {
			return result, err
		}
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AvatarTransferResult{}, err
	}
	rows, err := tx.QueryContext(ctx, selectAvatarInventory, characterID)
	if err != nil {
		return AvatarTransferResult{}, err
	}
	result.AvatarInventory = emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			_ = rows.Close()
			return AvatarTransferResult{}, scanErr
		}
		result.AvatarInventory.Items[slot] = item
	}
	if err = rows.Close(); err != nil {
		return AvatarTransferResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return AvatarTransferResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}

func (r *SQLAvatarInventoryRepository) ActivateDevilSpirit(ctx context.Context, characterID int64, slot byte) (activation DevilSpiritActivation, err error) {
	if r == nil || r.shard == nil {
		return activation, ErrAvatarRepositoryUnavailable
	}
	if characterID <= 0 || slot < 13 || slot >= BaseInventorySize {
		return activation, ErrAvatarSlotOutOfRange
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return activation, err
	}
	defer func() { _ = tx.Rollback() }()
	var itemID int
	var refID, durationMS, periodEnd uint32
	if err = tx.QueryRowContext(ctx, "SELECT inv.FK_ITEM,it.FK_REF_ITEM,it.DATA,it.RENT_PERIOD_END_TIME FROM `INVENTORY` inv JOIN `ITEM` it ON it.ID=inv.FK_ITEM WHERE inv.FK_CHAR=? AND inv.SLOT=? FOR UPDATE", characterID, slot).Scan(&itemID, &refID, &durationMS, &periodEnd); err != nil {
		return activation, ErrAvatarItemTypeInvalid
	}
	ref, known := RefItems[refID]
	if !known || !ref.IsDevilSpirit() {
		return activation, ErrAvatarItemTypeInvalid
	}
	if durationMS == 0 {
		return activation, ErrDevilSpiritDurationEmpty
	}
	now := time.Now()
	activation.ExpiresAt = time.Unix(int64(periodEnd), 0)
	if periodEnd <= uint32(now.Unix()) {
		activation.ExpiresAt = now.Add(time.Duration(durationMS) * time.Millisecond)
		if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `RENT_PERIOD_BEGIN_TIME`=?,`RENT_PERIOD_END_TIME`=? WHERE `ID`=?", uint32(now.Unix()), uint32(activation.ExpiresAt.Unix()), itemID); err != nil {
			return DevilSpiritActivation{}, err
		}
	}
	activation.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return DevilSpiritActivation{}, err
	}
	activation.Item = activation.Inventory.Items[slot]
	activation.Slot = slot
	if err = tx.Commit(); err != nil {
		return DevilSpiritActivation{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return activation, nil
}

func (r *SQLAvatarInventoryRepository) ExpireDevilSpirit(ctx context.Context, characterID int64, itemID int, now time.Time) (expiration DevilSpiritExpiration, err error) {
	if r == nil || r.shard == nil {
		return expiration, ErrAvatarRepositoryUnavailable
	}
	if characterID <= 0 || itemID <= 0 {
		return expiration, ErrAlchemyInvalidRequest
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return expiration, err
	}
	defer func() { _ = tx.Rollback() }()
	var refID, data, periodEnd uint32
	if err = tx.QueryRowContext(ctx, "SELECT FK_REF_ITEM,DATA,RENT_PERIOD_END_TIME FROM `ITEM` WHERE ID=? FOR UPDATE", itemID).Scan(&refID, &data, &periodEnd); err != nil {
		return expiration, err
	}
	ref, known := RefItems[refID]
	if !known || !ref.IsDevilSpirit() {
		return expiration, ErrAvatarItemTypeInvalid
	}
	if data == 0 || periodEnd == 0 || periodEnd > uint32(now.Unix()) {
		return expiration, nil
	}
	var avatarSlot byte
	avatarErr := tx.QueryRowContext(ctx, "SELECT SLOT FROM `AVATAR_INVENTORY` WHERE FK_CHAR=? AND FK_ITEM=? FOR UPDATE", characterID, itemID).Scan(&avatarSlot)
	if avatarErr != nil && !errors.Is(avatarErr, sql.ErrNoRows) {
		return expiration, avatarErr
	}
	if avatarErr == nil {
		occupied := make(map[byte]bool)
		rows, queryErr := tx.QueryContext(ctx, "SELECT SLOT FROM `INVENTORY` WHERE FK_CHAR=? FOR UPDATE", characterID)
		if queryErr != nil {
			return expiration, queryErr
		}
		for rows.Next() {
			var occupiedSlot byte
			if scanErr := rows.Scan(&occupiedSlot); scanErr != nil {
				_ = rows.Close()
				return expiration, scanErr
			}
			occupied[occupiedSlot] = true
		}
		if err = rows.Close(); err != nil {
			return expiration, err
		}
		for slot := byte(13); slot < BaseInventorySize; slot++ {
			if !occupied[slot] {
				expiration.InventorySlot = slot
				break
			}
		}
		if expiration.InventorySlot < 13 {
			return expiration, ErrAvatarInventoryFull
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `AVATAR_INVENTORY` WHERE FK_CHAR=? AND SLOT=? AND FK_ITEM=?", characterID, avatarSlot, itemID); err != nil {
			return expiration, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", characterID, expiration.InventorySlot, itemID); err != nil {
			return expiration, err
		}
		expiration.WasEquipped = true
		expiration.AvatarSlot = avatarSlot
	}
	if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET DATA=0,RENT_PERIOD_BEGIN_TIME=0,RENT_PERIOD_END_TIME=0 WHERE ID=?", itemID); err != nil {
		return expiration, err
	}
	expiration.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return DevilSpiritExpiration{}, err
	}
	expiration.AvatarInventory, err = loadAvatarInventoryInTransaction(ctx, tx, characterID)
	if err != nil {
		return DevilSpiritExpiration{}, err
	}
	if expiration.WasEquipped {
		expiration.Item = expiration.Inventory.Items[expiration.InventorySlot]
	}
	expiration.Expired = true
	if err = tx.Commit(); err != nil {
		return DevilSpiritExpiration{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return expiration, nil
}

func loadAvatarInventoryInTransaction(ctx context.Context, tx *sql.Tx, characterID int64) (Inventory, error) {
	rows, err := tx.QueryContext(ctx, selectAvatarInventory, characterID)
	if err != nil {
		return emptyInventory(), err
	}
	defer rows.Close()
	result := emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return emptyInventory(), scanErr
		}
		result.Items[slot] = item
	}
	return result, rows.Err()
}

func (r *SQLAvatarInventoryRepository) GetCharacterAvatarInventory(ctx context.Context, characterID int64) (Inventory, error) {
	if r == nil || r.shard == nil {
		return emptyInventory(), ErrAvatarRepositoryUnavailable
	}
	if characterID <= 0 {
		return emptyInventory(), ErrInventoryCharacterIDRequired
	}
	// A server restart must not leave an activated Devil Spirit equipped after
	// its persisted deadline. Reconcile it before the login snapshot is built.
	dueRows, err := r.shard.QueryContext(ctx, "SELECT ai.FK_ITEM,it.FK_REF_ITEM,it.DATA,it.RENT_PERIOD_END_TIME FROM `AVATAR_INVENTORY` ai JOIN `ITEM` it ON it.ID=ai.FK_ITEM WHERE ai.FK_CHAR=? AND it.RENT_PERIOD_END_TIME>0 AND it.RENT_PERIOD_END_TIME<=?", characterID, uint32(time.Now().Unix()))
	if err != nil {
		return emptyInventory(), fmt.Errorf("query expired avatar items: %w", err)
	}
	var due []int
	for dueRows.Next() {
		var itemID int
		var refID, data, periodEnd uint32
		if scanErr := dueRows.Scan(&itemID, &refID, &data, &periodEnd); scanErr != nil {
			_ = dueRows.Close()
			return emptyInventory(), scanErr
		}
		if ref, exists := RefItems[refID]; exists && ref.IsDevilSpirit() && data > 0 && periodEnd > 0 {
			due = append(due, itemID)
		}
	}
	if err = dueRows.Close(); err != nil {
		return emptyInventory(), err
	}
	for _, itemID := range due {
		if _, expireErr := r.ExpireDevilSpirit(ctx, characterID, itemID, time.Now()); expireErr != nil {
			return emptyInventory(), fmt.Errorf("expire devil spirit during login: %w", expireErr)
		}
	}
	rows, err := r.shard.QueryContext(ctx, selectAvatarInventory, characterID)
	if err != nil {
		return emptyInventory(), fmt.Errorf("query avatar inventory: %w", err)
	}
	defer rows.Close()
	result := emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return emptyInventory(), fmt.Errorf("scan avatar inventory: %w", scanErr)
		}
		if slot >= AvatarInventorySize {
			return emptyInventory(), ErrAvatarSlotOutOfRange
		}
		ref := RefItems[item.RefObjectID]
		want, ok := AvatarSlotFor(ref)
		if !ok || want != slot {
			return emptyInventory(), ErrAvatarItemTypeInvalid
		}
		result.Items[slot] = item
	}
	if err = rows.Err(); err != nil {
		return emptyInventory(), fmt.Errorf("iterate avatar inventory: %w", err)
	}
	return result, nil
}
