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
					t.Errorf("snapshot lacks applied ledger entry for migration %06d_%s with checksum %s", migration.Version, migration.Name, migration.Checksum)
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
	contents := string(shard)
	for _, continent := range []string{"Constantinople", "DELTA", "Donwhang", "Hotan", "Jangan", "Samarkand", "SD", "KingsValley", "Roc", "TQ", "TEMPLE"} {
		if !strings.Contains(contents, "'"+continent+"',1") {
			t.Fatalf("shard snapshot does not enable continent %q", continent)
		}
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

func TestFreshShardSnapshotContainsAggregateRuntimeFoundations(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"CREATE TABLE `AEGIS_AGGREGATE_VERSION`",
		"CREATE TABLE `AEGIS_COMMAND_IDEMPOTENCY`",
		"CREATE TABLE `AEGIS_OUTBOX_MESSAGE`",
		"CONSTRAINT `FK_AEGIS_COMMAND_AGGREGATE`",
		"CONSTRAINT `FK_AEGIS_OUTBOX_AGGREGATE`",
		"KEY `IX_AEGIS_OUTBOX_DISPATCH`",
		"`LEASE_OWNER` varchar(128)",
		"CONSTRAINT `CK_AEGIS_OUTBOX_STATE`",
		"CREATE TABLE `AEGIS_SCHEDULE_DEFINITION`",
		"CREATE TABLE `AEGIS_SCHEDULE_RUN`",
		"UNIQUE KEY `UX_AEGIS_SCHEDULE_RUN_OCCURRENCE`",
		"CONSTRAINT `CK_AEGIS_SCHEDULE_RUN_STATE`",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks aggregate runtime contract %q", required)
		}
	}
}

func TestFreshShardSnapshotContainsDimensionHoleWorldRegions(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"CREATE TABLE `REF_GAME_WORLD_REGION`",
		"PRIMARY KEY (`WORLD_ID`,`REGION_ID`)",
		"KEY `idx_ref_game_world_region_region` (`REGION_ID`)",
		"CONSTRAINT `fk_ref_game_world_region_world`",
		"FOREIGN KEY (`WORLD_ID`) REFERENCES `REF_GAME_WORLD` (`WORLD_ID`) ON DELETE CASCADE",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks Dimension Hole world-region contract %q", required)
		}
	}
}

func TestFreshShardSnapshotContainsDimensionHoleStartPositions(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"CREATE TABLE `REF_INSTANCE_WORLD_START_POSITION`",
		"PRIMARY KEY (`WORLD_ID`,`START_INDEX`)",
		"CONSTRAINT `fk_ref_instance_world_start_position_world`",
		"(63,0,32484,1063,693,1125,0)",
		"(69,0,-32750,10897,165,-6110,0)",
		"(84,0,28644,1257,-247,1771,0)",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks Dimension Hole start-position contract %q", required)
		}
	}
}

func TestFreshShardSnapshotContainsQuestReferenceCatalog(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"CREATE TABLE `REF_QUEST`",
		"CREATE TABLE `REF_QUEST_REWARD`",
		"CREATE TABLE `REF_QUEST_REWARD_ITEM`",
		"CREATE TABLE `REF_QUEST_SCRIPT_DECLARATION`",
		"CREATE TABLE `REF_QUEST_KILL_OBJECTIVE`",
		"CREATE TABLE `REF_QUEST_DELIVER_ITEM_EX`",
		"CREATE TABLE `REF_QUEST_DELIVER_ITEM`",
		"CREATE TABLE `REF_QUEST_DELIVER_ITEM_ENTRY`",
		"CREATE TABLE `REF_QUEST_DIALOG_OBJECTIVE`",
		"CREATE TABLE `REF_QUEST_KILL_PLAYER_OBJECTIVE`",
		"CREATE TABLE `REF_QUEST_KILL_PLAYER_JOB_STATE`",
		"CREATE TABLE `REF_QUEST_COLLECTION_ITEM_OBJECTIVE`",
		"CREATE TABLE `REF_QUEST_COLLECTION_ITEM_SOURCE`",
		"CREATE TABLE `REF_QUEST_EXTENDED_KILL_OBJECTIVE`",
		"CREATE TABLE `REF_QUEST_EXTENDED_KILL_TARGET`",
		"CREATE TABLE `REF_QUEST_TALISMAN_OBJECTIVE`",
		"CREATE TABLE `REF_QUEST_START_CONDITION`",
		"CREATE TABLE `REF_QUEST_START_METHOD`",
		"CREATE TABLE `REF_QUEST_START_NPC`",
		"CREATE TABLE `REF_QUEST_DEPENDENCY_GROUP`",
		"CREATE TABLE `REF_QUEST_DEPENDENCY_ENTRY`",
		"CREATE TABLE `REF_QUEST_COMPLETION_POLICY`",
		"CREATE TABLE `REF_QUEST_JOB_START_CONDITION`",
		"CREATE TABLE `REF_QUEST_RACE_START_CONDITION`",
		"CREATE TABLE `REF_QUEST_SCRIPT_POINT_REWARD`",
		"CREATE TABLE `REF_QUEST_SCRIPT_REWARD`",
		"CREATE TABLE `REF_QUEST_SCRIPT_REWARD_ITEM`",
		"CONSTRAINT `FK_REF_QUEST_REWARD_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_REWARD_ITEM_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_SCRIPT_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_KILL_TARGET`",
		"CONSTRAINT `FK_REF_QUEST_DELIVER_EX_ITEM`",
		"CONSTRAINT `FK_REF_QUEST_DELIVER_ENTRY_OBJECTIVE`",
		"CONSTRAINT `FK_REF_QUEST_DELIVER_ENTRY_ITEM`",
		"CONSTRAINT `FK_REF_QUEST_DIALOG_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_KILL_PLAYER_NPC`",
		"CONSTRAINT `FK_REF_QUEST_KILL_PLAYER_STATE_OBJECTIVE`",
		"CONSTRAINT `FK_REF_QUEST_COLLECTION_ITEM`",
		"CONSTRAINT `FK_REF_QUEST_COLLECTION_SOURCE_MONSTER`",
		"CONSTRAINT `FK_REF_QUEST_EXTENDED_KILL_TARGET_OBJECTIVE`",
		"CONSTRAINT `FK_REF_QUEST_EXTENDED_KILL_TARGET_OBJECT`",
		"CONSTRAINT `FK_REF_QUEST_TALISMAN_NPC`",
		"CONSTRAINT `FK_REF_QUEST_START_NPC_OBJECT`",
		"CONSTRAINT `FK_REF_QUEST_DEPENDENCY_ENTRY_GROUP`",
		"CONSTRAINT `FK_REF_QUEST_DEPENDENCY_ENTRY_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_COMPLETION_POLICY_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_JOB_START_CONDITION_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_SCRIPT_POINT_REWARD_QUEST`",
		"CONSTRAINT `FK_REF_QUEST_SCRIPT_REWARD_ITEM_REWARD`",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks quest reference contract %q", required)
		}
	}
}

func TestFreshShardSnapshotContainsRuntimeMediaCatalogData(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, required := range []string{
		"INSERT INTO `REF_LEVEL_DATA` VALUES",
		"INSERT INTO `REF_MAGIC_OPTION` ",
		"INSERT INTO `ITEM_MALL_GOODS` VALUES",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("shard snapshot lacks runtime Media catalog data %q", required)
		}
	}
	if count := strings.Count(contents, "CREATE TABLE `REF_MAGIC_OPTION`"); count != 1 {
		t.Fatalf("REF_MAGIC_OPTION table definitions = %d, want 1", count)
	}
}

func TestFreshShardSnapshotContainsDropCatalogTables(t *testing.T) {
	shard, err := os.ReadFile("create_sro_shard.sql")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(shard)
	for _, table := range []string{
		"CREATE TABLE `REF_DROP_GOLD`",
		"CREATE TABLE `REF_DROP_ITEM_GROUP`",
		"CREATE TABLE `REF_MONSTER_ASSIGNED_ITEM_RND_DROP`",
		"CREATE TABLE `REF_MONSTER_ASSIGNED_ITEM_DROP`",
	} {
		if !strings.Contains(contents, table) {
			t.Fatalf("shard snapshot lacks drop catalog table %q", table)
		}
	}
	if !strings.Contains(contents, "(78,'drop_reference_catalog','29b2660fb35dbc3df74acb7a37af523bf14d46ce74e6918bb33445ad08aeb316','applied'") {
		t.Fatal("shard snapshot lacks drop catalog migration ledger entry")
	}
	if !strings.Contains(contents, "CREATE TABLE `REF_SERVER_EVENT_REWARD`") ||
		!strings.Contains(contents, "CREATE TABLE `REF_SERVER_EVENT_REWARD_EVENT`") {
		t.Fatal("shard snapshot lacks server-event reward catalog tables")
	}
	if !strings.Contains(contents, "(79,'server_event_reward_catalog','837f3c1dcb0c4477e3c512d386375f12adce7a9b17b3e064a3102001af05caaa','applied'") {
		t.Fatal("shard snapshot lacks server-event reward migration ledger entry")
	}
	if !strings.Contains(contents, "`REF_MAGIC_GROUP_ID` int unsigned NOT NULL DEFAULT '0'") {
		t.Fatal("shard snapshot lacks drop-item magic-group column")
	}
	if !strings.Contains(contents, "(80,'drop_item_group_magic_reference','f7492f77c353bfcd30a132a72ad594cbb84b116939dc61b34dae656b07655077','applied'") {
		t.Fatal("shard snapshot lacks drop-item magic-group migration ledger entry")
	}
}
