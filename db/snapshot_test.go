package db_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	migrationassets "github.com/ferdoran/go-sro/db/migrations"
	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

func TestFreshInstallSnapshotsContainCompleteMigrationHistory(t *testing.T) {
	t.Parallel()

	targets := []struct {
		target   frameworkdb.MigrationTarget
		snapshot string
	}{
		{target: frameworkdb.MigrationTargetAccount, snapshot: "create_sro_account.sql"},
		{target: frameworkdb.MigrationTargetShard, snapshot: "create_sro_shard.sql"},
	}

	for _, target := range targets {
		target := target
		t.Run(string(target.target), func(t *testing.T) {
			t.Parallel()

			catalog, err := frameworkdb.LoadMigrationCatalog(migrationassets.Files, string(target.target))
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

func TestFreshInstallSnapshotsContainNoLoginOrCharacterFixtures(t *testing.T) {
	account, err := os.ReadFile("create_sro_account.sql")
	if err != nil {
		t.Fatal(err)
	}
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"INSERT INTO `USER`", "'$2a$12$EUJB", "'test'", "'test2'"} {
		if strings.Contains(string(account), forbidden) {
			t.Fatalf("account snapshot retains forbidden login fixture %q", forbidden)
		}
	}
	for _, forbidden := range []string{"INSERT INTO `CHAR` ", "'Chinse'", "'Mongo'", "INSERT INTO `INVENTORY` VALUES", "INSERT INTO `CHAR_MASTERY` VALUES", "INSERT INTO `CHAR_QUEST` VALUES", "INSERT INTO `CHAR_SKILL` VALUES"} {
		if strings.Contains(string(shard), forbidden) {
			t.Fatalf("shard snapshot retains forbidden character fixture %q", forbidden)
		}
	}
}

func TestFreshShardSnapshotEnablesChineseAndEuropeanCharacterCreation(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"(1907,1,1,1,0,0,20,20,0,200,200,950,40,1091,25256,1,45)",
		"(14875,2,1,1,0,0,20,20,0,200,200,950,84,1070,26959,20,45)",
		"(14900,2,0,1,0,0,20,20,0,200,200,950,84,1070,26959,20,45)",
		"item.`TypeID_4` BETWEEN 7 AND 15",
		"item.`TypeID_3` IN (9,10,11)",
		"item.`Req_Level_Type_1` IN (1,513,514)",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks character creation policy %q", required)
		}
	}
}
