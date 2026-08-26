package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

/*
	Slots:

	0 - Helm
	1 - Chest
	2 - Shoulder
	3 - Gauntlet
	4 - Pants
	5 - Boots
	6 - Primary Weapon
	7 - Secondary Weapon (Ammo, Shield)
	8 - Earring
	9 - Necklace
	10 - Left Ring
	11 - Right Ring
	12 - Extra Slot (PVP Cape, Job Suit)

	Avatar Inventory
	0 - Helm
	1 - Chest / Dress
	2 - Attachment
	3 - Flag
	4 - Devil Spirit
*/

const (
	SelectCharacterInventory string = "SELECT inv.SLOT,inv.FK_ITEM,it.VARIANCE,it.FK_REF_ITEM,it.STACK_COUNT,it.OPT_LEVEL,it.DATA,it.RENT_TYPE,it.RENT_CAN_DELETE,it.RENT_CAN_RECHARGE,it.RENT_PERIOD_BEGIN_TIME,it.RENT_PERIOD_END_TIME,it.RENT_METER_RATE_TIME,it.RENT_PACKING_TIME,COALESCE(cos.REF_OBJECT_ID,0),COALESCE(cos.KIND,0),COALESCE(cos.STATE,0),COALESCE(cos.NAME,''),COALESCE(cos.LEVEL,0),cos.RENT_ENDS_AT,COALESCE(cos.INVENTORY_SLOT,0),COALESCE(cos.HEALTH,0),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',mo.SLOT,'type',mo.TYPE,'value',mo.VALUE,'paramType',COALESCE(ro.PARAM_TYPE,0))) FROM `SRO_SHARD`.`ITEM_MAGIC_OPTION` mo LEFT JOIN `SRO_SHARD`.`REF_MAGIC_OPTION` ro ON ro.ID=mo.TYPE WHERE mo.FK_ITEM=it.ID),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `SRO_SHARD`.`ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=1),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `SRO_SHARD`.`ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=2),JSON_ARRAY()),COALESCE(ap.ASTRAL,0),COALESCE(ap.IMMORTAL,0),COALESCE(ap.STEADY,0) FROM `SRO_SHARD`.`INVENTORY` inv INNER JOIN `SRO_SHARD`.`ITEM` AS it ON inv.FK_ITEM=it.ID LEFT JOIN `SRO_SHARD`.`CHAR_COS` cos ON cos.ID=it.DATA AND cos.OWNER_CHAR_ID=inv.FK_CHAR LEFT JOIN `SRO_SHARD`.`ITEM_ALCHEMY_PROTECTION` ap ON ap.FK_ITEM=it.ID WHERE inv.FK_CHAR=?"
	InsertInventoryItem      string = "INSERT INTO `SRO_SHARD`.`INVENTORY` (FK_CHAR, SLOT, FK_ITEM) VALUES(?, ?, ?);"
)

var (
	// ErrInventoryRepositoryUnavailable identifies a missing injected shard
	// pool or transaction. New request paths must not fall back to a
	// request-scoped connection when this occurs.
	ErrInventoryRepositoryUnavailable = errors.New("inventory repository is unavailable")
	ErrInventoryCharacterIDRequired   = errors.New("inventory character ID is required")
	ErrInventoryItemIDRequired        = errors.New("inventory item ID is required")
	ErrInventorySlotOutOfRange        = errors.New("inventory slot is out of range")
	ErrInventoryDuplicateSlot         = errors.New("inventory contains duplicate slot")
	ErrInventoryReferenceItemNotFound = errors.New("inventory reference item is unavailable")
)

// InventoryRepositoryError identifies a repository boundary failure while
// preserving the driver, context, scan, or validation error for errors.Is and
// callers that need to decide whether an operation can be retried.
type InventoryRepositoryError struct {
	Operation string
	Err       error
}

func (e *InventoryRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("inventory repository %s: %v", e.Operation, e.Err)
}

func (e *InventoryRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// InventoryRepository is the context-aware persistence boundary for normal
// character inventory reads and slot inserts. The injected DBTX may be a
// long-lived shard pool today or a caller-owned transaction when the item
// mutation kernel is introduced.
type InventoryRepository interface {
	GetCharacterInventory(ctx context.Context, characterID int64) (Inventory, error)
	AddItemToInventory(ctx context.Context, characterID, itemID int64, slot byte) error
}

// SQLInventoryRepository uses an injected long-lived shard pool or
// transaction. It never opens or closes that dependency.
type SQLInventoryRepository struct {
	shard frameworkdb.DBTX
}

var _ InventoryRepository = (*SQLInventoryRepository)(nil)

// NewInventoryRepository constructs an inventory repository over an injected
// shard pool or transaction.
func NewInventoryRepository(shard frameworkdb.DBTX) (*SQLInventoryRepository, error) {
	if shard == nil {
		return nil, &InventoryRepositoryError{
			Operation: "configure shard database",
			Err:       ErrInventoryRepositoryUnavailable,
		}
	}
	return &SQLInventoryRepository{shard: shard}, nil
}

// GetCharacterInventory reads the persisted normal inventory for one
// character. It verifies every scan and the terminal rows result so a partial
// cursor cannot be presented as a complete inventory snapshot.
func (r *SQLInventoryRepository) GetCharacterInventory(ctx context.Context, characterID int64) (Inventory, error) {
	if err := validateInventoryCharacterID(characterID); err != nil {
		return emptyInventory(), err
	}
	if err := r.requireShard("query character inventory"); err != nil {
		return emptyInventory(), err
	}
	ctx, err := inventoryRepositoryContext(ctx, "query character inventory")
	if err != nil {
		return emptyInventory(), err
	}

	rows, err := r.shard.QueryContext(ctx, SelectCharacterInventory, characterID)
	if err != nil {
		return emptyInventory(), &InventoryRepositoryError{Operation: "query character inventory", Err: err}
	}
	defer func() { _ = rows.Close() }()

	inventory := emptyInventory()
	for rows.Next() {
		slot, item, err := scanInventoryItem(rows)
		if err != nil {
			return emptyInventory(), &InventoryRepositoryError{Operation: "scan character inventory", Err: err}
		}
		if _, exists := inventory.Items[slot]; exists {
			return emptyInventory(), &InventoryRepositoryError{
				Operation: "validate character inventory",
				Err:       fmt.Errorf("%w: %d", ErrInventoryDuplicateSlot, slot),
			}
		}
		inventory.Items[slot] = item
	}
	if err := rows.Err(); err != nil {
		return emptyInventory(), &InventoryRepositoryError{Operation: "iterate character inventory", Err: err}
	}
	if err := rows.Close(); err != nil {
		return emptyInventory(), &InventoryRepositoryError{Operation: "close character inventory rows", Err: err}
	}
	return inventory, nil
}

// AddItemToInventory inserts one item into a validated normal inventory slot.
// It deliberately does not provide a transaction, swap, or reservation API;
// callers that need multi-row atomicity can inject a *sql.Tx through DBTX in
// the future transaction kernel.
func (r *SQLInventoryRepository) AddItemToInventory(ctx context.Context, characterID, itemID int64, slot byte) error {
	if err := validateInventoryCharacterID(characterID); err != nil {
		return err
	}
	if err := validateInventoryItemID(itemID); err != nil {
		return err
	}
	if err := validateInventorySlot(slot); err != nil {
		return err
	}
	if err := r.requireShard("insert character inventory slot"); err != nil {
		return err
	}
	ctx, err := inventoryRepositoryContext(ctx, "insert character inventory slot")
	if err != nil {
		return err
	}

	if _, err := r.shard.ExecContext(ctx, InsertInventoryItem, characterID, slot, itemID); err != nil {
		return &InventoryRepositoryError{Operation: "insert character inventory slot", Err: err}
	}
	return nil
}

type inventoryScanner interface {
	Scan(dest ...any) error
}

func scanInventoryItem(scanner inventoryScanner) (byte, Item, error) {
	var slot, itemID int64
	var variance uint64
	var referenceItemID uint32
	var stackCount int
	var optLevel byte
	var data uint32
	var rent RentInfo
	var cosRefObjectID uint32
	var cosKind, cosState, cosLevel, cosInventorySlot byte
	var cosName string
	var cosRentEndsAt sql.NullTime
	var cosHealth int32
	var magicJSON, socketsJSON, advancedJSON []byte
	var protection EquipmentProtection
	if err := scanner.Scan(&slot, &itemID, &variance, &referenceItemID, &stackCount, &optLevel, &data, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime, &cosRefObjectID, &cosKind, &cosState, &cosName, &cosLevel, &cosRentEndsAt, &cosInventorySlot, &cosHealth, &magicJSON, &socketsJSON, &advancedJSON, &protection.Astral, &protection.Immortal, &protection.Steady); err != nil {
		return 0, Item{}, err
	}
	magicOptions, err := decodeMagicOptions(magicJSON)
	if err != nil {
		return 0, Item{}, err
	}
	sockets, err := decodeBindingOptions(socketsJSON)
	if err != nil {
		return 0, Item{}, err
	}
	advanced, err := decodeBindingOptions(advancedJSON)
	if err != nil || !protection.Valid() {
		return 0, Item{}, ErrAlchemyConflict
	}

	validatedSlot, err := validatedPersistedInventorySlot(slot)
	if err != nil {
		return 0, Item{}, err
	}
	if err := validateInventoryItemID(itemID); err != nil {
		return 0, Item{}, err
	}
	if itemID > int64(^uint(0)>>1) {
		return 0, Item{}, fmt.Errorf("%w: %d exceeds platform integer range", ErrInventoryItemIDRequired, itemID)
	}

	refItem, exists := RefItems[referenceItemID]
	if !exists {
		return 0, Item{}, fmt.Errorf("%w: %d", ErrInventoryReferenceItemNotFound, referenceItemID)
	}

	item := Item{
		ID:              int(itemID),
		Name:            refItem.CodeName,
		Variance:        variance,
		StackSize:       stackCount,
		OptLevel:        optLevel,
		Data:            data,
		RentInfo:        rent,
		MagicOptions:    magicOptions,
		SocketOptions:   sockets,
		AdvancedElixirs: advanced,
		Protection:      protection,
	}
	item.RefObjectID = refItem.ID
	item.LevelInfo = LevelInfo{
		RequiredLevelType1: refItem.ReqLevelType1,
		RequiredLevelType2: refItem.ReqLevelType2,
		RequiredLevelType3: refItem.ReqLevelType3,
		RequiredLevelType4: refItem.ReqLevelType4,
		RequiredLevel1:     refItem.ReqLevel1,
		RequiredLevel2:     refItem.ReqLevel2,
		RequiredLevel3:     refItem.ReqLevel3,
		RequiredLevel4:     refItem.ReqLevel4,
	}
	item.SetTypeInfo(TypeInfo{
		TypeID1: refItem.TypeID1,
		TypeID2: refItem.TypeID2,
		TypeID3: refItem.TypeID3,
		TypeID4: refItem.TypeID4,
	})
	if cosRefObjectID != 0 {
		state := COSItemState(cosState)
		if state < COSItemInactive || state > COSItemDead {
			state = COSItemActive
		}
		if cosHealth <= 0 {
			state = COSItemDead
		}
		item.COS = COSItemInfo{Present: true, RefObjectID: cosRefObjectID, Kind: COSKind(cosKind), State: state, Name: cosName, Level: cosLevel, InventorySlot: cosInventorySlot}
		if cosRentEndsAt.Valid {
			item.COS.RentEndsAt = cosRentEndsAt.Time.UTC()
		}
	}
	return validatedSlot, item, nil
}

func decodeMagicOptions(data []byte) ([]MagicOption, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var values []struct {
		Slot      byte   `json:"slot"`
		Type      uint32 `json:"type"`
		Value     uint32 `json:"value"`
		ParamType uint32 `json:"paramType"`
	}
	if err := json.Unmarshal(data, &values); err != nil || len(values) > 12 {
		return nil, fmt.Errorf("decode item magic options: %w", err)
	}
	result := make([]MagicOption, len(values))
	for i, value := range values {
		result[i] = MagicOption{Type: value.Type, Value: value.Value, ParamType: value.ParamType}
	}
	return result, nil
}

func decodeBindingOptions(data []byte) ([]BindingOption, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var values []BindingOption
	if err := json.Unmarshal(data, &values); err != nil || len(values) > 12 {
		return nil, fmt.Errorf("decode item binding options: %w", err)
	}
	return values, nil
}

func validateInventoryCharacterID(characterID int64) error {
	if characterID <= 0 {
		return fmt.Errorf("%w: %d", ErrInventoryCharacterIDRequired, characterID)
	}
	return nil
}

func validateInventoryItemID(itemID int64) error {
	if itemID <= 0 {
		return fmt.Errorf("%w: %d", ErrInventoryItemIDRequired, itemID)
	}
	return nil
}

func validateInventorySlot(slot byte) error {
	if slot >= BaseInventorySize {
		return fmt.Errorf("%w: %d (maximum %d)", ErrInventorySlotOutOfRange, slot, BaseInventorySize-1)
	}
	return nil
}

func validatedPersistedInventorySlot(slot int64) (byte, error) {
	if slot < 0 || slot >= int64(BaseInventorySize) {
		return 0, fmt.Errorf("%w: %d (maximum %d)", ErrInventorySlotOutOfRange, slot, BaseInventorySize-1)
	}
	return byte(slot), nil
}

func (r *SQLInventoryRepository) requireShard(operation string) error {
	if r == nil || r.shard == nil {
		return &InventoryRepositoryError{Operation: operation, Err: ErrInventoryRepositoryUnavailable}
	}
	return nil
}

func inventoryRepositoryContext(ctx context.Context, operation string) (context.Context, error) {
	if ctx == nil {
		return context.Background(), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, &InventoryRepositoryError{Operation: operation, Err: err}
	}
	return ctx, nil
}

func emptyInventory() Inventory {
	return NewInventory(nil)
}

// AddItemToInventory is a compatibility wrapper for legacy callers. It owns a
// one-call shard handle and intentionally does not make it available to new
// repository users. New code must inject a long-lived pool or transaction and
// handle the returned error from SQLInventoryRepository.AddItemToInventory.
// INVENTORY has a composite character/slot primary key, so it has no
// meaningful auto-increment ID for this historical return value.
func AddItemToInventory(characterID, itemID int64, slot byte) int64 {
	connection := frameworkdb.OpenConnShard()
	defer connection.Close()

	repository, err := NewInventoryRepository(connection)
	if err != nil {
		return 0
	}
	if err := repository.AddItemToInventory(context.Background(), characterID, itemID, slot); err != nil {
		return 0
	}
	return 0
}

// GetCharacterInventory is a compatibility wrapper for legacy callers. The
// error-returning InventoryRepository API is required for new request paths;
// this wrapper returns a safe empty inventory when its isolated legacy query
// cannot complete.
func GetCharacterInventory(characterID int64) Inventory {
	connection := frameworkdb.OpenConnShard()
	defer connection.Close()

	repository, err := NewInventoryRepository(connection)
	if err != nil {
		return emptyInventory()
	}
	inventory, err := repository.GetCharacterInventory(context.Background(), characterID)
	if err != nil {
		return emptyInventory()
	}
	return inventory
}
