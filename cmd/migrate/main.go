// Command migrate applies one target-bound schema catalog during an explicitly
// invoked maintenance window. It is intentionally not imported by either live
// server and has no automatic startup wiring.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	migrationassets "github.com/ferdoran/go-sro/db/migrations"
	"github.com/ferdoran/go-sro/framework/config"
	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "migration job failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) (runErr error) {
	target, err := selectMigrationTarget(arguments)
	if err != nil {
		return err
	}

	// The job opens only the selected target's bounded pool. Requiring the
	// unrelated schema to be online would make an account-only maintenance run
	// depend on shard availability (and vice versa). Target validation on the
	// runner's pinned connection remains the final guard before a ledger or DDL
	// can be created.
	config.Initialize()
	pool, err := openMigrationPool(ctx, target)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := pool.Close(); closeErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close %s database pool: %w", target, closeErr))
		}
	}()

	catalog, err := frameworkdb.LoadMigrationCatalog(migrationassets.Files, string(target))
	if err != nil {
		return fmt.Errorf("load %s migration catalog: %w", target, err)
	}

	runner, err := frameworkdb.NewMigrationRunner(pool, frameworkdb.MigrationRunnerOptions{
		Target:   target,
		LockName: "go-sro:migrations:" + string(target),
	})
	if err != nil {
		return fmt.Errorf("configure %s migration runner: %w", target, err)
	}
	if err := runner.Apply(ctx, catalog); err != nil {
		return fmt.Errorf("apply %s migrations: %w", target, err)
	}

	fmt.Printf("%s migrations applied successfully\n", target)
	return nil
}

func openMigrationPool(ctx context.Context, target frameworkdb.MigrationTarget) (*sql.DB, error) {
	switch target {
	case frameworkdb.MigrationTargetAccount:
		pool, err := frameworkdb.OpenPool(ctx, frameworkdb.AccountPool, frameworkdb.AccountPoolConfig())
		if err != nil {
			return nil, fmt.Errorf("open account database pool: %w", err)
		}
		return pool, nil
	case frameworkdb.MigrationTargetShard:
		pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
		if err != nil {
			return nil, fmt.Errorf("open shard database pool: %w", err)
		}
		return pool, nil
	default:
		return nil, fmt.Errorf("%w: unsupported target %q", errMigrationTargetSelection, target)
	}
}
