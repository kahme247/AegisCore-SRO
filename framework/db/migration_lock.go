package db

import (
	"context"
	"database/sql"
)

// migrationLockCoordinator is the single owner of the pinned MySQL connection
// and its connection-scoped advisory lock.
type migrationLockCoordinator struct {
	runner *MigrationRunner
}

func (r *MigrationRunner) withLock(ctx context.Context, operation func(context.Context, *sql.Conn) error) error {
	return (migrationLockCoordinator{runner: r}).run(ctx, operation)
}
