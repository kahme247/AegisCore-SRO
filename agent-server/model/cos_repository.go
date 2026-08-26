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
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return ErrCOSNotFound
	}
	return nil
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

func COSRecordFromSnapshot(snapshot COSSnapshot) COSRecord {
	return COSRecord{
		ID: snapshot.PersistentID, OwnerCharID: snapshot.OwnerCharID, RefObjectID: snapshot.RefObjectID,
		Kind: snapshot.Kind, State: COSItemSummoned, InventorySlot: snapshot.InventorySlot, Name: snapshot.Name,
		Level: snapshot.Level, Experience: snapshot.Experience, Health: snapshot.Health,
		MaxHealth: snapshot.MaxHealth, HGP: snapshot.HGP, Satiety: snapshot.Satiety, Settings: snapshot.Settings,
	}
}
