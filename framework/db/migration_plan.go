package db

import "fmt"

// migrationPlan is the immutable result of reconciling a catalog with the
// durable ledger. Planning is separate from SQL execution so Apply and
// Rollback cannot independently drift in their history interpretation.
type migrationPlan struct {
	migrations []Migration
}

func planPendingMigrations(catalog MigrationCatalog, applied []migrationLedgerEntry) (migrationPlan, error) {
	appliedByVersion, err := verifyMigrationHistory(catalog, applied)
	if err != nil {
		return migrationPlan{}, err
	}
	pending := make([]Migration, 0, len(catalog.migrations)-len(appliedByVersion))
	for _, migration := range catalog.migrations {
		if _, exists := appliedByVersion[migration.Version]; !exists {
			pending = append(pending, migration)
		}
	}
	return migrationPlan{migrations: pending}, nil
}

func planRollbackMigrations(catalog MigrationCatalog, applied []migrationLedgerEntry, steps int) (migrationPlan, error) {
	appliedByVersion, err := verifyMigrationHistory(catalog, applied)
	if err != nil {
		return migrationPlan{}, err
	}
	appliedMigrations := make([]Migration, 0, len(appliedByVersion))
	for _, migration := range catalog.migrations {
		if _, exists := appliedByVersion[migration.Version]; exists {
			appliedMigrations = append(appliedMigrations, migration)
		}
	}
	if steps > len(appliedMigrations) {
		return migrationPlan{}, fmt.Errorf("%w: requested %d steps but only %d migrations are applied", ErrMigrationRollback, steps, len(appliedMigrations))
	}
	rollback := make([]Migration, 0, steps)
	for index := len(appliedMigrations) - 1; index >= len(appliedMigrations)-steps; index-- {
		rollback = append(rollback, appliedMigrations[index])
	}
	return migrationPlan{migrations: rollback}, nil
}
