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
	"time"
)

const characterRepositoryTestDriverName = "go-sro-character-repository-test"

var characterRepositoryTestState struct {
	sync.Mutex

	characterRows [][]driver.Value
	listRows      [][]driver.Value
	nameRows      [][]driver.Value
	gmRows        [][]driver.Value
	queryErrors   map[string]error
	nextErrors    map[string]error
	execError     error
	affected      int64
	affectedError error
	queries       []characterRepositoryTestCall
	execs         []characterRepositoryTestCall
}

type characterRepositoryTestCall struct {
	query string
	args  []driver.NamedValue
}

func init() {
	sql.Register(characterRepositoryTestDriverName, characterRepositoryTestDriver{})
}

type characterRepositoryTestDriver struct{}

func (characterRepositoryTestDriver) Open(string) (driver.Conn, error) {
	return characterRepositoryTestConn{}, nil
}

type characterRepositoryTestConn struct{}

func (characterRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the character repository test driver")
}

func (characterRepositoryTestConn) Close() error { return nil }

func (characterRepositoryTestConn) Begin() (driver.Tx, error) {
	return characterRepositoryTestTx{}, nil
}

func (characterRepositoryTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	characterRepositoryTestState.Lock()
	defer characterRepositoryTestState.Unlock()
	characterRepositoryTestState.queries = append(characterRepositoryTestState.queries, characterRepositoryTestCall{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	if err := characterRepositoryTestState.queryErrors[query]; err != nil {
		return nil, err
	}

	var (
		source  [][]driver.Value
		columns []string
	)
	switch query {
	case SelectCharByNameAndOwner:
		source = characterRepositoryTestState.characterRows
		columns = characterRepositoryColumnsForLookup()
	case SelectCharsByAccountAndShard:
		source = characterRepositoryTestState.listRows
		columns = characterRepositoryColumnsForList()
	case select_does_charname_exist:
		source = characterRepositoryTestState.nameRows
		columns = []string{"EXISTS"}
	case SelectIsGm:
		source = characterRepositoryTestState.gmRows
		columns = []string{"IS_GM"}
	default:
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	rows := make([][]driver.Value, len(source))
	for index := range source {
		rows[index] = append([]driver.Value(nil), source[index]...)
	}
	return &characterRepositoryTestRows{columns: columns, rows: rows, nextErr: characterRepositoryTestState.nextErrors[query]}, nil
}

func (characterRepositoryTestConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	characterRepositoryTestState.Lock()
	defer characterRepositoryTestState.Unlock()
	characterRepositoryTestState.execs = append(characterRepositoryTestState.execs, characterRepositoryTestCall{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	if query != update_is_deleting_for_owner {
		return nil, fmt.Errorf("unexpected exec %q", query)
	}
	if characterRepositoryTestState.execError != nil {
		return nil, characterRepositoryTestState.execError
	}
	return characterRepositoryTestResult{
		affected: characterRepositoryTestState.affected,
		err:      characterRepositoryTestState.affectedError,
	}, nil
}

type characterRepositoryTestTx struct{}

func (characterRepositoryTestTx) Commit() error   { return nil }
func (characterRepositoryTestTx) Rollback() error { return nil }

type characterRepositoryTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
	nextErr error
}

func (r *characterRepositoryTestRows) Columns() []string {
	return append([]string(nil), r.columns...)
}

func (r *characterRepositoryTestRows) Close() error { return nil }

func (r *characterRepositoryTestRows) Next(dest []driver.Value) error {
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

type characterRepositoryTestResult struct {
	affected int64
	err      error
}

func (r characterRepositoryTestResult) LastInsertId() (int64, error) {
	return 0, errors.New("last insert ID is not implemented by the character repository test driver")
}

func (r characterRepositoryTestResult) RowsAffected() (int64, error) {
	return r.affected, r.err
}

func resetCharacterRepositoryTestState() {
	characterRepositoryTestState.Lock()
	defer characterRepositoryTestState.Unlock()
	characterRepositoryTestState.characterRows = nil
	characterRepositoryTestState.listRows = nil
	characterRepositoryTestState.nameRows = nil
	characterRepositoryTestState.gmRows = nil
	characterRepositoryTestState.queryErrors = make(map[string]error)
	characterRepositoryTestState.nextErrors = make(map[string]error)
	characterRepositoryTestState.execError = nil
	characterRepositoryTestState.affected = 0
	characterRepositoryTestState.affectedError = nil
	characterRepositoryTestState.queries = nil
	characterRepositoryTestState.execs = nil
}

func openCharacterRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(characterRepositoryTestDriverName, "character-repository")
	if err != nil {
		t.Fatalf("open character repository test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func newCharacterRepositoryForTest(t *testing.T) *SQLCharacterRepository {
	t.Helper()
	database := openCharacterRepositoryTestDB(t)
	repository, err := NewCharacterRepository(database, database)
	if err != nil {
		t.Fatalf("NewCharacterRepository: %v", err)
	}
	return repository
}

func TestSQLCharacterRepositoryUsesOwnerAndShardInEveryPrimaryOperation(t *testing.T) {
	resetCharacterRepositoryTestState()
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	characterRepositoryTestState.Lock()
	characterRepositoryTestState.characterRows = [][]driver.Value{characterRepositoryTestCharacterRow(now)}
	characterRepositoryTestState.listRows = [][]driver.Value{characterRepositoryTestListRow(now)}
	characterRepositoryTestState.gmRows = [][]driver.Value{{int64(1)}}
	characterRepositoryTestState.affected = 1
	characterRepositoryTestState.Unlock()

	repository := newCharacterRepositoryForTest(t)
	character, err := repository.GetCharacterForOwner(context.Background(), "Alice", 42, 7)
	if err != nil {
		t.Fatalf("GetCharacterForOwner: %v", err)
	}
	if character.User != 42 || character.Shard != 7 || !character.IsGm {
		t.Fatalf("GetCharacterForOwner = %#v, want owner 42, shard 7, and GM state", character)
	}

	characters, err := repository.GetCharactersForOwner(context.Background(), 42, 7)
	if err != nil {
		t.Fatalf("GetCharactersForOwner: %v", err)
	}
	if len(characters) != 1 || characters[0].User != 42 || characters[0].Shard != 7 {
		t.Fatalf("GetCharactersForOwner = %#v, want one owner-qualified character", characters)
	}

	updated, err := repository.SetCharacterDeletionForOwner(context.Background(), "Alice", 42, 7, true)
	if err != nil || !updated {
		t.Fatalf("SetCharacterDeletionForOwner = (%t, %v), want (true, nil)", updated, err)
	}

	characterRepositoryTestState.Lock()
	queries := append([]characterRepositoryTestCall(nil), characterRepositoryTestState.queries...)
	execs := append([]characterRepositoryTestCall(nil), characterRepositoryTestState.execs...)
	characterRepositoryTestState.Unlock()

	assertCharacterRepositoryCallArgs(t, findCharacterRepositoryCall(t, queries, SelectCharByNameAndOwner).args, "Alice", int64(42), int64(7))
	assertCharacterRepositoryCallArgs(t, findCharacterRepositoryCall(t, queries, SelectCharsByAccountAndShard).args, int64(42), int64(7))
	assertCharacterRepositoryCallArgs(t, findCharacterRepositoryCall(t, execs, update_is_deleting_for_owner).args, int64(1), "Alice", int64(42), int64(7))
}

func TestSQLCharacterRepositoryMapsMissingOwnerScopedCharacterToNotFound(t *testing.T) {
	resetCharacterRepositoryTestState()
	repository := newCharacterRepositoryForTest(t)

	_, err := repository.GetCharacterForOwner(context.Background(), "Unknown", 42, 7)
	if !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("GetCharacterForOwner error = %v, want ErrCharacterNotFound", err)
	}

	characterRepositoryTestState.Lock()
	queries := append([]characterRepositoryTestCall(nil), characterRepositoryTestState.queries...)
	characterRepositoryTestState.Unlock()
	if len(queries) != 1 || queries[0].query != SelectCharByNameAndOwner {
		t.Fatalf("queries = %#v, want only the owner-scoped character lookup", queries)
	}
}

func TestSQLCharacterRepositoryReturnsTypedQueryScanAndRowsErrors(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		resetCharacterRepositoryTestState()
		want := errors.New("shard database unavailable")
		characterRepositoryTestState.Lock()
		characterRepositoryTestState.queryErrors[SelectCharsByAccountAndShard] = want
		characterRepositoryTestState.Unlock()

		_, err := newCharacterRepositoryForTest(t).GetCharactersForOwner(context.Background(), 42, 7)
		assertCharacterRepositoryError(t, err, "query characters for owner", want)
	})

	t.Run("scan", func(t *testing.T) {
		resetCharacterRepositoryTestState()
		characterRepositoryTestState.Lock()
		characterRepositoryTestState.listRows = [][]driver.Value{{"not-an-id"}}
		characterRepositoryTestState.Unlock()

		_, err := newCharacterRepositoryForTest(t).GetCharactersForOwner(context.Background(), 42, 7)
		var repositoryError *CharacterRepositoryError
		if !errors.As(err, &repositoryError) || repositoryError.Operation != "scan character for owner" {
			t.Fatalf("GetCharactersForOwner error = %v, want typed scan error", err)
		}
	})

	t.Run("rows", func(t *testing.T) {
		resetCharacterRepositoryTestState()
		want := errors.New("connection lost while iterating")
		characterRepositoryTestState.Lock()
		characterRepositoryTestState.nextErrors[SelectCharsByAccountAndShard] = want
		characterRepositoryTestState.Unlock()

		_, err := newCharacterRepositoryForTest(t).GetCharactersForOwner(context.Background(), 42, 7)
		assertCharacterRepositoryError(t, err, "iterate characters for owner", want)
	})
}

func TestSQLCharacterRepositoryReturnsTypedMutationFailure(t *testing.T) {
	resetCharacterRepositoryTestState()
	want := errors.New("write failed")
	characterRepositoryTestState.Lock()
	characterRepositoryTestState.execError = want
	characterRepositoryTestState.Unlock()

	updated, err := newCharacterRepositoryForTest(t).SetCharacterDeletionForOwner(context.Background(), "Alice", 42, 7, true)
	if updated {
		t.Fatal("SetCharacterDeletionForOwner reported a mutation after an exec failure")
	}
	assertCharacterRepositoryError(t, err, "update character deletion state for owner", want)
}

func TestSQLCharacterRepositoryChecksCharacterNameThroughInjectedShardPool(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		resetCharacterRepositoryTestState()
		characterRepositoryTestState.Lock()
		characterRepositoryTestState.nameRows = [][]driver.Value{{int64(1)}}
		characterRepositoryTestState.Unlock()

		exists, err := newCharacterRepositoryForTest(t).CharacterNameExists(context.Background(), "Alice")
		if err != nil || !exists {
			t.Fatalf("CharacterNameExists = (%t, %v), want (true, nil)", exists, err)
		}
		characterRepositoryTestState.Lock()
		queries := append([]characterRepositoryTestCall(nil), characterRepositoryTestState.queries...)
		characterRepositoryTestState.Unlock()
		assertCharacterRepositoryCallArgs(t, findCharacterRepositoryCall(t, queries, select_does_charname_exist).args, "Alice")
	})

	t.Run("missing", func(t *testing.T) {
		resetCharacterRepositoryTestState()
		exists, err := newCharacterRepositoryForTest(t).CharacterNameExists(context.Background(), "Unknown")
		if err != nil || exists {
			t.Fatalf("CharacterNameExists = (%t, %v), want (false, nil)", exists, err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		resetCharacterRepositoryTestState()
		want := errors.New("shard database unavailable")
		characterRepositoryTestState.Lock()
		characterRepositoryTestState.queryErrors[select_does_charname_exist] = want
		characterRepositoryTestState.Unlock()

		_, err := newCharacterRepositoryForTest(t).CharacterNameExists(context.Background(), "Alice")
		assertCharacterRepositoryError(t, err, "query character name", want)
	})
}

func TestSQLCharacterRepositoryFailsClosedWithoutInjectedDependencies(t *testing.T) {
	repository, err := NewCharacterRepository(nil, nil)
	if repository != nil {
		t.Fatal("NewCharacterRepository returned a repository without a shard database")
	}
	assertCharacterRepositoryError(t, err, "configure shard database", ErrCharacterRepositoryUnavailable)

	resetCharacterRepositoryTestState()
	database := openCharacterRepositoryTestDB(t)
	repository, err = NewCharacterRepository(database, nil)
	if err != nil {
		t.Fatalf("NewCharacterRepository(list/delete only): %v", err)
	}
	characterRepositoryTestState.Lock()
	characterRepositoryTestState.characterRows = [][]driver.Value{characterRepositoryTestCharacterRow(time.Now())}
	characterRepositoryTestState.Unlock()
	if _, err := repository.GetCharacterForOwner(context.Background(), "Alice", 42, 7); !errors.Is(err, ErrCharacterRepositoryUnavailable) {
		t.Fatalf("GetCharacterForOwner without account queryer error = %v, want unavailable", err)
	}
}

func characterRepositoryTestCharacterRow(now time.Time) []driver.Value {
	return []driver.Value{
		int64(100), int64(200), int64(42), int64(7), "Alice", int64(1), int64(10), int64(1000), int64(50), int64(12), int64(13), int64(4), int64(100), int64(80), int64(0), float64(1.5), float64(2.5), float64(3.5), now, now, int64(25000), int64(9), int64(1_000_000_000), int64(120), int64(45),
	}
}

func characterRepositoryTestListRow(now time.Time) []driver.Value {
	return []driver.Value{
		int64(100), int64(200), int64(7), "Alice", int64(1), int64(10), int64(1000), int64(50), int64(12), int64(13), int64(4), int64(100), int64(80), int64(0), now, int64(25000), int64(45),
	}
}

func characterRepositoryColumnsForLookup() []string {
	return []string{
		"ID", "REF_OBJ_ID", "FK_USER", "FK_SHARD", "CHAR_NAME", "CHAR_SCALE", "CURRENT_LEVEL", "EXP", "SKILL_EXP", "STRENGTH", "INTELLECT", "STAT_POINTS", "HP", "MP", "DELETING", "POS_X", "POS_Y", "POS_Z", "CTIME", "UTIME", "FK_REGION", "SKILL_POINTS", "GOLD", "MAX_LEVEL", "INVENTORY_SIZE",
	}
}

func characterRepositoryColumnsForList() []string {
	return []string{
		"ID", "REF_OBJ_ID", "FK_SHARD", "CHAR_NAME", "CHAR_SCALE", "CURRENT_LEVEL", "EXP", "SKILL_EXP", "STRENGTH", "INTELLECT", "STAT_POINTS", "HP", "MP", "DELETING", "UTIME", "FK_REGION", "INVENTORY_SIZE",
	}
}

func findCharacterRepositoryCall(t *testing.T, calls []characterRepositoryTestCall, query string) characterRepositoryTestCall {
	t.Helper()
	for _, call := range calls {
		if call.query == query {
			return call
		}
	}
	t.Fatalf("no call for query %q in %#v", query, calls)
	return characterRepositoryTestCall{}
}

func assertCharacterRepositoryCallArgs(t *testing.T, args []driver.NamedValue, expected ...any) {
	t.Helper()
	if len(args) != len(expected) {
		t.Fatalf("argument count = %d, want %d (%#v)", len(args), len(expected), args)
	}
	for index := range expected {
		got := args[index].Value
		if wantString, ok := expected[index].(string); ok {
			if got != wantString {
				t.Fatalf("argument %d = %#v, want %q", index, got, wantString)
			}
			continue
		}
		want, ok := expected[index].(int64)
		if !ok {
			t.Fatalf("unsupported expected argument type %T", expected[index])
		}
		if value, ok := characterRepositoryInteger(got); !ok || value != want {
			t.Fatalf("argument %d = %#v, want numeric %d", index, got, want)
		}
	}
}

func characterRepositoryInteger(value any) (int64, bool) {
	switch value := value.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint16:
		return int64(value), true
	default:
		return 0, false
	}
}

func assertCharacterRepositoryError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	var repositoryError *CharacterRepositoryError
	if !errors.As(err, &repositoryError) {
		t.Fatalf("error %T, want *CharacterRepositoryError", err)
	}
	if repositoryError.Operation != operation {
		t.Fatalf("repository operation = %q, want %q", repositoryError.Operation, operation)
	}
}
