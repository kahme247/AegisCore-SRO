package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrItemUseRepositoryUnavailable = errors.New("item-use repository is unavailable")
	ErrItemUseInvalidRequest        = errors.New("invalid item-use request")
	ErrItemUseUnsupported           = errors.New("item use is not supported")
	ErrItemUseNoEffect              = errors.New("item use would have no effect")
	ErrItemUseOutcomeUnknown        = errors.New("item-use transaction outcome is unknown")
)

type ItemUseResult struct {
	Inventory       Inventory
	HP              int
	MP              int
	RefItemID       uint32
	ItemType        uint16
	SourceSlot      byte
	NewAmount       uint16
	EffectSkillID   uint32
	SpeedPercent    int
	EffectDuration  int
	EffectExpiresAt time.Time
}

type ItemUseVitals struct {
	HP, MP       int
	MaxHP, MaxMP int
}

type ItemUseRepository interface {
	UseRecoveryItem(context.Context, int64, byte, uint16, ItemUseVitals) (ItemUseResult, error)
}

type SpeedEffect struct {
	SkillID   uint32
	Percent   int
	ExpiresAt time.Time
}

type SpeedEffectRepository interface {
	LoadSpeedEffect(context.Context, int64) (SpeedEffect, bool, error)
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
	var skillID, durationMarker, duration, speedMarker, speedPercent int
	if err = tx.QueryRowContext(ctx, "SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,COALESCE(s.`ID`,0),COALESCE(s.`PARAM3`,0),COALESCE(s.`PARAM4`,0),COALESCE(s.`PARAM5`,0),COALESCE(s.`PARAM6`,0) FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` r ON r.`ItemID`=it.`FK_REF_ITEM` LEFT JOIN `REF_SKILL` s ON s.`BASIC_CODE`=r.`Desc_1` AND s.`SERVICE`=1 WHERE i.`FK_CHAR`=? AND i.`SLOT`=? FOR UPDATE", characterID, slot).Scan(&itemID, &refID, &stack, &skillID, &durationMarker, &duration, &speedMarker, &speedPercent); err != nil {
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
	newHP, newMP := vitals.HP, vitals.MP
	isRecovery := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 1 && ref.TypeID4 >= 1 && ref.TypeID4 <= 3
	isSpeedDrug := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 1
	switch {
	case !ref.IsUseable:
		return result, ErrItemUseUnsupported
	case isRecovery:
		var changed bool
		newHP, newMP, changed = recoveryValues(ref, vitals)
		if !changed {
			return result, ErrItemUseNoEffect
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `HP`=?,`MP`=? WHERE `id`=?", newHP, newMP, characterID); err != nil {
			return result, fmt.Errorf("persist item-use recovery: %w", err)
		}
	case isSpeedDrug:
		// vSRO speed-drug skills encode duration and movement percentage in
		// PARAM4/PARAM6, guarded by their standard parameter identifiers.
		if skillID == 0 || durationMarker != 1685418593 || speedMarker != 1752396901 || duration <= 0 || speedPercent <= 0 {
			return result, ErrItemUseUnsupported
		}
		now := time.Now().UTC()
		var currentExpiration time.Time
		err = tx.QueryRowContext(ctx, "SELECT `EXPIRES_AT` FROM `CHAR_SPEED_EFFECT` WHERE `CHAR_ID`=? FOR UPDATE", characterID).Scan(&currentExpiration)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("lock character speed effect: %w", err)
		}
		expiresAt, extended := extendedSpeedEffectExpiration(now, currentExpiration, time.Duration(duration)*time.Millisecond)
		if !extended {
			return result, ErrItemUseNoEffect
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_SPEED_EFFECT` (`CHAR_ID`,`SKILL_ID`,`SPEED_PERCENT`,`EXPIRES_AT`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `SKILL_ID`=VALUES(`SKILL_ID`),`SPEED_PERCENT`=VALUES(`SPEED_PERCENT`),`EXPIRES_AT`=VALUES(`EXPIRES_AT`)", characterID, skillID, speedPercent, expiresAt); err != nil {
			return result, fmt.Errorf("persist character speed effect: %w", err)
		}
		result.EffectSkillID = uint32(skillID)
		result.SpeedPercent = speedPercent
		result.EffectDuration = int(expiresAt.Sub(now).Milliseconds())
		result.EffectExpiresAt = expiresAt
	default:
		return result, ErrItemUseUnsupported
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

const maximumSpeedEffectDuration = 24 * time.Hour

func extendedSpeedEffectExpiration(now, current time.Time, added time.Duration) (time.Time, bool) {
	now = now.UTC()
	maximum := now.Add(maximumSpeedEffectDuration)
	base := now
	if current.After(now) {
		base = current
	}
	if !base.Before(maximum) {
		return current, false
	}
	expiresAt := base.Add(added)
	if expiresAt.After(maximum) {
		expiresAt = maximum
	}
	return expiresAt, expiresAt.After(current)
}

func (r *SQLItemUseRepository) LoadSpeedEffect(ctx context.Context, characterID int64) (SpeedEffect, bool, error) {
	if r == nil || r.shard == nil {
		return SpeedEffect{}, false, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 {
		return SpeedEffect{}, false, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var effect SpeedEffect
	err := r.shard.QueryRowContext(ctx, "SELECT `SKILL_ID`,`SPEED_PERCENT`,`EXPIRES_AT` FROM `CHAR_SPEED_EFFECT` WHERE `CHAR_ID`=? AND `EXPIRES_AT`>UTC_TIMESTAMP(6)", characterID).Scan(&effect.SkillID, &effect.Percent, &effect.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SpeedEffect{}, false, nil
	}
	if err != nil {
		return SpeedEffect{}, false, fmt.Errorf("load character speed effect: %w", err)
	}
	effect.ExpiresAt = effect.ExpiresAt.UTC()
	return effect, true, nil
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
