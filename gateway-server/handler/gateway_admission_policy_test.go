package handler

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

const gatewayAdmissionStoreTestDriverName = "aegiscore-gateway-admission-store-test"

var gatewayAdmissionStoreTestState struct {
	sync.Mutex

	accountRows [][]driver.Value
	shardRows   [][]driver.Value
	accountErr  error
	shardErr    error
	queries     []gatewayAdmissionStoreTestQuery
}

type gatewayAdmissionStoreTestQuery struct {
	query string
	args  []driver.NamedValue
}

func init() {
	sql.Register(gatewayAdmissionStoreTestDriverName, gatewayAdmissionStoreTestDriver{})
}

type gatewayAdmissionStoreTestDriver struct{}

func (gatewayAdmissionStoreTestDriver) Open(string) (driver.Conn, error) {
	return gatewayAdmissionStoreTestConn{}, nil
}

type gatewayAdmissionStoreTestConn struct{}

func (gatewayAdmissionStoreTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented by the admission store test driver")
}

func (gatewayAdmissionStoreTestConn) Close() error { return nil }

func (gatewayAdmissionStoreTestConn) Begin() (driver.Tx, error) {
	return gatewayAdmissionStoreTestTx{}, nil
}

func (gatewayAdmissionStoreTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	gatewayAdmissionStoreTestState.Lock()
	defer gatewayAdmissionStoreTestState.Unlock()
	gatewayAdmissionStoreTestState.queries = append(gatewayAdmissionStoreTestState.queries, gatewayAdmissionStoreTestQuery{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})

	var rows [][]driver.Value
	var queryErr error
	var columns []string
	switch query {
	case selectGatewayAdmissionAccount:
		rows = gatewayAdmissionStoreTestState.accountRows
		queryErr = gatewayAdmissionStoreTestState.accountErr
		columns = []string{"ID", "STATUS"}
	case selectGatewayAdmissionShard:
		rows = gatewayAdmissionStoreTestState.shardRows
		queryErr = gatewayAdmissionStoreTestState.shardErr
		columns = []string{"ID", "CONTENT_ID", "CAPACITY", "ONLINE_PLAYERS", "STATUS"}
	default:
		return nil, errors.New("unexpected gateway admission query")
	}
	if queryErr != nil {
		return nil, queryErr
	}

	rowsCopy := make([][]driver.Value, len(rows))
	for index, row := range rows {
		rowsCopy[index] = append([]driver.Value(nil), row...)
	}
	return &gatewayAdmissionStoreTestRows{columns: columns, rows: rowsCopy}, nil
}

type gatewayAdmissionStoreTestTx struct{}

func (gatewayAdmissionStoreTestTx) Commit() error   { return nil }
func (gatewayAdmissionStoreTestTx) Rollback() error { return nil }

type gatewayAdmissionStoreTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *gatewayAdmissionStoreTestRows) Columns() []string { return r.columns }
func (r *gatewayAdmissionStoreTestRows) Close() error      { return nil }

func (r *gatewayAdmissionStoreTestRows) Next(destination []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(destination, r.rows[r.index])
	r.index++
	return nil
}

func resetGatewayAdmissionStoreTestState() {
	gatewayAdmissionStoreTestState.Lock()
	defer gatewayAdmissionStoreTestState.Unlock()
	gatewayAdmissionStoreTestState.accountRows = nil
	gatewayAdmissionStoreTestState.shardRows = nil
	gatewayAdmissionStoreTestState.accountErr = nil
	gatewayAdmissionStoreTestState.shardErr = nil
	gatewayAdmissionStoreTestState.queries = nil
}

func openGatewayAdmissionStoreTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(gatewayAdmissionStoreTestDriverName, "gateway-admission")
	if err != nil {
		t.Fatalf("open gateway admission store test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

type fakeGatewayAdmissionStore struct {
	account      GatewayAdmissionAccount
	accountFound bool
	accountErr   error
	shard        GatewayAdmissionShard
	shardFound   bool
	shardErr     error
}

func (s fakeGatewayAdmissionStore) Account(context.Context, uint32) (GatewayAdmissionAccount, bool, error) {
	return s.account, s.accountFound, s.accountErr
}

func (s fakeGatewayAdmissionStore) Shard(context.Context, uint16) (GatewayAdmissionShard, bool, error) {
	return s.shard, s.shardFound, s.shardErr
}

func TestGatewayAdmissionEvaluatorMapsAccountAndShardStates(t *testing.T) {
	request := LoginAdmissionRequest{AccountID: 77, ShardID: 1, ContentID: 56}
	activeAccount := GatewayAdmissionAccount{ID: 77, Status: activeAccountStatus}
	activeShard := GatewayAdmissionShard{ID: 1, ContentID: 56, Status: activeShardStatus, Capacity: 1000, OnlinePlayers: 999}
	tests := []struct {
		name    string
		store   fakeGatewayAdmissionStore
		allowed bool
		code    byte
	}{
		{name: "active account and shard", store: fakeGatewayAdmissionStore{account: activeAccount, accountFound: true, shard: activeShard, shardFound: true}, allowed: true},
		{name: "blocked account", store: fakeGatewayAdmissionStore{account: GatewayAdmissionAccount{ID: 77, Status: 0}, accountFound: true, shard: activeShard, shardFound: true}, code: LoginFailureBlocked},
		{name: "missing account", store: fakeGatewayAdmissionStore{shard: activeShard, shardFound: true}, code: LoginFailureBlocked},
		{name: "account lookup outage", store: fakeGatewayAdmissionStore{accountErr: errors.New("database unavailable")}, code: LoginFailureShardUnavailable},
		{name: "missing shard", store: fakeGatewayAdmissionStore{account: activeAccount, accountFound: true}, code: LoginFailureShardUnavailable},
		{name: "content mismatch", store: fakeGatewayAdmissionStore{account: activeAccount, accountFound: true, shard: GatewayAdmissionShard{ID: 1, ContentID: 57, Status: activeShardStatus, Capacity: 1}, shardFound: true}, code: LoginFailureShardUnavailable},
		{name: "offline shard", store: fakeGatewayAdmissionStore{account: activeAccount, accountFound: true, shard: GatewayAdmissionShard{ID: 1, ContentID: 56, Status: 0, Capacity: 1}, shardFound: true}, code: LoginFailureShardUnavailable},
		{name: "full shard", store: fakeGatewayAdmissionStore{account: activeAccount, accountFound: true, shard: GatewayAdmissionShard{ID: 1, ContentID: 56, Status: activeShardStatus, Capacity: 1, OnlinePlayers: 1}, shardFound: true}, code: LoginFailureShardUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := NewGatewayAdmissionEvaluator(test.store)(context.Background(), request)
			if decision.Allowed != test.allowed {
				t.Fatalf("Allowed = %t, want %t; decision=%+v", decision.Allowed, test.allowed, decision)
			}
			if !test.allowed && decision.Failure.Code != test.code {
				t.Fatalf("failure code = %d, want %d", decision.Failure.Code, test.code)
			}
		})
	}
}

func TestAccountPoolGatewayAdmissionStoreQueriesInjectedPool(t *testing.T) {
	resetGatewayAdmissionStoreTestState()
	gatewayAdmissionStoreTestState.Lock()
	gatewayAdmissionStoreTestState.accountRows = [][]driver.Value{{int64(77), int64(activeAccountStatus)}}
	gatewayAdmissionStoreTestState.shardRows = [][]driver.Value{{int64(1), int64(56), int64(1000), int64(42), int64(activeShardStatus)}}
	gatewayAdmissionStoreTestState.Unlock()

	store, err := NewAccountPoolGatewayAdmissionStore(openGatewayAdmissionStoreTestDB(t))
	if err != nil {
		t.Fatalf("NewAccountPoolGatewayAdmissionStore: %v", err)
	}

	account, found, err := store.Account(nil, 77)
	if err != nil || !found {
		t.Fatalf("Account = (%+v, %t, %v), want active account", account, found, err)
	}
	if want := (GatewayAdmissionAccount{ID: 77, Status: activeAccountStatus}); account != want {
		t.Fatalf("Account = %+v, want %+v", account, want)
	}

	shard, found, err := store.Shard(context.Background(), 1)
	if err != nil || !found {
		t.Fatalf("Shard = (%+v, %t, %v), want active shard", shard, found, err)
	}
	wantShard := GatewayAdmissionShard{ID: 1, ContentID: 56, Capacity: 1000, OnlinePlayers: 42, Status: activeShardStatus}
	if shard != wantShard {
		t.Fatalf("Shard = %+v, want %+v", shard, wantShard)
	}

	gatewayAdmissionStoreTestState.Lock()
	queries := append([]gatewayAdmissionStoreTestQuery(nil), gatewayAdmissionStoreTestState.queries...)
	gatewayAdmissionStoreTestState.Unlock()
	if len(queries) != 2 {
		t.Fatalf("queries = %#v, want account and shard query", queries)
	}
	if queries[0].query != selectGatewayAdmissionAccount || len(queries[0].args) != 1 || queries[0].args[0].Value != int64(77) {
		t.Fatalf("account query = %#v, want %q with account ID 77", queries[0], selectGatewayAdmissionAccount)
	}
	if queries[1].query != selectGatewayAdmissionShard || len(queries[1].args) != 1 || queries[1].args[0].Value != int64(1) {
		t.Fatalf("shard query = %#v, want %q with shard ID 1", queries[1], selectGatewayAdmissionShard)
	}
}

func TestAccountPoolGatewayAdmissionStoreFailsClosedWithoutInjectedPool(t *testing.T) {
	store, err := NewAccountPoolGatewayAdmissionStore(nil)
	if store != nil || !errors.Is(err, ErrGatewayAdmissionStoreUnavailable) {
		t.Fatalf("nil store construction = (%v, %v), want unavailable error", store, err)
	}

	var nilStore *AccountPoolGatewayAdmissionStore
	if _, _, err := nilStore.Account(context.Background(), 77); !errors.Is(err, ErrGatewayAdmissionStoreUnavailable) {
		t.Fatalf("nil store account lookup error = %v, want unavailable error", err)
	}
	if _, _, err := nilStore.Shard(context.Background(), 1); !errors.Is(err, ErrGatewayAdmissionStoreUnavailable) {
		t.Fatalf("nil store shard lookup error = %v, want unavailable error", err)
	}

	decision := defaultGatewayAdmissionEvaluator(context.Background(), LoginAdmissionRequest{AccountID: 77, ShardID: 1, ContentID: 56})
	if decision.Allowed || decision.Failure.Code != LoginFailureShardUnavailable {
		t.Fatalf("default admission decision = %+v, want fail-closed shard-unavailable", decision)
	}
}

func TestAccountPoolGatewayAdmissionStoreWrapsErrorsWithoutExposingDriverText(t *testing.T) {
	resetGatewayAdmissionStoreTestState()
	underlying := errors.New("sensitive-driver-message")
	gatewayAdmissionStoreTestState.Lock()
	gatewayAdmissionStoreTestState.accountErr = underlying
	gatewayAdmissionStoreTestState.Unlock()

	store, err := NewAccountPoolGatewayAdmissionStore(openGatewayAdmissionStoreTestDB(t))
	if err != nil {
		t.Fatalf("NewAccountPoolGatewayAdmissionStore: %v", err)
	}
	_, found, err := store.Account(context.Background(), 77)
	if found {
		t.Fatal("Account reported a row after a database failure")
	}
	if !errors.Is(err, underlying) {
		t.Fatalf("Account error = %v, want wrapped driver error", err)
	}
	var storeErr *GatewayAdmissionStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("Account error type = %T, want *GatewayAdmissionStoreError", err)
	}
	if storeErr.Operation() != "account" {
		t.Fatalf("Account error operation = %q, want account", storeErr.Operation())
	}
	if strings.Contains(err.Error(), underlying.Error()) {
		t.Fatalf("Account error leaked driver text: %q", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = store.Shard(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Shard error = %v, want context.Canceled", err)
	}
	if !errors.As(err, &storeErr) || storeErr.Operation() != "shard" {
		t.Fatalf("canceled Shard error = %T (%v), want typed shard lookup error", err, err)
	}
}
