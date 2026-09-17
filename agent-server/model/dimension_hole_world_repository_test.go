package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
)

var dimensionHoleWorldColumns = []string{
	"WORLD_ID", "CODE_NAME", "WORLD_TYPE", "MAX_INSTANCE_COUNT", "MAX_USER_COUNT",
	"WORLD_ENTRY_TYPE", "WORLD_ENTRANCE_TYPE", "WORLD_LEAVE_TYPE", "WORLD_DURATION_MS", "WORLD_EMPTY_REMAIN_MS", "CONFIG_GROUP_CODE_NAME",
}

func TestDimensionHoleWorldRepositoryLoadsExactCompleteCatalog(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectDimensionHoleGameWorlds, catalogRepositoryTestResponse{
		columns: dimensionHoleWorldColumns,
		rows:    dimensionHoleWorldDriverRows(),
	})
	repository, err := NewDimensionHoleWorldRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := repository.LoadDimensionHoleWorldCatalog(context.Background())
	if err != nil {
		t.Fatalf("LoadDimensionHoleWorldCatalog() error = %v", err)
	}
	worlds := catalog.Worlds()
	if len(worlds) != len(DimensionHoleCatalog()) || worlds[0].WorldID != DimensionHoleFirstWorldID || worlds[len(worlds)-1].WorldID != DimensionHoleLastWorldID {
		t.Fatalf("worlds = %#v", worlds)
	}
	first, ok := catalog.World(DimensionHoleFirstWorldID)
	if !ok || first.CodeName != DimensionHoleCatalog()[0].WorldCodeName || first.MaxUserCount != DimensionHoleCatalog()[0].Capacity {
		t.Fatalf("first world = %#v, present=%t", first, ok)
	}
	worlds[0].CodeName = "mutated"
	if retained, _ := catalog.World(DimensionHoleFirstWorldID); retained.CodeName == "mutated" {
		t.Fatal("Worlds() exposed mutable catalog storage")
	}
	assertDimensionHoleWorldQuery(t)
}

func TestNewDimensionHoleWorldCatalogAppliesTheSamePreflightWithoutSQL(t *testing.T) {
	worlds := dimensionHoleWorldReferences()
	catalog, err := NewDimensionHoleWorldCatalog(worlds)
	if err != nil || len(catalog.Worlds()) != len(DimensionHoleCatalog()) {
		t.Fatalf("NewDimensionHoleWorldCatalog() = %#v, %v", catalog, err)
	}
	worlds[0].MaxUserCount++
	if catalog, err = NewDimensionHoleWorldCatalog(worlds); catalog != nil || !errors.Is(err, ErrDimensionHoleWorldCatalogMismatch) {
		t.Fatalf("NewDimensionHoleWorldCatalog(altered) = %#v, %v", catalog, err)
	}
	if catalog, err = NewDimensionHoleWorldCatalog(worlds[:len(worlds)-1]); catalog != nil || !errors.Is(err, ErrDimensionHoleWorldCatalogMismatch) {
		t.Fatalf("NewDimensionHoleWorldCatalog(incomplete) = %#v, %v", catalog, err)
	}
}

func TestDimensionHoleWorldRepositoryRejectsIncompleteOutOfOrderOrAlteredCatalog(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([][]driver.Value) [][]driver.Value
		world  uint16
	}{
		{name: "missing", world: DimensionHoleLastWorldID, mutate: func(rows [][]driver.Value) [][]driver.Value {
			return rows[:len(rows)-1]
		}},
		{name: "out of order", world: DimensionHoleFirstWorldID, mutate: func(rows [][]driver.Value) [][]driver.Value {
			rows[0], rows[1] = rows[1], rows[0]
			return rows
		}},
		{name: "code name", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(1, "INS_WRONG")},
		{name: "world type", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(2, int64(2))},
		{name: "instance count", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(3, int64(49))},
		{name: "capacity", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(4, int64(99))},
		{name: "entry type", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(5, int64(1))},
		{name: "entrance type", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(6, int64(1))},
		{name: "leave type", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(7, int64(1))},
		{name: "duration", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(8, int64(1))},
		{name: "empty remain", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(9, int64(1))},
		{name: "config group", world: DimensionHoleFirstWorldID, mutate: mutateDimensionHoleWorldField(10, "GROUP_WRONG")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetCatalogRepositoryTestState()
			setCatalogRepositoryTestResponse(SelectDimensionHoleGameWorlds, catalogRepositoryTestResponse{
				columns: dimensionHoleWorldColumns,
				rows:    test.mutate(dimensionHoleWorldDriverRows()),
			})
			repository, _ := NewDimensionHoleWorldRepository(openCatalogRepositoryTestDB(t))
			catalog, err := repository.LoadDimensionHoleWorldCatalog(context.Background())
			if catalog != nil || !errors.Is(err, ErrDimensionHoleWorldCatalogMismatch) {
				t.Fatalf("LoadDimensionHoleWorldCatalog() = %#v, %v", catalog, err)
			}
			var repositoryError *DimensionHoleWorldRepositoryError
			if !errors.As(err, &repositoryError) || repositoryError.Operation != "validate" || repositoryError.WorldID != test.world {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestDimensionHoleWorldRepositoryReturnsTypedStorageFailures(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		cause     error
	}{
		{name: "query", operation: "query", cause: errors.New("catalog unavailable")},
		{name: "scan", operation: "scan"},
		{name: "iterate", operation: "iterate", cause: errors.New("connection lost")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetCatalogRepositoryTestState()
			response := catalogRepositoryTestResponse{}
			switch test.name {
			case "query":
				response.queryErr = test.cause
			case "scan":
				response.columns = dimensionHoleWorldColumns
				response.rows = [][]driver.Value{{"not-a-world-id", "INS", int64(1), int64(50), int64(4), int64(0), int64(0), int64(0), int64(7_200_000), int64(600_000), "GROUP"}}
			case "iterate":
				response.columns = dimensionHoleWorldColumns
				response.nextErr = test.cause
			}
			setCatalogRepositoryTestResponse(SelectDimensionHoleGameWorlds, response)
			repository, _ := NewDimensionHoleWorldRepository(openCatalogRepositoryTestDB(t))
			_, err := repository.LoadDimensionHoleWorldCatalog(context.Background())
			var repositoryError *DimensionHoleWorldRepositoryError
			if !errors.As(err, &repositoryError) || repositoryError.Operation != test.operation || (test.cause != nil && !errors.Is(err, test.cause)) {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestDimensionHoleWorldRepositoryRejectsNilQueryerAndCanceledContext(t *testing.T) {
	repository, err := NewDimensionHoleWorldRepository(nil)
	if repository != nil || !errors.Is(err, ErrDimensionHoleWorldRepositoryUnavailable) {
		t.Fatalf("NewDimensionHoleWorldRepository(nil) = %#v, %v", repository, err)
	}

	resetCatalogRepositoryTestState()
	repository, _ = NewDimensionHoleWorldRepository(openCatalogRepositoryTestDB(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = repository.LoadDimensionHoleWorldCatalog(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadDimensionHoleWorldCatalog(canceled) error = %v", err)
	}
	catalogRepositoryTestState.Lock()
	queryCount := len(catalogRepositoryTestState.queries)
	catalogRepositoryTestState.Unlock()
	if queryCount != 0 {
		t.Fatalf("canceled preflight issued %d queries", queryCount)
	}
}

func dimensionHoleWorldDriverRows() [][]driver.Value {
	worlds := dimensionHoleWorldReferences()
	rows := make([][]driver.Value, 0, len(worlds))
	for _, world := range worlds {
		rows = append(rows, []driver.Value{
			int64(world.WorldID), world.CodeName, int64(world.WorldType),
			int64(world.MaxInstanceCount), int64(world.MaxUserCount), int64(world.WorldEntryType),
			int64(world.WorldEntranceType), int64(world.WorldLeaveType), int64(world.WorldDurationMS),
			int64(world.WorldEmptyRemainMS), world.ConfigGroupCodeName,
		})
	}
	return rows
}

func dimensionHoleWorldReferences() []DimensionHoleWorldReference {
	definitions := DimensionHoleCatalog()
	worlds := make([]DimensionHoleWorldReference, 0, len(definitions))
	for _, definition := range definitions {
		worlds = append(worlds, DimensionHoleWorldReference{
			WorldID: definition.WorldID, CodeName: definition.WorldCodeName, WorldType: DimensionHoleWorldType,
			MaxInstanceCount: DimensionHoleWorldMaxInstanceCount, MaxUserCount: definition.Capacity,
			WorldEntryType: DimensionHoleWorldEntryType, WorldEntranceType: DimensionHoleWorldEntranceType,
			WorldLeaveType: DimensionHoleWorldLeaveType, WorldDurationMS: DimensionHoleWorldDurationMS,
			WorldEmptyRemainMS:  DimensionHoleWorldEmptyRemainMS,
			ConfigGroupCodeName: "GROUP_" + strings.TrimPrefix(definition.WorldCodeName, "INS_"),
		})
	}
	return worlds
}

func mutateDimensionHoleWorldField(index int, value driver.Value) func([][]driver.Value) [][]driver.Value {
	return func(rows [][]driver.Value) [][]driver.Value {
		rows[0][index] = value
		return rows
	}
}

func assertDimensionHoleWorldQuery(t *testing.T) {
	t.Helper()
	catalogRepositoryTestState.Lock()
	queries := append([]catalogRepositoryTestQuery(nil), catalogRepositoryTestState.queries...)
	catalogRepositoryTestState.Unlock()
	wantArgs := []driver.NamedValue{
		{Ordinal: 1, Value: int64(DimensionHoleFirstWorldID)},
		{Ordinal: 2, Value: int64(DimensionHoleLastWorldID)},
	}
	if len(queries) != 1 || queries[0].query != SelectDimensionHoleGameWorlds || !reflect.DeepEqual(queries[0].args, wantArgs) {
		t.Fatalf("queries = %#v", queries)
	}
}
