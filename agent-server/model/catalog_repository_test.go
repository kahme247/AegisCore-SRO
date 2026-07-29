package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
)

const catalogRepositoryTestDriverName = "go-sro-agent-catalog-repository-test"

var catalogRepositoryTestState struct {
	sync.Mutex

	responses map[string]catalogRepositoryTestResponse
	queries   []catalogRepositoryTestQuery
}

type catalogRepositoryTestResponse struct {
	columns  []string
	rows     [][]driver.Value
	queryErr error
	nextErr  error
}

type catalogRepositoryTestQuery struct {
	query string
	args  []driver.NamedValue
}

func init() {
	sql.Register(catalogRepositoryTestDriverName, catalogRepositoryTestDriver{})
}

type catalogRepositoryTestDriver struct{}

func (catalogRepositoryTestDriver) Open(string) (driver.Conn, error) {
	return catalogRepositoryTestConn{}, nil
}

type catalogRepositoryTestConn struct{}

func (catalogRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the catalog repository test driver")
}

func (catalogRepositoryTestConn) Close() error              { return nil }
func (catalogRepositoryTestConn) Begin() (driver.Tx, error) { return catalogRepositoryTestTx{}, nil }

func (catalogRepositoryTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	catalogRepositoryTestState.Lock()
	catalogRepositoryTestState.queries = append(catalogRepositoryTestState.queries, catalogRepositoryTestQuery{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	response, ok := catalogRepositoryTestState.responses[query]
	catalogRepositoryTestState.Unlock()
	if !ok {
		return nil, errors.New("unexpected catalog repository query")
	}
	if response.queryErr != nil {
		return nil, response.queryErr
	}

	rows := make([][]driver.Value, len(response.rows))
	for index, row := range response.rows {
		rows[index] = append([]driver.Value(nil), row...)
	}
	return &catalogRepositoryTestRows{
		columns: append([]string(nil), response.columns...),
		rows:    rows,
		nextErr: response.nextErr,
	}, nil
}

type catalogRepositoryTestTx struct{}

func (catalogRepositoryTestTx) Commit() error   { return nil }
func (catalogRepositoryTestTx) Rollback() error { return nil }

type catalogRepositoryTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
	nextErr error
}

func (r *catalogRepositoryTestRows) Columns() []string { return r.columns }
func (r *catalogRepositoryTestRows) Close() error      { return nil }

func (r *catalogRepositoryTestRows) Next(destination []driver.Value) error {
	if r.index < len(r.rows) {
		copy(destination, r.rows[r.index])
		r.index++
		return nil
	}
	if r.nextErr != nil {
		err := r.nextErr
		r.nextErr = nil
		return err
	}
	return io.EOF
}

func resetCatalogRepositoryTestState() {
	catalogRepositoryTestState.Lock()
	defer catalogRepositoryTestState.Unlock()
	catalogRepositoryTestState.responses = make(map[string]catalogRepositoryTestResponse)
	catalogRepositoryTestState.queries = nil
}

func setCatalogRepositoryTestResponse(query string, response catalogRepositoryTestResponse) {
	catalogRepositoryTestState.Lock()
	catalogRepositoryTestState.responses[query] = response
	catalogRepositoryTestState.Unlock()
}

func openCatalogRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(catalogRepositoryTestDriverName, "catalog-repository")
	if err != nil {
		t.Fatalf("open catalog repository test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestGameServerRegionRepositoryListsDeterministically(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectGameserverRegions, catalogRepositoryTestResponse{
		columns: []string{"Continent_Name", "wRegionID"},
		rows: [][]driver.Value{
			{"Europe", int64(101)},
			{"Asia", int64(3)},
			{"Europe", int64(99)},
			{"Asia", int64(1)},
		},
	})

	repository, err := NewGameServerRegionRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewGameServerRegionRepository: %v", err)
	}

	regions, err := repository.GetRegionsForGameServer(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetRegionsForGameServer: %v", err)
	}
	want := []GameServerRegion{
		{ContinentName: "Asia", Regions: []int16{1, 3}},
		{ContinentName: "Europe", Regions: []int16{99, 101}},
	}
	if !reflect.DeepEqual(regions, want) {
		t.Fatalf("regions = %#v, want %#v", regions, want)
	}
	assertCatalogRepositoryQuery(t, SelectGameserverRegions, int64(7))
}

func TestContinentSpawnRepositoryListsDeterministically(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectSpawnsForContinent, catalogRepositoryTestResponse{
		columns: []string{"dwNestID", "wRegionID", "fLocalPosX", "fLocalPosY", "fLocalPosZ", "wInitialDir", "ObjID", "CodeName", "nRadius", "nGenerateRadius", "dwMaxTotalCount", "dwDelayTimeMin", "dwDelayTimeMax", "SightRange", "AggressType"},
		rows: [][]driver.Value{
			{int64(2), int64(3), float64(20), float64(21), float64(22), int64(90), int64(1002), "NPC_B", int64(5), int64(6), int64(7), int64(8), int64(9), int64(80), int64(0)},
			{int64(1), int64(4), float64(10), float64(11), float64(12), int64(45), int64(1001), "NPC_A", int64(1), int64(2), int64(3), int64(4), int64(5), int64(60), int64(1)},
			{int64(2), int64(1), float64(30), float64(31), float64(32), int64(180), int64(1003), "NPC_C", int64(9), int64(10), int64(11), int64(12), int64(13), int64(90), int64(0)},
		},
	})

	repository, err := NewContinentSpawnRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewContinentSpawnRepository: %v", err)
	}

	spawns, err := repository.GetSpawnsForContinent(context.Background(), "Europe")
	if err != nil {
		t.Fatalf("GetSpawnsForContinent: %v", err)
	}
	want := []SpawnDB{
		{NestID: 1, RegionID: 4, X: 10, Y: 11, Z: 12, Heading: 45, RefObjID: 1001, NpcCodeName: "NPC_A", Radius: 1, GenerateRadius: 2, MaxTotalCount: 3, DelayTimeMin: 4, DelayTimeMax: 5, SightRange: 60, AggressType: 1},
		{NestID: 2, RegionID: 1, X: 30, Y: 31, Z: 32, Heading: 180, RefObjID: 1003, NpcCodeName: "NPC_C", Radius: 9, GenerateRadius: 10, MaxTotalCount: 11, DelayTimeMin: 12, DelayTimeMax: 13, SightRange: 90, AggressType: 0},
		{NestID: 2, RegionID: 3, X: 20, Y: 21, Z: 22, Heading: 90, RefObjID: 1002, NpcCodeName: "NPC_B", Radius: 5, GenerateRadius: 6, MaxTotalCount: 7, DelayTimeMin: 8, DelayTimeMax: 9, SightRange: 80, AggressType: 0},
	}
	if !reflect.DeepEqual(spawns, want) {
		t.Fatalf("spawns = %#v, want %#v", spawns, want)
	}
	assertCatalogRepositoryQuery(t, SelectSpawnsForContinent, "Europe")
}

func TestCatalogRepositoriesReturnTypedQueryScanAndRowsFailures(t *testing.T) {
	t.Run("game-server regions", func(t *testing.T) {
		t.Run("query", func(t *testing.T) {
			resetCatalogRepositoryTestState()
			want := errors.New("region catalog unavailable")
			setCatalogRepositoryTestResponse(SelectGameserverRegions, catalogRepositoryTestResponse{queryErr: want})
			repository, err := NewGameServerRegionRepository(openCatalogRepositoryTestDB(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = repository.GetRegionsForGameServer(context.Background(), 1)
			assertGameServerRegionRepositoryError(t, err, "query", want)
		})

		t.Run("scan", func(t *testing.T) {
			resetCatalogRepositoryTestState()
			setCatalogRepositoryTestResponse(SelectGameserverRegions, catalogRepositoryTestResponse{
				columns: []string{"Continent_Name", "wRegionID"},
				rows:    [][]driver.Value{{"Europe", "not-a-region-id"}},
			})
			repository, err := NewGameServerRegionRepository(openCatalogRepositoryTestDB(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = repository.GetRegionsForGameServer(context.Background(), 1)
			assertGameServerRegionRepositoryOperation(t, err, "scan")
		})

		t.Run("rows", func(t *testing.T) {
			resetCatalogRepositoryTestState()
			want := errors.New("region connection lost while iterating")
			setCatalogRepositoryTestResponse(SelectGameserverRegions, catalogRepositoryTestResponse{
				columns: []string{"Continent_Name", "wRegionID"},
				nextErr: want,
			})
			repository, err := NewGameServerRegionRepository(openCatalogRepositoryTestDB(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = repository.GetRegionsForGameServer(context.Background(), 1)
			assertGameServerRegionRepositoryError(t, err, "iterate", want)
		})
	})

	t.Run("continent spawns", func(t *testing.T) {
		t.Run("query", func(t *testing.T) {
			resetCatalogRepositoryTestState()
			want := errors.New("spawn catalog unavailable")
			setCatalogRepositoryTestResponse(SelectSpawnsForContinent, catalogRepositoryTestResponse{queryErr: want})
			repository, err := NewContinentSpawnRepository(openCatalogRepositoryTestDB(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = repository.GetSpawnsForContinent(context.Background(), "Europe")
			assertContinentSpawnRepositoryError(t, err, "query", want)
		})

		t.Run("scan", func(t *testing.T) {
			resetCatalogRepositoryTestState()
			setCatalogRepositoryTestResponse(SelectSpawnsForContinent, catalogRepositoryTestResponse{
				columns: []string{"dwNestID", "wRegionID", "fLocalPosX", "fLocalPosY", "fLocalPosZ", "wInitialDir", "ObjID", "CodeName", "nRadius", "nGenerateRadius", "dwMaxTotalCount", "dwDelayTimeMin", "dwDelayTimeMax", "SightRange", "AggressType"},
				rows:    [][]driver.Value{{"not-a-nest-id", int64(1), float64(0), float64(0), float64(0), int64(0), int64(1), "NPC", int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0)}},
			})
			repository, err := NewContinentSpawnRepository(openCatalogRepositoryTestDB(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = repository.GetSpawnsForContinent(context.Background(), "Europe")
			assertContinentSpawnRepositoryOperation(t, err, "scan")
		})

		t.Run("rows", func(t *testing.T) {
			resetCatalogRepositoryTestState()
			want := errors.New("spawn connection lost while iterating")
			setCatalogRepositoryTestResponse(SelectSpawnsForContinent, catalogRepositoryTestResponse{
				columns: []string{"dwNestID", "wRegionID", "fLocalPosX", "fLocalPosY", "fLocalPosZ", "wInitialDir", "ObjID", "CodeName", "nRadius", "nGenerateRadius", "dwMaxTotalCount", "dwDelayTimeMin", "dwDelayTimeMax", "SightRange", "AggressType"},
				nextErr: want,
			})
			repository, err := NewContinentSpawnRepository(openCatalogRepositoryTestDB(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = repository.GetSpawnsForContinent(context.Background(), "Europe")
			assertContinentSpawnRepositoryError(t, err, "iterate", want)
		})
	})
}

func TestCatalogRepositoriesRejectNilQueryers(t *testing.T) {
	if repository, err := NewGameServerRegionRepository(nil); repository != nil {
		t.Fatal("NewGameServerRegionRepository returned a repository for a nil queryer")
	} else {
		assertGameServerRegionRepositoryError(t, err, "configure", ErrGameServerRegionRepositoryUnavailable)
	}
	if repository, err := NewContinentSpawnRepository(nil); repository != nil {
		t.Fatal("NewContinentSpawnRepository returned a repository for a nil queryer")
	} else {
		assertContinentSpawnRepositoryError(t, err, "configure", ErrContinentSpawnRepositoryUnavailable)
	}
}

func assertCatalogRepositoryQuery(t *testing.T, wantQuery string, wantArgument any) {
	t.Helper()
	catalogRepositoryTestState.Lock()
	queries := append([]catalogRepositoryTestQuery(nil), catalogRepositoryTestState.queries...)
	catalogRepositoryTestState.Unlock()
	if len(queries) != 1 || queries[0].query != wantQuery || len(queries[0].args) != 1 || queries[0].args[0].Value != wantArgument {
		t.Fatalf("queries = %#v, want %q with %#v", queries, wantQuery, wantArgument)
	}
}

func assertGameServerRegionRepositoryError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	assertGameServerRegionRepositoryOperation(t, err, operation)
}

func assertGameServerRegionRepositoryOperation(t *testing.T, err error, operation string) {
	t.Helper()
	var repositoryError *GameServerRegionRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != operation {
		t.Fatalf("error = %v, want game-server region repository %s failure", err, operation)
	}
}

func assertContinentSpawnRepositoryError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	assertContinentSpawnRepositoryOperation(t, err, operation)
}

func assertContinentSpawnRepositoryOperation(t *testing.T, err error, operation string) {
	t.Helper()
	var repositoryError *ContinentSpawnRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != operation {
		t.Fatalf("error = %v, want continent spawn repository %s failure", err, operation)
	}
}
