package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
)

const referenceDataRepositoryTestDriverName = "go-sro-agent-reference-data-repository-test"

var referenceDataRepositoryTestState struct {
	sync.Mutex

	responses map[string]referenceDataRepositoryTestResponse
	queries   []string
}

type referenceDataRepositoryTestResponse struct {
	columns  []string
	rows     [][]driver.Value
	queryErr error
	nextErr  error
}

func init() {
	sql.Register(referenceDataRepositoryTestDriverName, referenceDataRepositoryTestDriver{})
}

type referenceDataRepositoryTestDriver struct{}

func (referenceDataRepositoryTestDriver) Open(string) (driver.Conn, error) {
	return referenceDataRepositoryTestConn{}, nil
}

type referenceDataRepositoryTestConn struct{}

func (referenceDataRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the reference-data repository test driver")
}

func (referenceDataRepositoryTestConn) Close() error { return nil }
func (referenceDataRepositoryTestConn) Begin() (driver.Tx, error) {
	return referenceDataRepositoryTestTx{}, nil
}

func (referenceDataRepositoryTestConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	referenceDataRepositoryTestState.Lock()
	referenceDataRepositoryTestState.queries = append(referenceDataRepositoryTestState.queries, query)
	response, ok := referenceDataRepositoryTestState.responses[query]
	referenceDataRepositoryTestState.Unlock()
	if !ok {
		return nil, errors.New("unexpected reference-data repository query")
	}
	if response.queryErr != nil {
		return nil, response.queryErr
	}

	rows := make([][]driver.Value, len(response.rows))
	for index, row := range response.rows {
		rows[index] = append([]driver.Value(nil), row...)
	}
	return &referenceDataRepositoryTestRows{
		columns: append([]string(nil), response.columns...),
		rows:    rows,
		nextErr: response.nextErr,
	}, nil
}

type referenceDataRepositoryTestTx struct{}

func (referenceDataRepositoryTestTx) Commit() error   { return nil }
func (referenceDataRepositoryTestTx) Rollback() error { return nil }

type referenceDataRepositoryTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
	nextErr error
}

func (r *referenceDataRepositoryTestRows) Columns() []string { return r.columns }
func (r *referenceDataRepositoryTestRows) Close() error      { return nil }

func (r *referenceDataRepositoryTestRows) Next(destination []driver.Value) error {
	if r.index < len(r.rows) {
		copy(destination, r.rows[r.index])
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

func resetReferenceDataRepositoryTestState() {
	referenceDataRepositoryTestState.Lock()
	defer referenceDataRepositoryTestState.Unlock()
	referenceDataRepositoryTestState.responses = make(map[string]referenceDataRepositoryTestResponse)
	referenceDataRepositoryTestState.queries = nil
}

func setReferenceDataRepositoryTestResponse(query string, response referenceDataRepositoryTestResponse) {
	referenceDataRepositoryTestState.Lock()
	referenceDataRepositoryTestState.responses[query] = response
	referenceDataRepositoryTestState.Unlock()
}

func openReferenceDataRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(referenceDataRepositoryTestDriverName, "reference-data-repository")
	if err != nil {
		t.Fatalf("open reference-data repository test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestReferenceDataRepositoryLoadsCompleteSnapshotThroughInjectedPool(t *testing.T) {
	resetReferenceDataRepositoryTestState()
	itemColumns, itemRow := referenceDataItemDriverRow(t, 1001)
	characterColumns, characterRow := referenceDataCharacterDriverRow(t, 2002)
	setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(1))
	setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{
		columns: itemColumns,
		rows:    [][]driver.Value{itemRow},
	})
	setReferenceDataRepositoryTestResponse(SelectAllRefCharsCount, referenceDataCountResponse(1))
	setReferenceDataRepositoryTestResponse(SelectAllRefChars, referenceDataRepositoryTestResponse{
		columns: characterColumns,
		rows:    [][]driver.Value{characterRow},
	})
	skillColumns, skillRow := referenceDataSkillDriverRow(160)
	setReferenceDataRepositoryTestResponse(SelectAllRefSkills, referenceDataRepositoryTestResponse{columns: skillColumns, rows: [][]driver.Value{skillRow}})

	repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewReferenceDataRepository: %v", err)
	}

	snapshot, err := repository.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, ok := snapshot.Items[1001]; !ok || got.ID != 1001 {
		t.Fatalf("item snapshot = %#v, present = %t; want ID 1001", got, ok)
	}
	if got, ok := snapshot.Characters[2002]; !ok || got.ID != 2002 {
		t.Fatalf("character snapshot = %#v, present = %t; want ID 2002", got, ok)
	}
	if got, ok := snapshot.Skills[160]; !ok || got.BasicActivity != 2 || got.ConsumeMP != 50 || got.RequiredWeapon1 != 11 || !got.TargetEnemyMob || !got.IsMagicalAttack() || got.ActionDuration != 2400 || got.ReuseDelay != 3000 || got.ActionRange != 10 || got.AttackPercent != 325 || got.AttackMin != 32 || got.AttackMax != 39 {
		t.Fatalf("skill snapshot = %#v, present = %t; want reference timing for skill 160", got, ok)
	}
	if len(snapshot.Items) != 1 || len(snapshot.Characters) != 1 || len(snapshot.Skills) != 1 {
		t.Fatalf("snapshot sizes = items:%d characters:%d skills:%d, want 1, 1, and 1", len(snapshot.Items), len(snapshot.Characters), len(snapshot.Skills))
	}
	assertReferenceDataRepositoryQueries(t, []string{
		SelectCountOfRefItems,
		SelectAllRefItems,
		SelectAllRefCharsCount,
		SelectAllRefChars,
		SelectAllRefSkills,
	})
}

func TestReferenceDataRepositoryAcceptsLegacyNonZeroBooleanEncoding(t *testing.T) {
	resetReferenceDataRepositoryTestState()
	itemColumns, itemRow := referenceDataItemDriverRow(t, 1001)
	// ITEMDATA column 18 is CanBorrow. Historical SRO data uses 255 as true,
	// which database/sql rejects when the destination is a plain *bool.
	itemRow[18] = int64(255)
	setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(1))
	setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{
		columns: itemColumns,
		rows:    [][]driver.Value{itemRow},
	})
	setReferenceDataRepositoryTestResponse(SelectAllRefCharsCount, referenceDataCountResponse(0))
	setReferenceDataRepositoryTestResponse(SelectAllRefChars, referenceDataRepositoryTestResponse{columns: []string{"ID"}})
	setReferenceDataRepositoryTestResponse(SelectAllRefSkills, referenceDataRepositoryTestResponse{columns: []string{"ID"}})

	repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewReferenceDataRepository: %v", err)
	}
	snapshot, err := repository.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !snapshot.Items[1001].IsBorrowable {
		t.Fatal("IsBorrowable = false, want true for legacy value 255")
	}
}

func TestReferenceDataRepositoryReturnsTypedFailures(t *testing.T) {
	t.Run("nil queryer", func(t *testing.T) {
		repository, err := NewReferenceDataRepository(nil)
		if repository != nil {
			t.Fatal("NewReferenceDataRepository returned a repository for a nil queryer")
		}
		assertReferenceDataRepositoryError(t, err, "catalog", "configure", ErrReferenceDataRepositoryUnavailable)
	})

	t.Run("cancelled context", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = repository.Load(ctx)
		assertReferenceDataRepositoryError(t, err, "items", "load", context.Canceled)
		assertReferenceDataRepositoryQueries(t, nil)
	})

	t.Run("item count query", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		want := errors.New("item count unavailable")
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataRepositoryTestResponse{queryErr: want})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "items", "query count", want)
	})

	t.Run("item scan", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(1))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{
			columns: []string{"ID"},
			rows:    [][]driver.Value{{int64(1)}},
		})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryOperation(t, err, "items", "scan")
	})

	t.Run("item rows", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		want := errors.New("item cursor interrupted")
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{
			columns: []string{"ID"},
			nextErr: want,
		})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "items", "iterate", want)
	})

	t.Run("character query after complete item snapshot", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		want := errors.New("character catalog unavailable")
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{columns: []string{"ID"}})
		setReferenceDataRepositoryTestResponse(SelectAllRefCharsCount, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefChars, referenceDataRepositoryTestResponse{queryErr: want})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "characters", "query", want)
		assertReferenceDataRepositoryQueries(t, []string{
			SelectCountOfRefItems,
			SelectAllRefItems,
			SelectAllRefCharsCount,
			SelectAllRefChars,
		})
	})

	t.Run("skill query after complete item and character snapshots", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		want := errors.New("skill catalog unavailable")
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{columns: []string{"ID"}})
		setReferenceDataRepositoryTestResponse(SelectAllRefCharsCount, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefChars, referenceDataRepositoryTestResponse{columns: []string{"ID"}})
		setReferenceDataRepositoryTestResponse(SelectAllRefSkills, referenceDataRepositoryTestResponse{queryErr: want})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "skills", "query", want)
		assertReferenceDataRepositoryQueries(t, []string{
			SelectCountOfRefItems,
			SelectAllRefItems,
			SelectAllRefCharsCount,
			SelectAllRefChars,
			SelectAllRefSkills,
		})
	})
}

func TestReferenceDataRepositoryRejectsDuplicateSourceIDs(t *testing.T) {
	t.Run("items", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		columns, row := referenceDataItemDriverRow(t, 1001)
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(2))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{
			columns: columns,
			rows:    [][]driver.Value{row, append([]driver.Value(nil), row...)},
		})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "items", "duplicate id", ErrReferenceDataDuplicateID)
	})

	t.Run("characters", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		itemColumns, itemRow := referenceDataItemDriverRow(t, 1001)
		characterColumns, characterRow := referenceDataCharacterDriverRow(t, 2002)
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(1))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{
			columns: itemColumns,
			rows:    [][]driver.Value{itemRow},
		})
		setReferenceDataRepositoryTestResponse(SelectAllRefCharsCount, referenceDataCountResponse(2))
		setReferenceDataRepositoryTestResponse(SelectAllRefChars, referenceDataRepositoryTestResponse{
			columns: characterColumns,
			rows:    [][]driver.Value{characterRow, append([]driver.Value(nil), characterRow...)},
		})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "characters", "duplicate id", ErrReferenceDataDuplicateID)
	})

	t.Run("skills", func(t *testing.T) {
		resetReferenceDataRepositoryTestState()
		setReferenceDataRepositoryTestResponse(SelectCountOfRefItems, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefItems, referenceDataRepositoryTestResponse{columns: []string{"ID"}})
		setReferenceDataRepositoryTestResponse(SelectAllRefCharsCount, referenceDataCountResponse(0))
		setReferenceDataRepositoryTestResponse(SelectAllRefChars, referenceDataRepositoryTestResponse{columns: []string{"ID"}})
		skillColumns, skillRow := referenceDataSkillDriverRow(160)
		setReferenceDataRepositoryTestResponse(SelectAllRefSkills, referenceDataRepositoryTestResponse{
			columns: skillColumns,
			rows:    [][]driver.Value{skillRow, append([]driver.Value(nil), skillRow...)},
		})
		repository, err := NewReferenceDataRepository(openReferenceDataRepositoryTestDB(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = repository.Load(context.Background())
		assertReferenceDataRepositoryError(t, err, "skills", "duplicate id", ErrReferenceDataDuplicateID)
	})
}

func referenceDataCountResponse(count int64) referenceDataRepositoryTestResponse {
	return referenceDataRepositoryTestResponse{
		columns: []string{"count"},
		rows:    [][]driver.Value{{count}},
	}
}

func referenceDataSkillDriverRow(id int64) ([]string, []driver.Value) {
	columns := []string{"ID", "GROUP_ID", "BASIC_GROUP", "BASIC_ORIGINAL", "BASIC_LEVEL", "BASIC_ACTIVITY", "BASIC_CHAINCODE", "REQCOMMON_MASTERY1", "REQCOMMON_MASTERY2", "REQCOMMON_MASTERYLEVEL1", "REQCOMMON_MASTERYLEVEL2", "REQCOMMON_STR", "REQCOMMON_INT", "REQLEARN_SKILL1", "REQLEARN_SKILL2", "REQLEARN_SKILL3", "REQLEARN_SKILLLEVEL1", "REQLEARN_SKILLLEVEL2", "REQLEARN_SKILLLEVEL3", "REQLEARN_SP", "REQLEARN_RACE", "TARGET_REQUIRED", "TARGETGROUP_SELF", "TARGETGROUP_ALLY", "TARGETGROUP_PARTY", "TARGETGROUP_ENEMY_M", "TARGETGROUP_ENEMY_P", "TARGETETC_SELECTDEADBODY", "REQCAST_WEAPON1", "REQCAST_WEAPON2", "CONSUME_HP", "CONSUME_MP", "CONSUME_HPRATIO", "CONSUME_MPRATIO", "ACTION_ACTIONDURATION", "ACTION_REUSEDELAY", "ACTION_COOLTIME", "ACTION_RANGE"}
	for index := range 50 {
		columns = append(columns, fmt.Sprintf("PARAM%d", index+1))
	}
	row := make([]driver.Value, len(columns))
	for index := range row {
		row[index] = int64(0)
	}
	row[0], row[1], row[2], row[3] = id, int64(1), "GROUP", id
	row[4], row[5], row[7], row[9] = int64(1), int64(2), int64(257), int64(1)
	row[18], row[20], row[21], row[25] = int64(1), int64(0), int64(1), int64(1)
	row[28], row[29], row[31] = int64(11), int64(255), int64(50)
	row[34], row[35], row[36], row[37] = int64(2400), int64(3000), int64(3000), int64(10)
	row[39], row[40], row[41], row[42], row[43] = int64(skillParameterAttack), int64(9), int64(325), int64(32), int64(39)
	return columns, row
}

func referenceDataItemDriverRow(t *testing.T, id int64) ([]string, []driver.Value) {
	t.Helper()
	counter := &referenceDataDestinationCounter{}
	if err := scanReferenceDataItem(counter, &RefItem{}); !errors.Is(err, errReferenceDataCountDestinations) {
		t.Fatalf("count item scan destinations: %v", err)
	}
	return referenceDataDriverRow(counter.count, id)
}

func referenceDataCharacterDriverRow(t *testing.T, id int64) ([]string, []driver.Value) {
	t.Helper()
	counter := &referenceDataDestinationCounter{}
	if err := scanReferenceDataCharacter(counter, &RefChar{}); !errors.Is(err, errReferenceDataCountDestinations) {
		t.Fatalf("count character scan destinations: %v", err)
	}
	return referenceDataDriverRow(counter.count, id)
}

var errReferenceDataCountDestinations = errors.New("count reference data scan destinations")

type referenceDataDestinationCounter struct {
	count int
}

func (c *referenceDataDestinationCounter) Scan(dest ...any) error {
	c.count = len(dest)
	return errReferenceDataCountDestinations
}

func referenceDataDriverRow(destinationCount int, id int64) ([]string, []driver.Value) {
	columns := make([]string, destinationCount)
	row := make([]driver.Value, destinationCount)
	for index := range row {
		columns[index] = fmt.Sprintf("column_%d", index)
		// Numeric zero can be converted safely to every primitive destination
		// used by the historical SELECT * scanners, including bool and string.
		row[index] = int64(0)
	}
	row[0] = id
	return columns, row
}

func assertReferenceDataRepositoryQueries(t *testing.T, want []string) {
	t.Helper()
	referenceDataRepositoryTestState.Lock()
	got := append([]string(nil), referenceDataRepositoryTestState.queries...)
	referenceDataRepositoryTestState.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queries = %#v, want %#v", got, want)
	}
}

func assertReferenceDataRepositoryError(t *testing.T, err error, dataset, operation string, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	assertReferenceDataRepositoryOperation(t, err, dataset, operation)
}

func assertReferenceDataRepositoryOperation(t *testing.T, err error, dataset, operation string) {
	t.Helper()
	var repositoryError *ReferenceDataRepositoryError
	if !errors.As(err, &repositoryError) || repositoryError.Dataset != dataset || repositoryError.Operation != operation {
		t.Fatalf("error = %v, want reference-data repository %s %s failure", err, dataset, operation)
	}
}
