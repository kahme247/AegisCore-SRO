package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var (
	ErrGroundItemMaterializerUnavailable = errors.New("ground-item materializer is unavailable")
	ErrGroundItemMaterializationInvalid  = errors.New("ground-item materialization is invalid")
	errGroundItemNoInsertID              = errors.New("database returned no ground-item ID")
)

// GroundItemSpec is the persisted portion of a generated drop. It is
// intentionally independent from the recovered executable's private
// drop-description layout: a selector may fill it from reference data,
// captured evidence, or a later binary-derived adapter.
type GroundItemSpec struct {
	ReferenceItemID uint32
	// RentCodeName is the source reference token carried by direct-drop
	// records. It is preserved through materialization even though this
	// repository does not yet have the binary's rent-reference catalog needed
	// to resolve it into RentInfo fields.
	RentCodeName    string
	MagicGroupID    uint32
	Variance        uint64
	StackCount      uint16
	OptLevel        byte
	Data            uint32
	CreatorName     string
	DropType        byte
	DropRarity      byte
	MagicOptions    []MagicOption
	NavmeshPosition navmeshv2.RtNavmeshPosition
	Rent            RentInfo
}

// GroundItemMaterializer creates the ITEM row before the object is admitted
// to the live world. The returned item has a database ID and is safe for the
// GroundItemService ownership/visibility handoff.
type GroundItemMaterializer interface {
	Materialize(context.Context, GroundItemSpec) (*Item, error)
}

const insertGroundItem = "INSERT INTO `SRO_SHARD`.`ITEM` (`FK_REF_ITEM`,`VARIANCE`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)"
const insertGroundItemMagicOption = "INSERT INTO `SRO_SHARD`.`ITEM_MAGIC_OPTION` (`FK_ITEM`,`SLOT`,`TYPE`,`VALUE`) VALUES (?,?,?,?)"

// SQLGroundItemMaterializer is the live shard-backed ITEM-row creator. It
// deliberately does not select drops; selection and chance rolls belong to
// the producer that supplies GroundItemSpec.
type SQLGroundItemMaterializer struct {
	shard   frameworkdb.Execer
	beginTx func(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

var _ GroundItemMaterializer = (*SQLGroundItemMaterializer)(nil)

func NewGroundItemMaterializer(shard frameworkdb.Execer) (*SQLGroundItemMaterializer, error) {
	if shard == nil {
		return nil, fmt.Errorf("configure ground-item materializer: %w", ErrGroundItemMaterializerUnavailable)
	}
	materializer := &SQLGroundItemMaterializer{shard: shard}
	if database, ok := shard.(*sql.DB); ok {
		materializer.beginTx = database.BeginTx
	}
	return materializer, nil
}

func (r *SQLGroundItemMaterializer) Materialize(ctx context.Context, spec GroundItemSpec) (*Item, error) {
	if r == nil || r.shard == nil {
		return nil, ErrGroundItemMaterializerUnavailable
	}
	if spec.ReferenceItemID == 0 || spec.StackCount == 0 || len(spec.CreatorName) > 17 {
		return nil, ErrGroundItemMaterializationInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.beginTx != nil {
		tx, beginErr := r.beginTx(ctx, nil)
		if beginErr != nil {
			return nil, fmt.Errorf("begin ground-item materialization: %w", beginErr)
		}
		item, materializeErr := materializeGroundItem(ctx, tx, spec)
		if materializeErr != nil {
			_ = tx.Rollback()
			return nil, materializeErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit ground-item materialization: %w", commitErr)
		}
		return item, nil
	}
	return materializeGroundItem(ctx, r.shard, spec)
}

func materializeGroundItem(ctx context.Context, execer frameworkdb.Execer, spec GroundItemSpec) (*Item, error) {
	result, err := execer.ExecContext(ctx, insertGroundItem,
		spec.ReferenceItemID,
		spec.Variance,
		spec.StackCount,
		spec.OptLevel,
		spec.Data,
		spec.CreatorName,
		spec.Rent.Type,
		spec.Rent.CanDelete,
		spec.Rent.CanRecharge,
		spec.Rent.PeriodBeginTime,
		spec.Rent.PeriodEndTime,
		spec.Rent.MeterRateTime,
		spec.Rent.PackingTime,
	)
	if err != nil {
		return nil, fmt.Errorf("insert ground-item row: %w", err)
	}
	itemID, err := result.LastInsertId()
	if err != nil || itemID <= 0 {
		if err == nil {
			err = errGroundItemNoInsertID
		}
		return nil, fmt.Errorf("read ground-item ID: %w", err)
	}

	item := &Item{
		ID:           int(itemID),
		RentCodeName: spec.RentCodeName,
		MagicGroupID: spec.MagicGroupID,
		DropType:     spec.DropType,
		Variance:     spec.Variance,
		StackSize:    int(spec.StackCount),
		OptLevel:     spec.OptLevel,
		Data:         spec.Data,
		CreatorName:  spec.CreatorName,
		DropRarity:   spec.DropRarity,
		MagicOptions: append([]MagicOption(nil), spec.MagicOptions...),
		RentInfo:     spec.Rent,
		SRObject:     SRObject{RefObjectID: spec.ReferenceItemID, RtNavmeshPosition: spec.NavmeshPosition},
	}
	if ref, ok := RefItems[spec.ReferenceItemID]; ok {
		item.Name = ref.CodeName
		item.SRObject.TypeInfo = ref.TypeInfo
	}
	for index, option := range spec.MagicOptions {
		if _, err := execer.ExecContext(ctx, insertGroundItemMagicOption, itemID, index, option.Type, option.Value); err != nil {
			return nil, fmt.Errorf("insert ground-item magic option %d: %w", index, err)
		}
	}
	return item, nil
}
