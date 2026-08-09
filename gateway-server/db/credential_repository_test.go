package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/framework/security/hashing"
)

const credentialRepositoryTestDriverName = "aegiscore-gateway-credential-repository-test"

var credentialRepositoryTestState struct {
	sync.Mutex

	rows     [][]driver.Value
	queryErr error
	queries  []string
	args     [][]driver.NamedValue
}

func init() {
	sql.Register(credentialRepositoryTestDriverName, credentialRepositoryTestDriver{})
}

type credentialRepositoryTestDriver struct{}

func (credentialRepositoryTestDriver) Open(string) (driver.Conn, error) {
	return credentialRepositoryTestConn{}, nil
}

type credentialRepositoryTestConn struct{}

func (credentialRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the credential repository test driver")
}

func (credentialRepositoryTestConn) Close() error { return nil }

func (credentialRepositoryTestConn) Begin() (driver.Tx, error) {
	return credentialRepositoryTestTx{}, nil
}

func (credentialRepositoryTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credentialRepositoryTestState.Lock()
	defer credentialRepositoryTestState.Unlock()
	credentialRepositoryTestState.queries = append(credentialRepositoryTestState.queries, query)
	credentialRepositoryTestState.args = append(credentialRepositoryTestState.args, append([]driver.NamedValue(nil), args...))
	if credentialRepositoryTestState.queryErr != nil {
		return nil, credentialRepositoryTestState.queryErr
	}
	rows := make([][]driver.Value, len(credentialRepositoryTestState.rows))
	for index, row := range credentialRepositoryTestState.rows {
		rows[index] = append([]driver.Value(nil), row...)
	}
	return &credentialRepositoryTestRows{rows: rows}, nil
}

type credentialRepositoryTestTx struct{}

func (credentialRepositoryTestTx) Commit() error   { return nil }
func (credentialRepositoryTestTx) Rollback() error { return nil }

type credentialRepositoryTestRows struct {
	rows  [][]driver.Value
	index int
}

func (r *credentialRepositoryTestRows) Columns() []string { return []string{"ID", "PASSWORD"} }
func (r *credentialRepositoryTestRows) Close() error      { return nil }

func (r *credentialRepositoryTestRows) Next(destination []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(destination, r.rows[r.index])
	r.index++
	return nil
}

func resetCredentialRepositoryTestState() {
	credentialRepositoryTestState.Lock()
	defer credentialRepositoryTestState.Unlock()
	credentialRepositoryTestState.rows = nil
	credentialRepositoryTestState.queryErr = nil
	credentialRepositoryTestState.queries = nil
	credentialRepositoryTestState.args = nil
}

func openCredentialRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(credentialRepositoryTestDriverName, "gateway-credentials")
	if err != nil {
		t.Fatalf("open credential repository test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestAccountCredentialRepositoryAuthenticate(t *testing.T) {
	hash, err := hashing.GenerateHash("correct-password")
	if err != nil {
		t.Fatalf("generate password hash: %v", err)
	}

	testCases := []struct {
		name       string
		rows       [][]driver.Value
		password   string
		wantID     uint32
		wantValid  bool
		queryError error
	}{
		{name: "matching password", rows: [][]driver.Value{{int64(77), hash}}, password: "correct-password", wantID: 77, wantValid: true},
		{name: "wrong password", rows: [][]driver.Value{{int64(77), hash}}, password: "wrong-password"},
		{name: "missing account", password: "correct-password"},
		{name: "database failure", password: "correct-password", queryError: errors.New("database unavailable")},
		{name: "invalid account id", rows: [][]driver.Value{{int64(0), hash}}, password: "correct-password"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resetCredentialRepositoryTestState()
			credentialRepositoryTestState.Lock()
			credentialRepositoryTestState.rows = testCase.rows
			credentialRepositoryTestState.queryErr = testCase.queryError
			credentialRepositoryTestState.Unlock()

			repository, err := NewAccountCredentialRepository(openCredentialRepositoryTestDB(t))
			if err != nil {
				t.Fatalf("NewAccountCredentialRepository: %v", err)
			}
			accountID, valid, gotErr := repository.Authenticate(context.Background(), "Alice", testCase.password)
			if testCase.queryError != nil {
				if !errors.Is(gotErr, testCase.queryError) {
					t.Fatalf("Authenticate error = %v, want wrapped %v", gotErr, testCase.queryError)
				}
				return
			}
			if gotErr != nil {
				t.Fatalf("Authenticate error = %v", gotErr)
			}
			if accountID != testCase.wantID || valid != testCase.wantValid {
				t.Fatalf("Authenticate = (%d, %t), want (%d, %t)", accountID, valid, testCase.wantID, testCase.wantValid)
			}
		})
	}

	credentialRepositoryTestState.Lock()
	defer credentialRepositoryTestState.Unlock()
	if len(credentialRepositoryTestState.queries) == 0 || credentialRepositoryTestState.queries[0] != select_password {
		t.Fatalf("credential query = %#v, want %q", credentialRepositoryTestState.queries, select_password)
	}
	if len(credentialRepositoryTestState.args[0]) != 1 || credentialRepositoryTestState.args[0][0].Value != "Alice" {
		t.Fatalf("credential query arguments = %#v, want username Alice", credentialRepositoryTestState.args[0])
	}
}

func TestAccountCredentialRepositoryHonorsCanceledContextAndUnavailablePool(t *testing.T) {
	if repository, err := NewAccountCredentialRepository(nil); repository != nil || !errors.Is(err, ErrCredentialRepositoryUnavailable) {
		t.Fatalf("nil repository construction = (%v, %v), want unavailable error", repository, err)
	}

	resetCredentialRepositoryTestState()
	repository, err := NewAccountCredentialRepository(openCredentialRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewAccountCredentialRepository: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := repository.Authenticate(ctx, "Alice", "correct-password"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Authenticate error = %v, want context.Canceled", err)
	}
}

func TestAccountCredentialRepositoryRedactsDriverError(t *testing.T) {
	resetCredentialRepositoryTestState()
	secret := "mysql://operator:database-secret@db.internal:3306/account"
	underlying := errors.New("query failed for " + secret)
	credentialRepositoryTestState.Lock()
	credentialRepositoryTestState.queryErr = underlying
	credentialRepositoryTestState.Unlock()

	repository, err := NewAccountCredentialRepository(openCredentialRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewAccountCredentialRepository: %v", err)
	}
	_, _, err = repository.Authenticate(context.Background(), "Alice", "correct-password")
	if err == nil {
		t.Fatal("Authenticate unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential repository error leaked connection detail: %q", err)
	}
	if !errors.Is(err, underlying) {
		t.Fatal("credential repository error no longer unwraps the driver error")
	}
}
