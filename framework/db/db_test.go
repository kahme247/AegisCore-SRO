package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/config"
	"github.com/spf13/viper"
)

const poolTestDriverName = "aegiscore-pool-test"

var poolTestState struct {
	sync.Mutex

	openedByDSN map[string]int
	closedByDSN map[string]int
	pingCount   int
	pingErrors  map[string]error
	blockPing   bool
}

func init() {
	sql.Register(poolTestDriverName, poolTestDriver{})
}

type poolTestDriver struct{}

func (poolTestDriver) Open(dsn string) (driver.Conn, error) {
	poolTestState.Lock()
	poolTestState.openedByDSN[dsn]++
	poolTestState.Unlock()
	return &poolTestConn{dsn: dsn}, nil
}

type poolTestConn struct {
	dsn string
}

func (c *poolTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("queries are not implemented by the pool test driver")
}

func (c *poolTestConn) Close() error {
	poolTestState.Lock()
	poolTestState.closedByDSN[c.dsn]++
	poolTestState.Unlock()
	return nil
}

func (c *poolTestConn) Begin() (driver.Tx, error) {
	return poolTestTx{}, nil
}

func (c *poolTestConn) Ping(ctx context.Context) error {
	poolTestState.Lock()
	poolTestState.pingCount++
	blockPing := poolTestState.blockPing
	pingError := poolTestState.pingErrors[c.dsn]
	poolTestState.Unlock()

	if blockPing {
		<-ctx.Done()
		return ctx.Err()
	}
	return pingError
}

type poolTestTx struct{}

func (poolTestTx) Commit() error   { return nil }
func (poolTestTx) Rollback() error { return nil }

func resetPoolTestDriver() {
	poolTestState.Lock()
	poolTestState.openedByDSN = make(map[string]int)
	poolTestState.closedByDSN = make(map[string]int)
	poolTestState.pingErrors = make(map[string]error)
	poolTestState.pingCount = 0
	poolTestState.blockPing = false
	poolTestState.Unlock()
}

func TestOpenConnAccountCreatesDeferredHandle(t *testing.T) {
	configureDatabase(t)

	dbHandle := OpenConnAccount()
	if dbHandle == nil {
		t.Fatal("OpenConnAccount returned a nil database handle")
	}
	t.Cleanup(func() { _ = dbHandle.Close() })
}

func TestOpenConnShardCreatesDeferredHandle(t *testing.T) {
	configureDatabase(t)

	dbHandle := OpenConnShard()
	if dbHandle == nil {
		t.Fatal("OpenConnShard returned a nil database handle")
	}
	t.Cleanup(func() { _ = dbHandle.Close() })
}

func TestConfiguredPoolConfigsRejectMissingPasswords(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	for role, poolConfig := range map[string]PoolConfig{
		"account": AccountPoolConfig(),
		"shard":   ShardPoolConfig(),
	} {
		if _, err := normalizePoolConfig(poolConfig); !errors.Is(err, ErrInvalidPoolConfig) {
			t.Fatalf("%s configured pool error = %v, want ErrInvalidPoolConfig", role, err)
		}
	}
}

func TestNewPoolsConfiguresAndPingsBothPools(t *testing.T) {
	resetPoolTestDriver()

	pools, err := NewPools(context.Background(), PoolConfig{
		Driver:          poolTestDriverName,
		DSN:             "account",
		MaxOpenConns:    3,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Second,
		PingTimeout:     time.Second,
	}, PoolConfig{
		Driver:          poolTestDriverName,
		DSN:             "shard",
		MaxOpenConns:    5,
		MaxIdleConns:    4,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Second,
		PingTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("NewPools returned an error: %v", err)
	}

	account, err := pools.Account()
	if err != nil {
		t.Fatalf("Pools.Account returned an error: %v", err)
	}
	if got := account.Stats().MaxOpenConnections; got != 3 {
		t.Fatalf("account max open connections = %d, want 3", got)
	}

	shard, err := pools.Shard()
	if err != nil {
		t.Fatalf("Pools.Shard returned an error: %v", err)
	}
	if got := shard.Stats().MaxOpenConnections; got != 5 {
		t.Fatalf("shard max open connections = %d, want 5", got)
	}

	poolTestState.Lock()
	pingCount := poolTestState.pingCount
	poolTestState.Unlock()
	if pingCount != 2 {
		t.Fatalf("PingContext calls = %d, want 2", pingCount)
	}

	if err := pools.Close(); err != nil {
		t.Fatalf("first Pools.Close returned an error: %v", err)
	}
	if err := pools.Close(); err != nil {
		t.Fatalf("second Pools.Close returned an error: %v", err)
	}
	if _, err := pools.Account(); !errors.Is(err, ErrPoolsClosed) {
		t.Fatalf("Pools.Account after Close error = %v, want ErrPoolsClosed", err)
	}
}

func TestNewPoolsClosesAccountWhenShardPingFails(t *testing.T) {
	resetPoolTestDriver()
	wantPingError := errors.New("shard unavailable")
	poolTestState.Lock()
	poolTestState.pingErrors["shard"] = wantPingError
	poolTestState.Unlock()

	pools, err := NewPools(context.Background(), testPoolConfig("account"), testPoolConfig("shard"))
	if pools != nil {
		t.Fatal("NewPools returned pools after the shard ping failed")
	}
	if !errors.Is(err, wantPingError) {
		t.Fatalf("NewPools error = %v, want wrapped %v", err, wantPingError)
	}

	var poolError *PoolError
	if !errors.As(err, &poolError) {
		t.Fatalf("NewPools error %T, want *PoolError", err)
	}
	if poolError.Role != ShardPool || poolError.Operation != "ping" {
		t.Fatalf("PoolError = %#v, want shard ping error", poolError)
	}

	poolTestState.Lock()
	accountClosed := poolTestState.closedByDSN["account"]
	poolTestState.Unlock()
	if accountClosed == 0 {
		t.Fatal("account pool was not closed after shard startup failed")
	}
}

func TestPoolErrorRedactsUnderlyingConnectionDetail(t *testing.T) {
	secret := "mysql://operator:database-secret@db.internal:3306/account"
	underlying := errors.New("connect " + secret + " refused")
	err := &PoolError{Role: AccountPool, Operation: "ping", Err: underlying}

	if got := err.Error(); strings.Contains(got, secret) {
		t.Fatalf("PoolError leaked connection detail: %q", got)
	}
	if !errors.Is(err, underlying) {
		t.Fatal("PoolError no longer unwraps the underlying error")
	}
}

func TestOpenPoolHonorsParentContextDeadline(t *testing.T) {
	resetPoolTestDriver()
	poolTestState.Lock()
	poolTestState.blockPing = true
	poolTestState.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	pool, err := OpenPool(ctx, AccountPool, PoolConfig{
		Driver:      poolTestDriverName,
		DSN:         "account",
		PingTimeout: time.Second,
	})
	if pool != nil {
		t.Fatal("OpenPool returned a pool after PingContext timed out")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OpenPool error = %v, want context deadline exceeded", err)
	}
}

func TestOpenPoolRejectsInvalidConfiguration(t *testing.T) {
	testCases := []struct {
		name   string
		config PoolConfig
	}{
		{name: "empty driver", config: PoolConfig{DSN: "account"}},
		{name: "empty dsn", config: PoolConfig{Driver: poolTestDriverName}},
		{name: "negative limit", config: PoolConfig{Driver: poolTestDriverName, DSN: "account", MaxOpenConns: -1}},
		{name: "idle exceeds open", config: PoolConfig{Driver: poolTestDriverName, DSN: "account", MaxOpenConns: 1, MaxIdleConns: 2}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pool, err := OpenPool(context.Background(), AccountPool, testCase.config)
			if pool != nil {
				t.Fatal("OpenPool returned a pool for invalid configuration")
			}
			if !errors.Is(err, ErrInvalidPoolConfig) {
				t.Fatalf("OpenPool error = %v, want ErrInvalidPoolConfig", err)
			}

			var poolError *PoolError
			if !errors.As(err, &poolError) {
				t.Fatalf("OpenPool error %T, want *PoolError", err)
			}
			if poolError.Operation != "configure" {
				t.Fatalf("PoolError operation = %q, want configure", poolError.Operation)
			}
		})
	}
}

func TestOpenPoolAppliesSafeDefaults(t *testing.T) {
	resetPoolTestDriver()

	pool, err := OpenPool(context.Background(), AccountPool, PoolConfig{
		Driver: poolTestDriverName,
		DSN:    "defaults",
	})
	if err != nil {
		t.Fatalf("OpenPool returned an error: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	if got := pool.Stats().MaxOpenConnections; got != DefaultMaxOpenConns {
		t.Fatalf("max open connections = %d, want default %d", got, DefaultMaxOpenConns)
	}
}

func TestNormalizePoolConfigCapsImplicitIdleLimit(t *testing.T) {
	config, err := normalizePoolConfig(PoolConfig{
		Driver:       poolTestDriverName,
		DSN:          "account",
		MaxOpenConns: 3,
	})
	if err != nil {
		t.Fatalf("normalizePoolConfig returned an error: %v", err)
	}
	if config.MaxIdleConns != 3 {
		t.Fatalf("implicit max idle connections = %d, want 3", config.MaxIdleConns)
	}
}

func testPoolConfig(dsn string) PoolConfig {
	return PoolConfig{
		Driver:      poolTestDriverName,
		DSN:         dsn,
		PingTimeout: time.Second,
	}
}

func configureDatabase(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set(config.DatabaseAccountDriver, "mysql")
	viper.Set(config.DatabaseAccountHost, "127.0.0.1")
	viper.Set(config.DatabaseAccountPort, "1")
	viper.Set(config.DatabaseAccountUser, "test")
	viper.Set(config.DatabaseAccountPassword, "test")
	viper.Set(config.DatabaseAccountDatabase, "account")
	viper.Set(config.DatabaseAccountParams, "?parseTime=true")

	viper.Set(config.DatabaseShardDriver, "mysql")
	viper.Set(config.DatabaseShardHost, "127.0.0.1")
	viper.Set(config.DatabaseShardPort, "1")
	viper.Set(config.DatabaseShardUser, "test")
	viper.Set(config.DatabaseShardPassword, "test")
	viper.Set(config.DatabaseShardDatabase, "shard")
	viper.Set(config.DatabaseShardParams, "?parseTime=true")
}
