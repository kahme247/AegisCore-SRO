package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
)

const goldDropRateTestDriverName = "aegiscore-gold-drop-rate-test"

var goldDropRateTestState struct {
	sync.Mutex

	rows     [][]driver.Value
	queryErr error
	nextErr  error
	queries  []string
}

func init() {
	sql.Register(goldDropRateTestDriverName, goldDropRateTestDriver{})
}

type goldDropRateTestDriver struct{}

func (goldDropRateTestDriver) Open(string) (driver.Conn, error) {
	return goldDropRateTestConn{}, nil
}

type goldDropRateTestConn struct{}

func (goldDropRateTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the gold drop rate test driver")
}

func (goldDropRateTestConn) Close() error { return nil }

func (goldDropRateTestConn) Begin() (driver.Tx, error) {
	return goldDropRateTestTx{}, nil
}

func (goldDropRateTestConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	goldDropRateTestState.Lock()
	defer goldDropRateTestState.Unlock()
	goldDropRateTestState.queries = append(goldDropRateTestState.queries, query)
	if goldDropRateTestState.queryErr != nil {
		return nil, goldDropRateTestState.queryErr
	}

	rows := make([][]driver.Value, len(goldDropRateTestState.rows))
	for index, row := range goldDropRateTestState.rows {
		rows[index] = append([]driver.Value(nil), row...)
	}
	return &goldDropRateTestRows{rows: rows, nextErr: goldDropRateTestState.nextErr}, nil
}

type goldDropRateTestTx struct{}

func (goldDropRateTestTx) Commit() error   { return nil }
func (goldDropRateTestTx) Rollback() error { return nil }

type goldDropRateTestRows struct {
	rows    [][]driver.Value
	index   int
	nextErr error
}

func (r *goldDropRateTestRows) Columns() []string {
	return []string{"MOB_LVL", "PROB", "MIN", "MAX"}
}

func (r *goldDropRateTestRows) Close() error { return nil }

func (r *goldDropRateTestRows) Next(dest []driver.Value) error {
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

func resetGoldDropRateTestState() {
	goldDropRateTestState.Lock()
	goldDropRateTestState.rows = nil
	goldDropRateTestState.queryErr = nil
	goldDropRateTestState.nextErr = nil
	goldDropRateTestState.queries = nil
	goldDropRateTestState.Unlock()
}

func openGoldDropRateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(goldDropRateTestDriverName, "gold-drop-rates")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestGoldDropRateRepositoryList(t *testing.T) {
	resetGoldDropRateTestState()
	goldDropRateTestState.Lock()
	goldDropRateTestState.rows = [][]driver.Value{
		{int64(1), float64(0.5), int64(2), int64(5)},
		{int64(2), float64(0.75), int64(3), int64(8)},
	}
	goldDropRateTestState.Unlock()

	repository, err := NewGoldDropRateRepository(openGoldDropRateTestDB(t))
	if err != nil {
		t.Fatalf("NewGoldDropRateRepository: %v", err)
	}

	rates, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rates) != 2 {
		t.Fatalf("rate count = %d, want 2", len(rates))
	}
	if got := rates[2]; got != (DropRatesGold{MobLvl: 2, Prob: 0.75, Min: 3, Max: 8}) {
		t.Fatalf("rate[2] = %#v", got)
	}

	goldDropRateTestState.Lock()
	queries := append([]string(nil), goldDropRateTestState.queries...)
	goldDropRateTestState.Unlock()
	if len(queries) != 1 || queries[0] != select_drop_rates_gold {
		t.Fatalf("queries = %#v, want the explicit gold drop rate query", queries)
	}
}

func TestGoldDropRateRepositoryReturnsTypedQueryAndRowErrors(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		resetGoldDropRateTestState()
		want := errors.New("database unavailable")
		goldDropRateTestState.Lock()
		goldDropRateTestState.queryErr = want
		goldDropRateTestState.Unlock()

		repository, err := NewGoldDropRateRepository(openGoldDropRateTestDB(t))
		if err != nil {
			t.Fatalf("NewGoldDropRateRepository: %v", err)
		}
		_, err = repository.List(context.Background())
		assertGoldDropRateError(t, err, "query", want)
	})

	t.Run("scan", func(t *testing.T) {
		resetGoldDropRateTestState()
		goldDropRateTestState.Lock()
		goldDropRateTestState.rows = [][]driver.Value{{"not-an-integer", float64(0.5), int64(1), int64(2)}}
		goldDropRateTestState.Unlock()

		repository, err := NewGoldDropRateRepository(openGoldDropRateTestDB(t))
		if err != nil {
			t.Fatalf("NewGoldDropRateRepository: %v", err)
		}
		_, err = repository.List(context.Background())
		var repositoryError *GoldDropRateRepositoryError
		if !errors.As(err, &repositoryError) || repositoryError.Operation != "scan" {
			t.Fatalf("List error = %v, want typed scan error", err)
		}
	})

	t.Run("rows", func(t *testing.T) {
		resetGoldDropRateTestState()
		want := errors.New("connection lost while iterating")
		goldDropRateTestState.Lock()
		goldDropRateTestState.nextErr = want
		goldDropRateTestState.Unlock()

		repository, err := NewGoldDropRateRepository(openGoldDropRateTestDB(t))
		if err != nil {
			t.Fatalf("NewGoldDropRateRepository: %v", err)
		}
		_, err = repository.List(context.Background())
		assertGoldDropRateError(t, err, "iterate", want)
	})
}

func TestGoldDropRateRepositoryRejectsNilQueryer(t *testing.T) {
	repository, err := NewGoldDropRateRepository(nil)
	if repository != nil {
		t.Fatal("NewGoldDropRateRepository returned a repository for a nil queryer")
	}
	assertGoldDropRateError(t, err, "configure", ErrGoldDropRateRepositoryUnavailable)
}

func assertGoldDropRateError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	var repositoryError *GoldDropRateRepositoryError
	if !errors.As(err, &repositoryError) {
		t.Fatalf("error %T, want *GoldDropRateRepositoryError", err)
	}
	if repositoryError.Operation != operation {
		t.Fatalf("repository operation = %q, want %q", repositoryError.Operation, operation)
	}
}
