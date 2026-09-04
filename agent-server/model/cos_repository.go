package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var (
	ErrCOSRepositoryUnavailable = errors.New("COS repository is unavailable")
	ErrCOSNotFound              = errors.New("COS was not found")
)

type COSRecord struct {
	ID            uint64
	OwnerCharID   int
	RefObjectID   uint32
	Kind          COSKind
	State         COSItemState
	InventorySlot byte
	Name          string
	Level         byte
	Experience    int64
	Health        int32
	MaxHealth     int32
	HGP           uint16
	Satiety       int32
	Settings      uint32
	RentEndsAt    *time.Time
}

type COSRepository interface {
	LoadOwned(context.Context, int, uint64) (COSRecord, error)
	Save(context.Context, COSRecord) error
	Rename(context.Context, int, uint64, string) error
}

type COSInventoryRepository interface {
	LoadInventory(context.Context, int, uint64) (Inventory, error)
	StoreItem(context.Context, int, uint64, byte, int) error
}

type COSInventoryTransferRepository interface {
	TransferItem(context.Context, int, uint64, byte, byte, bool) (Inventory, Inventory, error)
}

// COSTimedServiceLoader restores active pet services before COS inventory and
// owner-data publication. Expired rows are deliberately excluded.
type COSTimedServiceLoader interface {
	LoadCOSTimedServices(context.Context, int, uint64, time.Time) ([]COSTimedService, error)
}

type SQLCOSRepository struct{ shard frameworkdb.DBTX }

func NewCOSRepository(shard frameworkdb.DBTX) (*SQLCOSRepository, error) {
	if shard == nil {
		return nil, ErrCOSRepositoryUnavailable
	}
	return &SQLCOSRepository{shard: shard}, nil
}

func (r *SQLCOSRepository) LoadOwned(ctx context.Context, ownerCharID int, id uint64) (record COSRecord, err error) {
	if r == nil || r.shard == nil || ownerCharID <= 0 || id == 0 {
		return record, ErrCOSRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var kind, state byte
	var rent sql.NullTime
	err = r.shard.QueryRowContext(ctx, "SELECT `ID`,`OWNER_CHAR_ID`,`REF_OBJECT_ID`,`KIND`,`STATE`,`INVENTORY_SLOT`,`NAME`,`LEVEL`,`EXPERIENCE`,`HEALTH`,`MAX_HEALTH`,`HGP`,`SATIETY`,`SETTINGS`,`RENT_ENDS_AT` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=?", id, ownerCharID).Scan(
		&record.ID, &record.OwnerCharID, &record.RefObjectID, &kind, &state, &record.InventorySlot, &record.Name,
		&record.Level, &record.Experience, &record.Health, &record.MaxHealth, &record.HGP,
		&record.Satiety, &record.Settings, &rent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return COSRecord{}, ErrCOSNotFound
	}
	if err != nil {
		return COSRecord{}, fmt.Errorf("load owned COS: %w", err)
	}
	record.Kind = COSKind(kind)
	if !record.Kind.Valid() {
		return COSRecord{}, ErrInvalidCOSKind
	}
	record.State = COSItemState(state)
	if record.State < COSItemInactive || record.State > COSItemDead {
		return COSRecord{}, fmt.Errorf("load owned COS: invalid state %d", state)
	}
	if rent.Valid {
		value := rent.Time.UTC()
		record.RentEndsAt = &value
	}
	return record, nil
}

func (r *SQLCOSRepository) LoadCOSTimedServices(ctx context.Context, ownerCharID int, id uint64, now time.Time) ([]COSTimedService, error) {
	if r == nil || r.shard == nil || ownerCharID <= 0 || id == 0 {
		return nil, ErrCOSRepositoryUnavailable
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT `CATEGORY`,`JOB_ID`,`DATA1`,`EXPIRES_AT` FROM `CHAR_COS_TIMED_SERVICE` WHERE `COS_ID`=? AND `OWNER_CHAR_ID`=? AND `EXPIRES_AT`>? ORDER BY `ID`", id, ownerCharID, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("load COS timed services: %w", err)
	}
	defer rows.Close()
	services := make([]COSTimedService, 0)
	for rows.Next() {
		var service COSTimedService
		if err = rows.Scan(&service.Category, &service.JobID, &service.Data1, &service.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan COS timed service: %w", err)
		}
		service.ExpiresAt = service.ExpiresAt.UTC()
		services = append(services, service)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate COS timed services: %w", err)
	}
	return services, nil
}

func (r *SQLCOSRepository) DeleteCOSTimedService(ctx context.Context, characterID int64, cosID uint64, jobID int) error {
	if r == nil || r.shard == nil || characterID <= 0 || cosID == 0 || jobID <= 0 {
		return ErrItemUseInvalidRequest
	}
	_, err := r.shard.ExecContext(ctx, "DELETE FROM `CHAR_COS_TIMED_SERVICE` WHERE `OWNER_CHAR_ID`=? AND `COS_ID`=? AND `JOB_ID`=? AND `EXPIRES_AT`<=CURRENT_TIMESTAMP(6)", characterID, cosID, jobID)
	return err
}

func (r *SQLCOSRepository) Save(ctx context.Context, record COSRecord) error {
	if r == nil || r.shard == nil || record.ID == 0 || record.OwnerCharID <= 0 || !record.Kind.Valid() || record.State < COSItemInactive || record.State > COSItemDead {
		return ErrCOSRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `CHAR_COS` SET `REF_OBJECT_ID`=?,`KIND`=?,`STATE`=?,`INVENTORY_SLOT`=?,`NAME`=?,`LEVEL`=?,`EXPERIENCE`=?,`HEALTH`=?,`MAX_HEALTH`=?,`HGP`=?,`SATIETY`=?,`SETTINGS`=? WHERE `ID`=? AND `OWNER_CHAR_ID`=?", record.RefObjectID, record.Kind, record.State, record.InventorySlot, record.Name, record.Level, record.Experience, record.Health, record.MaxHealth, record.HGP, record.Satiety, record.Settings, record.ID, record.OwnerCharID)
	if err != nil {
		return fmt.Errorf("save COS: %w", err)
	}
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil || !validCOSSaveAffected(affected) {
		return ErrCOSNotFound
	}
	return nil
}

// MySQL reports changed rows rather than matched rows unless clientFoundRows
// is enabled. A COS checkpoint frequently writes an identical summoned
// snapshot during GameReady or disconnect, so both zero and one are valid for
// an already-owned runtime record. More than one can never satisfy its primary
// key/owner predicate and indicates a broken persistence contract.
func validCOSSaveAffected(affected int64) bool {
	return affected == 0 || affected == 1
}

func (r *SQLCOSRepository) Rename(ctx context.Context, ownerCharID int, id uint64, name string) error {
	if r == nil || r.shard == nil || ownerCharID <= 0 || id == 0 {
		return ErrCOSRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `CHAR_COS` SET `NAME`=? WHERE `ID`=? AND `OWNER_CHAR_ID`=?", name, id, ownerCharID)
	if err != nil {
		return fmt.Errorf("rename COS: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return ErrCOSNotFound
	}
	return nil
}

const selectCOSInventory = "SELECT ci.SLOT,ci.FK_ITEM,it.VARIANCE,it.FK_REF_ITEM,it.STACK_COUNT,it.OPT_LEVEL,it.DATA,it.RENT_TYPE,it.RENT_CAN_DELETE,it.RENT_CAN_RECHARGE,it.RENT_PERIOD_BEGIN_TIME,it.RENT_PERIOD_END_TIME,it.RENT_METER_RATE_TIME,it.RENT_PACKING_TIME,COALESCE(linked.REF_OBJECT_ID,0),COALESCE(linked.KIND,0),COALESCE(linked.STATE,0),COALESCE(linked.NAME,''),COALESCE(linked.LEVEL,0),linked.RENT_ENDS_AT,COALESCE(linked.INVENTORY_SLOT,0),COALESCE(linked.HEALTH,0),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',mo.SLOT,'type',mo.TYPE,'value',mo.VALUE,'paramType',COALESCE(ro.PARAM_TYPE,0))) FROM `SRO_SHARD`.`ITEM_MAGIC_OPTION` mo LEFT JOIN `SRO_SHARD`.`REF_MAGIC_OPTION` ro ON ro.ID=mo.TYPE WHERE mo.FK_ITEM=it.ID),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `SRO_SHARD`.`ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=1),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `SRO_SHARD`.`ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=2),JSON_ARRAY()),COALESCE(ap.ASTRAL,0),COALESCE(ap.IMMORTAL,0),COALESCE(ap.STEADY,0) FROM `SRO_SHARD`.`COS_INVENTORY` ci INNER JOIN `SRO_SHARD`.`CHAR_COS` owned ON owned.ID=ci.COS_ID AND owned.OWNER_CHAR_ID=? INNER JOIN `SRO_SHARD`.`ITEM` it ON it.ID=ci.FK_ITEM LEFT JOIN `SRO_SHARD`.`CHAR_COS` linked ON linked.ID=it.DATA AND linked.OWNER_CHAR_ID=? LEFT JOIN `SRO_SHARD`.`ITEM_ALCHEMY_PROTECTION` ap ON ap.FK_ITEM=it.ID WHERE ci.COS_ID=? ORDER BY ci.SLOT"

func (r *SQLCOSRepository) LoadInventory(ctx context.Context, ownerCharID int, id uint64) (Inventory, error) {
	if r == nil || r.shard == nil || ownerCharID <= 0 || id == 0 {
		return NewInventory(nil), ErrCOSRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.shard.QueryContext(ctx, selectCOSInventory, ownerCharID, ownerCharID, id)
	if err != nil {
		return NewInventory(nil), fmt.Errorf("load COS inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	inventory := NewInventory(nil)
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return NewInventory(nil), fmt.Errorf("scan COS inventory: %w", scanErr)
		}
		if _, duplicate := inventory.Items[slot]; duplicate {
			return NewInventory(nil), fmt.Errorf("scan COS inventory: %w: %d", ErrInventoryDuplicateSlot, slot)
		}
		inventory.Items[slot] = item
	}
	if err = rows.Err(); err != nil {
		return NewInventory(nil), fmt.Errorf("iterate COS inventory: %w", err)
	}
	return inventory, nil
}

type cosTransactionBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func (r *SQLCOSRepository) StoreItem(ctx context.Context, ownerCharID int, id uint64, slot byte, itemID int) (err error) {
	if r == nil || r.shard == nil || ownerCharID <= 0 || id == 0 || itemID <= 0 {
		return ErrCOSRepositoryUnavailable
	}
	beginner, ok := r.shard.(cosTransactionBeginner)
	if !ok {
		return ErrCOSRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin COS pickup: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var ownedID uint64
	if err = tx.QueryRowContext(ctx, "SELECT `ID` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", id, ownerCharID).Scan(&ownedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCOSNotFound
		}
		return fmt.Errorf("lock COS for pickup: %w", err)
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO `COS_INVENTORY` (`COS_ID`,`SLOT`,`FK_ITEM`) SELECT ?,?,it.ID FROM `ITEM` it WHERE it.ID=? AND NOT EXISTS (SELECT 1 FROM `INVENTORY` inv WHERE inv.FK_ITEM=it.ID) AND NOT EXISTS (SELECT 1 FROM `STORAGE` st WHERE st.FK_ITEM=it.ID) AND NOT EXISTS (SELECT 1 FROM `COS_INVENTORY` ci WHERE ci.FK_ITEM=it.ID)", id, slot, itemID)
	if err != nil {
		return fmt.Errorf("store COS pickup: %w", err)
	}
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil || affected != 1 {
		return ErrCOSNotFound
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit COS pickup: %w", err)
	}
	return nil
}

func (r *SQLCOSRepository) TransferItem(ctx context.Context, ownerCharID int, id uint64, source, destination byte, toCOS bool) (Inventory, Inventory, error) {
	empty := NewInventory(nil)
	if r == nil || r.shard == nil || ownerCharID <= 0 || id == 0 {
		return empty, empty, ErrCOSRepositoryUnavailable
	}
	beginner, ok := r.shard.(cosTransactionBeginner)
	if !ok {
		return empty, empty, ErrCOSRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return empty, empty, fmt.Errorf("begin COS inventory transfer: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var lockedCharacter int
	if err = tx.QueryRowContext(ctx, "SELECT `id` FROM `CHAR` WHERE `id`=? FOR UPDATE", ownerCharID).Scan(&lockedCharacter); err != nil {
		return empty, empty, fmt.Errorf("lock COS transfer character: %w", err)
	}
	var lockedCOS uint64
	if err = tx.QueryRowContext(ctx, "SELECT `ID` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", id, ownerCharID).Scan(&lockedCOS); err != nil {
		return empty, empty, fmt.Errorf("lock COS transfer pet: %w", err)
	}
	var itemID int
	if toCOS {
		if err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", ownerCharID, source).Scan(&itemID); err != nil {
			return empty, empty, fmt.Errorf("lock player COS transfer item: %w", err)
		}
		var occupied int
		if scanErr := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `COS_INVENTORY` WHERE `COS_ID`=? AND `SLOT`=? FOR UPDATE", id, destination).Scan(&occupied); scanErr == nil || !errors.Is(scanErr, sql.ErrNoRows) {
			return empty, empty, ErrInventoryRelocationConflict
		}
		result, deleteErr := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", ownerCharID, source, itemID)
		if deleteErr != nil || !singleAffectedRow(result) {
			return empty, empty, ErrInventoryRelocationConflict
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `COS_INVENTORY` (`COS_ID`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", id, destination, itemID); err != nil {
			return empty, empty, fmt.Errorf("insert pet COS transfer item: %w", err)
		}
	} else {
		if err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `COS_INVENTORY` WHERE `COS_ID`=? AND `SLOT`=? FOR UPDATE", id, source).Scan(&itemID); err != nil {
			return empty, empty, fmt.Errorf("lock pet COS transfer item: %w", err)
		}
		var occupied int
		if scanErr := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", ownerCharID, destination).Scan(&occupied); scanErr == nil || !errors.Is(scanErr, sql.ErrNoRows) {
			return empty, empty, ErrInventoryRelocationConflict
		}
		result, deleteErr := tx.ExecContext(ctx, "DELETE FROM `COS_INVENTORY` WHERE `COS_ID`=? AND `SLOT`=? AND `FK_ITEM`=?", id, source, itemID)
		if deleteErr != nil || !singleAffectedRow(result) {
			return empty, empty, ErrInventoryRelocationConflict
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", ownerCharID, destination, itemID); err != nil {
			return empty, empty, fmt.Errorf("insert player COS transfer item: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return empty, empty, fmt.Errorf("commit COS inventory transfer: %w", errors.Join(ErrInventoryRelocationOutcomeUnknown, err))
	}
	committed = true
	playerRepository, repositoryErr := NewInventoryRepository(r.shard)
	if repositoryErr != nil {
		return empty, empty, repositoryErr
	}
	playerInventory, err := playerRepository.GetCharacterInventory(ctx, int64(ownerCharID))
	if err != nil {
		return empty, empty, err
	}
	cosInventory, err := r.LoadInventory(ctx, ownerCharID, id)
	if err != nil {
		return empty, empty, err
	}
	return playerInventory, cosInventory, nil
}

func singleAffectedRow(result sql.Result) bool {
	if result == nil {
		return false
	}
	affected, err := result.RowsAffected()
	return err == nil && affected == 1
}

func COSRecordFromSnapshot(snapshot COSSnapshot) COSRecord {
	return COSRecord{
		ID: snapshot.PersistentID, OwnerCharID: snapshot.OwnerCharID, RefObjectID: snapshot.RefObjectID,
		Kind: snapshot.Kind, State: COSItemSummoned, InventorySlot: snapshot.InventorySlot, Name: snapshot.Name,
		Level: snapshot.Level, Experience: snapshot.Experience, Health: snapshot.Health,
		MaxHealth: snapshot.MaxHealth, HGP: snapshot.HGP, Satiety: snapshot.Satiety, Settings: snapshot.Settings,
	}
}
