package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

const characterPositionRepositoryTestDriverName = "aegiscore-character-position-repository-test"

var characterPositionRepositoryTestState struct {
	sync.Mutex

	execError error
	execs     []characterPositionRepositoryTestCall
}

type characterPositionRepositoryTestCall struct {
	query string
	args  []driver.NamedValue
}

func init() {
	sql.Register(characterPositionRepositoryTestDriverName, characterPositionRepositoryTestDriver{})
}

type characterPositionRepositoryTestDriver struct{}

func (characterPositionRepositoryTestDriver) Open(string) (driver.Conn, error) {
	return characterPositionRepositoryTestConn{}, nil
}

type characterPositionRepositoryTestConn struct{}

func (characterPositionRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the character position repository test driver")
}

func (characterPositionRepositoryTestConn) Close() error { return nil }

func (characterPositionRepositoryTestConn) Begin() (driver.Tx, error) {
	return characterPositionRepositoryTestTx{}, nil
}

func (characterPositionRepositoryTestConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	characterPositionRepositoryTestState.Lock()
	defer characterPositionRepositoryTestState.Unlock()
	characterPositionRepositoryTestState.execs = append(characterPositionRepositoryTestState.execs, characterPositionRepositoryTestCall{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	if query != update_character_position {
		return nil, fmt.Errorf("unexpected exec %q", query)
	}
	if characterPositionRepositoryTestState.execError != nil {
		return nil, characterPositionRepositoryTestState.execError
	}
	return driver.RowsAffected(1), nil
}

type characterPositionRepositoryTestTx struct{}

func (characterPositionRepositoryTestTx) Commit() error   { return nil }
func (characterPositionRepositoryTestTx) Rollback() error { return nil }

func resetCharacterPositionRepositoryTestState() {
	characterPositionRepositoryTestState.Lock()
	defer characterPositionRepositoryTestState.Unlock()
	characterPositionRepositoryTestState.execError = nil
	characterPositionRepositoryTestState.execs = nil
}

func openCharacterPositionRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(characterPositionRepositoryTestDriverName, "character-position-repository")
	if err != nil {
		t.Fatalf("open character position repository test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func newCharacterPositionRepositoryForTest(t *testing.T) *SQLCharacterPositionRepository {
	t.Helper()
	repository, err := NewCharacterPositionRepository(openCharacterPositionRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewCharacterPositionRepository: %v", err)
	}
	return repository
}

func TestSQLCharacterPositionRepositoryUsesInjectedDBTX(t *testing.T) {
	resetCharacterPositionRepositoryTestState()
	repository := newCharacterPositionRepositoryForTest(t)

	if err := repository.UpdateCharacterPosition(context.Background(), 99, 25000, 1.5, -2.25, 3.75); err != nil {
		t.Fatalf("UpdateCharacterPosition: %v", err)
	}

	characterPositionRepositoryTestState.Lock()
	execs := append([]characterPositionRepositoryTestCall(nil), characterPositionRepositoryTestState.execs...)
	characterPositionRepositoryTestState.Unlock()
	if len(execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(execs))
	}
	if execs[0].query != update_character_position {
		t.Fatalf("query = %q, want %q", execs[0].query, update_character_position)
	}
	assertCharacterPositionRepositoryArgs(t, execs[0].args, 1.5, -2.25, 3.75, int64(25000), int64(99))
}

func TestSQLCharacterPositionRepositoryReturnsTypedSanitizedWriteError(t *testing.T) {
	resetCharacterPositionRepositoryTestState()
	want := errors.New("mysql://operator:secret@database.example:3306 write refused")
	characterPositionRepositoryTestState.Lock()
	characterPositionRepositoryTestState.execError = want
	characterPositionRepositoryTestState.Unlock()

	err := newCharacterPositionRepositoryForTest(t).UpdateCharacterPosition(context.Background(), 99, 25000, 1, 2, 3)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped driver error", err)
	}
	var repositoryError *CharacterPositionRepositoryError
	if !errors.As(err, &repositoryError) {
		t.Fatalf("error %T, want *CharacterPositionRepositoryError", err)
	}
	if repositoryError.Operation != "update character position" {
		t.Fatalf("operation = %q, want update character position", repositoryError.Operation)
	}
	if strings.Contains(err.Error(), "mysql://") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("repository error leaked driver detail: %q", err)
	}
}

func TestSQLCharacterPositionRepositoryRejectsUnavailableAndCanceledDependencies(t *testing.T) {
	repository, err := NewCharacterPositionRepository(nil)
	if repository != nil {
		t.Fatal("NewCharacterPositionRepository returned a repository without a shard database")
	}
	assertCharacterPositionRepositoryError(t, err, "configure shard database", ErrCharacterPositionRepositoryUnavailable)

	resetCharacterPositionRepositoryTestState()
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	err = newCharacterPositionRepositoryForTest(t).UpdateCharacterPosition(canceledContext, 99, 25000, 1, 2, 3)
	assertCharacterPositionRepositoryError(t, err, "update character position", context.Canceled)

	characterPositionRepositoryTestState.Lock()
	execCount := len(characterPositionRepositoryTestState.execs)
	characterPositionRepositoryTestState.Unlock()
	if execCount != 0 {
		t.Fatalf("canceled context reached database exec %d times", execCount)
	}
}

func TestSQLCharacterPositionRepositoryRejectsInvalidCharacterID(t *testing.T) {
	resetCharacterPositionRepositoryTestState()
	err := newCharacterPositionRepositoryForTest(t).UpdateCharacterPosition(context.Background(), 0, 25000, 1, 2, 3)
	if !errors.Is(err, ErrCharacterPositionIDRequired) {
		t.Fatalf("error = %v, want ErrCharacterPositionIDRequired", err)
	}
}

func assertCharacterPositionRepositoryArgs(t *testing.T, args []driver.NamedValue, expected ...any) {
	t.Helper()
	if len(args) != len(expected) {
		t.Fatalf("argument count = %d, want %d (%#v)", len(args), len(expected), args)
	}
	for index := range expected {
		switch want := expected[index].(type) {
		case float64:
			got, ok := characterPositionRepositoryFloat(args[index].Value)
			if !ok || got != want {
				t.Fatalf("argument %d = %#v, want %v", index, args[index].Value, want)
			}
		case int64:
			got, ok := characterPositionRepositoryInteger(args[index].Value)
			if !ok || got != want {
				t.Fatalf("argument %d = %#v, want %d", index, args[index].Value, want)
			}
		default:
			t.Fatalf("unsupported expected argument type %T", expected[index])
		}
	}
}

func characterPositionRepositoryFloat(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	default:
		return 0, false
	}
}

func characterPositionRepositoryInteger(value any) (int64, bool) {
	switch value := value.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint16:
		return int64(value), true
	default:
		return 0, false
	}
}

func assertCharacterPositionRepositoryError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	var repositoryError *CharacterPositionRepositoryError
	if !errors.As(err, &repositoryError) {
		t.Fatalf("error %T, want *CharacterPositionRepositoryError", err)
	}
	if repositoryError.Operation != operation {
		t.Fatalf("repository operation = %q, want %q", repositoryError.Operation, operation)
	}
}
