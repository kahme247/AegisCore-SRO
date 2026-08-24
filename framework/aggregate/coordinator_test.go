package aggregate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

const coordinatorTestDriverName = "aegis-aggregate-coordinator-test"

var (
	coordinatorDriverOnce sync.Once
	coordinatorStoresMu   sync.Mutex
	coordinatorStores     = map[string]*coordinatorTestStore{}
	coordinatorStoreID    atomic.Uint64
)

type coordinatorStep struct {
	kind     string
	contains string
	result   driver.Result
	rows     [][]driver.Value
	err      error
}

type coordinatorTestStore struct {
	mu        sync.Mutex
	steps     []coordinatorStep
	commits   int
	rollbacks int
}

func TestSQLCoordinatorCommitsVersionResultAndOutboxAtomically(t *testing.T) {
	store, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		execStep("INSERT IGNORE", 1),
		queryStep("SELECT `VERSION`", []driver.Value{uint64(3)}),
		execStep("INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY`", 1),
		execStep("UPDATE `AEGIS_AGGREGATE_VERSION`", 1),
		execStep("INSERT INTO `AEGIS_OUTBOX_MESSAGE`", 1),
		execStep("UPDATE `AEGIS_COMMAND_IDEMPOTENCY`", 1),
	})
	coordinator, err := NewSQLCoordinator(database)
	if err != nil {
		t.Fatal(err)
	}
	coordinator.now = func() time.Time { return time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC) }
	command := testCommand(3, "command-success")
	result, err := coordinator.Execute(context.Background(), command, func(_ context.Context, transaction Transaction) (json.RawMessage, error) {
		if transaction.CurrentVersion() != 3 || transaction.DB() == nil {
			t.Fatalf("transaction version=%d db=%v", transaction.CurrentVersion(), transaction.DB())
		}
		err := transaction.Enqueue(OutboxMessage{
			ID: "message-1", Aggregate: command.Aggregate, Topic: "character.saved",
			Payload: []byte(`{"saved":true}`), OccurredAt: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		})
		return []byte(`{"ok":true}`), err
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Version != 4 || result.Duplicate || string(result.Payload) != `{"ok":true}` {
		t.Fatalf("result = %+v", result)
	}
	store.assertFinished(t, 1, 0)
}

func TestSQLCoordinatorReturnsCompletedDuplicateWithoutMutation(t *testing.T) {
	store, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		execStep("INSERT IGNORE", 0),
		queryStep("SELECT `VERSION`", []driver.Value{uint64(9)}),
		{kind: "exec", contains: "INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY`", err: &mysql.MySQLError{Number: 1062, Message: "duplicate"}},
		queryStep("SELECT `COMMAND_NAME`", []driver.Value{"character.save", "character", "42", uint64(3), "completed", uint64(4), []byte(`{"ok":true}`)}),
	})
	coordinator, _ := NewSQLCoordinator(database)
	called := false
	result, err := coordinator.Execute(context.Background(), testCommand(3, "command-duplicate"), func(context.Context, Transaction) (json.RawMessage, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if called || !result.Duplicate || result.Version != 4 || string(result.Payload) != `{"ok":true}` {
		t.Fatalf("duplicate result=%+v mutationCalled=%t", result, called)
	}
	store.assertFinished(t, 0, 1)
}

func TestSQLCoordinatorExecuteLatestReplaysAfterVersionAdvanced(t *testing.T) {
	store, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		execStep("INSERT IGNORE", 0),
		queryStep("SELECT `VERSION`", []driver.Value{uint64(10)}),
		{kind: "exec", contains: "INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY`", err: &mysql.MySQLError{Number: 1062, Message: "duplicate"}},
		queryStep("SELECT `COMMAND_NAME`", []driver.Value{"character.save", "character", "42", uint64(9), "completed", uint64(10), []byte(`{"ok":true}`)}),
	})
	coordinator, _ := NewSQLCoordinator(database)
	called := false
	result, err := coordinator.ExecuteLatest(context.Background(), testCommand(0, "command-latest-duplicate"), func(context.Context, Transaction) (json.RawMessage, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("ExecuteLatest() error = %v", err)
	}
	if called || !result.Duplicate || result.Version != 10 {
		t.Fatalf("latest duplicate result=%+v mutationCalled=%t", result, called)
	}
	store.assertFinished(t, 0, 1)
}

func TestSQLCoordinatorRollsBackStaleVersionBeforeMutation(t *testing.T) {
	store, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		execStep("INSERT IGNORE", 0),
		queryStep("SELECT `VERSION`", []driver.Value{uint64(4)}),
		execStep("INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY`", 1),
	})
	coordinator, _ := NewSQLCoordinator(database)
	called := false
	_, err := coordinator.Execute(context.Background(), testCommand(3, "command-stale"), func(context.Context, Transaction) (json.RawMessage, error) {
		called = true
		return []byte(`{}`), nil
	})
	if !errors.Is(err, ErrStaleVersion) || called {
		t.Fatalf("stale error=%v mutationCalled=%t", err, called)
	}
	store.assertFinished(t, 0, 1)
}

func TestSQLCoordinatorRollsBackMutationFailure(t *testing.T) {
	store, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		execStep("INSERT IGNORE", 0),
		queryStep("SELECT `VERSION`", []driver.Value{uint64(3)}),
		execStep("INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY`", 1),
	})
	coordinator, _ := NewSQLCoordinator(database)
	want := errors.New("repository write failed")
	_, err := coordinator.Execute(context.Background(), testCommand(3, "command-failure"), func(context.Context, Transaction) (json.RawMessage, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("mutation error = %v, want %v", err, want)
	}
	store.assertFinished(t, 0, 1)
}

func testCommand(version Version, key string) Command {
	return Command{
		Aggregate: ID{Kind: Character, Value: "42"}, ExpectedVersion: version,
		IdempotencyKey: IdempotencyKey(key), Name: "character.save",
	}
}

func execStep(contains string, affected int64) coordinatorStep {
	return coordinatorStep{kind: "exec", contains: contains, result: driver.RowsAffected(affected)}
}

func queryStep(contains string, row []driver.Value) coordinatorStep {
	return coordinatorStep{kind: "query", contains: contains, rows: [][]driver.Value{row}}
}

func newCoordinatorTestDatabase(t *testing.T, steps []coordinatorStep) (*coordinatorTestStore, *sql.DB) {
	t.Helper()
	coordinatorDriverOnce.Do(func() { sql.Register(coordinatorTestDriverName, coordinatorTestDriver{}) })
	id := fmt.Sprintf("store-%d", coordinatorStoreID.Add(1))
	store := &coordinatorTestStore{steps: append([]coordinatorStep(nil), steps...)}
	coordinatorStoresMu.Lock()
	coordinatorStores[id] = store
	coordinatorStoresMu.Unlock()
	database, err := sql.Open(coordinatorTestDriverName, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
		coordinatorStoresMu.Lock()
		delete(coordinatorStores, id)
		coordinatorStoresMu.Unlock()
	})
	return store, database
}

func (store *coordinatorTestStore) next(kind, query string) (coordinatorStep, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.steps) == 0 {
		return coordinatorStep{}, fmt.Errorf("unexpected %s: %s", kind, query)
	}
	step := store.steps[0]
	store.steps = store.steps[1:]
	if step.kind != kind || !strings.Contains(query, step.contains) {
		return coordinatorStep{}, fmt.Errorf("%s %q, want %s containing %q", kind, query, step.kind, step.contains)
	}
	return step, nil
}

func (store *coordinatorTestStore) assertFinished(t *testing.T, commits, rollbacks int) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.steps) != 0 || store.commits != commits || store.rollbacks != rollbacks {
		t.Fatalf("steps=%d commits=%d rollbacks=%d, want steps=0 commits=%d rollbacks=%d", len(store.steps), store.commits, store.rollbacks, commits, rollbacks)
	}
}

type coordinatorTestDriver struct{}

func (coordinatorTestDriver) Open(name string) (driver.Conn, error) {
	coordinatorStoresMu.Lock()
	store := coordinatorStores[name]
	coordinatorStoresMu.Unlock()
	if store == nil {
		return nil, fmt.Errorf("unknown coordinator test store %q", name)
	}
	return &coordinatorTestConn{store: store}, nil
}

type coordinatorTestConn struct{ store *coordinatorTestStore }

func (*coordinatorTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (*coordinatorTestConn) Close() error { return nil }
func (connection *coordinatorTestConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}
func (connection *coordinatorTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &coordinatorTestTx{store: connection.store}, nil
}
func (connection *coordinatorTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	step, err := connection.store.next("exec", query)
	if err != nil || step.err != nil {
		if err != nil {
			return nil, err
		}
		return nil, step.err
	}
	return step.result, nil
}
func (connection *coordinatorTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	step, err := connection.store.next("query", query)
	if err != nil || step.err != nil {
		if err != nil {
			return nil, err
		}
		return nil, step.err
	}
	columns := make([]string, len(step.rows[0]))
	for index := range columns {
		columns[index] = fmt.Sprintf("column_%d", index)
	}
	return &coordinatorTestRows{columns: columns, rows: step.rows}, nil
}

type coordinatorTestTx struct{ store *coordinatorTestStore }

func (transaction *coordinatorTestTx) Commit() error {
	transaction.store.mu.Lock()
	transaction.store.commits++
	transaction.store.mu.Unlock()
	return nil
}
func (transaction *coordinatorTestTx) Rollback() error {
	transaction.store.mu.Lock()
	transaction.store.rollbacks++
	transaction.store.mu.Unlock()
	return nil
}

type coordinatorTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (rows *coordinatorTestRows) Columns() []string { return rows.columns }
func (rows *coordinatorTestRows) Close() error      { return nil }
func (rows *coordinatorTestRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.rows) {
		return io.EOF
	}
	copy(destination, rows.rows[rows.index])
	rows.index++
	return nil
}
