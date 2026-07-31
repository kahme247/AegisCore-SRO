package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrItemUseRepositoryUnavailable = errors.New("item-use repository is unavailable")
	ErrItemUseInvalidRequest        = errors.New("invalid item-use request")
	ErrItemUseUnsupported           = errors.New("item use is not supported")
	ErrItemUseNoEffect              = errors.New("item use would have no effect")
	ErrItemUseExpired               = errors.New("item use has expired")
	ErrItemUseCooldown              = errors.New("item use is on cooldown")
	ErrItemUseOutcomeUnknown        = errors.New("item-use transaction outcome is unknown")
	ErrItemServiceDailyUnavailable  = errors.New("item-service daily activation is unavailable")
)

type SummonMonsterCandidate struct {
	CodeName string
	Weight   int
	Rarity   MonsterRarity
}

type ItemSummonMonsterEffect struct {
	Candidates []SummonMonsterCandidate
	Radius     float32
	Lifetime   time.Duration
}

func IsPartyMonsterSummonScroll(ref RefItem) bool {
	return ref.ID == 9318 || ref.ID == 9319
}

type ItemUseResult struct {
	Inventory                   Inventory
	HP                          int
	MP                          int
	RefItemID                   uint32
	ItemType                    uint16
	SourceSlot                  byte
	NewAmount                   uint16
	EffectSkillID               uint32
	SpeedPercent                int
	EffectDuration              int
	EffectRecoveryHP            int
	EffectRecoveryMP            int
	EffectVisibilityRefresh     bool
	SkillPoints                 int
	OptLevelTargetSlot          byte
	OptLevelTargetSet           bool
	OptLevel                    byte
	EffectExpiresAt             time.Time
	ItemCure                    *ItemCureEffect
	ItemCureApplication         ItemCureApplication
	ItemCureAppliedAt           time.Time
	ItemService                 *ItemServiceEffect
	ItemServiceToken            uint32
	ReplacedItemServiceToken    uint32
	ReturnDestination           *TeleportDestination
	ReturnDelay                 time.Duration
	BuffModifiers               BuffModifiers
	COSPersistentID             uint64
	COSTemporaryRideRefObjectID uint32
	QuestInstanceEntryQuestID   int
	COSRevivedPersistentID      uint64
	COSRevivedHealth            int32
	COSRevivedItemState         COSItemState
	COSResourcePersistentID     uint64
	COSResourceHealth           int32
	COSResourceHGP              int
	COSResourceKind             byte
	COSCurePersistentID         uint64
	COSCure                     *ItemCureEffect
	COSExtendedPersistentID     uint64
	COSExtendedRentEndsAt       time.Time
	COSTimedServicePersistentID uint64
	COSTimedServiceExpiresAt    time.Time
	COSTimedServiceCategory     int
	COSTimedServiceJobID        int
	COSTimedServiceData1        int
	MaskRefCharID               uint32
	MaskDuration                time.Duration
	CollectionThemeID           uint32
	StallAvatarRefID            uint32
	CollectionSlotIndex         uint16
	CollectionThemeComplete     bool
	CollectionQuestProgress     []QuestProgressResult
	HwanSnapshot                *HwanSnapshot
	InventorySize               byte
	DimensionHolePortal         *DimensionHoleDefinition
	SummonMonster               *ItemSummonMonsterEffect
}

// COSItemTarget is the item-family-specific target carried by 0x704C.
// Coordinates remain raw wire values until their numeric representation is
// confirmed; ownership is resolved from ObjectID by the service/repository.
type COSItemTarget struct {
	ObjectID uint32
	Region   uint16
	X        uint32
	Y        uint32
	Z        uint32
}

type ItemUseVitals struct {
	HP, MP                 int
	MaxHP, MaxMP           int
	Level                  int
	Strength, Intelligence int
	ReverseReturnOption    byte
	ReverseReturnMapID     uint32
	// InvertHPRecovery is captured from the authoritative abnormal-state
	// snapshot before the atomic consume transaction. MP recovery stays positive.
	InvertHPRecovery bool
	// DimensionHolePreparedItemRefID is nonzero only after the runtime has
	// reserved a direct-world lease. SQL refuses to consume a portal item
	// through the ordinary unprepared UseItem path.
	DimensionHolePreparedItemRefID uint32
}

type ItemUseRepository interface {
	UseItem(context.Context, int64, byte, uint16, ItemUseVitals) (ItemUseResult, error)
	UseResurrectionItem(context.Context, int64, ItemUseVitals) (ItemUseResult, error)
}

// TargetedItemUseRepository atomically validates and mutates a COS target
// together with consuming the selected item.
type TargetedItemUseRepository interface {
	UseTargetedItem(context.Context, int64, byte, uint16, ItemUseVitals, COSItemTarget) (ItemUseResult, error)
}

type ItemBuffRecoveryRepository interface {
	PersistItemBuffRecovery(context.Context, int64, int, int, int, int) (bool, error)
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
	ItemServiceHwanDuration
	ItemServiceReinforceWeapon
	ItemServiceReinforceArmor
	ItemServiceReinforceAccessory
)

type ItemServiceEffect struct {
	Kind                        ItemServiceKind
	SourceRefItemID             uint32
	ExperienceBonusPercent      uint32
	SkillExperienceBonusPercent uint32
	EffectValue                 uint32
	DailyBudget                 time.Duration
	DailyUsedOn                 time.Time
	DailyActiveUntil            time.Time
	ActivatedAt                 time.Time
	ExpiresAt                   time.Time
	TargetSlot                  byte
	TargetSlotSet               bool
	TargetBaseOptLevel          byte
	TargetProjectedOptLevel     byte
}

type ItemServiceRepository interface {
	LoadItemServices(context.Context, int64) ([]ItemServiceEffect, error)
	ActivateItemServiceDaily(context.Context, int64, ItemServiceKind, time.Time) (time.Time, error)
}

type ItemBuffEffect struct {
	SourceRefItemID   uint32
	SkillID           uint32
	ExpiresAt         time.Time
	RecoveryHP        int
	RecoveryMP        int
	RefreshVisibility bool
	Modifiers         BuffModifiers
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

// CheckCooldowns exposes the preflight used by runtime publishers. Item
// consumption still claims the cooldown inside its own transaction.
func (r *SQLItemUseRepository) CheckCooldowns(ctx context.Context, characterID int, claims []CharacterCooldownClaim) error {
	return (&SQLCharacterCooldownRepository{shard: r.shard}).CheckCooldowns(ctx, characterID, claims)
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
	hpRecovery, mpRecovery := 0, 0
	if ref.Param1 != 0 || ref.Param3 != 0 {
		hpRecovery = scaledFlatPotionRecovery(ref.Param1, vitals.Strength, vitals.Level)
		mpRecovery = scaledFlatPotionRecovery(ref.Param3, vitals.Intelligence, vitals.Level)
	} else {
		hpRecovery = maxHP * ref.Param2 / 100
		mpRecovery = maxMP * ref.Param4 / 100
	}
	newHP := minInt(maxHP, vitals.HP+hpRecovery)
	if vitals.InvertHPRecovery && hpRecovery > 0 {
		newHP = maxInt(0, vitals.HP-hpRecovery)
	}
	newMP := minInt(maxMP, vitals.MP+mpRecovery)
	return newHP, newMP, newHP != vitals.HP || newMP != vitals.MP
}

func scaledFlatPotionRecovery(base, stat, level int) int {
	if base <= 0 {
		return 0
	}
	stat = maxInt(0, stat)
	level = maxInt(1, level)
	return int(float64(base) * (1 + float64(stat)/416) * math.Pow(1.02, float64(level-1)))
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

func IsSpecialReturnScroll(ref RefItem) bool {
	return ref.IsUseable &&
		ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 3 && ref.TypeID4 == 1 &&
		ref.Param1 > 0 && strings.EqualFold(strings.TrimSpace(ref.Param3Desc), "THIEFDEN") &&
		strings.TrimSpace(ref.Param4Desc) != "" && !strings.EqualFold(strings.TrimSpace(ref.Param4Desc), "xxx")
}

func IsReverseReturnScroll(ref RefItem) bool {
	return ref.IsUseable &&
		ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 3 && ref.TypeID4 == 3 &&
		ref.Param1 > 0
}

func IsConfiguredEnhancementScroll(ref RefItem) bool {
	return ref.ID >= 3789 && ref.ID <= 3794 || ref.ID >= 39364 && ref.ID <= 39366
}

func IsConfiguredOptionUpgradeScroll(ref RefItem) bool {
	return ref.ID >= 39364 && ref.ID <= 39366
}

func IsTeleportScroll(ref RefItem) bool {
	return IsReturnScroll(ref) || IsSpecialReturnScroll(ref) || IsReverseReturnScroll(ref)
}

func (r *SQLItemUseRepository) UseItem(ctx context.Context, characterID int64, slot byte, requestedType uint16, vitals ItemUseVitals) (ItemUseResult, error) {
	return r.useItem(ctx, characterID, slot, requestedType, vitals, nil)
}

// UseTargetedItem is the transactional entry point for COS item payloads.
// The target is deliberately resolved inside the same transaction as item
// consumption; callers cannot select an object they do not own.
func (r *SQLItemUseRepository) UseTargetedItem(ctx context.Context, characterID int64, slot byte, requestedType uint16, vitals ItemUseVitals, target COSItemTarget) (ItemUseResult, error) {
	if target.ObjectID == 0 {
		return ItemUseResult{}, ErrItemUseInvalidRequest
	}
	return r.useItem(ctx, characterID, slot, requestedType, vitals, &target)
}

func (r *SQLItemUseRepository) useItem(ctx context.Context, characterID int64, slot byte, requestedType uint16, vitals ItemUseVitals, target *COSItemTarget) (result ItemUseResult, err error) {
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
	if vitals.DimensionHolePreparedItemRefID != 0 && vitals.DimensionHolePreparedItemRefID != refID {
		return result, ErrItemUseInvalidRequest
	}
	newHP, newMP := vitals.HP, vitals.MP
	isRecovery := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 1 && ref.TypeID4 >= 1 && ref.TypeID4 <= 3
	isHwanPotion := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 1 && ref.TypeID4 == 8
	isDirectSuccessExpendable := ref.TypeID1 == 3 && ref.TypeID2 == 3 && (ref.TypeID3 == 9 || ref.TypeID3 == 15 || ref.TypeID3 == 6 && ref.TypeID4 == 1 || ref.TypeID3 == 3 && ref.TypeID4 == 4)
	isPartyMonsterSummon := IsPartyMonsterSummonScroll(ref)
	isInventoryExpansion := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 17 && strings.Contains(strings.ToUpper(ref.CodeName), "INVENTORY_ADDITION")
	isWarehouseExpansion := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 17 && strings.Contains(strings.ToUpper(ref.CodeName), "WAREHOUSE_ADDITION")
	isSpeedDrug := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 1
	isTimeService := isItemServiceReference(ref)
	isConfiguredTimedProgression := ref.ID >= 23938 && ref.ID <= 23941 || IsConfiguredEnhancementScroll(ref)
	isReturnScroll := IsReturnScroll(ref)
	isSpecialReturnScroll := IsSpecialReturnScroll(ref)
	isReverseReturnScroll := IsReverseReturnScroll(ref)
	isCOSScroll := IsCOSScroll(ref)
	isCOSTransportScroll := IsCOSTransportScroll(ref)
	isCOSHealthPotion := IsCOSHealthPotion(ref)
	isCOSHGPotion := IsCOSHGPotion(ref)
	isCOSExtension := IsCOSExtension(ref)
	isCOSPetService := IsCOSPetTimedService(ref)
	isCOSRevival := IsCOSRevival(ref)
	isCOSCure := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 2 && ref.TypeID4 == 7
	isMonsterMask := ref.TypeID1 == 3 && ref.TypeID2 == 2 && ref.TypeID3 == 2 && itemData != 0
	isCollectionTalisman := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 12 && ref.TypeID4 == 5
	isCharacterStatusCure := isCharacterStatusCureType(ref)
	itemCure, validItemCure := ItemCureFromReference(ref)
	cosCure, validCOSCure := COSItemCureFromReference(ref)
	questInstanceEntryQuestID, isQuestInstanceEntry := QuestInstanceEntryQuestID(ref.CodeName)
	dimensionHole, isDimensionHole := DimensionHoleDefinitionForReference(ref)
	linkedSkill, hasLinkedSkill := RefSkills[uint32(skillID)]
	isSkillBuff := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 1 &&
		hasLinkedSkill && linkedSkill.SupportsItemUseBuff()
	isConfiguredSkillPoint := ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 3 && ref.TypeID4 == 12
	isConfiguredOptUpgrade := IsConfiguredOptionUpgradeScroll(ref) && !IsConfiguredEnhancementScroll(ref)
	switch {
	case !ref.IsUseable:
		return result, ErrItemUseUnsupported
	case isQuestInstanceEntry:
		result.QuestInstanceEntryQuestID = questInstanceEntryQuestID
	case isDimensionHole:
		if target != nil || vitals.DimensionHolePreparedItemRefID != ref.ID {
			return result, ErrItemUseUnsupported
		}
		if err = claimCharacterCooldowns(ctx, tx, int(characterID), []CharacterCooldownClaim{{
			Kind:     CharacterCooldownItem,
			SourceID: ref.ID,
			Duration: dimensionHole.Cooldown,
		}}); err != nil {
			if errors.Is(err, ErrCharacterCooldownActive) {
				return result, ErrItemUseCooldown
			}
			return result, err
		}
		result.DimensionHolePortal = &dimensionHole
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
	case isCOSTransportScroll:
		refObjectID, resolveErr := resolveTemporaryRideReference(ctx, tx, ref, vitals.Level)
		if resolveErr != nil {
			return result, resolveErr
		}
		result.COSTemporaryRideRefObjectID = refObjectID
	case isCOSCure:
		if target == nil || target.ObjectID == 0 || !validCOSCure {
			return result, ErrItemUseInvalidRequest
		}
		var cosID uint64
		var kind byte
		var state COSItemState
		var health int32
		var rentEndsAt sql.NullTime
		if err = tx.QueryRowContext(ctx, "SELECT `ID`,`KIND`,`STATE`,`HEALTH`,`RENT_ENDS_AT` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", target.ObjectID, characterID).Scan(&cosID, &kind, &state, &health, &rentEndsAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrItemUseNoEffect
			}
			return result, fmt.Errorf("lock COS cure target: %w", err)
		}
		if err = validateCOSItemRental(COSKind(kind), rentEndsAt, time.Now().UTC()); err != nil {
			return result, err
		}
		if health <= 0 || state == COSItemDead || (COSKind(kind) != COSGrowthPet && COSKind(kind) != COSAbilityPet) {
			return result, ErrItemUseNoEffect
		}
		result.COSCurePersistentID = cosID
		result.COSCure = &cosCure
	case isCOSHealthPotion || isCOSHGPotion:
		if target == nil || target.ObjectID == 0 || ref.Param1 <= 0 {
			return result, ErrItemUseInvalidRequest
		}
		var cosID uint64
		var health, maxHealth int32
		var hgp int
		var kind byte
		var state COSItemState
		var rentEndsAt sql.NullTime
		err = tx.QueryRowContext(ctx, "SELECT `ID`,`HEALTH`,`MAX_HEALTH`,`HGP`,`KIND`,`STATE`,`RENT_ENDS_AT` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", target.ObjectID, characterID).Scan(&cosID, &health, &maxHealth, &hgp, &kind, &state, &rentEndsAt)
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrItemUseNoEffect
		}
		if err != nil {
			return result, fmt.Errorf("lock COS resource target: %w", err)
		}
		if err = validateCOSItemRental(COSKind(kind), rentEndsAt, time.Now().UTC()); err != nil {
			return result, err
		}
		if health <= 0 || maxHealth <= 0 || state == COSItemDead {
			return result, ErrItemUseNoEffect
		}
		if isCOSHealthPotion {
			newHealth := minInt(int(maxHealth), int(health)+ref.Param1)
			if newHealth == int(health) {
				return result, ErrItemUseNoEffect
			}
			mutation, mutationErr := tx.ExecContext(ctx, "UPDATE `CHAR_COS` SET `HEALTH`=? WHERE `ID`=? AND `OWNER_CHAR_ID`=? AND `HEALTH`=?", newHealth, cosID, characterID, health)
			if mutationErr != nil {
				return result, fmt.Errorf("persist COS health potion: %w", mutationErr)
			}
			if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
				return result, ErrItemUseNoEffect
			}
			health = int32(newHealth)
		} else {
			if COSKind(kind) != COSGrowthPet {
				return result, ErrItemUseNoEffect
			}
			const maxCOSHGP = 10000
			newHGP := minInt(maxCOSHGP, hgp+ref.Param1)
			if newHGP == hgp {
				return result, ErrItemUseNoEffect
			}
			mutation, mutationErr := tx.ExecContext(ctx, "UPDATE `CHAR_COS` SET `HGP`=? WHERE `ID`=? AND `OWNER_CHAR_ID`=? AND `HGP`=?", newHGP, cosID, characterID, hgp)
			if mutationErr != nil {
				return result, fmt.Errorf("persist COS HGP potion: %w", mutationErr)
			}
			if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
				return result, ErrItemUseNoEffect
			}
			hgp = newHGP
		}
		result.COSResourcePersistentID = cosID
		result.COSResourceHealth = health
		result.COSResourceHGP = hgp
		result.COSResourceKind = kind
	case isCOSPetService:
		if target == nil || target.ObjectID == 0 || ref.Param1 <= 0 {
			return result, ErrItemUseInvalidRequest
		}
		var cosID uint64
		var kind byte
		if err = tx.QueryRowContext(ctx, "SELECT `ID`,`KIND` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", target.ObjectID, characterID).Scan(&cosID, &kind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrItemUseNoEffect
			}
			return result, fmt.Errorf("lock COS timed-service target: %w", err)
		}
		category, jobID, data1, ok := COSPetTimedServiceFields(ref, COSKind(kind))
		if !ok {
			return result, ErrItemUseNoEffect
		}
		now := time.Now().UTC()
		expiresAt := now.Add(time.Duration(ref.Param1) * time.Second)
		mutation, mutationErr := tx.ExecContext(ctx, "INSERT INTO `CHAR_COS_TIMED_SERVICE` (`COS_ID`,`OWNER_CHAR_ID`,`CATEGORY`,`JOB_ID`,`TIME_TO_KEEP`,`DATA1`,`SERIAL64`,`EXPIRES_AT`) VALUES (?,?,?,?,?,?,?,?)", cosID, characterID, category, jobID, ref.Param1, data1, int64(ref.ID), expiresAt)
		if mutationErr != nil {
			return result, fmt.Errorf("persist COS timed service: %w", mutationErr)
		}
		if _, affectedErr := mutation.RowsAffected(); affectedErr != nil {
			return result, fmt.Errorf("read COS timed-service mutation: %w", affectedErr)
		}
		result.COSTimedServicePersistentID = cosID
		result.COSTimedServiceExpiresAt = expiresAt
		result.COSTimedServiceCategory = category
		result.COSTimedServiceJobID = jobID
		result.COSTimedServiceData1 = data1
	case isCOSExtension:
		if target == nil || target.ObjectID == 0 || ref.Param1 <= 0 {
			return result, ErrItemUseInvalidRequest
		}
		var cosID uint64
		var kind byte
		var rentEndsAt sql.NullTime
		err = tx.QueryRowContext(ctx, "SELECT `ID`,`KIND`,`RENT_ENDS_AT` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", target.ObjectID, characterID).Scan(&cosID, &kind, &rentEndsAt)
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrItemUseNoEffect
		}
		if err != nil {
			return result, fmt.Errorf("lock COS extension target: %w", err)
		}
		if COSKind(kind) != COSAbilityPet {
			return result, ErrItemUseNoEffect
		}
		now := time.Now().UTC()
		base := now
		if rentEndsAt.Valid && rentEndsAt.Time.After(base) {
			base = rentEndsAt.Time
		}
		newRentEndsAt := base.Add(time.Duration(ref.Param1) * time.Minute)
		mutation, mutationErr := tx.ExecContext(ctx, "UPDATE `CHAR_COS` SET `RENT_ENDS_AT`=? WHERE `ID`=? AND `OWNER_CHAR_ID`=?", newRentEndsAt, cosID, characterID)
		if mutationErr != nil {
			return result, fmt.Errorf("persist COS rental extension: %w", mutationErr)
		}
		if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrItemUseNoEffect
		}
		result.COSExtendedPersistentID = cosID
		result.COSExtendedRentEndsAt = newRentEndsAt
	case isCOSRevival:
		var cosID uint64
		var maxHealth int32
		var kind byte
		var state COSItemState
		var rentEndsAt sql.NullTime
		var revivalQuery string
		var revivalArgs []any
		if target == nil || target.ObjectID == 0 {
			// The vSRO 0x704C revival form carries a COS object ID. Never select
			// an arbitrary dead pet when a client omits or corrupts that target.
			return result, ErrItemUseInvalidRequest
		}
		revivalQuery = "SELECT `ID`,`MAX_HEALTH`,`KIND`,`STATE`,`RENT_ENDS_AT` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? AND (`HEALTH`=0 OR `STATE`=4) FOR UPDATE"
		revivalArgs = []any{target.ObjectID, characterID}
		err = tx.QueryRowContext(ctx, revivalQuery, revivalArgs...).Scan(&cosID, &maxHealth, &kind, &state, &rentEndsAt)
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrItemUseNoEffect
		}
		if err != nil {
			return result, fmt.Errorf("lock dead COS for revival: %w", err)
		}
		if err = validateCOSItemRental(COSKind(kind), rentEndsAt, time.Now().UTC()); err != nil {
			return result, err
		}
		if maxHealth <= 0 {
			return result, ErrItemUseNoEffect
		}
		healthPercent := ref.Param2
		if healthPercent <= 0 || healthPercent > 100 {
			healthPercent = 100
		}
		newHealth := maxHealth * int32(healthPercent) / 100
		if newHealth < 1 {
			newHealth = 1
		}
		newState := state
		if newState == COSItemDead || newState < COSItemInactive || newState > COSItemActive {
			newState = COSItemActive
		}
		mutation, mutationErr := tx.ExecContext(ctx, "UPDATE `CHAR_COS` SET `HEALTH`=?,`STATE`=? WHERE `ID`=? AND `OWNER_CHAR_ID`=? AND (`HEALTH`=0 OR `STATE`=4)", newHealth, newState, cosID, characterID)
		if mutationErr != nil {
			return result, fmt.Errorf("persist COS revival: %w", mutationErr)
		}
		if affected, affectedErr := mutation.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrItemUseNoEffect
		}
		result.COSRevivedPersistentID = cosID
		result.COSRevivedHealth = newHealth
		result.COSRevivedItemState = newState
	case isMonsterMask:
		if _, exists := RefChars[itemData]; !exists {
			return result, ErrItemUseNoEffect
		}
		result.MaskRefCharID = itemData
		result.MaskDuration = time.Duration(max(0, ref.DecayTime)) * time.Millisecond
	case isCollectionTalisman:
		var themeID uint32
		var slotIndex, completeCount uint16
		if err = tx.QueryRowContext(ctx, "SELECT item.`THEME_ID`,item.`SLOT_INDEX`,theme.`COMPLETE_COUNT` FROM `REF_COLLECTION_BOOK_ITEM` item JOIN `REF_COLLECTION_BOOK_THEME` theme ON theme.`THEME_ID`=item.`THEME_ID` WHERE item.`ITEM_REF_OBJECT_ID`=?", ref.ID).Scan(&themeID, &slotIndex, &completeCount); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrItemUseUnsupported
			}
			return result, fmt.Errorf("resolve collection-book talisman: %w", err)
		}
		registration, registrationErr := tx.ExecContext(ctx, "INSERT IGNORE INTO `CHAR_COLLECTION_BOOK` (`CHAR_ID`,`THEME_ID`,`SLOT_INDEX`,`ITEM_REF_OBJECT_ID`) VALUES (?,?,?,?)", characterID, themeID, slotIndex, ref.ID)
		if registrationErr != nil {
			return result, fmt.Errorf("register collection-book talisman: %w", registrationErr)
		}
		if affected, affectedErr := registration.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrItemUseNoEffect
		}
		var registered uint16
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `CHAR_COLLECTION_BOOK` WHERE `CHAR_ID`=? AND `THEME_ID`=?", characterID, themeID).Scan(&registered); err != nil {
			return result, fmt.Errorf("count collection-book theme: %w", err)
		}
		result.CollectionThemeID = themeID
		result.CollectionSlotIndex = slotIndex
		result.CollectionThemeComplete = completeCount > 0 && registered >= completeCount
		if result.CollectionThemeComplete {
			result.CollectionQuestProgress, err = completeTalismanQuestsInItemUseTransaction(ctx, tx, characterID, themeID)
			if err != nil {
				return ItemUseResult{}, err
			}
		}
	case isConfiguredSkillPoint:
		if target != nil {
			return result, ErrItemUseInvalidRequest
		}
		var effectKind string
		var value, maxValue uint64
		var requiresTarget bool
		if err = tx.QueryRowContext(ctx, "SELECT `EFFECT_KIND`,`VALUE`,`MAX_VALUE`,`REQUIRES_TARGET_SLOT` FROM `EXPENDABLE_EFFECT_CONFIG` WHERE `ITEM_REF_ID`=?", ref.ID).Scan(&effectKind, &value, &maxValue, &requiresTarget); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrItemUseUnsupported
			}
			return result, fmt.Errorf("load skill-point expendable configuration: %w", err)
		}
		if effectKind != "skill_points" || value == 0 || requiresTarget {
			return result, ErrItemUseUnsupported
		}
		var current uint64
		if err = tx.QueryRowContext(ctx, "SELECT `SKILL_POINTS` FROM `CHAR` WHERE `id`=?", characterID).Scan(&current); err != nil {
			return result, fmt.Errorf("load skill-point balance: %w", err)
		}
		limit := uint64(^uint32(0))
		if maxValue > 0 && maxValue < limit {
			limit = maxValue
		}
		if current >= limit {
			return result, ErrItemUseNoEffect
		}
		updated := min(limit, current+value)
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `SKILL_POINTS`=? WHERE `id`=?", updated, characterID); err != nil {
			return result, fmt.Errorf("persist skill-point expendable: %w", err)
		}
		result.SkillPoints = int(updated)
	case isConfiguredOptUpgrade:
		if target == nil || target.ObjectID == 0 || target.ObjectID > uint32(^uint8(0))+1 {
			return result, ErrItemUseInvalidRequest
		}
		targetSlot := byte(target.ObjectID - 1)
		if targetSlot == slot {
			return result, ErrItemUseInvalidRequest
		}
		var effectKind string
		var value, maxValue uint64
		var requiresTarget bool
		if err = tx.QueryRowContext(ctx, "SELECT `EFFECT_KIND`,`VALUE`,`MAX_VALUE`,`REQUIRES_TARGET_SLOT` FROM `EXPENDABLE_EFFECT_CONFIG` WHERE `ITEM_REF_ID`=?", ref.ID).Scan(&effectKind, &value, &maxValue, &requiresTarget); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrItemUseUnsupported
			}
			return result, fmt.Errorf("load option-upgrade expendable configuration: %w", err)
		}
		if effectKind != "opt_level" || value == 0 || maxValue == 0 || !requiresTarget {
			return result, ErrItemUseUnsupported
		}
		var targetItemID int64
		var current uint64
		if err = tx.QueryRowContext(ctx, "SELECT it.`id`,it.`OPT_LEVEL` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` r ON r.`ItemID`=it.`FK_REF_ITEM` WHERE i.`FK_CHAR`=? AND i.`SLOT`=? AND r.`TypeID1`=3 AND r.`TypeID2`=1 FOR UPDATE", characterID, targetSlot).Scan(&targetItemID, &current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrItemUseNoEffect
			}
			return result, fmt.Errorf("lock option-upgrade target: %w", err)
		}
		if current >= maxValue {
			return result, ErrItemUseNoEffect
		}
		updated := min(maxValue, current+value)
		if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `OPT_LEVEL`=? WHERE `id`=?", updated, targetItemID); err != nil {
			return result, fmt.Errorf("persist option-upgrade expendable: %w", err)
		}
		result.OptLevelTargetSlot = targetSlot
		result.OptLevelTargetSet = true
		result.OptLevel = byte(updated)
	case isPartyMonsterSummon:
		rows, queryErr := tx.QueryContext(ctx, "SELECT c.`CodeName`,s.`WEIGHT`,s.`RUNTIME_RARITY`,s.`SPAWN_RADIUS`,s.`LIFETIME_SECONDS` FROM `REF_ITEM_SUMMON_MONSTER` s JOIN `CHAR_REF_DATA` c ON c.`RefObjID`=s.`MONSTER_REF_ID` WHERE s.`ITEM_REF_ID`=? AND s.`MIN_PLAYER_LEVEL`<=? AND s.`MAX_PLAYER_LEVEL`>=? ORDER BY s.`MONSTER_REF_ID`", ref.ID, vitals.Level, vitals.Level)
		if queryErr != nil {
			return result, fmt.Errorf("load summon-scroll monster catalog: %w", queryErr)
		}
		effect := ItemSummonMonsterEffect{}
		for rows.Next() {
			var candidate SummonMonsterCandidate
			var radius, lifetimeSeconds int
			if scanErr := rows.Scan(&candidate.CodeName, &candidate.Weight, &candidate.Rarity, &radius, &lifetimeSeconds); scanErr != nil {
				_ = rows.Close()
				return result, fmt.Errorf("scan summon-scroll monster catalog: %w", scanErr)
			}
			if candidate.CodeName == "" || candidate.Weight <= 0 || !candidate.Rarity.Valid() || candidate.Rarity.IsUniqueBoss() ||
				radius <= 0 || lifetimeSeconds <= 0 || len(effect.Candidates) > 0 &&
				(effect.Radius != float32(radius) || effect.Lifetime != time.Duration(lifetimeSeconds)*time.Second) {
				_ = rows.Close()
				return result, ErrItemUseUnsupported
			}
			effect.Candidates = append(effect.Candidates, candidate)
			effect.Radius = float32(radius)
			effect.Lifetime = time.Duration(lifetimeSeconds) * time.Second
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			return result, fmt.Errorf("iterate summon-scroll monster catalog: %w", rowsErr)
		}
		if closeErr := rows.Close(); closeErr != nil {
			return result, fmt.Errorf("close summon-scroll monster catalog: %w", closeErr)
		}
		if len(effect.Candidates) == 0 {
			return result, ErrItemUseUnsupported
		}
		result.RefItemID = ref.ID
		result.SummonMonster = &effect
	case isDirectSuccessExpendable:
		// Stock 0x0049ACA0 returns success for TypeID3 9 and 15 without an
		// item-specific server callback. Consumption and 0x305C are the effect.
		result.RefItemID = ref.ID
		if ref.TypeID3 == 3 && ref.TypeID4 == 4 {
			result.StallAvatarRefID = ref.ID
		}
	case isHwanPotion:
		points := minInt(ref.Param1, int(MaxHwanPoints))
		if points <= 0 {
			return result, ErrItemUseUnsupported
		}
		var snapshot HwanSnapshot
		if err = tx.QueryRowContext(ctx, "SELECT `REMAIN_HWAN_COUNT`,`GATHERED_HWAN_POINT`,`HWAN_LEVEL` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&snapshot.Points, &snapshot.GatheredPoint, &snapshot.Level); err != nil {
			return result, fmt.Errorf("lock HWAN potion state: %w", err)
		}
		if snapshot.Points >= byte(points) {
			return result, ErrItemUseNoEffect
		}
		snapshot.Points = byte(points)
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `REMAIN_HWAN_COUNT`=? WHERE `id`=?", snapshot.Points, characterID); err != nil {
			return result, fmt.Errorf("persist HWAN potion: %w", err)
		}
		result.HwanSnapshot = &snapshot
	case isRecovery:
		var changed bool
		newHP, newMP, changed = recoveryValues(ref, vitals)
		if !changed {
			return result, ErrItemUseNoEffect
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `HP`=?,`MP`=? WHERE `id`=?", newHP, newMP, characterID); err != nil {
			return result, fmt.Errorf("persist item-use recovery: %w", err)
		}
	case isCharacterStatusCure:
		if !validItemCure {
			return result, ErrItemUseUnsupported
		}
		// The stock callback returns success even when no matching status is
		// active or its random cure roll fails. Carry the descriptor across the
		// committed consume boundary; runtime status authority applies it later.
		result.ItemCure = &itemCure
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
		result.EffectRecoveryHP = linkedSkill.RecoveryHP
		result.EffectRecoveryMP = linkedSkill.RecoveryMP
		result.EffectVisibilityRefresh = linkedSkill.DetectionLevel > 0
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
	case isInventoryExpansion:
		var inventorySize byte
		if err = tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=?", characterID).Scan(&inventorySize); err != nil {
			return result, fmt.Errorf("load inventory expansion capacity: %w", err)
		}
		current := inventorySize
		if current < InventoryEquipmentSlots {
			current = BaseInventorySize
		}
		expanded := minInt(int(MaxInventoryWireSize), int(current)+ref.Param1)
		if ref.Param1 <= 0 || expanded <= int(current) {
			return result, ErrItemUseNoEffect
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `INVENTORY_SIZE`=? WHERE `id`=?", expanded, characterID); err != nil {
			return result, fmt.Errorf("persist inventory expansion: %w", err)
		}
		result.InventorySize = byte(expanded)
	case isWarehouseExpansion:
		if ref.Param1 <= 0 {
			return result, ErrItemUseUnsupported
		}
		var userID int64
		if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=?", characterID).Scan(&userID); err != nil {
			return result, fmt.Errorf("resolve warehouse owner: %w", err)
		}
		var capacity int
		if err = tx.QueryRowContext(ctx, "SELECT `CAPACITY` FROM `STORAGE_INFO` WHERE `FK_USER`=? FOR UPDATE", userID).Scan(&capacity); err != nil {
			return result, fmt.Errorf("lock warehouse capacity: %w", err)
		}
		expanded := minInt(255, capacity+ref.Param1)
		if expanded <= capacity {
			return result, ErrItemUseNoEffect
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `STORAGE_INFO` SET `CAPACITY`=? WHERE `FK_USER`=?", expanded, userID); err != nil {
			return result, fmt.Errorf("persist warehouse expansion: %w", err)
		}
	case isTimeService || isConfiguredTimedProgression:
		now := time.Now().UTC()
		var effect ItemServiceEffect
		var effectErr error
		var cooldownSeconds uint32
		var reinforceEquipment bool
		if isTimeService {
			effect, effectErr = itemServiceFromReference(ref, now)
		} else {
			var effectKind string
			var value, maxValue, durationSeconds uint32
			var requiresTarget bool
			effectErr = tx.QueryRowContext(ctx, "SELECT `EFFECT_KIND`,`VALUE`,`MAX_VALUE`,`DURATION_SECONDS`,`COOLDOWN_SECONDS`,`REQUIRES_TARGET_SLOT` FROM `EXPENDABLE_EFFECT_CONFIG` WHERE `ITEM_REF_ID`=?", ref.ID).Scan(&effectKind, &value, &maxValue, &durationSeconds, &cooldownSeconds, &requiresTarget)
			if effectErr == nil {
				effect = ItemServiceEffect{SourceRefItemID: ref.ID, EffectValue: value, DailyBudget: time.Millisecond, ActivatedAt: now, ExpiresAt: now.Add(time.Duration(durationSeconds) * time.Second)}
				switch effectKind {
				case "hwan_duration":
					effect.Kind = ItemServiceHwanDuration
					if timeService, exists := RefItems[ref.ID+6]; exists &&
						timeService.TypeID1 == 3 && timeService.TypeID2 == 3 && timeService.TypeID3 == 13 && timeService.TypeID4 == 14 &&
						timeService.Param1 == ref.Param1 {
						effect.SourceRefItemID = timeService.ID
					}
				case "reinforce_weapon":
					effect.Kind = ItemServiceReinforceWeapon
				case "reinforce_armor":
					effect.Kind = ItemServiceReinforceArmor
				case "reinforce_accessory":
					effect.Kind = ItemServiceReinforceAccessory
				case "reinforce_equipment":
					reinforceEquipment = true
				case "opt_level":
					if !IsConfiguredOptionUpgradeScroll(ref) || ref.Param1 <= 0 {
						effectErr = ErrItemUseUnsupported
						break
					}
					reinforceEquipment = true
					durationSeconds = uint32(ref.Param1)
					effect.ExpiresAt = now.Add(time.Duration(durationSeconds) * time.Second)
				default:
					effectErr = ErrItemUseUnsupported
				}
				if value == 0 || maxValue == 0 || value > maxValue || durationSeconds == 0 ||
					requiresTarget != IsConfiguredEnhancementScroll(ref) {
					effectErr = ErrItemUseUnsupported
				}
			}
		}
		if errors.Is(effectErr, sql.ErrNoRows) {
			return result, ErrItemUseUnsupported
		}
		if effectErr != nil {
			return result, effectErr
		}
		if effect.Kind == ItemServiceHwanDuration {
			var points byte
			if err = tx.QueryRowContext(ctx, "SELECT `REMAIN_HWAN_COUNT` FROM `CHAR` WHERE `id`=?", characterID).Scan(&points); err != nil {
				return result, fmt.Errorf("load HWAN gauge for Energy of Life: %w", err)
			}
			if points >= MaxHwanPoints {
				return result, ErrItemUseNoEffect
			}
		}
		if IsConfiguredEnhancementScroll(ref) {
			if target == nil || target.ObjectID == 0 || target.ObjectID > uint32(^uint8(0))+1 {
				return result, ErrItemUseInvalidRequest
			}
			targetSlot := byte(target.ObjectID - 1)
			var targetTypeID3, targetOptLevel int
			if err = tx.QueryRowContext(ctx, "SELECT r.`TypeID_3`,it.`OPT_LEVEL` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` r ON r.`ItemID`=it.`FK_REF_ITEM` WHERE i.`FK_CHAR`=? AND i.`SLOT`=? FOR UPDATE", characterID, targetSlot).Scan(&targetTypeID3, &targetOptLevel); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return result, ErrItemUseNoEffect
				}
				return result, fmt.Errorf("lock reinforcement target: %w", err)
			}
			if reinforceEquipment {
				switch {
				case targetTypeID3 == TypeID3Weapon:
					effect.Kind = ItemServiceReinforceWeapon
				case targetTypeID3 >= 1 && targetTypeID3 <= 4:
					effect.Kind = ItemServiceReinforceArmor
				case targetTypeID3 == 5 || targetTypeID3 == 12:
					effect.Kind = ItemServiceReinforceAccessory
				default:
					return result, ErrItemUseNoEffect
				}
			}
			validTarget := effect.Kind == ItemServiceReinforceWeapon && targetTypeID3 == TypeID3Weapon ||
				effect.Kind == ItemServiceReinforceArmor && targetTypeID3 >= 1 && targetTypeID3 <= 4 ||
				effect.Kind == ItemServiceReinforceAccessory && (targetTypeID3 == 5 || targetTypeID3 == 12)
			if !validTarget {
				return result, ErrItemUseNoEffect
			}
			effect.TargetSlot = targetSlot
			effect.TargetSlotSet = true
			effect.TargetBaseOptLevel = byte(min(255, max(0, targetOptLevel)))
			effect.TargetProjectedOptLevel = byte(min(255, max(0, targetOptLevel)+int(effect.EffectValue)))
		}
		var currentActivation, currentExpiration time.Time
		err = tx.QueryRowContext(ctx, "SELECT `ACTIVATED_AT`,`EXPIRES_AT` FROM `CHAR_ITEM_SERVICE` WHERE `CHAR_ID`=? AND `SERVICE_KIND`=? FOR UPDATE", characterID, effect.Kind).Scan(&currentActivation, &currentExpiration)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("lock character item service: %w", err)
		}
		if currentExpiration.After(now) && (IsConfiguredEnhancementScroll(ref) || effect.Kind == ItemServiceHwanDuration) {
			return result, ErrItemUseNoEffect
		}
		base := effect.ActivatedAt
		if currentExpiration.After(base) {
			base = currentExpiration
		}
		effect.ExpiresAt = base.Add(effect.ExpiresAt.Sub(effect.ActivatedAt))
		var targetSlot, targetBaseOptLevel, targetProjectedOptLevel any
		if effect.TargetSlotSet {
			targetSlot = effect.TargetSlot
			targetBaseOptLevel = effect.TargetBaseOptLevel
			targetProjectedOptLevel = effect.TargetProjectedOptLevel
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_ITEM_SERVICE` (`CHAR_ID`,`SERVICE_KIND`,`SOURCE_REF_ITEM_ID`,`EXPERIENCE_BONUS_PERCENT`,`SKILL_EXPERIENCE_BONUS_PERCENT`,`EFFECT_VALUE`,`TARGET_SLOT`,`TARGET_BASE_OPT_LEVEL`,`TARGET_PROJECTED_OPT_LEVEL`,`DAILY_BUDGET_MS`,`ACTIVATED_AT`,`EXPIRES_AT`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `SOURCE_REF_ITEM_ID`=VALUES(`SOURCE_REF_ITEM_ID`),`EXPERIENCE_BONUS_PERCENT`=VALUES(`EXPERIENCE_BONUS_PERCENT`),`SKILL_EXPERIENCE_BONUS_PERCENT`=VALUES(`SKILL_EXPERIENCE_BONUS_PERCENT`),`EFFECT_VALUE`=VALUES(`EFFECT_VALUE`),`TARGET_SLOT`=VALUES(`TARGET_SLOT`),`TARGET_BASE_OPT_LEVEL`=VALUES(`TARGET_BASE_OPT_LEVEL`),`TARGET_PROJECTED_OPT_LEVEL`=VALUES(`TARGET_PROJECTED_OPT_LEVEL`),`DAILY_BUDGET_MS`=VALUES(`DAILY_BUDGET_MS`),`ACTIVATED_AT`=VALUES(`ACTIVATED_AT`),`EXPIRES_AT`=VALUES(`EXPIRES_AT`)", characterID, effect.Kind, effect.SourceRefItemID, effect.ExperienceBonusPercent, effect.SkillExperienceBonusPercent, effect.EffectValue, targetSlot, targetBaseOptLevel, targetProjectedOptLevel, effect.DailyBudget.Milliseconds(), effect.ActivatedAt, effect.ExpiresAt); err != nil {
			return result, fmt.Errorf("persist character item service: %w", err)
		}
		result.ItemService = &effect
	case isReturnScroll || isSpecialReturnScroll:
		var destination TeleportDestination
		if isSpecialReturnScroll {
			err = tx.QueryRowContext(ctx, "SELECT `ID`,`REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID`,0,0 FROM `TELEPORT_POINT` WHERE `CODE_NAME`=? AND `SERVICE`=1", strings.TrimSpace(ref.Param4Desc)).Scan(
				&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID, &destination.Fee, &destination.MinLevel,
			)
		} else {
			// RETURN_TELEPORT_ID is the authoritative per-character bind point.
			err = tx.QueryRowContext(ctx, "SELECT t.`ID`,t.`REGION_ID`,t.`POS_X`,t.`POS_Y`,t.`POS_Z`,t.`WORLD_ID`,0,0 FROM `CHAR` c JOIN `TELEPORT_POINT` t ON t.`ID`=c.`RETURN_TELEPORT_ID` AND t.`SERVICE`=1 WHERE c.`ID`=?", characterID).Scan(
				&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID, &destination.Fee, &destination.MinLevel,
			)
		}
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
		switch vitals.ReverseReturnOption {
		case 2:
			destination.ID = ref.ID
			err = tx.QueryRowContext(ctx, "SELECT `REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID` FROM `CHAR_REVERSE_RETURN_POSITION` WHERE `CHAR_ID`=? FOR UPDATE", characterID).Scan(&destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID)
		case 3:
			destination.ID = ref.ID
			err = tx.QueryRowContext(ctx, "SELECT `REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID` FROM `CHAR_DEATH_POSITION` WHERE `CHAR_ID`=? FOR UPDATE", characterID).Scan(&destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID)
		case 7:
			destination.ID = vitals.ReverseReturnMapID
			err = tx.QueryRowContext(ctx, "SELECT o.`ID`,o.`REGION_ID`,o.`POS_X`,o.`POS_Y`,o.`POS_Z`,o.`WORLD_ID`,0,o.`MIN_LEVEL` FROM `REF_OPTIONAL_TELEPORT` o JOIN `REGION_REFERENCE` r ON r.`wRegionID`=o.`REGION_ID` JOIN `GAME_SERVER_REGION` g ON g.`Continent_Name`=r.`ContinentName` AND g.`Game_Server_ID`=1 WHERE o.`ID`=? AND o.`SERVICE`=1 AND (o.`MIN_LEVEL`=0 OR o.`MIN_LEVEL`<=?) AND (o.`MAX_LEVEL`=0 OR o.`MAX_LEVEL`>=?)", vitals.ReverseReturnMapID, vitals.Level, vitals.Level).Scan(&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID, &destination.Fee, &destination.MinLevel)
		default:
			return result, ErrItemUseInvalidRequest
		}
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

func completeTalismanQuestsInItemUseTransaction(ctx context.Context, tx *sql.Tx, characterID int64, themeID uint32) ([]QuestProgressResult, error) {
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.OBJECTIVE_KEY
		FROM CHAR_QUEST quest
		JOIN REF_QUEST_TALISMAN_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		JOIN REF_COLLECTION_BOOK_THEME theme ON theme.CODE_NAME=objective.THEME_CODE
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1
		  AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0 AND objective.PARAMETER_6<>0
		  AND theme.THEME_ID=? ORDER BY quest.QUEST_ID FOR UPDATE`,
		characterID, QuestActive, QuestProgressed, themeID)
	if err != nil {
		return nil, fmt.Errorf("lock talisman quests: %w", err)
	}
	var quests []CharacterQuest
	for rows.Next() {
		var quest CharacterQuest
		if err = rows.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount, &quest.StartedAt,
			&quest.CompletedAt, &quest.Data1, &quest.Data2, &quest.ObjectiveKey); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan talisman quest: %w", err)
		}
		quest.TaskCount = 1
		quests = append(quests, quest)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate talisman quests: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close talisman quests: %w", err)
	}
	results := make([]QuestProgressResult, 0, len(quests))
	for _, quest := range quests {
		previousStatus := quest.Status
		quest.Status, err = TransitionQuest(quest.Status, QuestTransitionRewardReady)
		if err != nil {
			return nil, err
		}
		mutation, updateErr := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", quest.Status, characterID, quest.QuestID, previousStatus)
		if updateErr != nil {
			return nil, fmt.Errorf("complete talisman quest: %w", updateErr)
		}
		if updateErr = requireQuestRow(mutation, "complete talisman quest"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: quest, Changed: true})
	}
	return results, nil
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

// IsCOSTransportScroll identifies consumable ride-COS scrolls. Unlike pet
// instruments, these create a temporary transport and must not retain an
// ITEM.DATA-to-CHAR_COS association after the source stack is consumed.
func IsCOSTransportScroll(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 3 && ref.TypeID4 == 2 &&
		(strings.Contains(strings.ToUpper(ref.CodeName), "COS_T_") || strings.Contains(strings.ToUpper(ref.CodeName), "COS_C_"))
}

// DeleteCOSTimedService removes one committed timed service after runtime
// expiry. It is intentionally separate from COSRepository so existing
// repositories remain source-compatible.
type COSTimedServiceRepository interface {
	DeleteCOSTimedService(context.Context, int64, uint64, int) error
}

func (r *SQLItemUseRepository) DeleteCOSTimedService(ctx context.Context, characterID int64, cosID uint64, jobID int) error {
	if r == nil || r.shard == nil {
		return ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 || cosID == 0 || jobID <= 0 {
		return ErrItemUseInvalidRequest
	}
	_, err := r.shard.ExecContext(ctx, "DELETE FROM `CHAR_COS_TIMED_SERVICE` WHERE `OWNER_CHAR_ID`=? AND `COS_ID`=? AND `JOB_ID`=? AND `EXPIRES_AT`<=CURRENT_TIMESTAMP(6)", characterID, cosID, jobID)
	return err
}

// IsCOSHealthPotion identifies the target-bearing flat COS HP recovery family.
func IsCOSHealthPotion(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 1 && ref.TypeID4 == 4
}

// IsCOSHGPotion identifies the target-bearing growth-pet HGP recovery family.
func IsCOSHGPotion(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 1 && ref.TypeID4 == 9
}

// IsCOSPetTimedService identifies the TypeID4=15 growth/skill/watch family.
func IsCOSPetTimedService(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 15 &&
		(ref.ID == 22710 || ref.ID == 22711 || ref.ID == 22712 || ref.ID == 22713 || ref.ID == 22714 || ref.ID == 22715 || ref.ID == 22926 || ref.ID == 22927 || ref.ID == 35874)
}

// COSPetTimedServiceFields maps the confirmed catalog families to the binary
// service categories and resolves the actual JobID from the loaded REF_SKILL
// catalog. It rejects the use when that authoritative skill row is absent.
func COSPetTimedServiceTargetKind(ref RefItem) COSKind {
	switch ref.ID {
	case 22710, 22711, 22926, 22927:
		return COSAbilityPet
	default:
		return COSGrowthPet
	}
}

func COSPetTimedServiceFields(ref RefItem, kind COSKind) (category, jobID, data1 int, ok bool) {
	if !IsCOSPetTimedService(ref) || ref.Param1 <= 0 {
		return 0, 0, 0, false
	}
	codeName := ""
	switch ref.ID {
	case 22712, 35874:
		codeName = "SKILL_MALL_PET_GROWTH_POTION_01"
	case 22713:
		codeName = "SKILL_MALL_PET_SKILL_COLD"
	case 22714:
		codeName = "SKILL_MALL_PET_SKILL_FIRE"
	case 22715:
		codeName = "SKILL_MALL_PET_SKILL_LIGHTNING"
	case 22710:
		codeName = "SKILL_MALL_PET_WATCH_CHULHYEN_01"
	case 22711:
		codeName = "SKILL_MALL_PET_WATCH_AGOL_01"
	case 22926:
		codeName = "SKILL_MALL_PET_WATCH_CHULHYEN_02"
	case 22927:
		codeName = "SKILL_MALL_PET_WATCH_AGOL_02"
	default:
		return 0, 0, 0, false
	}
	var skillID uint32
	for id, skill := range RefSkills {
		if skill.CodeName == codeName {
			skillID = id
			break
		}
	}
	if skillID == 0 {
		return 0, 0, 0, false
	}
	if ref.ID == 22710 || ref.ID == 22711 || ref.ID == 22926 || ref.ID == 22927 {
		if kind != COSAbilityPet {
			return 0, 0, 0, false
		}
		return 5, int(skillID), 28, true
	}
	if kind != COSGrowthPet {
		return 0, 0, 0, false
	}
	return 0, int(skillID), 0, true
}

// IsCOSExtension identifies the ability-pet rental extension family. Param1
// is the catalog duration in minutes (1440 for one day, 40320 for 28 days).
func IsCOSExtension(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 12
}

// IsCOSRevival identifies the pet-revival consumable family. The pinned
// GameServer binary exposes the dead COS state and generic COS state SQL, but
// does not expose a stable symbolic item name. Keep the data-driven predicate
// deliberately narrow so character resurrection items stay on their own path.
func IsCOSRevival(ref RefItem) bool {
	if !ref.IsUseable {
		return false
	}
	name := strings.ToUpper(strings.TrimSpace(ref.CodeName))
	// Grass of Life (Type 3/3/2/7) has six distinct cure parameters and is not
	// promoted to revival without a recovered effect mapping.
	return (strings.Contains(name, "REVIV") && (strings.Contains(name, "COS") || strings.Contains(name, "PET"))) ||
		(ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 1 && ref.TypeID4 == 6 && strings.Contains(name, "COS_P_"))
}

func resolveTemporaryRideReference(ctx context.Context, tx *sql.Tx, item RefItem, characterLevel int) (uint32, error) {
	codeName := strings.TrimSpace(item.Param1Desc)
	if tx == nil || codeName == "" || strings.EqualFold(codeName, "xxx") {
		return 0, ErrItemUseNoEffect
	}
	var refObjectID uint32
	var typeID1, typeID2, typeID3, typeID4 int
	err := tx.QueryRowContext(ctx, "SELECT `RefObjID`,`TypeID_1`,`TypeID_2`,`TypeID_3`,`TypeID_4` FROM `CHAR_REF_DATA` WHERE `CodeName`=? LIMIT 1", codeName).Scan(
		&refObjectID, &typeID1, &typeID2, &typeID3, &typeID4,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// Stock trade transports such as the white elephant reference a base
		// family in ITEMDATA while CHAR_REF_DATA stores only level-suffixed
		// variants. Select the strongest variant the character may summon.
		err = tx.QueryRowContext(ctx, "SELECT `RefObjID`,`TypeID_1`,`TypeID_2`,`TypeID_3`,`TypeID_4` FROM `CHAR_REF_DATA` WHERE `CodeName` LIKE CONCAT(?, '\\_%') ESCAPE '\\\\' AND `Lvl`<=? ORDER BY `Lvl` DESC,`RefObjID` DESC LIMIT 1", codeName, characterLevel).Scan(
			&refObjectID, &typeID1, &typeID2, &typeID3, &typeID4,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrItemUseNoEffect
		}
	}
	if err != nil {
		return 0, fmt.Errorf("resolve temporary ride COS reference: %w", err)
	}
	// C-family riding mounts use TypeID4=1; T-family trade transports use
	// TypeID4=2. Both are temporary COS scroll targets, but their client
	// movement semantics are different and the catalog discriminator must be
	// preserved through runtime spawn serialization.
	if refObjectID == 0 || typeID1 != 1 || typeID2 != 2 || typeID3 != 3 || (typeID4 != 1 && typeID4 != 2) {
		return 0, ErrInvalidCOSKind
	}
	return refObjectID, nil
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
	rows, err := r.shard.QueryContext(ctx, "SELECT s.`SERVICE_KIND`,s.`SOURCE_REF_ITEM_ID`,s.`EXPERIENCE_BONUS_PERCENT`,s.`SKILL_EXPERIENCE_BONUS_PERCENT`,s.`EFFECT_VALUE`,s.`TARGET_SLOT`,s.`TARGET_BASE_OPT_LEVEL`,s.`TARGET_PROJECTED_OPT_LEVEL`,s.`DAILY_BUDGET_MS`,s.`ACTIVATED_AT`,s.`EXPIRES_AT`,d.`USED_ON`,d.`ACTIVE_UNTIL` FROM `CHAR_ITEM_SERVICE` s LEFT JOIN `CHAR_ITEM_SERVICE_DAILY` d ON d.`CHAR_ID`=s.`CHAR_ID` AND d.`SERVICE_KIND`=s.`SERVICE_KIND` WHERE s.`CHAR_ID`=? AND s.`EXPIRES_AT`>UTC_TIMESTAMP(6) ORDER BY s.`SERVICE_KIND`", characterID)
	if err != nil {
		return nil, fmt.Errorf("load character item services: %w", err)
	}
	defer rows.Close()
	var effects []ItemServiceEffect
	for rows.Next() {
		var effect ItemServiceEffect
		var budgetMS int64
		var targetSlot, targetBaseOptLevel, targetProjectedOptLevel sql.NullByte
		var usedOn, activeUntil sql.NullTime
		if err = rows.Scan(&effect.Kind, &effect.SourceRefItemID, &effect.ExperienceBonusPercent, &effect.SkillExperienceBonusPercent, &effect.EffectValue, &targetSlot, &targetBaseOptLevel, &targetProjectedOptLevel, &budgetMS, &effect.ActivatedAt, &effect.ExpiresAt, &usedOn, &activeUntil); err != nil {
			return nil, fmt.Errorf("scan character item service: %w", err)
		}
		if targetSlot.Valid && targetBaseOptLevel.Valid && targetProjectedOptLevel.Valid {
			effect.TargetSlot = targetSlot.Byte
			effect.TargetSlotSet = true
			effect.TargetBaseOptLevel = targetBaseOptLevel.Byte
			effect.TargetProjectedOptLevel = targetProjectedOptLevel.Byte
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
		if !exists || !skill.SupportsItemUseBuff() {
			return nil, fmt.Errorf("load character item buff skill %d: %w", effect.SkillID, ErrItemUseUnsupported)
		}
		effect.ExpiresAt = effect.ExpiresAt.UTC()
		effect.Modifiers = skill.BuffModifiers
		effect.RecoveryHP = skill.RecoveryHP
		effect.RecoveryMP = skill.RecoveryMP
		effect.RefreshVisibility = skill.DetectionLevel > 0
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
	maximum := now.Add(RuntimeMaxSpeedEffectDuration())
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

func (r *SQLItemUseRepository) PersistItemBuffRecovery(ctx context.Context, characterID int64, currentHP, currentMP, healedHP, healedMP int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrItemUseRepositoryUnavailable
	}
	if characterID <= 0 || healedHP < currentHP || healedMP < currentMP || healedHP == currentHP && healedMP == currentMP {
		return false, ErrItemUseInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `CHAR` SET `HP`=?,`MP`=? WHERE `id`=? AND `HP`=? AND `MP`=?", healedHP, healedMP, characterID, currentHP, currentMP)
	if err != nil {
		return false, fmt.Errorf("persist item-buff recovery: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect item-buff recovery: %w", err)
	}
	return affected == 1, nil
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
