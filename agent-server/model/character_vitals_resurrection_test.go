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

const vitalsResurrectionDriverName = "aegiscore-vitals-resurrection-test"

var vitalsResurrectionState struct {
	sync.Mutex
	casterRows     [][]driver.Value
	targetAffected int64
	debitAffected  int64
	targetErr      error
	debitErr       error
	execs          []string
	committed      bool
	rolledBack     bool
}

func init() { sql.Register(vitalsResurrectionDriverName, vitalsResurrectionDriver{}) }

type vitalsResurrectionDriver struct{}
type vitalsResurrectionConn struct{}
type vitalsResurrectionTx struct{}

func (vitalsResurrectionDriver) Open(string) (driver.Conn, error) {
	return vitalsResurrectionConn{}, nil
}
func (vitalsResurrectionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}
func (vitalsResurrectionConn) Close() error              { return nil }
func (vitalsResurrectionConn) Begin() (driver.Tx, error) { return vitalsResurrectionTx{}, nil }
func (vitalsResurrectionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return vitalsResurrectionTx{}, nil
}
func (vitalsResurrectionConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	vitalsResurrectionState.Lock()
	defer vitalsResurrectionState.Unlock()
	rows := make([][]driver.Value, len(vitalsResurrectionState.casterRows))
	for index := range vitalsResurrectionState.casterRows {
		rows[index] = append([]driver.Value(nil), vitalsResurrectionState.casterRows[index]...)
	}
	return &vitalsResurrectionRows{rows: rows}, nil
}
func (vitalsResurrectionConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	vitalsResurrectionState.Lock()
	defer vitalsResurrectionState.Unlock()
	vitalsResurrectionState.execs = append(vitalsResurrectionState.execs, query)
	if strings.Contains(query, "SET `HP`=?,`MP`=?") {
		if vitalsResurrectionState.targetErr != nil {
			return nil, vitalsResurrectionState.targetErr
		}
		return driver.RowsAffected(vitalsResurrectionState.targetAffected), nil
	}
	if strings.Contains(query, "SET `MP`=`MP`-?") {
		if vitalsResurrectionState.debitErr != nil {
			return nil, vitalsResurrectionState.debitErr
		}
		return driver.RowsAffected(vitalsResurrectionState.debitAffected), nil
	}
	return nil, errors.New("unexpected vitals resurrection statement")
}
func (vitalsResurrectionTx) Commit() error {
	vitalsResurrectionState.Lock()
	vitalsResurrectionState.committed = true
	vitalsResurrectionState.Unlock()
	return nil
}
func (vitalsResurrectionTx) Rollback() error {
	vitalsResurrectionState.Lock()
	vitalsResurrectionState.rolledBack = true
	vitalsResurrectionState.Unlock()
	return nil
}

type vitalsResurrectionRows struct {
	rows  [][]driver.Value
	index int
}

func (*vitalsResurrectionRows) Columns() []string { return []string{"ID"} }
func (*vitalsResurrectionRows) Close() error      { return nil }
func (r *vitalsResurrectionRows) Next(values []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}

func resetVitalsResurrectionState() {
	vitalsResurrectionState.Lock()
	vitalsResurrectionState.casterRows = nil
	vitalsResurrectionState.targetAffected = 0
	vitalsResurrectionState.debitAffected = 0
	vitalsResurrectionState.targetErr = nil
	vitalsResurrectionState.debitErr = nil
	vitalsResurrectionState.execs = nil
	vitalsResurrectionState.committed = false
	vitalsResurrectionState.rolledBack = false
	vitalsResurrectionState.Unlock()
}

func newVitalsResurrectionRepository(t *testing.T) *SQLCharacterVitalsRepository {
	t.Helper()
	database, err := sql.Open(vitalsResurrectionDriverName, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := NewCharacterVitalsRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestCastResurrectionCommitsTargetAndCasterTogether(t *testing.T) {
	resetVitalsResurrectionState()
	vitalsResurrectionState.casterRows = [][]driver.Value{{int64(42)}}
	vitalsResurrectionState.targetAffected = 1
	vitalsResurrectionState.debitAffected = 1
	committed, err := newVitalsResurrectionRepository(t).CastResurrection(context.Background(), 42, 84, 150, 30, 200, 100)
	if err != nil || !committed {
		t.Fatalf("CastResurrection = (%t, %v)", committed, err)
	}
	vitalsResurrectionState.Lock()
	defer vitalsResurrectionState.Unlock()
	if !vitalsResurrectionState.committed || vitalsResurrectionState.rolledBack || len(vitalsResurrectionState.execs) != 2 {
		t.Fatalf("transaction committed=%t rolledBack=%t execs=%d", vitalsResurrectionState.committed, vitalsResurrectionState.rolledBack, len(vitalsResurrectionState.execs))
	}
}

func TestCastResurrectionRollsBackWhenTargetIsNoLongerDead(t *testing.T) {
	resetVitalsResurrectionState()
	vitalsResurrectionState.casterRows = [][]driver.Value{{int64(42)}}
	vitalsResurrectionState.targetAffected = 0
	committed, err := newVitalsResurrectionRepository(t).CastResurrection(context.Background(), 42, 84, 150, 30, 200, 100)
	if err != nil || committed {
		t.Fatalf("CastResurrection = (%t, %v), want false nil", committed, err)
	}
	vitalsResurrectionState.Lock()
	defer vitalsResurrectionState.Unlock()
	if vitalsResurrectionState.committed || !vitalsResurrectionState.rolledBack || len(vitalsResurrectionState.execs) != 1 {
		t.Fatalf("superseded transaction committed=%t rolledBack=%t execs=%d", vitalsResurrectionState.committed, vitalsResurrectionState.rolledBack, len(vitalsResurrectionState.execs))
	}
}

func TestCastResurrectionRollsBackTargetWhenMPDebitFails(t *testing.T) {
	resetVitalsResurrectionState()
	vitalsResurrectionState.casterRows = [][]driver.Value{{int64(42)}}
	vitalsResurrectionState.targetAffected = 1
	vitalsResurrectionState.debitErr = errors.New("debit failed")
	committed, err := newVitalsResurrectionRepository(t).CastResurrection(context.Background(), 42, 84, 150, 30, 200, 100)
	if err == nil || committed || !strings.Contains(err.Error(), "debit resurrection caster MP") {
		t.Fatalf("CastResurrection = (%t, %v)", committed, err)
	}
	vitalsResurrectionState.Lock()
	defer vitalsResurrectionState.Unlock()
	if vitalsResurrectionState.committed || !vitalsResurrectionState.rolledBack || len(vitalsResurrectionState.execs) != 2 {
		t.Fatalf("failed debit transaction committed=%t rolledBack=%t execs=%d", vitalsResurrectionState.committed, vitalsResurrectionState.rolledBack, len(vitalsResurrectionState.execs))
	}
}
