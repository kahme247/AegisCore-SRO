package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

const gatewayCatalogTestDriverName = "go-sro-gateway-catalog-test"

var gatewayCatalogTestState struct {
	sync.Mutex

	noticeRows [][]driver.Value
	shardRows  [][]driver.Value
	queryErr   error
	nextErr    error
	queries    []string
}

func init() {
	sql.Register(gatewayCatalogTestDriverName, gatewayCatalogTestDriver{})
}

type gatewayCatalogTestDriver struct{}

func (gatewayCatalogTestDriver) Open(string) (driver.Conn, error) {
	return gatewayCatalogTestConn{}, nil
}

type gatewayCatalogTestConn struct{}

func (gatewayCatalogTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the gateway catalog test driver")
}

func (gatewayCatalogTestConn) Close() error              { return nil }
func (gatewayCatalogTestConn) Begin() (driver.Tx, error) { return gatewayCatalogTestTx{}, nil }

func (gatewayCatalogTestConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	gatewayCatalogTestState.Lock()
	defer gatewayCatalogTestState.Unlock()
	gatewayCatalogTestState.queries = append(gatewayCatalogTestState.queries, query)
	if gatewayCatalogTestState.queryErr != nil {
		return nil, gatewayCatalogTestState.queryErr
	}

	var source [][]driver.Value
	var columns []string
	switch query {
	case selectNotice:
		source = gatewayCatalogTestState.noticeRows
		columns = []string{"id", "SUBJECT", "ARTICLE", "CTIME"}
	case selectShards:
		source = gatewayCatalogTestState.shardRows
		columns = []string{"ID", "CONTENT_ID", "NAME", "CAPACITY", "ONLINE_PLAYERS", "STATUS"}
	default:
		return nil, errors.New("unexpected query")
	}
	rows := make([][]driver.Value, len(source))
	for index, row := range source {
		rows[index] = append([]driver.Value(nil), row...)
	}
	return &gatewayCatalogTestRows{
		columns: columns,
		rows:    rows,
		nextErr: gatewayCatalogTestState.nextErr,
	}, nil
}

type gatewayCatalogTestTx struct{}

func (gatewayCatalogTestTx) Commit() error   { return nil }
func (gatewayCatalogTestTx) Rollback() error { return nil }

type gatewayCatalogTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
	nextErr error
}

func (r *gatewayCatalogTestRows) Columns() []string { return r.columns }
func (r *gatewayCatalogTestRows) Close() error      { return nil }

func (r *gatewayCatalogTestRows) Next(dest []driver.Value) error {
	if r.index < len(r.rows) {
		copy(dest, r.rows[r.index])
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

func resetGatewayCatalogTestState() {
	gatewayCatalogTestState.Lock()
	gatewayCatalogTestState.noticeRows = nil
	gatewayCatalogTestState.shardRows = nil
	gatewayCatalogTestState.queryErr = nil
	gatewayCatalogTestState.nextErr = nil
	gatewayCatalogTestState.queries = nil
	gatewayCatalogTestState.Unlock()
}

func openGatewayCatalogTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(gatewayCatalogTestDriverName, "gateway-catalog")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestNoticeRepositoryList(t *testing.T) {
	resetGatewayCatalogTestState()
	created := time.Date(2026, time.July, 13, 20, 0, 0, 0, time.UTC)
	gatewayCatalogTestState.Lock()
	gatewayCatalogTestState.noticeRows = [][]driver.Value{{int64(7), "Maintenance", "Tonight", created}}
	gatewayCatalogTestState.Unlock()

	repository, err := NewNoticeRepository(openGatewayCatalogTestDB(t))
	if err != nil {
		t.Fatalf("NewNoticeRepository: %v", err)
	}
	notices, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := notices, []Notice{{Id: 7, Subject: "Maintenance", Article: "Tonight", Ctime: created}}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("notices = %#v, want %#v", got, want)
	}
	assertGatewayCatalogQuery(t, selectNotice)
}

func TestShardRepositoryList(t *testing.T) {
	resetGatewayCatalogTestState()
	gatewayCatalogTestState.Lock()
	gatewayCatalogTestState.shardRows = [][]driver.Value{{int64(1), int64(122), "GoSRO", int64(1000), int64(42), int64(1)}}
	gatewayCatalogTestState.Unlock()

	repository, err := NewShardRepository(openGatewayCatalogTestDB(t))
	if err != nil {
		t.Fatalf("NewShardRepository: %v", err)
	}
	shards, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := shards, []Shard{{Id: 1, ContentId: 122, Name: "GoSRO", Capacity: 1000, OnlinePlayers: 42, Status: 1}}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("shards = %#v, want %#v", got, want)
	}
	assertGatewayCatalogQuery(t, selectShards)
}

func TestCatalogRepositoriesReturnTypedFailures(t *testing.T) {
	t.Run("notice query", func(t *testing.T) {
		resetGatewayCatalogTestState()
		want := errors.New("notice database unavailable")
		gatewayCatalogTestState.Lock()
		gatewayCatalogTestState.queryErr = want
		gatewayCatalogTestState.Unlock()
		repository, err := NewNoticeRepository(openGatewayCatalogTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.List(context.Background())
		assertNoticeRepositoryError(t, err, "query", want)
	})

	t.Run("shard rows", func(t *testing.T) {
		resetGatewayCatalogTestState()
		want := errors.New("connection lost while iterating")
		gatewayCatalogTestState.Lock()
		gatewayCatalogTestState.nextErr = want
		gatewayCatalogTestState.Unlock()
		repository, err := NewShardRepository(openGatewayCatalogTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.List(context.Background())
		assertShardRepositoryError(t, err, "iterate", want)
	})
}

func TestCatalogRepositoriesRejectNilQueryer(t *testing.T) {
	if repository, err := NewNoticeRepository(nil); repository != nil {
		t.Fatal("NewNoticeRepository returned a repository for nil queryer")
	} else {
		assertNoticeRepositoryError(t, err, "configure", ErrNoticeRepositoryUnavailable)
	}
	if repository, err := NewShardRepository(nil); repository != nil {
		t.Fatal("NewShardRepository returned a repository for nil queryer")
	} else {
		assertShardRepositoryError(t, err, "configure", ErrShardRepositoryUnavailable)
	}
}

func assertGatewayCatalogQuery(t *testing.T, want string) {
	t.Helper()
	gatewayCatalogTestState.Lock()
	queries := append([]string(nil), gatewayCatalogTestState.queries...)
	gatewayCatalogTestState.Unlock()
	if len(queries) != 1 || queries[0] != want {
		t.Fatalf("queries = %#v, want [%q]", queries, want)
	}
}

func assertNoticeRepositoryError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	var repositoryError *NoticeRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != operation {
		t.Fatalf("error = %v, want notice repository %s failure", err, operation)
	}
}

func assertShardRepositoryError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	var repositoryError *ShardRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != operation {
		t.Fatalf("error = %v, want shard repository %s failure", err, operation)
	}
}
