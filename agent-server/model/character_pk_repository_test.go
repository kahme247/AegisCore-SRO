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

const characterPKTestDriverName = "aegiscore-character-pk-test"

var characterPKTestState struct {
	sync.Mutex
	loadRows   [][]driver.Value
	lockRows   [][]driver.Value
	execErr    error
	execArgs   []driver.NamedValue
	committed  bool
	rolledBack bool
}

func init() { sql.Register(characterPKTestDriverName, characterPKTestDriver{}) }

type characterPKTestDriver struct{}
type characterPKTestConn struct{}
type characterPKTestTx struct{}

func (characterPKTestDriver) Open(string) (driver.Conn, error) { return characterPKTestConn{}, nil }
func (characterPKTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}
func (characterPKTestConn) Close() error              { return nil }
func (characterPKTestConn) Begin() (driver.Tx, error) { return characterPKTestTx{}, nil }
func (characterPKTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return characterPKTestTx{}, nil
}
func (characterPKTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	characterPKTestState.Lock()
	defer characterPKTestState.Unlock()
	columns, source := []string{"id"}, characterPKTestState.lockRows
	if strings.Contains(query, "SELECT `DAILY_PK`") {
		columns, source = []string{"DAILY_PK", "TOTAL_PK", "PK_PENALTY_POINT"}, characterPKTestState.loadRows
	}
	rows := make([][]driver.Value, len(source))
	for index := range source {
		rows[index] = append([]driver.Value(nil), source[index]...)
	}
	return &characterPKTestRows{columns: columns, rows: rows}, nil
}
func (characterPKTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "SET `DAILY_PK`=?,`TOTAL_PK`=?,`PK_PENALTY_POINT`=?") {
		return nil, errors.New("unexpected character PK update")
	}
	characterPKTestState.Lock()
	defer characterPKTestState.Unlock()
	characterPKTestState.execArgs = append([]driver.NamedValue(nil), args...)
	if characterPKTestState.execErr != nil {
		return nil, characterPKTestState.execErr
	}
	return driver.RowsAffected(1), nil
}
func (characterPKTestTx) Commit() error {
	characterPKTestState.Lock()
	characterPKTestState.committed = true
	characterPKTestState.Unlock()
	return nil
}
func (characterPKTestTx) Rollback() error {
	characterPKTestState.Lock()
	characterPKTestState.rolledBack = true
	characterPKTestState.Unlock()
	return nil
}

type characterPKTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *characterPKTestRows) Columns() []string { return r.columns }
func (r *characterPKTestRows) Close() error      { return nil }
func (r *characterPKTestRows) Next(values []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}

func resetCharacterPKTestState() {
	characterPKTestState.Lock()
	characterPKTestState.loadRows = nil
	characterPKTestState.lockRows = nil
	characterPKTestState.execErr = nil
	characterPKTestState.execArgs = nil
	characterPKTestState.committed = false
	characterPKTestState.rolledBack = false
	characterPKTestState.Unlock()
}

func newCharacterPKRepositoryForTest(t *testing.T) *SQLCharacterPKRepository {
	t.Helper()
	database, err := sql.Open(characterPKTestDriverName, t.Name())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := NewCharacterPKRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterPKRepository: %v", err)
	}
	return repository
}

func TestSQLCharacterPKRepositoryLoadsRecoveredFields(t *testing.T) {
	resetCharacterPKTestState()
	characterPKTestState.loadRows = [][]driver.Value{{int64(7), int64(-12), int64(345)}}
	state, err := newCharacterPKRepositoryForTest(t).LoadByName(context.Background(), " Example ")
	if err != nil || state != (CharacterPKState{DailyPK: 7, TotalPK: -12, PKPenaltyPoint: 345}) {
		t.Fatalf("LoadByName = (%+v, %v)", state, err)
	}
}

func TestSQLCharacterPKRepositoryEditsExistingCharacterAtomically(t *testing.T) {
	resetCharacterPKTestState()
	characterPKTestState.lockRows = [][]driver.Value{{int64(42)}}
	want := CharacterPKState{DailyPK: 255, TotalPK: -123, PKPenaltyPoint: 4567}
	if err := newCharacterPKRepositoryForTest(t).EditByName(context.Background(), "Example", want); err != nil {
		t.Fatalf("EditByName: %v", err)
	}
	characterPKTestState.Lock()
	defer characterPKTestState.Unlock()
	if !characterPKTestState.committed || characterPKTestState.rolledBack || len(characterPKTestState.execArgs) != 4 {
		t.Fatalf("transaction state committed=%t rolledBack=%t args=%#v", characterPKTestState.committed, characterPKTestState.rolledBack, characterPKTestState.execArgs)
	}
	values := []any{characterPKTestState.execArgs[0].Value, characterPKTestState.execArgs[1].Value, characterPKTestState.execArgs[2].Value, characterPKTestState.execArgs[3].Value}
	if values[0] != int64(255) || values[1] != int64(-123) || values[2] != int64(4567) || values[3] != int64(42) {
		t.Fatalf("update args = %#v", values)
	}
}

func TestSQLCharacterPKRepositoryReturnsLegacyNotFoundWithoutMutation(t *testing.T) {
	resetCharacterPKTestState()
	err := newCharacterPKRepositoryForTest(t).EditByName(context.Background(), "Missing", CharacterPKState{})
	if !errors.Is(err, ErrCharacterPKNotFound) {
		t.Fatalf("EditByName error = %v, want %v", err, ErrCharacterPKNotFound)
	}
	characterPKTestState.Lock()
	defer characterPKTestState.Unlock()
	if characterPKTestState.committed || len(characterPKTestState.execArgs) != 0 || !characterPKTestState.rolledBack {
		t.Fatalf("not-found transaction committed=%t rolledBack=%t args=%#v", characterPKTestState.committed, characterPKTestState.rolledBack, characterPKTestState.execArgs)
	}
}

func TestSQLCharacterPKRepositoryRollsBackFailedUpdate(t *testing.T) {
	resetCharacterPKTestState()
	characterPKTestState.lockRows = [][]driver.Value{{int64(42)}}
	characterPKTestState.execErr = errors.New("write failed")
	err := newCharacterPKRepositoryForTest(t).EditByName(context.Background(), "Example", CharacterPKState{})
	if err == nil || !strings.Contains(err.Error(), "persist character PK state") {
		t.Fatalf("EditByName error = %v", err)
	}
	characterPKTestState.Lock()
	defer characterPKTestState.Unlock()
	if characterPKTestState.committed || !characterPKTestState.rolledBack {
		t.Fatalf("failed transaction committed=%t rolledBack=%t", characterPKTestState.committed, characterPKTestState.rolledBack)
	}
}
