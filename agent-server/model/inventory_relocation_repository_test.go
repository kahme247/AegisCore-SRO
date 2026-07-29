package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
)

const inventoryRelocationRepositoryTestDriverName = "go-sro-inventory-relocation-repository-test"

var inventoryRelocationRepositoryTestRegistry struct {
	sync.Mutex
	nextID int
	stores map[string]*inventoryRelocationRepositoryTestStore
}

func init() {
	sql.Register(inventoryRelocationRepositoryTestDriverName, inventoryRelocationRepositoryTestDriver{})
}

type inventoryRelocationRepositoryTestDriver struct{}

func (inventoryRelocationRepositoryTestDriver) Open(name string) (driver.Conn, error) {
	inventoryRelocationRepositoryTestRegistry.Lock()
	store := inventoryRelocationRepositoryTestRegistry.stores[name]
	inventoryRelocationRepositoryTestRegistry.Unlock()
	if store == nil {
		return nil, fmt.Errorf("inventory relocation test store %q is unavailable", name)
	}
	return &inventoryRelocationRepositoryTestConn{store: store}, nil
}

type inventoryRelocationRepositoryTestStore struct {
	sync.Mutex

	characterExists bool
	lockedItems     [][]driver.Value

	beginErr      error
	beginContexts []context.Context
	commitErr     error
	commitCount   int
	rollbackCount int

	failQuery      string
	failOccurrence int
	failErr        error
	queryCounts    map[string]int
	rowsAffected   map[string]int64
	calls          []inventoryRelocationRepositoryTestCall
}

type inventoryRelocationRepositoryTestCall struct {
	query string
	args  []driver.NamedValue
}

type inventoryRelocationRepositoryTestConn struct {
	store *inventoryRelocationRepositoryTestStore
}

var _ driver.Conn = (*inventoryRelocationRepositoryTestConn)(nil)
var _ driver.ConnBeginTx = (*inventoryRelocationRepositoryTestConn)(nil)
var _ driver.QueryerContext = (*inventoryRelocationRepositoryTestConn)(nil)
var _ driver.ExecerContext = (*inventoryRelocationRepositoryTestConn)(nil)

func (*inventoryRelocationRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the inventory relocation test driver")
}

func (*inventoryRelocationRepositoryTestConn) Close() error { return nil }

func (c *inventoryRelocationRepositoryTestConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *inventoryRelocationRepositoryTestConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.store.Lock()
	defer c.store.Unlock()
	c.store.beginContexts = append(c.store.beginContexts, ctx)
	if c.store.beginErr != nil {
		return nil, c.store.beginErr
	}
	return &inventoryRelocationRepositoryTestTx{store: c.store}, nil
}

func (c *inventoryRelocationRepositoryTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.store.Lock()
	defer c.store.Unlock()
	if err := c.store.recordCallLocked(query, args); err != nil {
		return nil, err
	}

	switch query {
	case LockCharacterForInventoryRelocation:
		if !c.store.characterExists {
			return &inventoryRelocationRepositoryTestRows{columns: []string{"id"}}, nil
		}
		return &inventoryRelocationRepositoryTestRows{
			columns: []string{"id"},
			rows:    [][]driver.Value{{int64FromNamedValue(args, 0)}},
		}, nil
	case LockCharacterInventoryForRelocation:
		return &inventoryRelocationRepositoryTestRows{
			columns: []string{"SLOT", "FK_ITEM"},
			rows:    cloneInventoryRelocationRows(c.store.lockedItems),
		}, nil
	default:
		return nil, fmt.Errorf("unexpected inventory relocation query %q", query)
	}
}

func (c *inventoryRelocationRepositoryTestConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.store.Lock()
	defer c.store.Unlock()
	if err := c.store.recordCallLocked(query, args); err != nil {
		return nil, err
	}
	if !inventoryRelocationRepositoryRecognizesMutation(query) {
		return nil, fmt.Errorf("unexpected inventory relocation exec %q", query)
	}
	rowsAffected := int64(1)
	if c.store.rowsAffected != nil {
		if configured, exists := c.store.rowsAffected[query]; exists {
			rowsAffected = configured
		}
	}
	return driver.RowsAffected(rowsAffected), nil
}

func (s *inventoryRelocationRepositoryTestStore) recordCallLocked(query string, args []driver.NamedValue) error {
	if s.queryCounts == nil {
		s.queryCounts = make(map[string]int)
	}
	s.queryCounts[query]++
	s.calls = append(s.calls, inventoryRelocationRepositoryTestCall{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	if query == s.failQuery && s.queryCounts[query] == s.failOccurrence {
		return s.failErr
	}
	return nil
}

type inventoryRelocationRepositoryTestTx struct {
	store *inventoryRelocationRepositoryTestStore
}

func (tx *inventoryRelocationRepositoryTestTx) Commit() error {
	tx.store.Lock()
	defer tx.store.Unlock()
	tx.store.commitCount++
	return tx.store.commitErr
}

func (tx *inventoryRelocationRepositoryTestTx) Rollback() error {
	tx.store.Lock()
	defer tx.store.Unlock()
	tx.store.rollbackCount++
	return nil
}

type inventoryRelocationRepositoryTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *inventoryRelocationRepositoryTestRows) Columns() []string { return r.columns }
func (*inventoryRelocationRepositoryTestRows) Close() error        { return nil }

func (r *inventoryRelocationRepositoryTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func cloneInventoryRelocationRows(rows [][]driver.Value) [][]driver.Value {
	cloned := make([][]driver.Value, len(rows))
	for index, row := range rows {
		cloned[index] = append([]driver.Value(nil), row...)
	}
	return cloned
}

func int64FromNamedValue(values []driver.NamedValue, index int) int64 {
	if index >= len(values) {
		return 0
	}
	switch value := values[index].Value.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case uint32:
		return int64(value)
	case byte:
		return int64(value)
	default:
		return 0
	}
}

func inventoryRelocationRepositoryRecognizesMutation(query string) bool {
	return query == MoveInventoryRelocationSource ||
		query == DeleteInventoryRelocationSource ||
		query == InsertInventoryRelocationSource
}

func newInventoryRelocationRepositoryForTest(t *testing.T, store *inventoryRelocationRepositoryTestStore) (*SQLInventoryRelocationRepository, *inventoryRelocationRepositoryTestStore) {
	t.Helper()
	inventoryRelocationRepositoryTestRegistry.Lock()
	if inventoryRelocationRepositoryTestRegistry.stores == nil {
		inventoryRelocationRepositoryTestRegistry.stores = make(map[string]*inventoryRelocationRepositoryTestStore)
	}
	inventoryRelocationRepositoryTestRegistry.nextID++
	name := fmt.Sprintf("inventory-relocation-%d", inventoryRelocationRepositoryTestRegistry.nextID)
	inventoryRelocationRepositoryTestRegistry.stores[name] = store
	inventoryRelocationRepositoryTestRegistry.Unlock()

	database, err := sql.Open(inventoryRelocationRepositoryTestDriverName, name)
	if err != nil {
		t.Fatalf("sql.Open inventory relocation test database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = database.Close()
		inventoryRelocationRepositoryTestRegistry.Lock()
		delete(inventoryRelocationRepositoryTestRegistry.stores, name)
		inventoryRelocationRepositoryTestRegistry.Unlock()
	})

	repository, err := NewInventoryRelocationRepository(database)
	if err != nil {
		t.Fatalf("NewInventoryRelocationRepository: %v", err)
	}
	return repository, store
}

func TestNewInventoryRelocationRepositoryRequiresPool(t *testing.T) {
	repository, err := NewInventoryRelocationRepository(nil)
	if repository != nil || !errors.Is(err, ErrInventoryRelocationRepositoryUnavailable) {
		t.Fatalf("NewInventoryRelocationRepository(nil) = (%v, %v), want unavailable error", repository, err)
	}
}

func TestSQLInventoryRelocationRepositoryCommitsLockedMove(t *testing.T) {
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems:     [][]driver.Value{{int64(14), int64(101)}},
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "move-request")

	err := repository.Relocate(ctx, 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
	if err != nil {
		t.Fatalf("Relocate() error = %v", err)
	}

	store.Lock()
	beginContexts := append([]context.Context(nil), store.beginContexts...)
	calls := append([]inventoryRelocationRepositoryTestCall(nil), store.calls...)
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	if len(beginContexts) != 1 || beginContexts[0].Value(contextKey{}) != "move-request" {
		t.Fatalf("BeginTx contexts = %#v, want request context", beginContexts)
	}
	if commitCount != 1 || rollbackCount != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want one commit and no rollback", commitCount, rollbackCount)
	}
	wantQueries := []string{
		LockCharacterForInventoryRelocation,
		LockCharacterInventoryForRelocation,
		MoveInventoryRelocationSource,
	}
	assertInventoryRelocationRepositoryQueries(t, calls, wantQueries)
	assertInventoryRelocationInteger(t, calls[0].args[0].Value, 44)
	assertInventoryRelocationInteger(t, calls[2].args[0].Value, int64(15))
	assertInventoryRelocationInteger(t, calls[2].args[1].Value, 44)
	assertInventoryRelocationInteger(t, calls[2].args[2].Value, int64(14))
	assertInventoryRelocationInteger(t, calls[2].args[3].Value, 101)
}

func TestSQLInventoryRelocationRepositoryClassifiesUnknownCommitOutcome(t *testing.T) {
	commitFailure := errors.New("connection dropped during commit")
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems:     [][]driver.Value{{int64(14), int64(101)}},
		commitErr:       commitFailure,
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)

	err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
	if !errors.Is(err, ErrInventoryRelocationOutcomeUnknown) || !errors.Is(err, commitFailure) {
		t.Fatalf("Relocate() error = %v, want unknown-outcome wrapper around %v", err, commitFailure)
	}
	var repositoryError *InventoryRelocationRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != "commit relocation transaction" {
		t.Fatalf("Relocate() error = %v, want typed commit failure", err)
	}
	store.Lock()
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	if commitCount != 1 || rollbackCount != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want one uncertain commit and no post-commit rollback", commitCount, rollbackCount)
	}
}

func TestSQLInventoryRelocationRepositoryCommitsLockedSwapWithoutUniqueSlotCollision(t *testing.T) {
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems: [][]driver.Value{
			{int64(14), int64(101)},
			{int64(15), int64(202)},
		},
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)

	if err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 202)); err != nil {
		t.Fatalf("Relocate() error = %v", err)
	}
	store.Lock()
	calls := append([]inventoryRelocationRepositoryTestCall(nil), store.calls...)
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	if commitCount != 1 || rollbackCount != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want one commit and no rollback", commitCount, rollbackCount)
	}
	assertInventoryRelocationRepositoryQueries(t, calls, []string{
		LockCharacterForInventoryRelocation,
		LockCharacterInventoryForRelocation,
		DeleteInventoryRelocationSource,
		MoveInventoryRelocationTarget,
		InsertInventoryRelocationSource,
	})
	assertInventoryRelocationInteger(t, calls[2].args[0].Value, 44)
	assertInventoryRelocationInteger(t, calls[2].args[1].Value, int64(14))
	assertInventoryRelocationInteger(t, calls[2].args[2].Value, 101)
	assertInventoryRelocationInteger(t, calls[3].args[0].Value, int64(14))
	assertInventoryRelocationInteger(t, calls[3].args[2].Value, int64(15))
	assertInventoryRelocationInteger(t, calls[4].args[0].Value, 44)
	assertInventoryRelocationInteger(t, calls[4].args[1].Value, int64(15))
	assertInventoryRelocationInteger(t, calls[4].args[2].Value, 101)
}

func TestSQLInventoryRelocationRepositoryRollsBackWriteFailure(t *testing.T) {
	persistFailure := errors.New("write failed")
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems:     [][]driver.Value{{int64(14), int64(101)}},
		failQuery:       MoveInventoryRelocationSource,
		failOccurrence:  1,
		failErr:         persistFailure,
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)

	err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
	if !errors.Is(err, persistFailure) {
		t.Fatalf("Relocate() error = %v, want wrapped %v", err, persistFailure)
	}
	var repositoryError *InventoryRelocationRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != "move source item" {
		t.Fatalf("Relocate() error = %v, want typed move source item error", err)
	}
	store.Lock()
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	if commitCount != 0 || rollbackCount != 1 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want no commit and one rollback", commitCount, rollbackCount)
	}
}

func TestSQLInventoryRelocationRepositoryRollsBackSwapAfterSourceRemoval(t *testing.T) {
	persistFailure := errors.New("target move failed")
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems: [][]driver.Value{
			{int64(14), int64(101)},
			{int64(15), int64(202)},
		},
		// Source and target moves share one SQL statement shape. In a swap the
		// first such statement is the locked target move, after the source-link
		// delete has completed inside the transaction.
		failQuery:      MoveInventoryRelocationTarget,
		failOccurrence: 1,
		failErr:        persistFailure,
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)

	err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 202))
	if !errors.Is(err, persistFailure) {
		t.Fatalf("Relocate() error = %v, want wrapped %v", err, persistFailure)
	}
	var repositoryError *InventoryRelocationRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != "move target item for swap" {
		t.Fatalf("Relocate() error = %v, want typed target swap failure", err)
	}
	store.Lock()
	calls := append([]inventoryRelocationRepositoryTestCall(nil), store.calls...)
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	assertInventoryRelocationRepositoryQueries(t, calls, []string{
		LockCharacterForInventoryRelocation,
		LockCharacterInventoryForRelocation,
		DeleteInventoryRelocationSource,
		MoveInventoryRelocationTarget,
	})
	if commitCount != 0 || rollbackCount != 1 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want no commit and one rollback", commitCount, rollbackCount)
	}
}

func TestSQLInventoryRelocationRepositoryRejectsStalePersistedInventoryBeforeWrites(t *testing.T) {
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems:     [][]driver.Value{{int64(14), int64(999)}},
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)

	err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
	if !errors.Is(err, ErrInventoryRelocationConflict) {
		t.Fatalf("Relocate() error = %v, want persisted conflict", err)
	}
	var repositoryError *InventoryRelocationRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Operation != "verify relocation source" {
		t.Fatalf("Relocate() error = %v, want typed source verification error", err)
	}
	store.Lock()
	calls := append([]inventoryRelocationRepositoryTestCall(nil), store.calls...)
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	assertInventoryRelocationRepositoryQueries(t, calls, []string{
		LockCharacterForInventoryRelocation,
		LockCharacterInventoryForRelocation,
	})
	if commitCount != 0 || rollbackCount != 1 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want no commit and one rollback", commitCount, rollbackCount)
	}
}

func TestSQLInventoryRelocationRepositoryRejectsMissingCharacterAndCanceledContext(t *testing.T) {
	t.Run("missing character", func(t *testing.T) {
		store := &inventoryRelocationRepositoryTestStore{}
		repository, store := newInventoryRelocationRepositoryForTest(t, store)
		err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
		if !errors.Is(err, ErrInventoryRelocationCharacterNotFound) {
			t.Fatalf("Relocate() error = %v, want missing character", err)
		}
		store.Lock()
		commitCount, rollbackCount := store.commitCount, store.rollbackCount
		store.Unlock()
		if commitCount != 0 || rollbackCount != 1 {
			t.Fatalf("transaction commits=%d rollbacks=%d, want no commit and one rollback", commitCount, rollbackCount)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		store := &inventoryRelocationRepositoryTestStore{characterExists: true}
		repository, store := newInventoryRelocationRepositoryForTest(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := repository.Relocate(ctx, 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Relocate() error = %v, want context cancellation", err)
		}
		store.Lock()
		beginCount := len(store.beginContexts)
		store.Unlock()
		if beginCount != 0 {
			t.Fatalf("BeginTx calls = %d, want 0 after context cancellation", beginCount)
		}
	})
}

func TestSQLInventoryRelocationRepositoryRejectsZeroRowsAffectedAsConflict(t *testing.T) {
	store := &inventoryRelocationRepositoryTestStore{
		characterExists: true,
		lockedItems:     [][]driver.Value{{int64(14), int64(101)}},
		rowsAffected:    map[string]int64{MoveInventoryRelocationSource: 0},
	}
	repository, store := newInventoryRelocationRepositoryForTest(t, store)

	err := repository.Relocate(context.Background(), 44, inventoryRelocationRepositoryPlan(14, 15, 101, 0))
	if !errors.Is(err, ErrInventoryRelocationConflict) {
		t.Fatalf("Relocate() error = %v, want persisted conflict", err)
	}
	store.Lock()
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	if commitCount != 0 || rollbackCount != 1 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want no commit and one rollback", commitCount, rollbackCount)
	}
}

func inventoryRelocationRepositoryPlan(sourceSlot, targetSlot byte, sourceItemID, targetItemID int64) InventoryRelocationPlan {
	return InventoryRelocationPlan{
		SourceSlot:   sourceSlot,
		TargetSlot:   targetSlot,
		SourceItemID: sourceItemID,
		TargetItemID: targetItemID,
		Action:       InventoryRelocationMove,
	}
}

func assertInventoryRelocationRepositoryQueries(t *testing.T, calls []inventoryRelocationRepositoryTestCall, want []string) {
	t.Helper()
	if len(calls) != len(want) {
		t.Fatalf("database calls = %d, want %d (%#v)", len(calls), len(want), calls)
	}
	for index, query := range want {
		if calls[index].query != query {
			t.Fatalf("database call %d query = %q, want %q", index, calls[index].query, query)
		}
	}
}

func assertInventoryRelocationInteger(t *testing.T, value any, want int64) {
	t.Helper()
	got := int64FromNamedValue([]driver.NamedValue{{Value: value}}, 0)
	if got != want {
		t.Fatalf("integer = %d, want %d (value %T(%v))", got, want, value, value)
	}
}
