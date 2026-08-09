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
)

const inventoryRepositoryTestDriverName = "aegiscore-inventory-repository-test"

var inventoryRepositoryTestRegistry struct {
	sync.Mutex
	nextID int
	stores map[string]*inventoryRepositoryTestStore
}

func init() {
	sql.Register(inventoryRepositoryTestDriverName, inventoryRepositoryTestDriver{})
}

type inventoryRepositoryTestDriver struct{}

func (inventoryRepositoryTestDriver) Open(name string) (driver.Conn, error) {
	inventoryRepositoryTestRegistry.Lock()
	store := inventoryRepositoryTestRegistry.stores[name]
	inventoryRepositoryTestRegistry.Unlock()
	if store == nil {
		return nil, fmt.Errorf("inventory repository test store %q is unavailable", name)
	}
	return &inventoryRepositoryTestConn{store: store}, nil
}

type inventoryRepositoryTestStore struct {
	mu sync.Mutex

	queryRows     [][]driver.Value
	queryErr      error
	nextErr       error
	nextErrAt     int
	execErr       error
	queryCalls    int
	execCalls     int
	lastQuery     string
	lastQueryArgs []driver.NamedValue
	lastExec      string
	lastExecArgs  []driver.NamedValue
}

type inventoryRepositoryTestConn struct {
	store *inventoryRepositoryTestStore
}

var _ driver.Conn = (*inventoryRepositoryTestConn)(nil)
var _ driver.QueryerContext = (*inventoryRepositoryTestConn)(nil)
var _ driver.ExecerContext = (*inventoryRepositoryTestConn)(nil)

func (c *inventoryRepositoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the inventory repository test driver")
}

func (c *inventoryRepositoryTestConn) Close() error { return nil }

func (c *inventoryRepositoryTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented by the inventory repository test driver")
}

func (c *inventoryRepositoryTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.store.mu.Lock()
	c.store.queryCalls++
	c.store.lastQuery = query
	c.store.lastQueryArgs = append([]driver.NamedValue(nil), args...)
	queryErr := c.store.queryErr
	rows := cloneInventoryRepositoryRows(c.store.queryRows)
	nextErr := c.store.nextErr
	nextErrAt := c.store.nextErrAt
	c.store.mu.Unlock()

	if queryErr != nil {
		return nil, queryErr
	}
	return &inventoryRepositoryTestRows{
		rows:      rows,
		nextErr:   nextErr,
		nextErrAt: nextErrAt,
	}, nil
}

func (c *inventoryRepositoryTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.store.mu.Lock()
	c.store.execCalls++
	c.store.lastExec = query
	c.store.lastExecArgs = append([]driver.NamedValue(nil), args...)
	execErr := c.store.execErr
	c.store.mu.Unlock()

	if execErr != nil {
		return nil, execErr
	}
	return driver.RowsAffected(1), nil
}

type inventoryRepositoryTestRows struct {
	rows      [][]driver.Value
	nextErr   error
	nextErrAt int
	index     int
}

func (r *inventoryRepositoryTestRows) Columns() []string {
	return []string{"SLOT", "FK_ITEM", "VARIANCE", "FK_REF_ITEM", "STACK_COUNT", "OPT_LEVEL", "DATA", "RENT_TYPE", "RENT_CAN_DELETE", "RENT_CAN_RECHARGE", "RENT_PERIOD_BEGIN_TIME", "RENT_PERIOD_END_TIME", "RENT_METER_RATE_TIME", "RENT_PACKING_TIME"}
}

func (*inventoryRepositoryTestRows) Close() error { return nil }

func (r *inventoryRepositoryTestRows) Next(dest []driver.Value) error {
	if r.nextErr != nil && r.index >= r.nextErrAt {
		return r.nextErr
	}
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func cloneInventoryRepositoryRows(rows [][]driver.Value) [][]driver.Value {
	cloned := make([][]driver.Value, len(rows))
	for index, row := range rows {
		cloned[index] = append([]driver.Value(nil), row...)
	}
	return cloned
}

func inventoryTestRow(values ...driver.Value) []driver.Value {
	return append(values, int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0))
}

func openInventoryRepositoryTestDB(t *testing.T, store *inventoryRepositoryTestStore) *sql.DB {
	t.Helper()

	inventoryRepositoryTestRegistry.Lock()
	if inventoryRepositoryTestRegistry.stores == nil {
		inventoryRepositoryTestRegistry.stores = make(map[string]*inventoryRepositoryTestStore)
	}
	inventoryRepositoryTestRegistry.nextID++
	name := fmt.Sprintf("inventory-repository-%d", inventoryRepositoryTestRegistry.nextID)
	inventoryRepositoryTestRegistry.stores[name] = store
	inventoryRepositoryTestRegistry.Unlock()

	database, err := sql.Open(inventoryRepositoryTestDriverName, name)
	if err != nil {
		t.Fatalf("sql.Open inventory test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
		inventoryRepositoryTestRegistry.Lock()
		delete(inventoryRepositoryTestRegistry.stores, name)
		inventoryRepositoryTestRegistry.Unlock()
	})
	return database
}

func setInventoryRepositoryRefItems(t *testing.T, items map[uint32]RefItem) {
	t.Helper()
	previous := RefItems
	RefItems = items
	t.Cleanup(func() { RefItems = previous })
}

func newInventoryRepositoryForTest(t *testing.T, store *inventoryRepositoryTestStore) (*SQLInventoryRepository, *inventoryRepositoryTestStore) {
	t.Helper()
	repository, err := NewInventoryRepository(openInventoryRepositoryTestDB(t, store))
	if err != nil {
		t.Fatalf("NewInventoryRepository: %v", err)
	}
	return repository, store
}

func TestNewInventoryRepositoryRequiresInjectedDatabase(t *testing.T) {
	if repository, err := NewInventoryRepository(nil); repository != nil || !errors.Is(err, ErrInventoryRepositoryUnavailable) {
		t.Fatalf("NewInventoryRepository(nil) = (%v, %v), want (nil, ErrInventoryRepositoryUnavailable)", repository, err)
	}
}

func TestSQLInventoryRepositoryReadsCharacterInventory(t *testing.T) {
	setInventoryRepositoryRefItems(t, map[uint32]RefItem{
		1001: {
			RefObject: RefObject{
				ID:            1001,
				CodeName:      "ITEM_CH_SWORD_01_A_RARE",
				TypeInfo:      TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: TypeID3Weapon, TypeID4: 2},
				ReqLevelType1: 1,
				ReqLevelType2: 2,
				ReqLevelType3: 3,
				ReqLevelType4: 4,
				ReqLevel1:     5,
				ReqLevel2:     6,
				ReqLevel3:     7,
				ReqLevel4:     8,
			},
		},
	})
	store := &inventoryRepositoryTestStore{
		queryRows: [][]driver.Value{inventoryTestRow(int64(SlotPrimaryWeapon), int64(91), int64(7), int64(1001), int64(1), int64(0), int64(10))},
	}
	repository, _ := newInventoryRepositoryForTest(t, store)

	inventory, err := repository.GetCharacterInventory(context.Background(), 44)
	if err != nil {
		t.Fatalf("GetCharacterInventory returned an error: %v", err)
	}
	item, exists := inventory.Items[SlotPrimaryWeapon]
	if !exists {
		t.Fatalf("inventory does not contain slot %d", SlotPrimaryWeapon)
	}
	if item.ID != 91 || item.Name != "ITEM_CH_SWORD_01_A_RARE" || item.Variance != 7 || item.RefObjectID != 1001 {
		t.Fatalf("inventory item = %+v, want persisted item 91 with reference 1001", item)
	}
	if item.RequiredLevelType1 != 1 || item.RequiredLevel4 != 8 || item.GetTypeInfo().TypeID4 != 2 {
		t.Fatalf("inventory item did not retain reference metadata: %+v", item)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.queryCalls != 1 || store.lastQuery != SelectCharacterInventory {
		t.Fatalf("query = (%d calls, %q), want one SelectCharacterInventory call", store.queryCalls, store.lastQuery)
	}
	if len(store.lastQueryArgs) != 1 || store.lastQueryArgs[0].Value != int64(44) {
		t.Fatalf("query arguments = %#v, want character ID 44", store.lastQueryArgs)
	}
}

func TestSQLInventoryRepositoryReadRejectsInvalidInputBeforeQuery(t *testing.T) {
	store := &inventoryRepositoryTestStore{}
	repository, _ := newInventoryRepositoryForTest(t, store)

	for _, characterID := range []int64{0, -1} {
		inventory, err := repository.GetCharacterInventory(context.Background(), characterID)
		if !errors.Is(err, ErrInventoryCharacterIDRequired) {
			t.Fatalf("GetCharacterInventory(%d) error = %v, want ErrInventoryCharacterIDRequired", characterID, err)
		}
		if inventory.Items == nil {
			t.Fatalf("GetCharacterInventory(%d) returned a nil item map", characterID)
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.queryCalls != 0 {
		t.Fatalf("invalid input issued %d queries", store.queryCalls)
	}
}

func TestSQLInventoryRepositoryReadReturnsSafeErrors(t *testing.T) {
	queryFailure := errors.New("query failed")
	iterationFailure := errors.New("rows failed")

	tests := []struct {
		name     string
		store    *inventoryRepositoryTestStore
		refItems map[uint32]RefItem
		want     error
		op       string
	}{
		{
			name:  "query",
			store: &inventoryRepositoryTestStore{queryErr: queryFailure},
			want:  queryFailure,
			op:    "query character inventory",
		},
		{
			name: "scan",
			store: &inventoryRepositoryTestStore{
				queryRows: [][]driver.Value{inventoryTestRow("not-a-slot", int64(1), int64(0), int64(1), int64(1), int64(0), int64(0))},
			},
			op: "scan character inventory",
		},
		{
			name:  "rows iteration",
			store: &inventoryRepositoryTestStore{nextErr: iterationFailure},
			want:  iterationFailure,
			op:    "iterate character inventory",
		},
		{
			name: "persisted slot range",
			store: &inventoryRepositoryTestStore{
				queryRows: [][]driver.Value{inventoryTestRow(int64(BaseInventorySize), int64(1), int64(0), int64(1), int64(1), int64(0), int64(0))},
			},
			want: ErrInventorySlotOutOfRange,
			op:   "scan character inventory",
		},
		{
			name: "missing reference item",
			store: &inventoryRepositoryTestStore{
				queryRows: [][]driver.Value{inventoryTestRow(int64(SlotChest), int64(1), int64(0), int64(9001), int64(1), int64(0), int64(0))},
			},
			want: ErrInventoryReferenceItemNotFound,
			op:   "scan character inventory",
		},
		{
			name: "duplicate slot",
			store: &inventoryRepositoryTestStore{
				queryRows: [][]driver.Value{
					inventoryTestRow(int64(SlotChest), int64(1), int64(0), int64(1), int64(1), int64(0), int64(0)),
					inventoryTestRow(int64(SlotChest), int64(2), int64(0), int64(1), int64(1), int64(0), int64(0)),
				},
			},
			refItems: map[uint32]RefItem{1: {RefObject: RefObject{ID: 1, CodeName: "ITEM"}}},
			want:     ErrInventoryDuplicateSlot,
			op:       "validate character inventory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setInventoryRepositoryRefItems(t, test.refItems)
			repository, _ := newInventoryRepositoryForTest(t, test.store)

			inventory, err := repository.GetCharacterInventory(context.Background(), 10)
			if err == nil {
				t.Fatal("GetCharacterInventory returned nil error")
			}
			if inventory.Items == nil {
				t.Fatal("GetCharacterInventory returned a nil item map")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("GetCharacterInventory error = %v, want errors.Is(..., %v)", err, test.want)
			}
			var repositoryError *InventoryRepositoryError
			if !errors.As(err, &repositoryError) || repositoryError.Operation != test.op {
				t.Fatalf("GetCharacterInventory error = %v, want InventoryRepositoryError operation %q", err, test.op)
			}
		})
	}
}

func TestSQLInventoryRepositoryRespectsCanceledContext(t *testing.T) {
	store := &inventoryRepositoryTestStore{}
	repository, _ := newInventoryRepositoryForTest(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repository.GetCharacterInventory(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetCharacterInventory(canceled context) error = %v, want context.Canceled", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.queryCalls != 0 {
		t.Fatalf("canceled context issued %d queries", store.queryCalls)
	}
}

func TestSQLInventoryRepositoryInsertsValidatedSlot(t *testing.T) {
	store := &inventoryRepositoryTestStore{}
	repository, _ := newInventoryRepositoryForTest(t, store)

	if err := repository.AddItemToInventory(context.Background(), 44, 91, SlotChest); err != nil {
		t.Fatalf("AddItemToInventory returned an error: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.execCalls != 1 || store.lastExec != InsertInventoryItem {
		t.Fatalf("exec = (%d calls, %q), want one InsertInventoryItem call", store.execCalls, store.lastExec)
	}
	if len(store.lastExecArgs) != 3 || store.lastExecArgs[0].Value != int64(44) || store.lastExecArgs[1].Value != int64(SlotChest) || store.lastExecArgs[2].Value != int64(91) {
		t.Fatalf("exec arguments = %#v, want character 44, slot %d, item 91", store.lastExecArgs, SlotChest)
	}
}

func TestSQLInventoryRepositoryInsertReturnsSafeErrors(t *testing.T) {
	execFailure := errors.New("insert failed")

	tests := []struct {
		name        string
		characterID int64
		itemID      int64
		slot        byte
		store       *inventoryRepositoryTestStore
		want        error
		op          string
	}{
		{
			name:        "character ID",
			characterID: 0,
			itemID:      1,
			slot:        SlotChest,
			store:       &inventoryRepositoryTestStore{},
			want:        ErrInventoryCharacterIDRequired,
		},
		{
			name:        "item ID",
			characterID: 1,
			itemID:      0,
			slot:        SlotChest,
			store:       &inventoryRepositoryTestStore{},
			want:        ErrInventoryItemIDRequired,
		},
		{
			name:        "slot range",
			characterID: 1,
			itemID:      1,
			slot:        BaseInventorySize,
			store:       &inventoryRepositoryTestStore{},
			want:        ErrInventorySlotOutOfRange,
		},
		{
			name:        "exec",
			characterID: 1,
			itemID:      1,
			slot:        SlotChest,
			store:       &inventoryRepositoryTestStore{execErr: execFailure},
			want:        execFailure,
			op:          "insert character inventory slot",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, store := newInventoryRepositoryForTest(t, test.store)
			err := repository.AddItemToInventory(context.Background(), test.characterID, test.itemID, test.slot)
			if !errors.Is(err, test.want) {
				t.Fatalf("AddItemToInventory error = %v, want errors.Is(..., %v)", err, test.want)
			}
			if test.op != "" {
				var repositoryError *InventoryRepositoryError
				if !errors.As(err, &repositoryError) || repositoryError.Operation != test.op {
					t.Fatalf("AddItemToInventory error = %v, want InventoryRepositoryError operation %q", err, test.op)
				}
			}
			if test.op == "" {
				store.mu.Lock()
				execCalls := store.execCalls
				store.mu.Unlock()
				if execCalls != 0 {
					t.Fatalf("invalid input issued %d insert calls", execCalls)
				}
			}
		})
	}
}
