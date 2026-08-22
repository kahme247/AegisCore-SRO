package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrItemUseRepositoryUnavailable = errors.New("item-use repository is unavailable")
	ErrItemUseInvalidRequest        = errors.New("invalid item-use request")
	ErrItemUseUnsupported           = errors.New("item use is not supported")
	ErrItemUseNoEffect              = errors.New("item use would have no effect")
	ErrItemUseExpired               = errors.New("item use has expired")
	ErrItemUseOutcomeUnknown        = errors.New("item-use transaction outcome is unknown")
	ErrItemServiceDailyUnavailable  = errors.New("item-service daily activation is unavailable")
)

type ItemUseResult struct {
	Inventory                Inventory
	HP                       int
	MP                       int
	RefItemID                uint32
	ItemType                 uint16
	SourceSlot               byte
	NewAmount                uint16
	EffectSkillID            uint32
	SpeedPercent             int
	EffectDuration           int
	EffectExpiresAt          time.Time
	ItemService              *ItemServiceEffect
	ItemServiceToken         uint32
	ReplacedItemServiceToken uint32
	ReturnDestination        *TeleportDestination
	ReturnDelay              time.Duration
	BuffModifiers            BuffModifiers
	COSPersistentID          uint64
	MaskRefCharID            uint32
	MaskDuration             time.Duration
}

type ItemUseVitals struct {
	HP, MP       int
	MaxHP, MaxMP int
}

type ItemUseRepository interface {
	UseItem(context.Context, int64, byte, uint16, ItemUseVitals) (ItemUseResult, error)
	UseResurrectionItem(context.Context, int64, ItemUseVitals) (ItemUseResult, error)
}

type SpeedEffect struct {
	SkillID   uint32
	Percent   int
	ExpiresAt time.Time
}

type SpeedEffectRepository interface {
	LoadSpeedEffect(context.Context, int64) (SpeedEffect, bool, error)
}

type ItemServiceKind byte

const (
	ItemServiceGold ItemServiceKind = iota + 1
	ItemServiceSkillGold
	ItemServiceExperience
	ItemServiceSkillExperience
)

type ItemServiceEffect struct {
	Kind                        ItemServiceKind
	SourceRefItemID             uint32
	ExperienceBonusPercent      uint32
	SkillExperienceBonusPercent uint32
	DailyBudget                 time.Duration
	DailyUsedOn                 time.Time
	DailyActiveUntil            time.Time
	ActivatedAt                 time.Time
	ExpiresAt                   time.Time
}

type ItemServiceRepository interface {
	LoadItemServices(context.Context, int64) ([]ItemServiceEffect, error)
	ActivateItemServiceDaily(context.Context, int64, ItemServiceKind, time.Time) (time.Time, error)
}

type ItemBuffEffect struct {
	SourceRefItemID uint32
	SkillID         uint32
	ExpiresAt       time.Time
	Modifiers       BuffModifiers
}

type ItemBuffRepository interface {
	LoadItemBuffs(context.Context, int64) ([]ItemBuffEffect, error)
}

type ResurrectionItemRepository interface {
	UseResurrectionItem(context.Context, int64, ItemUseVitals) (ItemUseResult, error)
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

// IsReturnScroll identifies the ordinary return-city scroll family from the
// reference-data contract. This includes the normal, fast, mall, event, and
// newbie variants. Reverse/friend/special-destination scrolls have different
// state or target requirements and deliberately do not match this predicate.
func IsReturnScroll(ref RefItem) bool {
	return ref.IsUseable &&
		ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 3 && ref.TypeID4 == 1 &&
		ref.Param1 > 0 && strings.EqualFold(strings.TrimSpace(ref.Param3Desc), "RESURRECT")
}

func IsReverseReturnScroll(ref RefItem) bool {
	return ref.IsUseable &&
		ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 3 && ref.TypeID4 == 3 &&
		ref.Param1 > 0
}

func IsTeleportScroll(ref RefItem) bool {
	return IsReturnScroll(ref) || IsReverseReturnScroll(ref)
}

func (r *SQLItemUseRepository) UseItem(ctx context.Context, characterID int64, slot byte, requestedType uint16, vitals ItemUseVitals) (result ItemUseResult, err error) {
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
	var origin TeleportDestination
	if err = tx.QueryRowContext(ctx, "SELECT `id`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&lockedCharacterID, &origin.Region, &origin.X, &origin.Y, &origin.Z); err != nil {
		return result, fmt.Errorf("lock item-use character: %w", err)
	}
	origin.WorldID = 1
	var itemID int64
	var refID uint32
	var itemData uint32
	var stack int
	var skillID, durationMarker, duration, speedMarker, speedPercent int
	if err = tx.QueryRowContext(ctx, "SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,it.`DATA`,COALESCE(s.`ID`,0),COALESCE(s.`PARAM3`,0),COALESCE(s.`PARAM4`,0),COALESCE(s.`PARAM5`,0),COALESCE(s.`PARAM6`,0) FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` r ON r.`ItemID`=it.`FK_REF_ITEM` LEFT JOIN `REF_SKILL` s ON s.`BASIC_CODE`=r.`Desc_1` AND s.`SERVICE`=1 WHERE i.`FK_CHAR`=? AND i.`SLOT`=? FOR UPDATE", characterID, slot).Scan(&itemID, &refID, &stack, &itemData, &skillID, &durationMarker, &duration, &speedMarker, &speedPercent); err != nil {
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
	isTimeService := isItemServiceReference(ref)
	isReturnScroll := IsReturnScroll(ref)
	isReverseReturnScroll := IsReverseReturnScroll(ref)
	isCOSScroll := IsCOSScroll(ref)
	isMonsterMask := ref.TypeID1 == 3 && ref.TypeID2 == 2 && ref.TypeID3 == 2 && itemData != 0
	linkedSkill, hasLinkedSkill := RefSkills[uint32(skillID)]
	isSkillBuff := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 1 &&
		hasLinkedSkill && linkedSkill.ExecutionKind() == SkillExecutionBuff && linkedSkill.BuffDuration > 0 && linkedSkill.BuffModifiers != (BuffModifiers{})
	switch {
	case !ref.IsUseable:
		return result, ErrItemUseUnsupported
	case isCOSScroll:
		if itemData != 0 {
			var cosOwnerID int64
			var cosKind byte
			var cosRentEndsAt sql.NullTime
			err = tx.QueryRowContext(ctx, "SELECT `OWNER_CHAR_ID`,`KIND`,`RENT_ENDS_AT` FROM `CHAR_COS` WHERE `ID`=? FOR UPDATE", itemData).Scan(&cosOwnerID, &cosKind, &cosRentEndsAt)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				// Items imported from the legacy inventory can retain a stale COS
				// identifier even when _CharCOS itself was not migrated. Recreate
				// the missing owned row and replace DATA atomically below.
				itemData = 0
			case err != nil:
				return result, fmt.Errorf("lock item COS: %w", err)
			case cosOwnerID != characterID:
				return result, ErrItemUseInvalidRequest
			case validateCOSItemRental(COSKind(cosKind), cosRentEndsAt, time.Now().UTC()) != nil:
				return result, ErrItemUseExpired
			}
		}
		if itemData == 0 {
			createdID, createErr := createCOSForItemUse(ctx, tx, characterID, itemID, slot, ref)
			if createErr != nil {
				return result, createErr
			}
			itemData = createdID
		}
		result.COSPersistentID = uint64(itemData)
	case isMonsterMask:
		if _, exists := RefChars[itemData]; !exists {
			return result, ErrItemUseNoEffect
		}
		result.MaskRefCharID = itemData
		result.MaskDuration = time.Duration(max(0, ref.DecayTime)) * time.Millisecond
	case isRecovery:
		var changed bool
		newHP, newMP, changed = recoveryValues(ref, vitals)
		if !changed {
			return result, ErrItemUseNoEffect
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `HP`=?,`MP`=? WHERE `id`=?", newHP, newMP, characterID); err != nil {
			return result, fmt.Errorf("persist item-use recovery: %w", err)
		}
	case isSkillBuff:
		now := time.Now().UTC()
		expiresAt := now.Add(time.Duration(linkedSkill.BuffDuration) * time.Millisecond)
		if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_ITEM_BUFF` (`CHAR_ID`,`SKILL_ID`,`SOURCE_REF_ITEM_ID`,`EXPIRES_AT`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `SOURCE_REF_ITEM_ID`=VALUES(`SOURCE_REF_ITEM_ID`),`EXPIRES_AT`=VALUES(`EXPIRES_AT`)", characterID, linkedSkill.ID, ref.ID, expiresAt); err != nil {
			return result, fmt.Errorf("persist character item buff: %w", err)
		}
		result.EffectSkillID = linkedSkill.ID
		result.EffectDuration = linkedSkill.BuffDuration
		result.EffectExpiresAt = expiresAt
		result.BuffModifiers = linkedSkill.BuffModifiers
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
	case isTimeService:
		effect, effectErr := itemServiceFromReference(ref, time.Now().UTC())
		if effectErr != nil {
			return result, effectErr
		}
		var currentExpiration time.Time
		err = tx.QueryRowContext(ctx, "SELECT `EXPIRES_AT` FROM `CHAR_ITEM_SERVICE` WHERE `CHAR_ID`=? AND `SERVICE_KIND`=? FOR UPDATE", characterID, effect.Kind).Scan(&currentExpiration)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("lock character item service: %w", err)
		}
		base := effect.ActivatedAt
		if currentExpiration.After(base) {
			base = currentExpiration
		}
		effect.ExpiresAt = base.Add(effect.ExpiresAt.Sub(effect.ActivatedAt))
		if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_ITEM_SERVICE` (`CHAR_ID`,`SERVICE_KIND`,`SOURCE_REF_ITEM_ID`,`EXPERIENCE_BONUS_PERCENT`,`SKILL_EXPERIENCE_BONUS_PERCENT`,`DAILY_BUDGET_MS`,`ACTIVATED_AT`,`EXPIRES_AT`) VALUES (?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `SOURCE_REF_ITEM_ID`=VALUES(`SOURCE_REF_ITEM_ID`),`EXPERIENCE_BONUS_PERCENT`=VALUES(`EXPERIENCE_BONUS_PERCENT`),`SKILL_EXPERIENCE_BONUS_PERCENT`=VALUES(`SKILL_EXPERIENCE_BONUS_PERCENT`),`DAILY_BUDGET_MS`=VALUES(`DAILY_BUDGET_MS`),`ACTIVATED_AT`=VALUES(`ACTIVATED_AT`),`EXPIRES_AT`=VALUES(`EXPIRES_AT`)", characterID, effect.Kind, effect.SourceRefItemID, effect.ExperienceBonusPercent, effect.SkillExperienceBonusPercent, effect.DailyBudget.Milliseconds(), effect.ActivatedAt, effect.ExpiresAt); err != nil {
			return result, fmt.Errorf("persist character item service: %w", err)
		}
		result.ItemService = &effect
	case isReturnScroll:
		// RETURN_TELEPORT_ID is the authoritative per-character bind point.
		// Resolve it while the character row is locked and commit the item and
		// destination together so reconnecting can never duplicate the scroll
		// or leave the character at the old position.
		var destination TeleportDestination
		err = tx.QueryRowContext(ctx, "SELECT t.`ID`,t.`REGION_ID`,t.`POS_X`,t.`POS_Y`,t.`POS_Z`,t.`WORLD_ID`,0,0 FROM `CHAR` c JOIN `TELEPORT_POINT` t ON t.`ID`=c.`RETURN_TELEPORT_ID` AND t.`SERVICE`=1 WHERE c.`ID`=?", characterID).Scan(
			&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID, &destination.Fee, &destination.MinLevel,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrItemUseUnsupported
		}
		if err != nil {
			return result, fmt.Errorf("resolve return-scroll destination: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_REVERSE_RETURN_POSITION` (`CHAR_ID`,`REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID`) VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `REGION_ID`=VALUES(`REGION_ID`),`POS_X`=VALUES(`POS_X`),`POS_Y`=VALUES(`POS_Y`),`POS_Z`=VALUES(`POS_Z`),`WORLD_ID`=VALUES(`WORLD_ID`),`SAVED_AT`=CURRENT_TIMESTAMP(6)", characterID, origin.Region, origin.X, origin.Y, origin.Z, origin.WorldID); err != nil {
			return result, fmt.Errorf("persist reverse-return origin: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `FK_REGION`=?,`POS_X`=?,`POS_Y`=?,`POS_Z`=?,`UTIME`=CURRENT_TIMESTAMP WHERE `id`=?", destination.Region, destination.X, destination.Y, destination.Z, characterID); err != nil {
			return result, fmt.Errorf("persist return-scroll destination: %w", err)
		}
		result.ReturnDestination = &destination
		result.ReturnDelay = time.Duration(ref.Param1) * time.Millisecond
	case isReverseReturnScroll:
		var destination TeleportDestination
		destination.ID = ref.ID
		err = tx.QueryRowContext(ctx, "SELECT `REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID` FROM `CHAR_REVERSE_RETURN_POSITION` WHERE `CHAR_ID`=? FOR UPDATE", characterID).Scan(&destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID)
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrItemUseNoEffect
		}
		if err != nil {
			return result, fmt.Errorf("load reverse-return destination: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `FK_REGION`=?,`POS_X`=?,`POS_Y`=?,`POS_Z`=?,`UTIME`=CURRENT_TIMESTAMP WHERE `id`=?", destination.Region, destination.X, destination.Y, destination.Z, characterID); err != nil {
			return result, fmt.Errorf("persist reverse-return destination: %w", err)
		}
		result.ReturnDestination = &destination
		result.ReturnDelay = time.Duration(ref.Param1) * time.Millisecond
	default:
		return result, ErrItemUseUnsupported
	}
	consumeItem := !isCOSScroll
	if consumeItem && stack > 1 {
		mutation, mutationErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1 WHERE `id`=? AND `STACK_COUNT`=?", itemID, stack)
		if mutationErr != nil {
			err = mutationErr
			return result, fmt.Errorf("decrement used item: %w", err)
		}
		if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrItemUseInvalidRequest
		}
	} else if consumeItem {
		mutation, mutationErr := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, itemID)
		if mutationErr != nil {
			err = mutationErr
			return result, fmt.Errorf("unlink used item: %w", err)
		}
		if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrItemUseInvalidRequest
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
	if consumeItem && stack > 1 {
		result.NewAmount = uint16(stack - 1)
	} else if !consumeItem {
		result.NewAmount = uint16(stack)
	}
	if err = tx.Commit(); err != nil {
		return ItemUseResult{}, fmt.Errorf("%w: %v", ErrItemUseOutcomeUnknown, err)
	}
	return result, nil
}

func validateCOSItemRental(kind COSKind, endsAt sql.NullTime, now time.Time) error {
	if kind != COSAbilityPet {
		return nil
	}
	if !endsAt.Valid || !endsAt.Time.UTC().After(now.UTC()) {
		return ErrItemUseExpired
	}
	return nil
}

// IsCOSScroll identifies both persistent growth-pet instruments (TypeID4=1)
// and ability/grab-pet scrolls (TypeID4=2). Their actual summoned RefChar is
// linked by ITEMDATA.Desc_1; the item TypeID4 is not the COS TypeID4.
func IsCOSScroll(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 2 && ref.TypeID3 == 1 &&
		(ref.TypeID4 == 1 || ref.TypeID4 == 2) && strings.Contains(strings.ToUpper(ref.CodeName), "COS_")
}

func createCOSForItemUse(ctx context.Context, tx *sql.Tx, characterID, itemID int64, inventorySlot byte, item RefItem) (uint32, error) {
	codeName := strings.TrimSpace(item.Param1Desc)
	if tx == nil || characterID <= 0 || itemID <= 0 || codeName == "" || strings.EqualFold(codeName, "xxx") {
		return 0, ErrItemUseNoEffect
	}
	var refObjectID uint32
	var typeID1, typeID2, typeID3, typeID4 int
	var level int
	var maxHP, maxMP int32
	err := tx.QueryRowContext(ctx, "SELECT `RefObjID`,`TypeID_1`,`TypeID_2`,`TypeID_3`,`TypeID_4`,`Lvl`,`Max_HP`,`Max_MP` FROM `CHAR_REF_DATA` WHERE `CodeName`=? LIMIT 1", codeName).Scan(
		&refObjectID, &typeID1, &typeID2, &typeID3, &typeID4, &level, &maxHP, &maxMP,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrItemUseNoEffect
	}
	if err != nil {
		return 0, fmt.Errorf("resolve COS item reference: %w", err)
	}
	kind := COSKind(typeID4)
	if typeID1 != 1 || typeID2 != 2 || typeID3 != 3 || !kind.Valid() || refObjectID == 0 {
		return 0, ErrInvalidCOSKind
	}
	if level < 1 {
		level = 1
	}
	if maxHP < 1 {
		maxHP = 1
	}
	hgp := 0
	if kind == COSGrowthPet {
		hgp = 10000
	}
	var rentEndsAt any
	if item.Param1 > 0 {
		// Fixed-term COS item references encode their lifetime in minutes;
		// 40320 is the stock 28-day value in the v188 catalog.
		rentEndsAt = time.Now().UTC().Add(time.Duration(item.Param1) * time.Minute)
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `CHAR_COS` (`OWNER_CHAR_ID`,`REF_OBJECT_ID`,`KIND`,`INVENTORY_SLOT`,`MP`,`STATE`,`LEVEL`,`HEALTH`,`MAX_HEALTH`,`HGP`,`RENT_ENDS_AT`) VALUES (?,?,?,?,?,3,?,?,?,?,?)", characterID, refObjectID, kind, inventorySlot, maxMP, level, maxHP, maxHP, hgp, rentEndsAt)
	if err != nil {
		return 0, fmt.Errorf("create COS for inventory item: %w", err)
	}
	createdID, err := insert.LastInsertId()
	if err != nil || createdID <= 0 || uint64(createdID) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("read created COS identity: %w", errors.Join(err, ErrItemUseOutcomeUnknown))
	}
	mutation, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `DATA`=? WHERE `id`=? AND (`DATA`=0 OR NOT EXISTS (SELECT 1 FROM `CHAR_COS` existing WHERE existing.`ID`=`ITEM`.`DATA`))", createdID, itemID)
	if err != nil {
		return 0, fmt.Errorf("link inventory item to COS: %w", err)
	}
	if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
		return 0, ErrItemUseInvalidRequest
	}
	return uint32(createdID), nil
}

func (r *SQLItemUseRepository) UseResurrectionItem(ctx context.Context, characterID int64, vitals ItemUseVitals) (result ItemUseResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 || vitals.HP != 0 || vitals.MaxHP <= 0 || vitals.MaxMP <= 0 {
		return result, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin resurrection-item transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var persistedHP int
	if err = tx.QueryRowContext(ctx, "SELECT `HP` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&persistedHP); err != nil {
		return result, fmt.Errorf("lock resurrection-item character: %w", err)
	}
	if persistedHP != 0 {
		return result, ErrItemUseNoEffect
	}
	inventory, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	var sourceSlot byte
	var source Item
	var ref RefItem
	found := false
	for _, slot := range SortedInventorySlots(inventory.Items) {
		item := inventory.Items[slot]
		candidate, exists := RefItems[item.RefObjectID]
		if !exists || !candidate.IsUseable || candidate.TypeID1 != 3 || candidate.TypeID2 != 3 || candidate.TypeID3 != 13 || candidate.TypeID4 != 6 {
			continue
		}
		sourceSlot, source, ref, found = slot, item, candidate, true
		break
	}
	if !found {
		return result, ErrItemUseUnsupported
	}
	hpPercent := ref.Param2
	if hpPercent <= 0 || hpPercent > 100 {
		hpPercent = 100
	}
	newHP := maxInt(1, vitals.MaxHP*hpPercent/100)
	newMP := vitals.MaxMP
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `HP`=?,`MP`=? WHERE `id`=? AND `HP`=0", newHP, newMP, characterID); err != nil {
		return result, fmt.Errorf("persist resurrection-item vitals: %w", err)
	}
	if err = consumeAlchemyMaterial(ctx, tx, characterID, sourceSlot, source); err != nil {
		return result, err
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return ItemUseResult{}, err
	}
	result.HP, result.MP = newHP, newMP
	result.RefItemID, result.ItemType, result.SourceSlot = ref.ID, InventoryItemType(ref), sourceSlot
	result.NewAmount = remainingAlchemyStack(source)
	if err = tx.Commit(); err != nil {
		return ItemUseResult{}, fmt.Errorf("%w: %v", ErrItemUseOutcomeUnknown, err)
	}
	return result, nil
}

func itemServiceFromReference(ref RefItem, now time.Time) (ItemServiceEffect, error) {
	if !isItemServiceReference(ref) {
		return ItemServiceEffect{}, ErrItemUseUnsupported
	}
	effect := ItemServiceEffect{SourceRefItemID: ref.ID, ActivatedAt: now.UTC(), ExpiresAt: now.UTC().Add(time.Duration(ref.Param1) * time.Second)}
	switch {
	case ref.TypeID3 == 13 && ref.TypeID4 == 4:
		effect.Kind = ItemServiceGold
		effect.ExperienceBonusPercent = uint32(maxInt(0, ref.Param4))
		effect.SkillExperienceBonusPercent = uint32(maxInt(0, ref.Param5))
		effect.DailyBudget = time.Duration(maxInt(0, ref.Param3)) * time.Millisecond
	case ref.TypeID3 == 13 && ref.TypeID4 == 5:
		effect.Kind = ItemServiceSkillGold
		effect.SkillExperienceBonusPercent = uint32(maxInt(0, ref.Param5))
		effect.DailyBudget = time.Duration(maxInt(0, ref.Param3)) * time.Millisecond
	case ref.TypeID3 == 13 && ref.TypeID4 == 14:
		effect.Kind = ItemServiceGold
		effect.ExperienceBonusPercent = 100
		effect.SkillExperienceBonusPercent = 100
		effect.DailyBudget = 3 * time.Hour
	case ref.TypeID3 == 3 && ref.TypeID4 == 10 && ref.Param2 == itemServiceExperienceMarker:
		effect.Kind = ItemServiceExperience
		effect.ExperienceBonusPercent = uint32(ref.Param3)
		effect.DailyBudget = time.Millisecond
	case ref.TypeID3 == 3 && ref.TypeID4 == 10 && (ref.Param2 == itemServiceSkillExperienceMarker || ref.Param2 == itemServiceSkillExperienceEventMarker):
		effect.Kind = ItemServiceSkillExperience
		effect.SkillExperienceBonusPercent = uint32(ref.Param3)
		effect.DailyBudget = time.Millisecond
	default:
		return ItemServiceEffect{}, ErrItemUseUnsupported
	}
	if effect.DailyBudget <= 0 || (effect.ExperienceBonusPercent == 0 && effect.SkillExperienceBonusPercent == 0) {
		return ItemServiceEffect{}, ErrItemUseUnsupported
	}
	return effect, nil
}

const (
	itemServiceExperienceMarker           = 1885696629
	itemServiceSkillExperienceMarker      = 1886614133
	itemServiceSkillExperienceEventMarker = 1936028277
)

func isItemServiceReference(ref RefItem) bool {
	if !ref.IsUseable || ref.TypeID1 != 3 || ref.TypeID2 != 3 || ref.Param1 <= 0 {
		return false
	}
	if ref.TypeID3 == 13 {
		return ref.TypeID4 == 4 || ref.TypeID4 == 5 || ref.TypeID4 == 14
	}
	if ref.TypeID3 != 3 || ref.TypeID4 != 10 || ref.Param3 <= 0 {
		return false
	}
	return ref.Param2 == itemServiceExperienceMarker || ref.Param2 == itemServiceSkillExperienceMarker || ref.Param2 == itemServiceSkillExperienceEventMarker
}

func (r *SQLItemUseRepository) LoadItemServices(ctx context.Context, characterID int64) ([]ItemServiceEffect, error) {
	if r == nil || r.shard == nil {
		return nil, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT s.`SERVICE_KIND`,s.`SOURCE_REF_ITEM_ID`,s.`EXPERIENCE_BONUS_PERCENT`,s.`SKILL_EXPERIENCE_BONUS_PERCENT`,s.`DAILY_BUDGET_MS`,s.`ACTIVATED_AT`,s.`EXPIRES_AT`,d.`USED_ON`,d.`ACTIVE_UNTIL` FROM `CHAR_ITEM_SERVICE` s LEFT JOIN `CHAR_ITEM_SERVICE_DAILY` d ON d.`CHAR_ID`=s.`CHAR_ID` AND d.`SERVICE_KIND`=s.`SERVICE_KIND` WHERE s.`CHAR_ID`=? AND s.`EXPIRES_AT`>UTC_TIMESTAMP(6) ORDER BY s.`SERVICE_KIND`", characterID)
	if err != nil {
		return nil, fmt.Errorf("load character item services: %w", err)
	}
	defer rows.Close()
	var effects []ItemServiceEffect
	for rows.Next() {
		var effect ItemServiceEffect
		var budgetMS int64
		var usedOn, activeUntil sql.NullTime
		if err = rows.Scan(&effect.Kind, &effect.SourceRefItemID, &effect.ExperienceBonusPercent, &effect.SkillExperienceBonusPercent, &budgetMS, &effect.ActivatedAt, &effect.ExpiresAt, &usedOn, &activeUntil); err != nil {
			return nil, fmt.Errorf("scan character item service: %w", err)
		}
		effect.DailyBudget = time.Duration(budgetMS) * time.Millisecond
		effect.ActivatedAt = effect.ActivatedAt.UTC()
		effect.ExpiresAt = effect.ExpiresAt.UTC()
		if usedOn.Valid {
			effect.DailyUsedOn = usedOn.Time.UTC()
		}
		if activeUntil.Valid {
			effect.DailyActiveUntil = activeUntil.Time.UTC()
		}
		effects = append(effects, effect)
	}
	return effects, rows.Err()
}

func (r *SQLItemUseRepository) LoadItemBuffs(ctx context.Context, characterID int64) ([]ItemBuffEffect, error) {
	if r == nil || r.shard == nil {
		return nil, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT `SOURCE_REF_ITEM_ID`,`SKILL_ID`,`EXPIRES_AT` FROM `CHAR_ITEM_BUFF` WHERE `CHAR_ID`=? AND `EXPIRES_AT`>UTC_TIMESTAMP(6) ORDER BY `SKILL_ID`", characterID)
	if err != nil {
		return nil, fmt.Errorf("load character item buffs: %w", err)
	}
	defer rows.Close()
	effects := make([]ItemBuffEffect, 0)
	for rows.Next() {
		var effect ItemBuffEffect
		if err = rows.Scan(&effect.SourceRefItemID, &effect.SkillID, &effect.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan character item buff: %w", err)
		}
		skill, exists := RefSkills[effect.SkillID]
		if !exists || skill.ExecutionKind() != SkillExecutionBuff || skill.BuffModifiers == (BuffModifiers{}) {
			return nil, fmt.Errorf("load character item buff skill %d: %w", effect.SkillID, ErrItemUseUnsupported)
		}
		effect.ExpiresAt = effect.ExpiresAt.UTC()
		effect.Modifiers = skill.BuffModifiers
		effects = append(effects, effect)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate character item buffs: %w", err)
	}
	return effects, nil
}

func (r *SQLItemUseRepository) ActivateItemServiceDaily(ctx context.Context, characterID int64, kind ItemServiceKind, now time.Time) (activeUntil time.Time, err error) {
	if r == nil || r.shard == nil {
		return time.Time{}, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 || kind == 0 {
		return time.Time{}, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now = now.UTC()
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin item-service daily activation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var sourceRefItemID uint32
	var budgetMS int64
	var expiresAt time.Time
	if err = tx.QueryRowContext(ctx, "SELECT `SOURCE_REF_ITEM_ID`,`DAILY_BUDGET_MS`,`EXPIRES_AT` FROM `CHAR_ITEM_SERVICE` WHERE `CHAR_ID`=? AND `SERVICE_KIND`=? FOR UPDATE", characterID, kind).Scan(&sourceRefItemID, &budgetMS, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrItemServiceDailyUnavailable
		}
		return time.Time{}, fmt.Errorf("lock item-service daily activation: %w", err)
	}
	if premiumDailyServiceRefItemID(ItemServiceEffect{SourceRefItemID: sourceRefItemID}) == 0 || budgetMS <= 0 || !expiresAt.After(now) {
		return time.Time{}, ErrItemServiceDailyUnavailable
	}
	var usedOn time.Time
	err = tx.QueryRowContext(ctx, "SELECT `USED_ON` FROM `CHAR_ITEM_SERVICE_DAILY` WHERE `CHAR_ID`=? AND `SERVICE_KIND`=?", characterID, kind).Scan(&usedOn)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("load item-service daily activation: %w", err)
	}
	if err == nil && sameUTCDate(usedOn, now) {
		return time.Time{}, ErrItemServiceDailyUnavailable
	}
	activeUntil = now.Add(time.Duration(budgetMS) * time.Millisecond)
	if activeUntil.After(expiresAt) {
		activeUntil = expiresAt
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_ITEM_SERVICE_DAILY` (`CHAR_ID`,`SERVICE_KIND`,`USED_ON`,`ACTIVE_UNTIL`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `USED_ON`=VALUES(`USED_ON`),`ACTIVE_UNTIL`=VALUES(`ACTIVE_UNTIL`)", characterID, kind, now.Format("2006-01-02"), activeUntil); err != nil {
		return time.Time{}, fmt.Errorf("persist item-service daily activation: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return time.Time{}, fmt.Errorf("commit item-service daily activation: %w", err)
	}
	return activeUntil.UTC(), nil
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
