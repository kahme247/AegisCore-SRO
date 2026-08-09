package handler

import (
	"context"
	"database/sql"
	"errors"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const (
	activeAccountStatus = 1
	activeShardStatus   = 1

	selectGatewayAdmissionAccount = "SELECT ID, STATUS FROM `SRO_ACCOUNT`.`USER` WHERE ID=? LIMIT 1"
	selectGatewayAdmissionShard   = "SELECT ID, CONTENT_ID, CAPACITY, ONLINE_PLAYERS, STATUS FROM `SRO_ACCOUNT`.`SHARD` WHERE ID=? LIMIT 1"
)

// GatewayAdmissionAccount is the minimal account state needed after a
// password has been verified. It deliberately excludes password and profile
// data from the admission-policy boundary.
type GatewayAdmissionAccount struct {
	ID     uint32
	Status int
}

// GatewayAdmissionShard is the minimal shard state needed to decide whether
// the selected content/shard can accept a new admission.
type GatewayAdmissionShard struct {
	ID            uint16
	ContentID     byte
	Capacity      int
	OnlinePlayers int
	Status        int
}

// GatewayAdmissionStore is dependency-inverted so the decision table can be
// tested without a live database and production database failures fail closed.
type GatewayAdmissionStore interface {
	Account(context.Context, uint32) (GatewayAdmissionAccount, bool, error)
	Shard(context.Context, uint16) (GatewayAdmissionShard, bool, error)
}

// ErrGatewayAdmissionStoreUnavailable is returned when startup did not inject
// the long-lived account pool required by the production admission policy.
// Callers must fail closed rather than opening a database handle in a login
// request path.
var ErrGatewayAdmissionStoreUnavailable = errors.New("gateway admission store is unavailable")

// GatewayAdmissionStoreError records an operational lookup failure without
// exposing a driver error, query text, connection string, or account data in
// Error(). The original error remains available through errors.Is/As for
// operational metrics and tests.
type GatewayAdmissionStoreError struct {
	operation string
	err       error
}

func (e *GatewayAdmissionStoreError) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch e.operation {
	case "account":
		return "gateway admission account lookup failed"
	case "shard":
		return "gateway admission shard lookup failed"
	default:
		return "gateway admission lookup failed"
	}
}

func (e *GatewayAdmissionStoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// Operation identifies the safe, coarse-grained failed lookup for metrics or
// diagnostics. It intentionally never returns a query or driver message.
func (e *GatewayAdmissionStoreError) Operation() string {
	if e == nil {
		return ""
	}
	return e.operation
}

// AccountPoolGatewayAdmissionStore reads the post-password account and shard
// state from an injected, long-lived account pool (or a transaction in a unit
// test). It does not own or close queryer.
type AccountPoolGatewayAdmissionStore struct {
	queryer frameworkdb.RowQueryer
}

// NewAccountPoolGatewayAdmissionStore constructs the production admission
// store from the account pool returned by framework/db.Pools.Account. Gateway
// startup must retain and close the owning Pools instance during its ordered
// shutdown; this store never opens or closes connections per login request.
func NewAccountPoolGatewayAdmissionStore(queryer frameworkdb.RowQueryer) (*AccountPoolGatewayAdmissionStore, error) {
	if queryer == nil {
		return nil, ErrGatewayAdmissionStoreUnavailable
	}
	return &AccountPoolGatewayAdmissionStore{queryer: queryer}, nil
}

// NewGatewayAdmissionEvaluator returns a concrete post-password policy:
// inactive/blocked accounts receive code 2; missing, mismatched, offline, or
// full shards receive code 6. Active duplicate accounts are rejected by the
// Agent's atomic reservation during ticket installation and map to code 4.
func NewGatewayAdmissionEvaluator(store GatewayAdmissionStore) LoginAdmissionEvaluator {
	return func(ctx context.Context, request LoginAdmissionRequest) LoginAdmissionDecision {
		if store == nil || request.AccountID == 0 || request.ShardID == 0 {
			return DenyLoginAdmission(LoginFailure{Code: LoginFailureShardUnavailable})
		}
		if ctx == nil {
			ctx = context.Background()
		}
		account, found, err := store.Account(ctx, request.AccountID)
		if err != nil {
			return DenyLoginAdmission(LoginFailure{Code: LoginFailureShardUnavailable})
		}
		if !found || account.ID != request.AccountID || account.Status != activeAccountStatus {
			return DenyLoginAdmission(LoginFailure{Code: LoginFailureBlocked})
		}
		shard, found, err := store.Shard(ctx, request.ShardID)
		if err != nil {
			return DenyLoginAdmission(LoginFailure{Code: LoginFailureShardUnavailable})
		}
		if !found || shard.ID != request.ShardID || shard.ContentID != request.ContentID || shard.Status != activeShardStatus || shard.Capacity <= 0 || shard.OnlinePlayers >= shard.Capacity {
			return DenyLoginAdmission(LoginFailure{Code: LoginFailureShardUnavailable})
		}
		return AllowLoginAdmission()
	}
}

// defaultGatewayAdmissionEvaluator is intentionally concrete in production;
// callers may inject a different evaluator only for deployment-specific bans
// or tests. A password-valid account no longer receives an unconditional A102
// success solely because credential lookup passed.
func defaultGatewayAdmissionEvaluator(ctx context.Context, request LoginAdmissionRequest) LoginAdmissionDecision {
	return unavailableGatewayAdmissionEvaluator(ctx, request)
}

var unavailableGatewayAdmissionEvaluator = NewGatewayAdmissionEvaluator(unavailableGatewayAdmissionStore{})

// unavailableGatewayAdmissionStore is deliberately the compatibility default
// until application startup injects an AccountPoolGatewayAdmissionStore. It
// preserves the policy's fail-closed code-6 mapping without reintroducing the
// legacy per-request sql.Open path.
type unavailableGatewayAdmissionStore struct{}

func (unavailableGatewayAdmissionStore) Account(context.Context, uint32) (GatewayAdmissionAccount, bool, error) {
	return GatewayAdmissionAccount{}, false, ErrGatewayAdmissionStoreUnavailable
}

func (unavailableGatewayAdmissionStore) Shard(context.Context, uint16) (GatewayAdmissionShard, bool, error) {
	return GatewayAdmissionShard{}, false, ErrGatewayAdmissionStoreUnavailable
}

func (s *AccountPoolGatewayAdmissionStore) Account(ctx context.Context, accountID uint32) (GatewayAdmissionAccount, bool, error) {
	if accountID == 0 {
		return GatewayAdmissionAccount{}, false, nil
	}
	if s == nil || s.queryer == nil {
		return GatewayAdmissionAccount{}, false, ErrGatewayAdmissionStoreUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var id uint32
	var status int
	row := s.queryer.QueryRowContext(ctx, selectGatewayAdmissionAccount, accountID)
	if row == nil {
		return GatewayAdmissionAccount{}, false, ErrGatewayAdmissionStoreUnavailable
	}
	err := row.Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayAdmissionAccount{}, false, nil
	}
	if err != nil {
		return GatewayAdmissionAccount{}, false, &GatewayAdmissionStoreError{operation: "account", err: err}
	}
	return GatewayAdmissionAccount{ID: id, Status: status}, true, nil
}

func (s *AccountPoolGatewayAdmissionStore) Shard(ctx context.Context, shardID uint16) (GatewayAdmissionShard, bool, error) {
	if shardID == 0 {
		return GatewayAdmissionShard{}, false, nil
	}
	if s == nil || s.queryer == nil {
		return GatewayAdmissionShard{}, false, ErrGatewayAdmissionStoreUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var id uint16
	var contentID byte
	var capacity, onlinePlayers, status int
	row := s.queryer.QueryRowContext(ctx, selectGatewayAdmissionShard, shardID)
	if row == nil {
		return GatewayAdmissionShard{}, false, ErrGatewayAdmissionStoreUnavailable
	}
	err := row.Scan(&id, &contentID, &capacity, &onlinePlayers, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayAdmissionShard{}, false, nil
	}
	if err != nil {
		return GatewayAdmissionShard{}, false, &GatewayAdmissionStoreError{operation: "shard", err: err}
	}
	return GatewayAdmissionShard{ID: id, ContentID: contentID, Capacity: capacity, OnlinePlayers: onlinePlayers, Status: status}, true, nil
}
