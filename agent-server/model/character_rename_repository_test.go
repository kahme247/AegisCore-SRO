package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

const characterRenameTestDriverName = "aegiscore-character-rename-test"

var characterRenameTestState struct {
	sync.Mutex
	targetRows       [][]driver.Value
	duplicateRows    [][]driver.Value
	availabilityRows [][]driver.Value
	queries          []renameTestCall
	execs            []renameTestCall
	affected         int64
	committed        bool
}

type renameTestCall struct {
	query string
	args  []driver.NamedValue
}

func init() { sql.Register(characterRenameTestDriverName, characterRenameTestDriver{}) }

type characterRenameTestDriver struct{}
type characterRenameTestConn struct{}
type characterRenameTestTx struct{}

func (characterRenameTestDriver) Open(string) (driver.Conn, error) {
	return characterRenameTestConn{}, nil
}
func (characterRenameTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}
func (characterRenameTestConn) Close() error              { return nil }
func (characterRenameTestConn) Begin() (driver.Tx, error) { return characterRenameTestTx{}, nil }
func (characterRenameTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return characterRenameTestTx{}, nil
}

func (characterRenameTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	characterRenameTestState.Lock()
	defer characterRenameTestState.Unlock()
	characterRenameTestState.queries = append(characterRenameTestState.queries, renameTestCall{query: query, args: append([]driver.NamedValue(nil), args...)})
	var columns []string
	var source [][]driver.Value
	switch {
	case strings.Contains(query, "SELECT g.`ID`,g.`RENAME_REQUIRED`"):
		columns, source = []string{"ID", "RENAME_REQUIRED"}, characterRenameTestState.targetRows
	case strings.Contains(query, "AND `ID`<>?"):
		columns, source = []string{"EXISTS"}, characterRenameTestState.duplicateRows
	case strings.Contains(query, "SELECT 1 FROM `GUILD`"):
		columns, source = []string{"EXISTS"}, characterRenameTestState.availabilityRows
	default:
		return nil, errors.New("unexpected character rename query")
	}
	rows := make([][]driver.Value, len(source))
	for i := range source {
		rows[i] = append([]driver.Value(nil), source[i]...)
	}
	return &characterRenameTestRows{columns: columns, rows: rows}, nil
}

func (characterRenameTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	characterRenameTestState.Lock()
	defer characterRenameTestState.Unlock()
	characterRenameTestState.execs = append(characterRenameTestState.execs, renameTestCall{query: query, args: append([]driver.NamedValue(nil), args...)})
	if !strings.Contains(query, "UPDATE `GUILD` SET `NAME`=?,`RENAME_REQUIRED`=FALSE") {
		return nil, errors.New("unexpected character rename exec")
	}
	return driver.RowsAffected(characterRenameTestState.affected), nil
}

func (characterRenameTestTx) Commit() error {
	characterRenameTestState.Lock()
	characterRenameTestState.committed = true
	characterRenameTestState.Unlock()
	return nil
}
func (characterRenameTestTx) Rollback() error { return nil }

type characterRenameTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *characterRenameTestRows) Columns() []string { return r.columns }
func (r *characterRenameTestRows) Close() error      { return nil }
func (r *characterRenameTestRows) Next(values []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}

func resetCharacterRenameTestState() {
	characterRenameTestState.Lock()
	characterRenameTestState.targetRows = nil
	characterRenameTestState.duplicateRows = nil
	characterRenameTestState.availabilityRows = nil
	characterRenameTestState.queries = nil
	characterRenameTestState.execs = nil
	characterRenameTestState.affected = 0
	characterRenameTestState.committed = false
	characterRenameTestState.Unlock()
}

func newCharacterRenameRepositoryForTest(t *testing.T) *SQLCharacterRenameRepository {
	t.Helper()
	database, err := sql.Open(characterRenameTestDriverName, t.Name())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := NewCharacterRenameRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterRenameRepository: %v", err)
	}
	return repository
}

func TestSQLCharacterRenameRepositoryConsumesOwnedForcedGuildRename(t *testing.T) {
	resetCharacterRenameTestState()
	characterRenameTestState.Lock()
	characterRenameTestState.targetRows = [][]driver.Value{{int64(12), true}}
	characterRenameTestState.affected = 1
	characterRenameTestState.Unlock()
	repository := newCharacterRenameRepositoryForTest(t)
	if err := repository.RenameGuild(context.Background(), 42, 7, "OldGuild", "NewGuild"); err != nil {
		t.Fatalf("RenameGuild: %v", err)
	}
	characterRenameTestState.Lock()
	defer characterRenameTestState.Unlock()
	if !characterRenameTestState.committed || len(characterRenameTestState.execs) != 1 {
		t.Fatalf("guild rename transaction committed=%t execs=%d", characterRenameTestState.committed, len(characterRenameTestState.execs))
	}
	lock := characterRenameTestState.queries[0]
	if len(lock.args) != 3 || lock.args[0].Value != "OldGuild" || lock.args[1].Value != int64(42) || lock.args[2].Value != int64(7) {
		t.Fatalf("guild ownership lock args = %#v", lock.args)
	}
	update := characterRenameTestState.execs[0]
	if len(update.args) != 2 || update.args[0].Value != "NewGuild" || update.args[1].Value != int64(12) {
		t.Fatalf("guild rename update args = %#v", update.args)
	}
}

func TestSQLCharacterRenameRepositoryRequiresForcedFlag(t *testing.T) {
	resetCharacterRenameTestState()
	characterRenameTestState.Lock()
	characterRenameTestState.targetRows = [][]driver.Value{{int64(12), false}}
	characterRenameTestState.Unlock()
	err := newCharacterRenameRepositoryForTest(t).RenameGuild(context.Background(), 42, 7, "OldGuild", "NewGuild")
	if !errors.Is(err, ErrGuildRenameNotRequired) {
		t.Fatalf("RenameGuild error = %v, want %v", err, ErrGuildRenameNotRequired)
	}
	characterRenameTestState.Lock()
	defer characterRenameTestState.Unlock()
	if characterRenameTestState.committed || len(characterRenameTestState.execs) != 0 {
		t.Fatalf("non-required rename mutated state: committed=%t execs=%d", characterRenameTestState.committed, len(characterRenameTestState.execs))
	}
}

func TestSQLCharacterRenameRepositoryChecksGuildNameAvailabilityReadOnly(t *testing.T) {
	resetCharacterRenameTestState()
	available, err := newCharacterRenameRepositoryForTest(t).GuildNameAvailable(context.Background(), "UnusedGuild")
	if err != nil || !available {
		t.Fatalf("GuildNameAvailable = (%t, %v), want (true, nil)", available, err)
	}
	characterRenameTestState.Lock()
	defer characterRenameTestState.Unlock()
	if len(characterRenameTestState.execs) != 0 || len(characterRenameTestState.queries) != 1 {
		t.Fatalf("availability operations = queries %d execs %d", len(characterRenameTestState.queries), len(characterRenameTestState.execs))
	}
}
