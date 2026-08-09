package db

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	log "github.com/sirupsen/logrus"
)

type Shard struct {
	Id            int
	ContentId     int
	Name          string
	Capacity      int
	OnlinePlayers int
	Status        int
}

const selectShards = "SELECT ID, CONTENT_ID, NAME, CAPACITY, ONLINE_PLAYERS, STATUS FROM SHARD ORDER BY ID ASC"

var ErrShardRepositoryUnavailable = errors.New("shard repository is unavailable")

// ShardRepositoryError identifies the failed catalog read operation without
// requiring callers to parse a database-driver error string.
type ShardRepositoryError struct {
	Operation string
	Err       error
}

func (e *ShardRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("shard repository %s: %v", e.Operation, e.Err)
}

func (e *ShardRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ShardCatalog is the Gateway-facing read contract. It is intentionally small
// so handlers can use a long-lived pool in production or a safe fake in tests.
type ShardCatalog interface {
	List(ctx context.Context) ([]Shard, error)
}

// ShardCatalogFunc adapts a context-aware function to ShardCatalog.
type ShardCatalogFunc func(context.Context) ([]Shard, error)

func (f ShardCatalogFunc) List(ctx context.Context) ([]Shard, error) {
	return f(ctx)
}

// ShardRepository receives a long-lived account pool or transaction from its
// caller. It never opens or closes a database connection itself.
type ShardRepository struct {
	queryer frameworkdb.Queryer
}

func NewShardRepository(queryer frameworkdb.Queryer) (*ShardRepository, error) {
	if queryer == nil {
		return nil, &ShardRepositoryError{
			Operation: "configure",
			Err:       ErrShardRepositoryUnavailable,
		}
	}
	return &ShardRepository{queryer: queryer}, nil
}

// List reads the current shard catalog and reports query, scan, and row
// iteration failures to the caller instead of turning a bad database response
// into a process-wide panic.
func (r *ShardRepository) List(ctx context.Context) ([]Shard, error) {
	if r == nil || r.queryer == nil {
		return nil, &ShardRepositoryError{
			Operation: "query",
			Err:       ErrShardRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.queryer.QueryContext(ctx, selectShards)
	if err != nil {
		return nil, &ShardRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	shards := make([]Shard, 0)
	for rows.Next() {
		shard := Shard{}
		if err := rows.Scan(
			&shard.Id,
			&shard.ContentId,
			&shard.Name,
			&shard.Capacity,
			&shard.OnlinePlayers,
			&shard.Status,
		); err != nil {
			return nil, &ShardRepositoryError{Operation: "scan", Err: err}
		}
		shards = append(shards, shard)
	}
	if err := rows.Err(); err != nil {
		return nil, &ShardRepositoryError{Operation: "iterate", Err: err}
	}
	return shards, nil
}

// LegacyShardCatalog keeps older startup paths working until they inject a
// long-lived framework/db pool. New handlers should receive ShardRepository
// (or another ShardCatalog) directly; this adapter is deliberately the only
// catalog read path that opens a short-lived connection per request.
func LegacyShardCatalog() ShardCatalog {
	return ShardCatalogFunc(func(ctx context.Context) ([]Shard, error) {
		conn := frameworkdb.OpenConnAccount()
		defer conn.Close()

		repository, err := NewShardRepository(conn)
		if err != nil {
			return nil, err
		}
		return repository.List(ctx)
	})
}

// GetShards is retained for callers that have not moved to an injected
// ShardCatalog. The handler path uses List and returns errors; this legacy
// helper logs and returns an empty list to preserve its historical []Shard
// shape without relying on framework/db.CheckError.
func GetShards() []Shard {
	shards, err := LegacyShardCatalog().List(context.Background())
	if err != nil {
		log.WithError(err).Error("load Gateway shards")
		return []Shard{}
	}
	return shards
}
