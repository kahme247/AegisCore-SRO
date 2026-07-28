package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ferdoran/go-sro/framework/config"
	"github.com/spf13/viper"
)

const (
	// DefaultMaxOpenConns is deliberately modest until production capacity
	// measurements establish a higher per-process limit.
	DefaultMaxOpenConns = 16
	DefaultMaxIdleConns = 8

	DefaultConnMaxLifetime = 30 * time.Minute
	DefaultConnMaxIdleTime = 5 * time.Minute
	DefaultPingTimeout     = 5 * time.Second
)

var (
	ErrInvalidPoolConfig = errors.New("invalid database pool configuration")
	ErrPoolsClosed       = errors.New("database pools are closed")
	ErrPoolsUnavailable  = errors.New("database pools are unavailable")
)

// PoolRole identifies a database role without exposing connection strings in
// errors or logs.
type PoolRole string

const (
	AccountPool PoolRole = "account"
	ShardPool   PoolRole = "shard"
)

// PoolConfig configures one long-lived sql.DB pool. A zero-valued tuning
// field uses the safe default above; negative values are rejected.
type PoolConfig struct {
	Driver string
	DSN    string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration
}

// PoolError gives callers a typed, role-aware error while deliberately omitting
// DSNs and credentials.
type PoolError struct {
	Role      PoolRole
	Operation string
	Err       error
}

func (e *PoolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	// Driver errors can include the DSN supplied to sql.Open or details from a
	// database authentication failure. Keep the role and failed operation for
	// operators, but do not render the underlying error in a value that startup
	// paths commonly log. Unwrap still preserves programmatic classification.
	return fmt.Sprintf("%s database pool %s failed", e.Role, e.Operation)
}

func (e *PoolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Queryer, Execer, and RowQueryer are the small repository-facing interfaces
// implemented by both *sql.DB and *sql.Tx. Repositories can receive one of
// these interfaces instead of opening or closing a database per operation.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type RowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DBTX is suitable for repositories that can run against either a long-lived
// pool or a transaction owned by a higher-level unit of work.
type DBTX interface {
	Queryer
	Execer
	RowQueryer
}

// Pools owns the long-lived account and shard pools for one process. It is
// safe to read from multiple goroutines and Close is idempotent.
type Pools struct {
	mu sync.RWMutex

	account *sql.DB
	shard   *sql.DB
	closed  bool
}

// NewConfiguredPools builds both pools from the current application
// configuration. Applications should call this during startup and call Close
// during their ordered shutdown.
func NewConfiguredPools(ctx context.Context) (*Pools, error) {
	return NewPools(ctx, AccountPoolConfig(), ShardPoolConfig())
}

// AccountPoolConfig returns the account connection settings without opening a
// connection. Pool tuning defaults are applied by OpenPool.
func AccountPoolConfig() PoolConfig {
	return PoolConfig{
		Driver: viper.GetString(config.DatabaseAccountDriver),
		DSN:    config.ConnStringAccount(),
	}
}

// ShardPoolConfig returns the shard connection settings without opening a
// connection. Pool tuning defaults are applied by OpenPool.
func ShardPoolConfig() PoolConfig {
	return PoolConfig{
		Driver: viper.GetString(config.DatabaseShardDriver),
		DSN:    config.ConnStringShard(),
	}
}

// NewPools opens, configures, and verifies both database pools. If either
// pool cannot be prepared, any already-opened pool is closed before the error
// is returned.
func NewPools(ctx context.Context, accountConfig, shardConfig PoolConfig) (*Pools, error) {
	account, err := OpenPool(ctx, AccountPool, accountConfig)
	if err != nil {
		return nil, err
	}

	shard, err := OpenPool(ctx, ShardPool, shardConfig)
	if err != nil {
		_ = account.Close()
		return nil, err
	}

	return &Pools{account: account, shard: shard}, nil
}

// OpenPool opens one long-lived pool, applies bounded connection settings, and
// verifies connectivity with PingContext before returning it.
func OpenPool(ctx context.Context, role PoolRole, poolConfig PoolConfig) (*sql.DB, error) {
	poolConfig, err := normalizePoolConfig(poolConfig)
	if err != nil {
		return nil, &PoolError{Role: role, Operation: "configure", Err: err}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pool, err := sql.Open(poolConfig.Driver, poolConfig.DSN)
	if err != nil {
		return nil, &PoolError{Role: role, Operation: "open", Err: err}
	}

	pool.SetMaxOpenConns(poolConfig.MaxOpenConns)
	pool.SetMaxIdleConns(poolConfig.MaxIdleConns)
	pool.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(poolConfig.ConnMaxIdleTime)

	pingContext, cancel := context.WithTimeout(ctx, poolConfig.PingTimeout)
	defer cancel()
	if err := pool.PingContext(pingContext); err != nil {
		_ = pool.Close()
		return nil, &PoolError{Role: role, Operation: "ping", Err: err}
	}

	return pool, nil
}

func normalizePoolConfig(poolConfig PoolConfig) (PoolConfig, error) {
	poolConfig.Driver = strings.TrimSpace(poolConfig.Driver)
	if poolConfig.Driver == "" {
		return PoolConfig{}, fmt.Errorf("%w: driver is required", ErrInvalidPoolConfig)
	}
	if strings.TrimSpace(poolConfig.DSN) == "" {
		return PoolConfig{}, fmt.Errorf("%w: data source name is required", ErrInvalidPoolConfig)
	}

	if poolConfig.MaxOpenConns < 0 || poolConfig.MaxIdleConns < 0 ||
		poolConfig.ConnMaxLifetime < 0 || poolConfig.ConnMaxIdleTime < 0 || poolConfig.PingTimeout < 0 {
		return PoolConfig{}, fmt.Errorf("%w: limits and durations cannot be negative", ErrInvalidPoolConfig)
	}

	maxIdleSpecified := poolConfig.MaxIdleConns != 0
	if poolConfig.MaxOpenConns == 0 {
		poolConfig.MaxOpenConns = DefaultMaxOpenConns
	}
	if poolConfig.MaxIdleConns == 0 {
		poolConfig.MaxIdleConns = DefaultMaxIdleConns
	}
	if poolConfig.MaxIdleConns > poolConfig.MaxOpenConns {
		if !maxIdleSpecified {
			poolConfig.MaxIdleConns = poolConfig.MaxOpenConns
		} else {
			return PoolConfig{}, fmt.Errorf("%w: max idle connections exceed max open connections", ErrInvalidPoolConfig)
		}
	}
	if poolConfig.ConnMaxLifetime == 0 {
		poolConfig.ConnMaxLifetime = DefaultConnMaxLifetime
	}
	if poolConfig.ConnMaxIdleTime == 0 {
		poolConfig.ConnMaxIdleTime = DefaultConnMaxIdleTime
	}
	if poolConfig.PingTimeout == 0 {
		poolConfig.PingTimeout = DefaultPingTimeout
	}
	return poolConfig, nil
}

// Account returns the injected account pool while it remains available.
func (p *Pools) Account() (*sql.DB, error) {
	return p.pool(AccountPool)
}

// Shard returns the injected shard pool while it remains available.
func (p *Pools) Shard() (*sql.DB, error) {
	return p.pool(ShardPool)
}

func (p *Pools) pool(role PoolRole) (*sql.DB, error) {
	if p == nil {
		return nil, ErrPoolsUnavailable
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, ErrPoolsClosed
	}

	switch role {
	case AccountPool:
		if p.account != nil {
			return p.account, nil
		}
	case ShardPool:
		if p.shard != nil {
			return p.shard, nil
		}
	}
	return nil, ErrPoolsUnavailable
}

// Close stops both pools. It is idempotent so ordered shutdown paths can call
// it safely even when a partial startup already cleaned up a pool.
func (p *Pools) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	account := p.account
	shard := p.shard
	p.account = nil
	p.shard = nil
	p.mu.Unlock()

	var closeErrors []error
	if account != nil {
		closeErrors = append(closeErrors, account.Close())
	}
	if shard != nil {
		closeErrors = append(closeErrors, shard.Close())
	}
	return errors.Join(closeErrors...)
}
