package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
)

const teleportRepositoryTestDriverName = "aegiscore-teleport-repository-test"

var teleportRepositoryTestState struct {
	sync.Mutex
	query string
	args  []driver.NamedValue
	rows  [][]driver.Value
}

func init() {
	sql.Register(teleportRepositoryTestDriverName, teleportRepositoryTestDriver{})
}

type teleportRepositoryTestDriver struct{}

func (teleportRepositoryTestDriver) Open(string) (driver.Conn, error) {
	return teleportRepositoryTestConn{}, nil
}

type teleportRepositoryTestConn struct{}

func (teleportRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the teleport repository test driver")
}
func (teleportRepositoryTestConn) Close() error { return nil }
func (teleportRepositoryTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented by the teleport repository test driver")
}
func (teleportRepositoryTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	teleportRepositoryTestState.Lock()
	defer teleportRepositoryTestState.Unlock()
	teleportRepositoryTestState.query = query
	teleportRepositoryTestState.args = append([]driver.NamedValue(nil), args...)
	rows := append([][]driver.Value(nil), teleportRepositoryTestState.rows...)
	return &teleportRepositoryTestRows{rows: rows}, nil
}

type teleportRepositoryTestRows struct {
	rows [][]driver.Value
	at   int
}

func (*teleportRepositoryTestRows) Columns() []string {
	return []string{"ID", "REGION_ID", "POS_X", "POS_Y", "POS_Z", "WORLD_ID", "FEE", "MIN_LEVEL"}
}
func (*teleportRepositoryTestRows) Close() error { return nil }
func (r *teleportRepositoryTestRows) Next(dest []driver.Value) error {
	if r.at >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.at])
	r.at++
	return nil
}

func newTeleportRepositoryForTest(t *testing.T, rows [][]driver.Value) *SQLTeleportRepository {
	t.Helper()
	teleportRepositoryTestState.Lock()
	teleportRepositoryTestState.query = ""
	teleportRepositoryTestState.args = nil
	teleportRepositoryTestState.rows = rows
	teleportRepositoryTestState.Unlock()
	database, err := sql.Open(teleportRepositoryTestDriverName, "teleport-repository")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := NewTeleportRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestReturnTeleportDestinationLoadsCharacterBindPoint(t *testing.T) {
	repository := newTeleportRepositoryForTest(t, [][]driver.Value{{int64(7), int64(25000), float64(969), float64(0), float64(1369), int64(0), int64(0), int64(0)}})
	destination, err := repository.ReturnTeleportDestination(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if destination.ID != 7 || destination.Region != 25000 || destination.X != 969 || destination.Z != 1369 || destination.WorldID != 0 {
		t.Fatalf("destination = %+v", destination)
	}
	teleportRepositoryTestState.Lock()
	query, args := teleportRepositoryTestState.query, append([]driver.NamedValue(nil), teleportRepositoryTestState.args...)
	teleportRepositoryTestState.Unlock()
	if query != selectReturnTeleportDestination || len(args) != 1 || args[0].Value != int64(42) {
		t.Fatalf("return-town query=%q args=%+v", query, args)
	}
}

func TestReturnTeleportDestinationRejectsMissingBindPoint(t *testing.T) {
	repository := newTeleportRepositoryForTest(t, nil)
	if _, err := repository.ReturnTeleportDestination(context.Background(), 42); !errors.Is(err, ErrTeleportPointNotFound) {
		t.Fatalf("missing return point error = %v", err)
	}
}
