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
	"testing"
)

const magicCubeTestDriverName = "aegis-magic-cube-test"

var (
	magicCubeDriverOnce sync.Once
	magicCubeScriptMu   sync.Mutex
	magicCubeScript     *magicCubeTestScript
)

type magicCubeTestStep struct {
	contains string
	columns  []string
	rows     [][]driver.Value
	affected int64
	insertID int64
	err      error
}

type magicCubeTestScript struct {
	steps              []magicCubeTestStep
	commits, rollbacks int
}

type magicCubeTestDriver struct{}
type magicCubeTestConn struct{ script *magicCubeTestScript }
type magicCubeTestTx struct{ script *magicCubeTestScript }
type magicCubeTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}
type magicCubeTestResult struct{ affected, insertID int64 }

func (magicCubeTestDriver) Open(string) (driver.Conn, error) {
	magicCubeScriptMu.Lock()
	defer magicCubeScriptMu.Unlock()
	if magicCubeScript == nil {
		return nil, errors.New("missing magic cube test script")
	}
	return &magicCubeTestConn{script: magicCubeScript}, nil
}
func (c *magicCubeTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *magicCubeTestConn) Close() error                        { return nil }
func (c *magicCubeTestConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *magicCubeTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &magicCubeTestTx{script: c.script}, nil
}
func (c *magicCubeTestConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (c *magicCubeTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	step, err := c.script.pop(query)
	if err != nil || step.err != nil {
		if err != nil {
			return nil, err
		}
		return nil, step.err
	}
	return &magicCubeTestRows{columns: step.columns, rows: step.rows}, nil
}
func (c *magicCubeTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	step, err := c.script.pop(query)
	if err != nil || step.err != nil {
		if err != nil {
			return nil, err
		}
		return nil, step.err
	}
	return magicCubeTestResult{affected: step.affected, insertID: step.insertID}, nil
}
func (s *magicCubeTestScript) pop(query string) (magicCubeTestStep, error) {
	if len(s.steps) == 0 {
		return magicCubeTestStep{}, fmt.Errorf("unexpected SQL: %s", query)
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if !strings.Contains(query, step.contains) {
		return magicCubeTestStep{}, fmt.Errorf("SQL %q does not contain %q", query, step.contains)
	}
	return step, nil
}
func (t *magicCubeTestTx) Commit() error       { t.script.commits++; return nil }
func (t *magicCubeTestTx) Rollback() error     { t.script.rollbacks++; return nil }
func (r *magicCubeTestRows) Columns() []string { return r.columns }
func (r *magicCubeTestRows) Close() error      { return nil }
func (r *magicCubeTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
func (r magicCubeTestResult) LastInsertId() (int64, error) { return r.insertID, nil }
func (r magicCubeTestResult) RowsAffected() (int64, error) { return r.affected, nil }

func magicCubeQuery(contains string, rows ...[]driver.Value) magicCubeTestStep {
	columnCount := 1
	if len(rows) > 0 {
		columnCount = len(rows[0])
	}
	columns := make([]string, columnCount)
	for i := range columns {
		columns[i] = fmt.Sprintf("c%d", i)
	}
	return magicCubeTestStep{contains: contains, columns: columns, rows: rows}
}
func magicCubeExec(contains string, affected, insertID int64) magicCubeTestStep {
	return magicCubeTestStep{contains: contains, affected: affected, insertID: insertID}
}
func magicCubePrefix(sourceRow []driver.Value, destinationRows ...[]driver.Value) []magicCubeTestStep {
	steps := []magicCubeTestStep{
		magicCubeQuery("SELECT `FK_USER`,`INVENTORY_SIZE`", []driver.Value{int64(9), int64(40)}),
		magicCubeQuery("ref.`Param9`", []driver.Value{int64(2)}),
		magicCubeExec("INSERT IGNORE INTO `MAGIC_CUBE_INVENTORY`", 1, 0),
		magicCubeExec("INSERT IGNORE INTO `MAGIC_CUBE_INVENTORY`", 1, 0),
		magicCubeQuery("ORDER BY `SLOT` FOR UPDATE", []driver.Value{int64(0), nil}, []driver.Value{int64(1), nil}),
		magicCubeQuery("item.`STACK_COUNT`", sourceRow),
	}
	steps = append(steps, magicCubeQuery("ref.`Stack_Size`", destinationRows...))
	return steps
}
func runMagicCubeScript(t *testing.T, steps []magicCubeTestStep, call func(*SQLMagicCubeRepository) error) *magicCubeTestScript {
	t.Helper()
	magicCubeDriverOnce.Do(func() { sql.Register(magicCubeTestDriverName, magicCubeTestDriver{}) })
	script := &magicCubeTestScript{steps: steps}
	magicCubeScriptMu.Lock()
	magicCubeScript = script
	magicCubeScriptMu.Unlock()
	db, err := sql.Open(magicCubeTestDriverName, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); magicCubeScriptMu.Lock(); magicCubeScript = nil; magicCubeScriptMu.Unlock() })
	repository, err := NewMagicCubeRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = call(repository); err != nil {
		t.Fatal(err)
	}
	if len(script.steps) != 0 {
		t.Fatalf("%d SQL steps remain", len(script.steps))
	}
	return script
}

func TestMagicCubePushWholeStackRelinksAndCommits(t *testing.T) {
	steps := magicCubePrefix([]driver.Value{int64(100), int64(200), int64(5), int64(10)})
	steps = append(steps,
		magicCubeExec("DELETE FROM `INVENTORY`", 1, 0),
		magicCubeExec("UPDATE `MAGIC_CUBE_INVENTORY` SET `FK_ITEM`=?", 1, 0),
		magicCubeQuery("FROM `SRO_SHARD`.`INVENTORY`"),
	)
	script := runMagicCubeScript(t, steps, func(r *SQLMagicCubeRepository) error {
		_, err := r.Push(context.Background(), 44, 9, 555, 14, 1, 5)
		return err
	})
	if script.commits != 1 || script.rollbacks != 0 {
		t.Fatalf("commit/rollback = %d/%d", script.commits, script.rollbacks)
	}
}

func TestMagicCubePushPartialStackSplitsItem(t *testing.T) {
	steps := magicCubePrefix([]driver.Value{int64(100), int64(200), int64(5), int64(10)})
	steps = append(steps,
		magicCubeExec("INSERT INTO `ITEM`", 1, 101),
		magicCubeExec("UPDATE `ITEM` SET `DATA`=IF", 1, 0),
		magicCubeExec("UPDATE `MAGIC_CUBE_INVENTORY` SET `FK_ITEM`=?", 1, 0),
		magicCubeQuery("FROM `SRO_SHARD`.`INVENTORY`"),
	)
	script := runMagicCubeScript(t, steps, func(r *SQLMagicCubeRepository) error {
		result, err := r.Push(context.Background(), 44, 9, 555, 14, 1, 2)
		if err == nil && result.ItemID != 101 {
			t.Fatalf("split item ID = %d", result.ItemID)
		}
		return err
	})
	if script.commits != 1 {
		t.Fatalf("commits = %d", script.commits)
	}
}

func TestMagicCubePushMergesAndDeletesConsumedSource(t *testing.T) {
	steps := magicCubePrefix(
		[]driver.Value{int64(100), int64(200), int64(2), int64(10)},
		[]driver.Value{int64(101), int64(200), int64(3), int64(10)},
	)
	steps = append(steps,
		magicCubeExec("UPDATE `ITEM` SET `DATA`=IF", 1, 0),
		magicCubeExec("DELETE FROM `INVENTORY`", 1, 0),
		magicCubeExec("DELETE FROM `ITEM`", 1, 0),
		magicCubeQuery("FROM `SRO_SHARD`.`INVENTORY`"),
	)
	script := runMagicCubeScript(t, steps, func(r *SQLMagicCubeRepository) error {
		result, err := r.Push(context.Background(), 44, 9, 555, 14, 1, 2)
		if err == nil && result.ItemID != 101 {
			t.Fatalf("merged item ID = %d", result.ItemID)
		}
		return err
	})
	if script.commits != 1 {
		t.Fatalf("commits = %d", script.commits)
	}
}

func TestMagicCubePopWholeStackRelinksToNormalInventory(t *testing.T) {
	steps := magicCubePrefix([]driver.Value{int64(100), int64(200), int64(5), int64(10)})
	steps = append(steps,
		magicCubeExec("UPDATE `MAGIC_CUBE_INVENTORY` SET `FK_ITEM`=NULL", 1, 0),
		magicCubeExec("INSERT INTO `INVENTORY`", 1, 0),
		magicCubeQuery("FROM `SRO_SHARD`.`INVENTORY`"),
	)
	script := runMagicCubeScript(t, steps, func(r *SQLMagicCubeRepository) error {
		_, err := r.Pop(context.Background(), 44, 9, 555, 1, 14, 5)
		return err
	})
	if script.commits != 1 || script.rollbacks != 0 {
		t.Fatalf("commit/rollback = %d/%d", script.commits, script.rollbacks)
	}
}

func TestMagicCubeStaleDestinationRollsBack(t *testing.T) {
	steps := magicCubePrefix([]driver.Value{int64(100), int64(200), int64(5), int64(10)})
	steps = append(steps,
		magicCubeExec("DELETE FROM `INVENTORY`", 1, 0),
		magicCubeExec("UPDATE `MAGIC_CUBE_INVENTORY` SET `FK_ITEM`=?", 0, 0),
	)
	magicCubeDriverOnce.Do(func() { sql.Register(magicCubeTestDriverName, magicCubeTestDriver{}) })
	script := &magicCubeTestScript{steps: steps}
	magicCubeScriptMu.Lock()
	magicCubeScript = script
	magicCubeScriptMu.Unlock()
	db, _ := sql.Open(magicCubeTestDriverName, t.Name())
	defer db.Close()
	repository, _ := NewMagicCubeRepository(db)
	_, err := repository.Push(context.Background(), 44, 9, 555, 14, 1, 5)
	if !errors.Is(err, ErrMagicCubeConflict) || script.commits != 0 || script.rollbacks != 1 {
		t.Fatalf("error/commit/rollback = %v/%d/%d", err, script.commits, script.rollbacks)
	}
	magicCubeScriptMu.Lock()
	magicCubeScript = nil
	magicCubeScriptMu.Unlock()
}
