package scheduler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const schedulerTestDriverName = "aegis-scheduler-test"

var (
	schedulerDriverOnce sync.Once
	schedulerStoresMu   sync.Mutex
	schedulerStores     = map[string]*schedulerTestState{}
	schedulerStoreID    atomic.Uint64
)

type schedulerStep struct {
	kind, contains string
	rows           [][]driver.Value
	result         driver.Result
	err            error
}

type schedulerTestState struct {
	mu                 sync.Mutex
	steps              []schedulerStep
	commits, rollbacks int
}

func TestSQLStoreCreatesDefinitionWithComputedInitialRun(t *testing.T) {
	state, database := newSchedulerDatabase(t, []schedulerStep{schedulerExec("INSERT INTO `AEGIS_SCHEDULE_DEFINITION`", 1)})
	store, _ := NewSQLStore(database)
	from := mustDate(t, 2026, time.July, 19)
	until := mustDate(t, 2026, time.July, 20)
	definition := recurringDefinition(Daily, from, until)
	definition.Timezone = "UTC"
	definition.SecondOfDay = 3600
	created, err := store.Create(context.Background(), definition)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 1 || !created.NextRunAt.Equal(time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("created definition = %+v", created)
	}
	state.assert(t, 0, 0)
}

func TestSQLStoreMaterializesMissedRunOnceAndAdvancesPastNow(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	from := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC)
	definitionRow := []driver.Value{
		"schedule-1", "Schedule", "event.start", []byte(`{}`), "daily", "start", "shard", int64(1), "UTC",
		from, until, nil, int64(3600), int64(1), int64(0), "run_once", true, uint64(1), due,
	}
	state, database := newSchedulerDatabase(t, []schedulerStep{
		schedulerQuery("FOR UPDATE SKIP LOCKED", definitionRow),
		schedulerExec("INSERT INTO `AEGIS_SCHEDULE_RUN`", 1),
		schedulerExec("UPDATE `AEGIS_SCHEDULE_DEFINITION`", 1),
	})
	store, _ := NewSQLStore(database)
	result, err := store.MaterializeDue(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("MaterializeDue() error = %v", err)
	}
	if result != (MaterializeResult{Pending: 1}) {
		t.Fatalf("materialize result = %+v", result)
	}
	state.assert(t, 1, 0)
}

func TestSQLStoreMaterializesSkipMisfireAsTerminalSkippedRun(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	definitionRow := []driver.Value{
		"schedule-1", "Schedule", "event.start", []byte(`{}`), "one_time", "start", "all_shards", int64(0), "UTC",
		nil, nil, time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC), int64(0), int64(0), int64(0), "skip", true, uint64(1), time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC),
	}
	state, database := newSchedulerDatabase(t, []schedulerStep{
		schedulerQuery("FOR UPDATE SKIP LOCKED", definitionRow),
		schedulerExec("INSERT INTO `AEGIS_SCHEDULE_RUN`", 1),
		schedulerExec("UPDATE `AEGIS_SCHEDULE_DEFINITION`", 1),
	})
	store, _ := NewSQLStore(database)
	result, err := store.MaterializeDue(context.Background(), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result != (MaterializeResult{Skipped: 1, Exhausted: 1}) {
		t.Fatalf("materialize result = %+v", result)
	}
	state.assert(t, 1, 0)
}

func TestSQLStoreClaimsRunsWithAttemptAndLease(t *testing.T) {
	now := time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC)
	state, database := newSchedulerDatabase(t, []schedulerStep{
		schedulerQuery("FROM `AEGIS_SCHEDULE_RUN`", []driver.Value{uint64(7), "schedule-1", uint64(2), now.Add(-time.Minute), "event.start", []byte(`{}`), "start", "all_shards", int64(0), uint64(2)}),
		schedulerExec("SET `STATUS`='processing'", 1),
	})
	store, _ := NewSQLStore(database)
	runs, err := store.ClaimRuns(context.Background(), "worker-1", now, 5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Attempt != 3 || runs[0].LeaseOwner != "worker-1" || !runs[0].LeaseUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("runs = %+v", runs)
	}
	state.assert(t, 1, 0)
}

func TestSQLStoreRejectsLostRunLease(t *testing.T) {
	state, database := newSchedulerDatabase(t, []schedulerStep{schedulerExec("SET `STATUS`='succeeded'", 0)})
	store, _ := NewSQLStore(database)
	run := Run{ID: 1, Attempt: 1, LeaseOwner: "worker-1", LeaseUntil: time.Now().Add(time.Minute)}
	if err := store.MarkSucceeded(context.Background(), run, time.Now()); !errors.Is(err, ErrRunLeaseLost) {
		t.Fatalf("MarkSucceeded() error = %v, want ErrRunLeaseLost", err)
	}
	state.assert(t, 0, 0)
}

func TestRetryPolicyDistinguishesPermanentAndExhaustedRuns(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 2 * time.Second}
	if status, delay := policy.Decide(1, errors.New("temporary")); status != "pending" || delay != time.Second {
		t.Fatalf("temporary decision = %s %s", status, delay)
	}
	if status, _ := policy.Decide(1, schedulerPermanentError{}); status != "failed" {
		t.Fatalf("permanent decision = %s", status)
	}
	if status, _ := policy.Decide(3, errors.New("temporary")); status != "dead" {
		t.Fatalf("exhausted decision = %s", status)
	}
}

type schedulerPermanentError struct{}

func (schedulerPermanentError) Error() string   { return "permanent" }
func (schedulerPermanentError) Permanent() bool { return true }

func schedulerExec(contains string, affected int64) schedulerStep {
	return schedulerStep{kind: "exec", contains: contains, result: driver.RowsAffected(affected)}
}

func schedulerQuery(contains string, row []driver.Value) schedulerStep {
	return schedulerStep{kind: "query", contains: contains, rows: [][]driver.Value{row}}
}

func newSchedulerDatabase(t *testing.T, steps []schedulerStep) (*schedulerTestState, *sql.DB) {
	t.Helper()
	schedulerDriverOnce.Do(func() { sql.Register(schedulerTestDriverName, schedulerTestDriver{}) })
	id := fmt.Sprintf("scheduler-%d", schedulerStoreID.Add(1))
	state := &schedulerTestState{steps: append([]schedulerStep(nil), steps...)}
	schedulerStoresMu.Lock()
	schedulerStores[id] = state
	schedulerStoresMu.Unlock()
	database, err := sql.Open(schedulerTestDriverName, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
		schedulerStoresMu.Lock()
		delete(schedulerStores, id)
		schedulerStoresMu.Unlock()
	})
	return state, database
}

func (state *schedulerTestState) next(kind, query string) (schedulerStep, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.steps) == 0 {
		return schedulerStep{}, fmt.Errorf("unexpected %s: %s", kind, query)
	}
	step := state.steps[0]
	state.steps = state.steps[1:]
	if step.kind != kind || !strings.Contains(query, step.contains) {
		return schedulerStep{}, fmt.Errorf("%s %q, want %s containing %q", kind, query, step.kind, step.contains)
	}
	return step, nil
}

func (state *schedulerTestState) assert(t *testing.T, commits, rollbacks int) {
	t.Helper()
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.steps) != 0 || state.commits != commits || state.rollbacks != rollbacks {
		t.Fatalf("steps=%d commits=%d rollbacks=%d, want 0 %d %d", len(state.steps), state.commits, state.rollbacks, commits, rollbacks)
	}
}

type schedulerTestDriver struct{}

func (schedulerTestDriver) Open(name string) (driver.Conn, error) {
	schedulerStoresMu.Lock()
	state := schedulerStores[name]
	schedulerStoresMu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("unknown scheduler state %q", name)
	}
	return &schedulerTestConn{state: state}, nil
}

type schedulerTestConn struct{ state *schedulerTestState }

func (*schedulerTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (*schedulerTestConn) Close() error { return nil }
func (connection *schedulerTestConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}
func (connection *schedulerTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &schedulerTestTx{state: connection.state}, nil
}
func (connection *schedulerTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	step, err := connection.state.next("exec", query)
	if err != nil {
		return nil, err
	}
	if step.err != nil {
		return nil, step.err
	}
	return step.result, nil
}
func (connection *schedulerTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	step, err := connection.state.next("query", query)
	if err != nil {
		return nil, err
	}
	if step.err != nil {
		return nil, step.err
	}
	columns := make([]string, len(step.rows[0]))
	for index := range columns {
		columns[index] = fmt.Sprintf("column_%d", index)
	}
	return &schedulerTestRows{columns: columns, rows: step.rows}, nil
}

type schedulerTestTx struct{ state *schedulerTestState }

func (transaction *schedulerTestTx) Commit() error {
	transaction.state.mu.Lock()
	transaction.state.commits++
	transaction.state.mu.Unlock()
	return nil
}
func (transaction *schedulerTestTx) Rollback() error {
	transaction.state.mu.Lock()
	transaction.state.rollbacks++
	transaction.state.mu.Unlock()
	return nil
}

type schedulerTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (rows *schedulerTestRows) Columns() []string { return rows.columns }
func (*schedulerTestRows) Close() error           { return nil }
func (rows *schedulerTestRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.rows) {
		return io.EOF
	}
	copy(destination, rows.rows[rows.index])
	rows.index++
	return nil
}
