package model

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

const characterDeletionTestDriverName = "aegiscore-character-deletion-test"

type characterDeletionStep struct {
	kind     string
	contains string
	rows     [][]driver.Value
	affected int64
	err      error
}

type characterDeletionTestState struct {
	sync.Mutex
	steps     []characterDeletionStep
	commits   int
	rollbacks int
}

var (
	characterDeletionDriverOnce sync.Once
	characterDeletionStatesMu   sync.Mutex
	characterDeletionStates     = map[string]*characterDeletionTestState{}
	characterDeletionStateID    atomic.Uint64
)

func deletionQuery(contains string, rows ...[]driver.Value) characterDeletionStep {
	return characterDeletionStep{kind: "query", contains: contains, rows: rows}
}

func deletionExec(contains string, affected int64) characterDeletionStep {
	return characterDeletionStep{kind: "exec", contains: contains, affected: affected}
}

func newCharacterDeletionTestDatabase(t *testing.T, steps ...characterDeletionStep) (*characterDeletionTestState, *sql.DB) {
	t.Helper()
	characterDeletionDriverOnce.Do(func() { sql.Register(characterDeletionTestDriverName, characterDeletionTestDriver{}) })
	id := fmt.Sprintf("deletion-%d", characterDeletionStateID.Add(1))
	state := &characterDeletionTestState{steps: append([]characterDeletionStep(nil), steps...)}
	characterDeletionStatesMu.Lock()
	characterDeletionStates[id] = state
	characterDeletionStatesMu.Unlock()
	database, err := sql.Open(characterDeletionTestDriverName, id)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
		characterDeletionStatesMu.Lock()
		delete(characterDeletionStates, id)
		characterDeletionStatesMu.Unlock()
	})
	return state, database
}

func (s *characterDeletionTestState) next(kind, query string) (characterDeletionStep, error) {
	s.Lock()
	defer s.Unlock()
	if len(s.steps) == 0 {
		return characterDeletionStep{}, fmt.Errorf("unexpected %s: %s", kind, query)
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.kind != kind || !strings.Contains(query, step.contains) {
		return characterDeletionStep{}, fmt.Errorf("%s %q, want %s containing %q", kind, query, step.kind, step.contains)
	}
	return step, step.err
}

func (s *characterDeletionTestState) assert(t *testing.T, commits, rollbacks int) {
	t.Helper()
	s.Lock()
	defer s.Unlock()
	if len(s.steps) != 0 || s.commits != commits || s.rollbacks != rollbacks {
		t.Fatalf("deletion state = remaining %d commits %d rollbacks %d, want 0/%d/%d", len(s.steps), s.commits, s.rollbacks, commits, rollbacks)
	}
}

type characterDeletionTestDriver struct{}
type characterDeletionTestConn struct{ state *characterDeletionTestState }
type characterDeletionTestTx struct{ state *characterDeletionTestState }

func (characterDeletionTestDriver) Open(name string) (driver.Conn, error) {
	characterDeletionStatesMu.Lock()
	state := characterDeletionStates[name]
	characterDeletionStatesMu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("unknown character deletion state %q", name)
	}
	return &characterDeletionTestConn{state: state}, nil
}
func (*characterDeletionTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}
func (*characterDeletionTestConn) Close() error { return nil }
func (c *characterDeletionTestConn) Begin() (driver.Tx, error) {
	return &characterDeletionTestTx{state: c.state}, nil
}
func (c *characterDeletionTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &characterDeletionTestTx{state: c.state}, nil
}
func (c *characterDeletionTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	step, err := c.state.next("query", query)
	if err != nil {
		return nil, err
	}
	columns := make([]string, 0)
	if len(step.rows) != 0 {
		columns = make([]string, len(step.rows[0]))
		for index := range columns {
			columns[index] = fmt.Sprintf("column_%d", index)
		}
	}
	return &characterDeletionTestRows{columns: columns, rows: step.rows}, nil
}
func (c *characterDeletionTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	step, err := c.state.next("exec", query)
	if err != nil {
		return nil, err
	}
	return driver.RowsAffected(step.affected), nil
}
func (tx *characterDeletionTestTx) Commit() error {
	tx.state.Lock()
	tx.state.commits++
	tx.state.Unlock()
	return nil
}
func (tx *characterDeletionTestTx) Rollback() error {
	tx.state.Lock()
	tx.state.rollbacks++
	tx.state.Unlock()
	return nil
}

type characterDeletionTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *characterDeletionTestRows) Columns() []string { return r.columns }
func (*characterDeletionTestRows) Close() error        { return nil }
func (r *characterDeletionTestRows) Next(destination []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(destination, r.rows[r.index])
	r.index++
	return nil
}

func TestDeleteCharacterTransactionRemovesOwnedAggregateAtomically(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `DELETING`,`DELETION_REQUESTED_AT`", []driver.Value{true, now.Add(-8 * 24 * time.Hour)}),
		deletionQuery("FROM `SRO_SHARD`.`GUILD_MEMBER`"),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY`", []driver.Value{int64(101)}, []driver.Value{int64(102)}),
		deletionQuery("FROM `SRO_SHARD`.`AVATAR_INVENTORY`", []driver.Value{int64(103)}),
		deletionExec("DELETE FROM `SRO_SHARD`.`INVENTORY`", 2),
		deletionExec("DELETE FROM `SRO_SHARD`.`AVATAR_INVENTORY`", 1),
		deletionExec("DELETE FROM `SRO_SHARD`.`ITEM`", 1),
		deletionExec("DELETE FROM `SRO_SHARD`.`ITEM`", 1),
		deletionExec("DELETE FROM `SRO_SHARD`.`ITEM`", 1),
		deletionExec("DELETE FROM `SRO_SHARD`.`CHAR`", 1),
	)
	if err := deleteCharacterTransaction(context.Background(), database, 7, now, true); err != nil {
		t.Fatalf("deleteCharacterTransaction: %v", err)
	}
	state.assert(t, 1, 0)
}

func TestDeleteCharacterTransactionRejectsGuildMaster(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `DELETING`,`DELETION_REQUESTED_AT`", []driver.Value{true, now.Add(-8 * 24 * time.Hour)}),
		deletionQuery("FROM `SRO_SHARD`.`GUILD_MEMBER`", []driver.Value{int64(1)}),
	)
	err := deleteCharacterTransaction(context.Background(), database, 7, now, true)
	if !errors.Is(err, ErrCharacterDeletionGuildMaster) {
		t.Fatalf("deleteCharacterTransaction error = %v, want %v", err, ErrCharacterDeletionGuildMaster)
	}
	state.assert(t, 0, 1)
}

func TestDeleteCharacterTransactionRollsBackFailedItemDelete(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	want := errors.New("item delete refused")
	failedDelete := deletionExec("DELETE FROM `SRO_SHARD`.`ITEM`", 0)
	failedDelete.err = want
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `DELETING`,`DELETION_REQUESTED_AT`", []driver.Value{true, now.Add(-8 * 24 * time.Hour)}),
		deletionQuery("FROM `SRO_SHARD`.`GUILD_MEMBER`"),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY`", []driver.Value{int64(101)}),
		deletionQuery("FROM `SRO_SHARD`.`AVATAR_INVENTORY`"),
		deletionExec("DELETE FROM `SRO_SHARD`.`INVENTORY`", 1),
		deletionExec("DELETE FROM `SRO_SHARD`.`AVATAR_INVENTORY`", 0),
		failedDelete,
	)
	err := deleteCharacterTransaction(context.Background(), database, 7, now, true)
	if !errors.Is(err, want) {
		t.Fatalf("deleteCharacterTransaction error = %v, want wrapped item failure", err)
	}
	state.assert(t, 0, 1)
}

func TestPurgeExpiredCharactersRunsBoundedDueSweep(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("ORDER BY c.`DELETION_REQUESTED_AT`,c.`ID` LIMIT ?", []driver.Value{int64(7)}),
		deletionQuery("SELECT `DELETING`,`DELETION_REQUESTED_AT`", []driver.Value{true, now.Add(-8 * 24 * time.Hour)}),
		deletionQuery("FROM `SRO_SHARD`.`GUILD_MEMBER`"),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY`"),
		deletionQuery("FROM `SRO_SHARD`.`AVATAR_INVENTORY`"),
		deletionExec("DELETE FROM `SRO_SHARD`.`INVENTORY`", 0),
		deletionExec("DELETE FROM `SRO_SHARD`.`AVATAR_INVENTORY`", 0),
		deletionExec("DELETE FROM `SRO_SHARD`.`CHAR`", 1),
	)
	repository := &SQLCharacterRepository{shard: database}
	deleted, err := repository.PurgeExpiredCharacters(context.Background(), now, 100)
	if err != nil || deleted != 1 {
		t.Fatalf("PurgeExpiredCharacters = (%d, %v), want (1, nil)", deleted, err)
	}
	state.assert(t, 1, 0)
}
