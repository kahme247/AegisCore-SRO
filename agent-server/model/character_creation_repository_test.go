package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
)

const characterCreationRepositoryTestDriverName = "go-sro-character-creation-repository-test"

var characterCreationRepositoryTestRegistry struct {
	sync.Mutex
	nextID int
	stores map[string]*characterCreationRepositoryTestStore
}

func init() {
	sql.Register(characterCreationRepositoryTestDriverName, characterCreationRepositoryTestDriver{})
}

type characterCreationRepositoryTestDriver struct{}

func (characterCreationRepositoryTestDriver) Open(name string) (driver.Conn, error) {
	characterCreationRepositoryTestRegistry.Lock()
	store := characterCreationRepositoryTestRegistry.stores[name]
	characterCreationRepositoryTestRegistry.Unlock()
	if store == nil {
		return nil, fmt.Errorf("character creation test store %q is unavailable", name)
	}
	return &characterCreationRepositoryTestConn{store: store}, nil
}

type characterCreationRepositoryTestStore struct {
	sync.Mutex

	characterID int64
	nextItemID  int64

	beginErr      error
	beginContexts []context.Context
	commitErr     error
	commitCount   int
	rollbackCount int

	failQuery      string
	failOccurrence int
	failErr        error
	queryCounts    map[string]int
	calls          []characterCreationRepositoryTestCall
}

type characterCreationRepositoryTestCall struct {
	query string
	args  []driver.NamedValue
}

type characterCreationRepositoryTestConn struct {
	store *characterCreationRepositoryTestStore
}

var _ driver.Conn = (*characterCreationRepositoryTestConn)(nil)
var _ driver.ConnBeginTx = (*characterCreationRepositoryTestConn)(nil)
var _ driver.ExecerContext = (*characterCreationRepositoryTestConn)(nil)

func (*characterCreationRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the character creation test driver")
}

func (*characterCreationRepositoryTestConn) Close() error { return nil }

func (c *characterCreationRepositoryTestConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *characterCreationRepositoryTestConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.store.Lock()
	defer c.store.Unlock()
	c.store.beginContexts = append(c.store.beginContexts, ctx)
	if c.store.beginErr != nil {
		return nil, c.store.beginErr
	}
	return &characterCreationRepositoryTestTx{store: c.store}, nil
}

func (c *characterCreationRepositoryTestConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.store.Lock()
	defer c.store.Unlock()
	if !characterCreationRepositoryRecognizesQuery(query) {
		return nil, fmt.Errorf("unexpected exec %q", query)
	}
	if c.store.queryCounts == nil {
		c.store.queryCounts = make(map[string]int)
	}
	c.store.queryCounts[query]++
	c.store.calls = append(c.store.calls, characterCreationRepositoryTestCall{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	if query == c.store.failQuery && c.store.queryCounts[query] == c.store.failOccurrence {
		return nil, c.store.failErr
	}

	switch query {
	case insert_char:
		return characterCreationRepositoryTestResult{id: c.store.characterID}, nil
	case InsertItem:
		id := c.store.nextItemID
		c.store.nextItemID++
		return characterCreationRepositoryTestResult{id: id}, nil
	case InsertInventoryItem:
		return characterCreationRepositoryTestResult{}, nil
	case insertCharacterSkill, insertReferenceCharacterSkills, insertCharacterMastery, insertReferenceCharacterMasteries, insertReferenceCharacterQuests:
		return characterCreationRepositoryTestResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec %q", query)
	}
}

type characterCreationRepositoryTestTx struct {
	store *characterCreationRepositoryTestStore
}

func (tx *characterCreationRepositoryTestTx) Commit() error {
	tx.store.Lock()
	defer tx.store.Unlock()
	tx.store.commitCount++
	return tx.store.commitErr
}

func (tx *characterCreationRepositoryTestTx) Rollback() error {
	tx.store.Lock()
	defer tx.store.Unlock()
	tx.store.rollbackCount++
	return nil
}

type characterCreationRepositoryTestResult struct {
	id int64
}

func (r characterCreationRepositoryTestResult) LastInsertId() (int64, error) { return r.id, nil }
func (characterCreationRepositoryTestResult) RowsAffected() (int64, error)   { return 1, nil }

func characterCreationRepositoryRecognizesQuery(query string) bool {
	switch query {
	case insert_char, InsertItem, InsertInventoryItem, insertCharacterSkill, insertReferenceCharacterSkills, insertCharacterMastery, insertReferenceCharacterMasteries, insertReferenceCharacterQuests:
		return true
	default:
		return false
	}
}

func newCharacterCreationRepositoryForTest(t *testing.T, store *characterCreationRepositoryTestStore) *SQLCharacterCreationRepository {
	t.Helper()
	if store.characterID == 0 {
		store.characterID = 700
	}
	if store.nextItemID == 0 {
		store.nextItemID = 900
	}
	characterCreationRepositoryTestRegistry.Lock()
	if characterCreationRepositoryTestRegistry.stores == nil {
		characterCreationRepositoryTestRegistry.stores = make(map[string]*characterCreationRepositoryTestStore)
	}
	characterCreationRepositoryTestRegistry.nextID++
	name := fmt.Sprintf("character-creation-%d", characterCreationRepositoryTestRegistry.nextID)
	characterCreationRepositoryTestRegistry.stores[name] = store
	characterCreationRepositoryTestRegistry.Unlock()

	database, err := sql.Open(characterCreationRepositoryTestDriverName, name)
	if err != nil {
		t.Fatalf("sql.Open character creation test database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = database.Close()
		characterCreationRepositoryTestRegistry.Lock()
		delete(characterCreationRepositoryTestRegistry.stores, name)
		characterCreationRepositoryTestRegistry.Unlock()
	})
	repository, err := NewCharacterCreationRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterCreationRepository: %v", err)
	}
	return repository
}

func TestSQLCharacterCreationRepositoryCommitsCharacterAndInitialProgressInOneContextTransaction(t *testing.T) {
	store := &characterCreationRepositoryTestStore{characterID: 77, nextItemID: 501}
	repository := newCharacterCreationRepositoryForTest(t, store)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-context")

	characterID, err := repository.CreateCharacter(ctx, characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems())
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	if characterID != 77 {
		t.Fatalf("CreateCharacter() ID = %d, want 77", characterID)
	}

	store.Lock()
	beginContexts := append([]context.Context(nil), store.beginContexts...)
	calls := append([]characterCreationRepositoryTestCall(nil), store.calls...)
	commitCount, rollbackCount := store.commitCount, store.rollbackCount
	store.Unlock()
	if len(beginContexts) != 1 || beginContexts[0].Value(contextKey{}) != "request-context" {
		t.Fatalf("BeginTx contexts = %#v, want the handler request context", beginContexts)
	}
	if commitCount != 1 || rollbackCount != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want one commit and no rollback", commitCount, rollbackCount)
	}
	wantQueries := []string{insert_char, InsertItem, InsertInventoryItem, InsertItem, InsertInventoryItem, InsertItem, InsertInventoryItem, InsertItem, InsertInventoryItem, insertReferenceCharacterSkills, insertReferenceCharacterMasteries, insertReferenceCharacterQuests}
	if len(calls) != len(wantQueries) {
		t.Fatalf("exec calls = %d, want %d (%#v)", len(calls), len(wantQueries), calls)
	}
	for index, want := range wantQueries {
		if calls[index].query != want {
			t.Fatalf("exec %d query = %q, want %q", index, calls[index].query, want)
		}
	}
	assertCharacterCreationRepositoryInteger(t, calls[0].args[0].Value, 2001)
	assertCharacterCreationRepositoryInteger(t, calls[0].args[1].Value, 42)
	assertCharacterCreationRepositoryInteger(t, calls[0].args[2].Value, 7)
	assertCharacterCreationRepositoryInteger(t, calls[1].args[0].Value, 1001)
	assertCharacterCreationRepositoryInteger(t, calls[2].args[0].Value, 77)
	assertCharacterCreationRepositoryInteger(t, calls[2].args[1].Value, int64(SlotPrimaryWeapon))
	assertCharacterCreationRepositoryInteger(t, calls[2].args[2].Value, 501)
	assertCharacterCreationRepositoryInteger(t, calls[9].args[0].Value, 77)
	assertCharacterCreationRepositoryInteger(t, calls[10].args[0].Value, 77)
	assertCharacterCreationRepositoryInteger(t, calls[11].args[0].Value, 77)
}

func TestSQLCharacterCreationRepositoryPreservesOriginalCreatorAPI(t *testing.T) {
	store := &characterCreationRepositoryTestStore{characterID: 78, nextItemID: 601}
	var creator CharacterCreator = newCharacterCreationRepositoryForTest(t, store)
	if _, err := creator.CreateCharacter(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems()); err != nil {
		t.Fatalf("CreateCharacter() compatibility wrapper: %v", err)
	}
	store.Lock()
	skillInserts := store.queryCounts[insertCharacterSkill]
	referenceSkillInserts := store.queryCounts[insertReferenceCharacterSkills]
	commitCount := store.commitCount
	masteryInserts := store.queryCounts[insertReferenceCharacterMasteries]
	questInserts := store.queryCounts[insertReferenceCharacterQuests]
	store.Unlock()
	if skillInserts != 0 || referenceSkillInserts != 1 || masteryInserts != 1 || questInserts != 1 || commitCount != 1 {
		t.Fatalf("compatibility wrapper skill inserts=%d reference skill inserts=%d reference mastery inserts=%d reference quest inserts=%d commits=%d, want 0/1/1/1/1", skillInserts, referenceSkillInserts, masteryInserts, questInserts, commitCount)
	}
}

func TestSQLCharacterCreationRepositoryPreservesExplicitProgressCreatorAPI(t *testing.T) {
	store := &characterCreationRepositoryTestStore{characterID: 79, nextItemID: 701}
	var creator CharacterProgressCreator = newCharacterCreationRepositoryForTest(t, store)
	masteries := characterCreationRepositoryTestStarterMasteries()
	if _, err := creator.CreateCharacterWithProgress(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems(), characterCreationRepositoryTestStarterSkills(), masteries); err != nil {
		t.Fatalf("CreateCharacterWithProgress() compatibility wrapper: %v", err)
	}
	store.Lock()
	explicitMasteryInserts := store.queryCounts[insertCharacterMastery]
	referenceMasteryInserts := store.queryCounts[insertReferenceCharacterMasteries]
	questInserts := store.queryCounts[insertReferenceCharacterQuests]
	commitCount := store.commitCount
	store.Unlock()
	if explicitMasteryInserts != len(masteries) || referenceMasteryInserts != 0 || questInserts != 1 || commitCount != 1 {
		t.Fatalf("explicit mastery inserts=%d reference mastery inserts=%d reference quest inserts=%d commits=%d, want %d/0/1/1", explicitMasteryInserts, referenceMasteryInserts, questInserts, commitCount, len(masteries))
	}
}

func TestSQLCharacterCreationRepositoryRollsBackEveryWriteFailure(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		occurrence    int
		wantOperation string
		wantCallCount int
	}{
		{name: "character insert", query: insert_char, occurrence: 1, wantOperation: "insert character", wantCallCount: 1},
		{name: "starter item insert", query: InsertItem, occurrence: 2, wantOperation: "insert starter item", wantCallCount: 4},
		{name: "inventory link insert", query: InsertInventoryItem, occurrence: 3, wantOperation: "insert starter inventory link", wantCallCount: 7},
		{name: "starter skill insert", query: insertCharacterSkill, occurrence: 2, wantOperation: "insert starter skill", wantCallCount: 11},
		{name: "reference starter skill insert", query: insertReferenceCharacterSkills, occurrence: 1, wantOperation: "insert reference starter skills", wantCallCount: 10},
		{name: "reference starter mastery insert", query: insertReferenceCharacterMasteries, occurrence: 1, wantOperation: "insert reference starter masteries", wantCallCount: 11},
		{name: "reference starter quest insert", query: insertReferenceCharacterQuests, occurrence: 1, wantOperation: "insert reference starter quests", wantCallCount: 12},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New("write failed")
			store := &characterCreationRepositoryTestStore{
				failQuery:      test.query,
				failOccurrence: test.occurrence,
				failErr:        failure,
			}
			repository := newCharacterCreationRepositoryForTest(t, store)
			var err error
			if test.query == insertCharacterSkill {
				_, err = repository.CreateCharacterWithSkills(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems(), characterCreationRepositoryTestStarterSkills())
			} else {
				_, err = repository.CreateCharacter(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems())
			}
			if !errors.Is(err, failure) {
				t.Fatalf("CreateCharacter() error = %v, want wrapped %v", err, failure)
			}
			var creationError *CharacterCreationError
			if !errors.As(err, &creationError) || creationError.Operation != test.wantOperation {
				t.Fatalf("CreateCharacter() error = %v, want typed %q error", err, test.wantOperation)
			}
			store.Lock()
			commitCount, rollbackCount, callCount := store.commitCount, store.rollbackCount, len(store.calls)
			store.Unlock()
			if commitCount != 0 || rollbackCount != 1 {
				t.Fatalf("transaction commits=%d rollbacks=%d, want no commit and one rollback", commitCount, rollbackCount)
			}
			if callCount != test.wantCallCount {
				t.Fatalf("write calls = %d, want %d before rollback", callCount, test.wantCallCount)
			}
		})
	}
}

func TestSQLCharacterCreationRepositoryMapsDriverDuplicateAndRollsBack(t *testing.T) {
	duplicate := &mysql.MySQLError{Number: mysqlDuplicateEntryErrorCode, Message: "duplicate key"}
	store := &characterCreationRepositoryTestStore{
		failQuery:      insert_char,
		failOccurrence: 1,
		failErr:        duplicate,
	}
	_, err := newCharacterCreationRepositoryForTest(t, store).CreateCharacterWithSkills(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems(), characterCreationRepositoryTestStarterSkills())
	if !errors.Is(err, ErrCharacterCreationDuplicate) {
		t.Fatalf("CreateCharacter() error = %v, want ErrCharacterCreationDuplicate", err)
	}
	if !errors.Is(err, duplicate) {
		t.Fatalf("CreateCharacter() error = %v, want original driver error", err)
	}
	store.Lock()
	rollbackCount := store.rollbackCount
	store.Unlock()
	if rollbackCount != 1 {
		t.Fatalf("rollbacks = %d, want 1 after duplicate", rollbackCount)
	}
}

func TestSQLCharacterCreationRepositoryRejectsInvalidStarterReferencesBeforeBeginTx(t *testing.T) {
	store := &characterCreationRepositoryTestStore{}
	starterItems := characterCreationRepositoryTestStarterItems()
	starterItems[0].ReferenceItemID = 0
	_, err := newCharacterCreationRepositoryForTest(t, store).CreateCharacterWithSkills(context.Background(), characterCreationRepositoryTestCharacter(), starterItems, characterCreationRepositoryTestStarterSkills())
	if !errors.Is(err, ErrCharacterCreationStarterReferenceRequired) {
		t.Fatalf("CreateCharacter() error = %v, want ErrCharacterCreationStarterReferenceRequired", err)
	}
	store.Lock()
	beginCount := len(store.beginContexts)
	store.Unlock()
	if beginCount != 0 {
		t.Fatalf("BeginTx calls = %d, want 0 for invalid input", beginCount)
	}
}

func TestSQLCharacterCreationRepositoryRejectsDuplicateStarterSkillsBeforeBeginTx(t *testing.T) {
	store := &characterCreationRepositoryTestStore{}
	starterSkills := characterCreationRepositoryTestStarterSkills()
	starterSkills = append(starterSkills, starterSkills[0])
	_, err := newCharacterCreationRepositoryForTest(t, store).CreateCharacterWithSkills(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems(), starterSkills)
	if !errors.Is(err, ErrCharacterCreationDuplicateStarterSkill) {
		t.Fatalf("CreateCharacter() error = %v, want ErrCharacterCreationDuplicateStarterSkill", err)
	}
	store.Lock()
	beginCount := len(store.beginContexts)
	store.Unlock()
	if beginCount != 0 {
		t.Fatalf("BeginTx calls = %d, want 0 for invalid skills", beginCount)
	}
}

func TestSQLCharacterCreationRepositoryRejectsInvalidStarterMasteriesBeforeBeginTx(t *testing.T) {
	tests := []struct {
		name      string
		masteries []MasteryProgress
		want      error
	}{
		{name: "duplicate", masteries: append(characterCreationRepositoryTestStarterMasteries(), MasteryProgress{ID: 257}), want: ErrCharacterCreationDuplicateStarterMastery},
		{name: "nonzero level", masteries: []MasteryProgress{{ID: 257, Level: 1}}, want: ErrCharacterCreationStarterMasteryInvalid},
		{name: "zero ID", masteries: []MasteryProgress{{ID: 0}}, want: ErrCharacterCreationStarterMasteryInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &characterCreationRepositoryTestStore{}
			_, err := newCharacterCreationRepositoryForTest(t, store).CreateCharacterWithProgress(context.Background(), characterCreationRepositoryTestCharacter(), characterCreationRepositoryTestStarterItems(), characterCreationRepositoryTestStarterSkills(), test.masteries)
			if !errors.Is(err, test.want) {
				t.Fatalf("CreateCharacterWithProgress() error = %v, want %v", err, test.want)
			}
			store.Lock()
			beginCount := len(store.beginContexts)
			store.Unlock()
			if beginCount != 0 {
				t.Fatalf("BeginTx calls = %d, want 0 for invalid masteries", beginCount)
			}
		})
	}
}

func characterCreationRepositoryTestCharacter() Char {
	return Char{
		RefObjID: 2001,
		User:     42,
		Shard:    7,
		Name:     "transactional-character",
		Scale:    1,
		Level:    1,
		Str:      20,
		Int:      20,
		HP:       200,
		MP:       200,
		PosX:     950,
		PosY:     40,
		PosZ:     1091,
		Region:   0x62A8,
	}
}

func characterCreationRepositoryTestStarterItems() []CharacterStarterItem {
	return []CharacterStarterItem{
		{ReferenceItemID: 1001, Slot: SlotPrimaryWeapon},
		{ReferenceItemID: 1002, Slot: SlotChest},
		{ReferenceItemID: 1003, Slot: SlotBoots},
		{ReferenceItemID: 1004, Slot: SlotPants},
	}
}

func characterCreationRepositoryTestStarterSkills() []CharacterSkill {
	return []CharacterSkill{
		{ID: 1, Enabled: 1},
		{ID: 2, Enabled: 1},
		{ID: 40, Enabled: 1},
		{ID: 70, Enabled: 1},
	}
}

func characterCreationRepositoryTestStarterMasteries() []MasteryProgress {
	return []MasteryProgress{
		{ID: 257}, {ID: 258}, {ID: 259}, {ID: 273}, {ID: 274}, {ID: 275}, {ID: 276},
	}
}

func assertCharacterCreationRepositoryInteger(t *testing.T, value any, want int64) {
	t.Helper()
	var got int64
	switch typed := value.(type) {
	case int64:
		got = typed
	case int:
		got = int64(typed)
	case int32:
		got = int64(typed)
	case uint32:
		got = int64(typed)
	case uint16:
		got = int64(typed)
	case byte:
		got = int64(typed)
	default:
		t.Fatalf("value %T(%v) is not an integer, want %d", value, value, want)
	}
	if got != want {
		t.Fatalf("integer = %d, want %d", got, want)
	}
}
