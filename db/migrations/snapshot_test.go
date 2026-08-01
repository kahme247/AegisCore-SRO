package migrations

import (
	"fmt"
	"os"
	"strings"
	"testing"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

func TestFreshInstallSnapshotsContainCompleteMigrationHistory(t *testing.T) {
	t.Parallel()

	targets := []struct {
		target   frameworkdb.MigrationTarget
		snapshot string
	}{
		{target: frameworkdb.MigrationTargetAccount, snapshot: "../create_sro_account.sql"},
		{target: frameworkdb.MigrationTargetShard, snapshot: "../create_sro_shard.sql"},
	}

	for _, target := range targets {
		target := target
		t.Run(string(target.target), func(t *testing.T) {
			t.Parallel()

			catalog, err := frameworkdb.LoadMigrationCatalog(Files, string(target.target))
			if err != nil {
				t.Fatalf("LoadMigrationCatalog: %v", err)
			}
			snapshot, err := os.ReadFile(target.snapshot)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", target.snapshot, err)
			}

			contents := string(snapshot)
			for _, migration := range catalog.Migrations() {
				ledgerEntry := fmt.Sprintf("(%d,'%s','%s','applied'", migration.Version, migration.Name, migration.Checksum)
				if !strings.Contains(contents, ledgerEntry) {
					t.Errorf("snapshot lacks applied ledger entry for migration %06d_%s", migration.Version, migration.Name)
				}
			}
		})
	}
}
