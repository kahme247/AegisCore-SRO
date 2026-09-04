package model

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type materializerTestExecer struct {
	query   string
	args    []any
	queries []string
}

func (e *materializerTestExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.query = query
	e.args = append([]any(nil), args...)
	e.queries = append(e.queries, query)
	return materializerTestResult{}, nil
}

type materializerTestResult struct{}

func (materializerTestResult) LastInsertId() (int64, error) { return 41, nil }
func (materializerTestResult) RowsAffected() (int64, error) { return 1, nil }

func TestSQLGroundItemMaterializerPersistsAllItemFields(t *testing.T) {
	execer := &materializerTestExecer{}
	materializer, err := NewGroundItemMaterializer(execer)
	if err != nil {
		t.Fatal(err)
	}
	old := RefItems
	RefItems = map[uint32]RefItem{900: {RefObject: RefObject{CodeName: "ITEM_ETC_TEST", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3}}}}
	t.Cleanup(func() { RefItems = old })

	item, err := materializer.Materialize(context.Background(), GroundItemSpec{
		ReferenceItemID: 900,
		DropType:        3,
		MagicGroupID:    71,
		Variance:        0x1122334455667788,
		StackCount:      321,
		OptLevel:        7,
		Data:            1234,
		Rent:            RentInfo{Type: 2, CanDelete: 3, CanRecharge: 4, PeriodBeginTime: 5, PeriodEndTime: 6, MeterRateTime: 7, PackingTime: 8},
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if item.ID != 41 || item.GetRefObjectID() != 900 || item.StackSize != 321 || item.OptLevel != 7 || item.Data != 1234 || item.DropType != 3 || item.MagicGroupID != 71 || item.Name != "ITEM_ETC_TEST" {
		t.Fatalf("materialized item = %+v", item)
	}
	if len(execer.args) != 12 || execer.args[0] != uint32(900) || execer.args[2] != uint16(321) || execer.args[11] != uint32(8) {
		t.Fatalf("insert args = %+v", execer.args)
	}
	if execer.query != insertGroundItem {
		t.Fatalf("query = %q, want %q", execer.query, insertGroundItem)
	}
}

func TestSQLGroundItemMaterializerRejectsInvalidSpec(t *testing.T) {
	materializer, err := NewGroundItemMaterializer(&materializerTestExecer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []GroundItemSpec{{}, {ReferenceItemID: 1}, {StackCount: 1}} {
		if _, err := materializer.Materialize(context.Background(), spec); !errors.Is(err, ErrGroundItemMaterializationInvalid) {
			t.Fatalf("spec %+v error = %v, want ErrGroundItemMaterializationInvalid", spec, err)
		}
	}
}

func TestSQLGroundItemMaterializerPersistsMagicOptionsWithTheItem(t *testing.T) {
	execer := &materializerTestExecer{}
	materializer, err := NewGroundItemMaterializer(execer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = materializer.Materialize(context.Background(), GroundItemSpec{
		ReferenceItemID: 900,
		StackCount:      1,
		MagicOptions:    []MagicOption{{Type: 12, Value: 34}, {Type: 56, Value: 78}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(execer.queries) != 3 || execer.queries[0] != insertGroundItem || execer.queries[1] != insertGroundItemMagicOption || execer.queries[2] != insertGroundItemMagicOption {
		t.Fatalf("queries = %v", execer.queries)
	}
}
