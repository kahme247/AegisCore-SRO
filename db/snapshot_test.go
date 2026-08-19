package db_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	migrationassets "github.com/Harvi8/AegisCore/db/migrations"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
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

func TestFreshAccountSnapshotContainsControlledPatchCatalog(t *testing.T) {
	account, err := os.ReadFile("create_sro_account.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(account)
	for _, required := range []string{
		"CREATE TABLE `PATCH_MODULE_STATE`",
		"`ACTIVE_VERSION` int unsigned NOT NULL",
		"CREATE TABLE `PATCH_RELEASE`",
		"CREATE TABLE `PATCH_RELEASE_FILE`",
		"`PREPARED_SHA256` char(64)",
		"CREATE TABLE `PATCH_DOWNLOAD_SERVER`",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("account snapshot lacks patch catalog contract %q", required)
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
		"(1907,1,1,1,0,0,20,20,0,200,200,950,40,1091,25256,1,109)",
		"(14875,2,1,1,0,0,20,20,0,200,200,950,84,1070,26959,20,109)",
		"(14900,2,0,1,0,0,20,20,0,200,200,950,84,1070,26959,20,109)",
		"item.`TypeID_4` BETWEEN 7 AND 15",
		"item.`TypeID_3` IN (9,10,11)",
		"item.`Req_Level_Type_1` IN (1,513,514)",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks character creation policy %q", required)
		}
	}
}

func TestFreshShardSnapshotEnablesSupportedContinents(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shard), "('Constantinople',1),('DELTA',1),('Donwhang',1),('Hotan',1),('Jangan',1),('Samarkand',1)") {
		t.Fatal("shard snapshot does not enable the supported continent set")
	}
}

func TestFreshShardSnapshotContainsAlexandriaTravelCatalog(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"`MIN_LEVEL` tinyint unsigned NOT NULL DEFAULT '1'",
		"(175,'GATE_SD1',27753,23088,663,863,526,1,1)",
		"(176,'GATE_SD2',27754,23603,111,1537,524,1,1)",
		"(1,175,40000,95,1)",
		"(5,176,30000,95,1)",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks Alexandria travel data %q", required)
		}
	}
}

func TestFreshShardSnapshotContainsCharacterBuyback(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"CREATE TABLE `CHAR_BUYBACK`",
		"PRIMARY KEY (`FK_CHAR`,`SLOT`)",
		"UNIQUE KEY `UX_CHAR_BUYBACK_ITEM` (`FK_ITEM`)",
		"CONSTRAINT `CK_CHAR_BUYBACK_SLOT` CHECK ((`SLOT` < 5))",
		"CONSTRAINT `FK_CHAR_BUYBACK_CHAR`",
		"CONSTRAINT `FK_CHAR_BUYBACK_ITEM`",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks buy-back contract %q", required)
		}
	}
}
