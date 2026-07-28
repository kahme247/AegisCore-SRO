package db

import "testing"

func TestMigrationPlannerKeepsApplyAndRollbackOrder(t *testing.T) {
	catalog, err := NewMigrationCatalog(MigrationTargetShard, []Migration{
		{Version: 1, Name: "one", UpSQL: "SELECT 1", DownSQL: "SELECT 1"},
		{Version: 2, Name: "two", UpSQL: "SELECT 1", DownSQL: "SELECT 1"},
		{Version: 3, Name: "three", UpSQL: "SELECT 1", DownSQL: "SELECT 1"},
	})
	if err != nil {
		t.Fatalf("NewMigrationCatalog() error = %v", err)
	}
	applied := []migrationLedgerEntry{
		{Version: 1, Name: "one", Checksum: MigrationChecksum(catalog.migrations[0]), State: migrationStateApplied},
		{Version: 2, Name: "two", Checksum: MigrationChecksum(catalog.migrations[1]), State: migrationStateApplied},
	}
	pending, err := planPendingMigrations(catalog, applied)
	if err != nil || len(pending.migrations) != 1 || pending.migrations[0].Version != 3 {
		t.Fatalf("pending plan = %+v, error = %v", pending, err)
	}
	rollback, err := planRollbackMigrations(catalog, applied, 2)
	if err != nil || len(rollback.migrations) != 2 || rollback.migrations[0].Version != 2 || rollback.migrations[1].Version != 1 {
		t.Fatalf("rollback plan = %+v, error = %v", rollback, err)
	}
}
