package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const migrationTestDriverName = "go-sro-migration-test"

var migrationTestState struct {
	sync.Mutex

	nextConnectionID  int
	closedConnections []int
	lockResult        int64
	releaseResult     int64
	lockConnection    int
	releaseConnection int

	databaseName         string
	ledgerExists         bool
	ledgerHasStateColumn bool
	ledger               []migrationLedgerEntry
	operations           []string
	execErrors           map[string]error
	queryErrors          map[string]error
	preflightRows        map[string]bool
	commitError          error
}

func init() {
	sql.Register(migrationTestDriverName, migrationTestDriver{})
}

type migrationTestDriver struct{}

func (migrationTestDriver) Open(string) (driver.Conn, error) {
	migrationTestState.Lock()
	migrationTestState.nextConnectionID++
	connectionID := migrationTestState.nextConnectionID
	migrationTestState.Unlock()
	return &migrationTestConn{connectionID: connectionID}, nil
}

type migrationTestConn struct {
	connectionID int
	closed       bool
}

func (c *migrationTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not used by the migration test driver")
}

func (c *migrationTestConn) Close() error {
	migrationTestState.Lock()
	defer migrationTestState.Unlock()
	if !c.closed {
		c.closed = true
		migrationTestState.closedConnections = append(migrationTestState.closedConnections, c.connectionID)
	}
	return nil
}

func (*migrationTestConn) Begin() (driver.Tx, error) {
	return &migrationTestTx{}, nil
}

func (c *migrationTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	query = strings.TrimSpace(query)
	migrationTestState.Lock()
	defer migrationTestState.Unlock()

	if err := migrationTestState.queryErrors[query]; err != nil {
		return nil, err
	}

	switch {
	case query == "SELECT DATABASE()":
		migrationTestState.operations = append(migrationTestState.operations, "database:read")
		return &migrationRows{columns: []string{"DATABASE()"}, values: [][]driver.Value{{migrationTestState.databaseName}}}, nil
	case strings.HasPrefix(query, "SELECT GET_LOCK"):
		migrationTestState.lockConnection = c.connectionID
		migrationTestState.operations = append(migrationTestState.operations, "lock:get")
		return &migrationRows{columns: []string{"GET_LOCK"}, values: [][]driver.Value{{migrationTestState.lockResult}}}, nil
	case strings.HasPrefix(query, "SELECT RELEASE_LOCK"):
		migrationTestState.releaseConnection = c.connectionID
		migrationTestState.operations = append(migrationTestState.operations, "lock:release")
		return &migrationRows{columns: []string{"RELEASE_LOCK"}, values: [][]driver.Value{{migrationTestState.releaseResult}}}, nil
	case strings.HasPrefix(query, "SELECT COUNT(*) FROM information_schema.columns"):
		migrationTestState.operations = append(migrationTestState.operations, "ledger:state-column")
		stateColumnCount := int64(0)
		if migrationTestState.ledgerHasStateColumn {
			stateColumnCount = 1
		}
		return &migrationRows{columns: []string{"COUNT(*)"}, values: [][]driver.Value{{stateColumnCount}}}, nil
	case strings.Contains(query, "FROM `"+MigrationLedgerTable+"`"):
		migrationTestState.operations = append(migrationTestState.operations, "ledger:read")
		values := make([][]driver.Value, 0, len(migrationTestState.ledger))
		for _, entry := range migrationTestState.ledger {
			values = append(values, []driver.Value{int64(entry.Version), entry.Name, entry.Checksum, string(entry.State)})
		}
		return &migrationRows{columns: []string{"version", "name", "checksum", "state"}, values: values}, nil
	case strings.Contains(query, "PREFLIGHT_"):
		migrationTestState.operations = append(migrationTestState.operations, "preflight:"+query)
		if migrationTestState.preflightRows[query] {
			return &migrationRows{columns: []string{"violation"}, values: [][]driver.Value{{"duplicate"}}}, nil
		}
		return &migrationRows{columns: []string{"violation"}}, nil
	default:
		return nil, fmt.Errorf("unexpected migration test query %q", query)
	}
}

func (c *migrationTestConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	query = strings.TrimSpace(query)
	migrationTestState.Lock()
	defer migrationTestState.Unlock()

	if err := migrationTestState.execErrors[query]; err != nil {
		migrationTestState.operations = append(migrationTestState.operations, "exec-error:"+query)
		return nil, err
	}

	switch {
	case strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS `"+MigrationLedgerTable+"`"):
		migrationTestState.operations = append(migrationTestState.operations, "ledger:create")
		if !migrationTestState.ledgerExists {
			migrationTestState.ledgerExists = true
			migrationTestState.ledgerHasStateColumn = true
		}
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(query, "ALTER TABLE `"+MigrationLedgerTable+"` ADD COLUMN `state`"):
		migrationTestState.ledgerHasStateColumn = true
		for index := range migrationTestState.ledger {
			if migrationTestState.ledger[index].State == "" {
				migrationTestState.ledger[index].State = migrationStateApplied
			}
		}
		migrationTestState.operations = append(migrationTestState.operations, "ledger:state-column-upgrade")
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(query, "INSERT INTO `"+MigrationLedgerTable+"`"):
		if len(arguments) != 4 {
			return nil, fmt.Errorf("ledger insert arguments = %d, want 4", len(arguments))
		}
		version, ok := driverValueUint64(arguments[0].Value)
		if !ok {
			return nil, fmt.Errorf("ledger version argument %T is not an unsigned integer", arguments[0].Value)
		}
		name, ok := driverValueString(arguments[1].Value)
		if !ok {
			return nil, fmt.Errorf("ledger name argument %T is not a string", arguments[1].Value)
		}
		checksum, ok := driverValueString(arguments[2].Value)
		if !ok {
			return nil, fmt.Errorf("ledger checksum argument %T is not a string", arguments[2].Value)
		}
		state, ok := driverValueString(arguments[3].Value)
		if !ok {
			return nil, fmt.Errorf("ledger state argument %T is not a string", arguments[3].Value)
		}
		migrationTestState.ledger = append(migrationTestState.ledger, migrationLedgerEntry{Version: version, Name: name, Checksum: checksum, State: migrationLedgerState(state)})
		migrationTestState.operations = append(migrationTestState.operations, fmt.Sprintf("ledger:insert:%d", version))
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "UPDATE `"+MigrationLedgerTable+"` SET `state`=?"):
		if len(arguments) != 5 {
			return nil, fmt.Errorf("ledger state update arguments = %d, want 5", len(arguments))
		}
		nextState, ok := driverValueString(arguments[0].Value)
		if !ok {
			return nil, fmt.Errorf("ledger next state argument %T is not a string", arguments[0].Value)
		}
		version, ok := driverValueUint64(arguments[1].Value)
		if !ok {
			return nil, fmt.Errorf("ledger version argument %T is not an unsigned integer", arguments[1].Value)
		}
		name, nameOK := driverValueString(arguments[2].Value)
		checksum, checksumOK := driverValueString(arguments[3].Value)
		expectedState, stateOK := driverValueString(arguments[4].Value)
		if !nameOK || !checksumOK || !stateOK {
			return nil, fmt.Errorf("ledger state update arguments are invalid")
		}
		updated := int64(0)
		for index := range migrationTestState.ledger {
			entry := &migrationTestState.ledger[index]
			if entry.Version != version || entry.Name != name || entry.Checksum != checksum || entry.State != migrationLedgerState(expectedState) {
				continue
			}
			entry.State = migrationLedgerState(nextState)
			updated++
		}
		migrationTestState.operations = append(migrationTestState.operations, fmt.Sprintf("ledger:state:%s:%d", nextState, version))
		return driver.RowsAffected(updated), nil
	case strings.HasPrefix(query, "DELETE FROM `"+MigrationLedgerTable+"`"):
		if len(arguments) != 4 {
			return nil, fmt.Errorf("ledger delete arguments = %d, want 4", len(arguments))
		}
		version, ok := driverValueUint64(arguments[0].Value)
		if !ok {
			return nil, fmt.Errorf("ledger version argument %T is not an unsigned integer", arguments[0].Value)
		}
		name, nameOK := driverValueString(arguments[1].Value)
		checksum, checksumOK := driverValueString(arguments[2].Value)
		expectedState, stateOK := driverValueString(arguments[3].Value)
		if !nameOK || !checksumOK || !stateOK {
			return nil, fmt.Errorf("ledger delete arguments are invalid")
		}
		removed := int64(0)
		remaining := migrationTestState.ledger[:0]
		for _, entry := range migrationTestState.ledger {
			if entry.Version == version && entry.Name == name && entry.Checksum == checksum && entry.State == migrationLedgerState(expectedState) {
				removed++
				continue
			}
			remaining = append(remaining, entry)
		}
		migrationTestState.ledger = remaining
		migrationTestState.operations = append(migrationTestState.operations, fmt.Sprintf("ledger:delete:%d", version))
		return driver.RowsAffected(removed), nil
	default:
		migrationTestState.operations = append(migrationTestState.operations, "sql:"+query)
		return driver.RowsAffected(0), nil
	}
}

type migrationTestTx struct{}

func (*migrationTestTx) Commit() error {
	migrationTestState.Lock()
	defer migrationTestState.Unlock()
	return migrationTestState.commitError
}

func (*migrationTestTx) Rollback() error { return nil }

type migrationRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r migrationRows) Columns() []string { return r.columns }
func (r migrationRows) Close() error      { return nil }

func (r *migrationRows) Next(destination []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(destination, r.values[r.index])
	r.index++
	return nil
}

func driverValueUint64(value any) (uint64, bool) {
	switch typed := value.(type) {
	case int64:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	case uint64:
		return typed, true
	case []byte:
		var parsed uint64
		_, err := fmt.Sscan(string(typed), &parsed)
		return parsed, err == nil
	case string:
		var parsed uint64
		_, err := fmt.Sscan(typed, &parsed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func driverValueString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	default:
		return "", false
	}
}

func resetMigrationTestDriver() {
	migrationTestState.Lock()
	migrationTestState.nextConnectionID = 0
	migrationTestState.closedConnections = nil
	migrationTestState.lockResult = 1
	migrationTestState.releaseResult = 1
	migrationTestState.lockConnection = 0
	migrationTestState.releaseConnection = 0
	migrationTestState.databaseName = "SRO_ACCOUNT"
	migrationTestState.ledgerExists = false
	migrationTestState.ledgerHasStateColumn = false
	migrationTestState.ledger = nil
	migrationTestState.operations = nil
	migrationTestState.execErrors = make(map[string]error)
	migrationTestState.queryErrors = make(map[string]error)
	migrationTestState.preflightRows = make(map[string]bool)
	migrationTestState.commitError = nil
	migrationTestState.Unlock()
}

func migrationTestSnapshot() (operations []string, ledger []migrationLedgerEntry, lockConnection, releaseConnection int) {
	migrationTestState.Lock()
	defer migrationTestState.Unlock()
	return append([]string(nil), migrationTestState.operations...), append([]migrationLedgerEntry(nil), migrationTestState.ledger...), migrationTestState.lockConnection, migrationTestState.releaseConnection
}

func migrationTestClosedConnections() []int {
	migrationTestState.Lock()
	defer migrationTestState.Unlock()
	return append([]int(nil), migrationTestState.closedConnections...)
}

func migrationTestSetLedger(entries ...migrationLedgerEntry) {
	migrationTestState.Lock()
	defer migrationTestState.Unlock()
	migrationTestState.ledgerExists = true
	migrationTestState.ledgerHasStateColumn = true
	migrationTestState.ledger = append([]migrationLedgerEntry(nil), entries...)
}

func migrationTestRunner(t *testing.T) *MigrationRunner {
	return migrationTestRunnerForTarget(t, MigrationTargetAccount)
}

func migrationTestRunnerForTarget(t *testing.T, target MigrationTarget) *MigrationRunner {
	t.Helper()
	resetMigrationTestDriver()

	database, err := sql.Open(migrationTestDriverName, "migrations")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	runner, err := NewMigrationRunner(database, MigrationRunnerOptions{
		Target:      target,
		LockName:    "go-sro:migrations:test",
		LockTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewMigrationRunner: %v", err)
	}
	return runner
}

func migrationTestCatalog(t *testing.T, migrations ...Migration) MigrationCatalog {
	return migrationTestCatalogForTarget(t, MigrationTargetAccount, migrations...)
}

func migrationTestCatalogForTarget(t *testing.T, target MigrationTarget, migrations ...Migration) MigrationCatalog {
	t.Helper()
	catalog, err := NewMigrationCatalog(target, migrations)
	if err != nil {
		t.Fatalf("NewMigrationCatalog: %v", err)
	}
	return catalog
}

func migrationTestPreflight(label string) string {
	return "SELECT 'PREFLIGHT_" + label + "' WHERE FALSE"
}

func TestLoadMigrationCatalogReadsSortedChecksumProtectedSQL(t *testing.T) {
	migrationsRoot := filepath.Join("..", "..", "db", "migrations")
	catalog, err := LoadMigrationCatalog(os.DirFS(migrationsRoot), "account")
	if err != nil {
		t.Fatalf("LoadMigrationCatalog: %v", err)
	}
	migrations := catalog.Migrations()
	if len(migrations) != 1 {
		t.Fatalf("account migration count = %d, want 1", len(migrations))
	}
	if catalog.Target() != MigrationTargetAccount {
		t.Fatalf("account catalog target = %q, want %q", catalog.Target(), MigrationTargetAccount)
	}
	migration := migrations[0]
	if migration.Version != 1 || migration.Name != "normalize_username" {
		t.Fatalf("migration = %#v, want version 1 normalize_username", migration)
	}
	if migration.Checksum == "" || migration.Checksum != MigrationChecksum(migration) {
		t.Fatal("catalog did not assign the checksum derived from the complete migration")
	}
	if !strings.Contains(migration.PreflightSQL, "HAVING COUNT(*) > 1") {
		t.Fatal("username migration is missing duplicate preflight SQL")
	}
	if !strings.Contains(migration.UpSQL, "UX_USER_USERNAME_NORMALIZED") || !strings.Contains(migration.DownSQL, "DROP INDEX") {
		t.Fatal("username migration is not a reversible normalized unique-key migration")
	}
	if strings.Contains(migration.UpSQL, "SRO_ACCOUNT") || strings.Contains(migration.PreflightSQL, "SRO_ACCOUNT") {
		t.Fatal("account migration SQL must use the runner-validated selected schema, not hard-coded qualifiers")
	}

	shardCatalog, err := LoadMigrationCatalog(os.DirFS(migrationsRoot), "shard")
	if err != nil {
		t.Fatalf("LoadMigrationCatalog shard: %v", err)
	}
	shardMigrations := shardCatalog.Migrations()
	if shardCatalog.Target() != MigrationTargetShard {
		t.Fatalf("shard catalog target = %q, want %q", shardCatalog.Target(), MigrationTargetShard)
	}
	if len(shardMigrations) != 26 {
		t.Fatalf("shard migration count = %d, want 26", len(shardMigrations))
	}
	returnCityMigration := shardMigrations[len(shardMigrations)-5]
	if returnCityMigration.Version != 23 || returnCityMigration.Name != "character_return_city" ||
		!strings.Contains(returnCityMigration.UpSQL, "RETURN_TELEPORT_ID") || !strings.Contains(returnCityMigration.DownSQL, "DROP COLUMN") {
		t.Fatalf("return city migration = %#v, want reversible version 23 character_return_city", returnCityMigration)
	}
	progressionRatesMigration := shardMigrations[len(shardMigrations)-4]
	if progressionRatesMigration.Version != 24 || progressionRatesMigration.Name != "gameplay_progression_rates" ||
		!strings.Contains(progressionRatesMigration.UpSQL, "GAMEPLAY_PROGRESSION_RATES") ||
		!strings.Contains(progressionRatesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("progression rates migration = %#v, want reversible version 24 gameplay_progression_rates", progressionRatesMigration)
	}
	mobRatesMigration := shardMigrations[len(shardMigrations)-3]
	if mobRatesMigration.Version != 25 || mobRatesMigration.Name != "gameplay_mob_rates" ||
		!strings.Contains(mobRatesMigration.UpSQL, "GAMEPLAY_MOB_RATES") ||
		!strings.Contains(mobRatesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("mob rates migration = %#v, want reversible version 25 gameplay_mob_rates", mobRatesMigration)
	}
	quickSlotsMigration := shardMigrations[len(shardMigrations)-2]
	if quickSlotsMigration.Version != 26 || quickSlotsMigration.Name != "character_quick_slots" ||
		!strings.Contains(quickSlotsMigration.UpSQL, "CHAR_QUICK_SLOT") ||
		!strings.Contains(quickSlotsMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quick-slot migration = %#v, want reversible version 26 character_quick_slots", quickSlotsMigration)
	}
	speedEffectMigration := shardMigrations[len(shardMigrations)-1]
	if speedEffectMigration.Version != 27 || speedEffectMigration.Name != "character_speed_effect" ||
		!strings.Contains(speedEffectMigration.UpSQL, "CHAR_SPEED_EFFECT") ||
		!strings.Contains(speedEffectMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("speed-effect migration = %#v, want reversible version 27 character_speed_effect", speedEffectMigration)
	}
	shardMigration := shardMigrations[0]
	if shardMigration.Name != "normalize_character_name" || !strings.Contains(shardMigration.PreflightSQL, "HAVING COUNT(*) > 1") {
		t.Fatal("character migration is missing its normalized duplicate preflight")
	}
	inventoryMigration := shardMigrations[1]
	if inventoryMigration.Version != 2 || inventoryMigration.Name != "unique_inventory_item_link" {
		t.Fatalf("inventory migration = %#v, want version 2 unique_inventory_item_link", inventoryMigration)
	}
	if !strings.Contains(inventoryMigration.PreflightSQL, "GROUP BY `FK_ITEM`") ||
		!strings.Contains(inventoryMigration.UpSQL, "UX_INVENTORY_FK_ITEM") ||
		!strings.Contains(inventoryMigration.DownSQL, "DROP INDEX") {
		t.Fatal("inventory migration is missing its duplicate preflight or reversible unique key")
	}
	questMigration := shardMigrations[2]
	if questMigration.Version != 3 || questMigration.Name != "character_quests" ||
		!strings.Contains(questMigration.UpSQL, "CHAR_QUEST") || !strings.Contains(questMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest migration = %#v, want version 3 character_quests", questMigration)
	}
	economyMigration := shardMigrations[3]
	if economyMigration.Version != 4 || economyMigration.Name != "economy_storage" ||
		!strings.Contains(economyMigration.UpSQL, "SHOP_GOODS") || !strings.Contains(economyMigration.UpSQL, "STORAGE_INFO") ||
		!strings.Contains(economyMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("economy migration = %#v, want reversible version 4 economy_storage", economyMigration)
	}
	shopMigration := shardMigrations[4]
	if shopMigration.Version != 5 || shopMigration.Name != "reference_shop_catalog" ||
		!strings.Contains(shopMigration.UpSQL, "NPC_CH_ACCESSORY") || !strings.Contains(shopMigration.DownSQL, "DELETE FROM `SHOP_GOODS`") {
		t.Fatalf("shop migration = %#v, want version 5 reference_shop_catalog", shopMigration)
	}
	teleportMigration := shardMigrations[5]
	if teleportMigration.Version != 6 || teleportMigration.Name != "teleport_catalog" ||
		!strings.Contains(teleportMigration.UpSQL, "TELEPORT_POINT") || !strings.Contains(teleportMigration.UpSQL, "GATE_CH") ||
		!strings.Contains(teleportMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("teleport migration = %#v, want reversible version 6 teleport_catalog", teleportMigration)
	}
	masteryMigration := shardMigrations[6]
	if masteryMigration.Version != 8 || masteryMigration.Name != "character_masteries" {
		t.Fatalf("mastery migration = %#v, want version 8 character_masteries", masteryMigration)
	}
	guildStorageMigration := shardMigrations[7]
	if guildStorageMigration.Version != 9 || guildStorageMigration.Name != "guild_storage" ||
		!strings.Contains(guildStorageMigration.UpSQL, "GUILD_STORAGE_LEASE") ||
		!strings.Contains(guildStorageMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("guild storage migration = %#v, want reversible version 9 guild_storage", guildStorageMigration)
	}
	skillMigration := shardMigrations[8]
	if skillMigration.Version != 10 || skillMigration.Name != "character_skills" ||
		!strings.Contains(skillMigration.UpSQL, "CHAR_SKILL") ||
		!strings.Contains(skillMigration.UpSQL, "PRIMARY KEY (`CHAR_ID`, `SKILL_ID`)") ||
		!strings.Contains(skillMigration.UpSQL, "Default_Skill_10") ||
		!strings.Contains(skillMigration.UpSQL, "SELECT DISTINCT existing_skills") ||
		!strings.Contains(skillMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("skill migration = %#v, want reversible version 10 character_skills", skillMigration)
	}
	defaultMasteryMigration := shardMigrations[9]
	if defaultMasteryMigration.Version != 11 || defaultMasteryMigration.Name != "character_default_masteries" ||
		!strings.Contains(defaultMasteryMigration.UpSQL, "DROP CHECK `chk_char_mastery_level`") ||
		!strings.Contains(defaultMasteryMigration.UpSQL, "INSERT IGNORE INTO `CHAR_MASTERY`") ||
		!strings.Contains(defaultMasteryMigration.UpSQL, "UNION ALL SELECT 1, 518") ||
		!strings.Contains(defaultMasteryMigration.DownSQL, "CHECK (`LEVEL` > 0)") {
		t.Fatalf("default mastery migration = %#v, want reversible version 11 character_default_masteries", defaultMasteryMigration)
	}
	referenceMasteryMigration := shardMigrations[10]
	if referenceMasteryMigration.Version != 12 || referenceMasteryMigration.Name != "reference_default_masteries" ||
		!strings.Contains(referenceMasteryMigration.UpSQL, "CREATE TABLE `REF_CHAR_DEFAULT_MASTERY`") ||
		!strings.Contains(referenceMasteryMigration.UpSQL, "CHECK (`RACE` IN (0, 1, 3))") ||
		!strings.Contains(referenceMasteryMigration.UpSQL, "(0, 257, 0)") ||
		!strings.Contains(referenceMasteryMigration.UpSQL, "(1, 518, 0)") ||
		!strings.Contains(referenceMasteryMigration.UpSQL, "defaults.`RACE` IN (r.`Country`, 3)") ||
		!strings.Contains(referenceMasteryMigration.DownSQL, "DROP TABLE `REF_CHAR_DEFAULT_MASTERY`") {
		t.Fatalf("reference mastery migration = %#v, want reversible version 12 reference_default_masteries", referenceMasteryMigration)
	}
	referenceSkillMigration := shardMigrations[11]
	if referenceSkillMigration.Version != 13 || referenceSkillMigration.Name != "reference_default_skills" ||
		!strings.Contains(referenceSkillMigration.UpSQL, "CREATE TABLE `REF_CHAR_DEFAULT_SKILL`") ||
		!strings.Contains(referenceSkillMigration.UpSQL, "(0, 2, 1)") ||
		!strings.Contains(referenceSkillMigration.UpSQL, "(1, 11526, 1)") ||
		!strings.Contains(referenceSkillMigration.UpSQL, "(3, 1, 1)") ||
		!strings.Contains(referenceSkillMigration.UpSQL, "defaults.`RACE` IN (r.`Country`, 3)") ||
		!strings.Contains(referenceSkillMigration.DownSQL, "DROP TABLE `REF_CHAR_DEFAULT_SKILL`") {
		t.Fatalf("reference skill migration = %#v, want reversible version 13 reference_default_skills", referenceSkillMigration)
	}
	referenceQuestMigration := shardMigrations[12]
	if referenceQuestMigration.Version != 14 || referenceQuestMigration.Name != "reference_default_quests" ||
		!strings.Contains(referenceQuestMigration.UpSQL, "CREATE TABLE `REF_CHAR_DEFAULT_QUEST`") ||
		!strings.Contains(referenceQuestMigration.UpSQL, "(0, 2, 'QTUTORIAL_CH', 1, 1)") ||
		!strings.Contains(referenceQuestMigration.UpSQL, "(3, 1, 'QEVENT_GUIDE', 1, 1)") ||
		!strings.Contains(referenceQuestMigration.UpSQL, "(3, 397, 'QEV_ALL_BASIC_0', 1, 1)") ||
		!strings.Contains(referenceQuestMigration.UpSQL, "INSERT IGNORE INTO `CHAR_QUEST`") ||
		!strings.Contains(referenceQuestMigration.DownSQL, "DROP TABLE `REF_CHAR_DEFAULT_QUEST`") {
		t.Fatalf("reference quest migration = %#v, want reversible version 14 reference_default_quests", referenceQuestMigration)
	}
	referenceProgressionMigration := shardMigrations[13]
	if referenceProgressionMigration.Version != 15 || referenceProgressionMigration.Name != "reference_level_and_skill_data" ||
		!strings.Contains(referenceProgressionMigration.UpSQL, "CREATE TABLE `REF_LEVEL_DATA`") ||
		!strings.Contains(referenceProgressionMigration.UpSQL, "CREATE TABLE `REF_SKILL_DATA`") ||
		!strings.Contains(referenceProgressionMigration.UpSQL, "`RAW_FIELDS` JSON NOT NULL") ||
		!strings.Contains(referenceProgressionMigration.DownSQL, "DROP TABLE `REF_SKILL_DATA`") ||
		!strings.Contains(referenceProgressionMigration.DownSQL, "DROP TABLE `REF_LEVEL_DATA`") {
		t.Fatalf("reference progression migration = %#v, want reversible version 15 reference_level_and_skill_data", referenceProgressionMigration)
	}
	vsroSkillMigration := shardMigrations[14]
	if vsroSkillMigration.Version != 16 || vsroSkillMigration.Name != "vsro_reference_skill_catalog" ||
		!strings.Contains(vsroSkillMigration.UpSQL, "CREATE TABLE `REF_SKILL`") ||
		!strings.Contains(vsroSkillMigration.UpSQL, "CREATE TABLE `REF_SKILL_GROUP`") ||
		!strings.Contains(vsroSkillMigration.UpSQL, "fk_ref_skill_group") ||
		!strings.Contains(vsroSkillMigration.DownSQL, "DROP TABLE `REF_SKILL`") {
		t.Fatalf("vSRO skill migration = %#v, want reversible version 16 vsro_reference_skill_catalog", vsroSkillMigration)
	}
	unsignedTinyintMigration := shardMigrations[15]
	if unsignedTinyintMigration.Version != 17 || unsignedTinyintMigration.Name != "vsro_skill_unsigned_tinyints" ||
		!strings.Contains(unsignedTinyintMigration.UpSQL, "MODIFY `WEAPON` TINYINT UNSIGNED") ||
		!strings.Contains(unsignedTinyintMigration.UpSQL, "MODIFY `REQLEARN_RACE` TINYINT UNSIGNED") {
		t.Fatalf("vSRO tinyint migration = %#v, want version 17 vsro_skill_unsigned_tinyints", unsignedTinyintMigration)
	}
	groupCompatibilityMigration := shardMigrations[16]
	if groupCompatibilityMigration.Version != 18 || groupCompatibilityMigration.Name != "vsro_skill_group_reference_compatibility" ||
		!strings.Contains(groupCompatibilityMigration.UpSQL, "DROP FOREIGN KEY `fk_ref_skill_group`") {
		t.Fatalf("vSRO group compatibility migration = %#v, want version 18", groupCompatibilityMigration)
	}
	itemOptLevelCompatibilityMigration := shardMigrations[17]
	if itemOptLevelCompatibilityMigration.Version != 19 || itemOptLevelCompatibilityMigration.Name != "vsro_skill_item_opt_level_link_compatibility" ||
		!strings.Contains(itemOptLevelCompatibilityMigration.UpSQL, "DROP PRIMARY KEY") {
		t.Fatalf("vSRO item-opt-level compatibility migration = %#v, want version 19", itemOptLevelCompatibilityMigration)
	}
	itemCatalogMigration := shardMigrations[18]
	if itemCatalogMigration.Version != 20 || itemCatalogMigration.Name != "vsro_item_reference_catalog" ||
		!strings.Contains(itemCatalogMigration.UpSQL, "CREATE TABLE `REF_OBJ_COMMON_VSRO`") ||
		!strings.Contains(itemCatalogMigration.UpSQL, "CREATE TABLE `REF_OBJ_ITEM_VSRO`") ||
		!strings.Contains(itemCatalogMigration.UpSQL, "CREATE TABLE `REF_PACKAGE_ITEM_VSRO`") {
		t.Fatalf("vSRO item catalog migration = %#v, want version 20", itemCatalogMigration)
	}
	movementSettingsMigration := shardMigrations[19]
	if movementSettingsMigration.Version != 21 || movementSettingsMigration.Name != "gameplay_movement_settings" ||
		!strings.Contains(movementSettingsMigration.UpSQL, "CREATE TABLE `GAMEPLAY_MOVEMENT_SETTINGS`") ||
		!strings.Contains(movementSettingsMigration.UpSQL, "VALUES (1, 16, 150)") ||
		!strings.Contains(movementSettingsMigration.DownSQL, "DROP TABLE `GAMEPLAY_MOVEMENT_SETTINGS`") {
		t.Fatalf("movement settings migration = %#v, want reversible version 21", movementSettingsMigration)
	}
}

func TestNewMigrationCatalogRejectsUnsafeOrEmptySQL(t *testing.T) {
	validMigration := Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"}
	tests := []struct {
		name      string
		target    MigrationTarget
		migration Migration
		wantErr   bool
	}{
		{
			name:      "missing target",
			migration: validMigration,
			wantErr:   true,
		},
		{
			name:      "non select preflight",
			target:    MigrationTargetAccount,
			migration: Migration{Version: 1, Name: "first", PreflightSQL: "DELETE FROM `USER`", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"},
			wantErr:   true,
		},
		{
			name:      "semicolon only up",
			target:    MigrationTargetAccount,
			migration: Migration{Version: 1, Name: "first", UpSQL: "-- no-op\n;", DownSQL: "DOWN_FIRST"},
			wantErr:   true,
		},
		{
			name:      "comment prefixed select",
			target:    MigrationTargetAccount,
			migration: Migration{Version: 1, Name: "first", PreflightSQL: "/* inspect only */\nSELECT 1 WHERE FALSE", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewMigrationCatalog(test.target, []Migration{test.migration})
			if (err != nil) != test.wantErr {
				t.Fatalf("NewMigrationCatalog error = %v, want error=%t", err, test.wantErr)
			}
			if test.wantErr && !errors.Is(err, ErrInvalidMigrationCatalog) {
				t.Fatalf("NewMigrationCatalog error = %v, want ErrInvalidMigrationCatalog", err)
			}
		})
	}
}

func TestMigrationRunnerAppliesMigrationsInVersionOrder(t *testing.T) {
	runner := migrationTestRunner(t)
	firstPreflight := migrationTestPreflight("FIRST")
	secondPreflight := migrationTestPreflight("SECOND")
	catalog := migrationTestCatalog(t,
		Migration{Version: 2, Name: "second", PreflightSQL: secondPreflight, UpSQL: "UP_SECOND", DownSQL: "DOWN_SECOND"},
		Migration{Version: 1, Name: "first", PreflightSQL: firstPreflight, UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"},
	)

	if err := runner.Apply(context.Background(), catalog); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	operations, ledger, lockConnection, releaseConnection := migrationTestSnapshot()
	want := []string{
		"lock:get",
		"database:read",
		"ledger:create",
		"ledger:state-column",
		"ledger:read",
		"preflight:" + firstPreflight,
		"ledger:insert:1",
		"sql:UP_FIRST",
		"ledger:state:applied:1",
		"preflight:" + secondPreflight,
		"ledger:insert:2",
		"sql:UP_SECOND",
		"ledger:state:applied:2",
		"lock:release",
	}
	if strings.Join(operations, "|") != strings.Join(want, "|") {
		t.Fatalf("operation order = %#v, want %#v", operations, want)
	}
	if len(ledger) != 2 || ledger[0].Version != 1 || ledger[0].State != migrationStateApplied || ledger[1].Version != 2 || ledger[1].State != migrationStateApplied {
		t.Fatalf("ledger = %#v, want versions 1 then 2", ledger)
	}
	if lockConnection == 0 || lockConnection != releaseConnection {
		t.Fatalf("GET_LOCK connection = %d, RELEASE_LOCK connection = %d; advisory lock must stay on one connection", lockConnection, releaseConnection)
	}
}

func TestMigrationRunnerRejectsChecksumMismatchBeforeMutation(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})
	migration := catalog.Migrations()[0]

	migrationTestSetLedger(migrationLedgerEntry{Version: migration.Version, Name: migration.Name, Checksum: "not-the-catalog-checksum", State: migrationStateApplied})

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("Apply error = %v, want ErrMigrationChecksum", err)
	}
	operations, _, _, _ := migrationTestSnapshot()
	if strings.Contains(strings.Join(operations, "|"), "sql:UP_FIRST") {
		t.Fatalf("Apply executed SQL after checksum mismatch: %#v", operations)
	}
	if !strings.Contains(strings.Join(operations, "|"), "lock:release") {
		t.Fatalf("checksum failure did not release advisory lock: %#v", operations)
	}
}

func TestMigrationRunnerRefusesUnavailableAdvisoryLock(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})

	migrationTestState.Lock()
	migrationTestState.lockResult = 0
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationLockUnavailable) {
		t.Fatalf("Apply error = %v, want ErrMigrationLockUnavailable", err)
	}
	operations, _, _, _ := migrationTestSnapshot()
	if strings.Join(operations, "|") != "lock:get" {
		t.Fatalf("lock rejection performed other work: %#v", operations)
	}
}

func TestMigrationRunnerLeavesDirtyMarkerWhenUpSQLFails(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "failure", UpSQL: "UP_FAIL", DownSQL: "DOWN_FAIL"})
	wantFailure := errors.New("ddl failed")

	migrationTestState.Lock()
	migrationTestState.execErrors["UP_FAIL"] = wantFailure
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationExecution) || !errors.Is(err, wantFailure) {
		t.Fatalf("Apply error = %v, want execution wrapper around %v", err, wantFailure)
	}
	operations, ledger, lockConnection, releaseConnection := migrationTestSnapshot()
	if len(ledger) != 1 || ledger[0].State != migrationStateApplying {
		t.Fatalf("failed migration did not leave an applying marker: %#v", ledger)
	}
	if lockConnection == 0 || lockConnection != releaseConnection || !strings.Contains(strings.Join(operations, "|"), "lock:release") {
		t.Fatalf("failed migration did not release its advisory lock: operations=%#v lock=%d release=%d", operations, lockConnection, releaseConnection)
	}

	operationsBeforeRetry := len(operations)
	migrationTestState.Lock()
	delete(migrationTestState.execErrors, "UP_FAIL")
	migrationTestState.Unlock()
	err = runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationDirty) {
		t.Fatalf("retry error = %v, want ErrMigrationDirty", err)
	}
	operations, _, _, _ = migrationTestSnapshot()
	for _, operation := range operations[operationsBeforeRetry:] {
		if operation == "sql:UP_FAIL" {
			t.Fatalf("dirty retry executed DDL: %#v", operations[operationsBeforeRetry:])
		}
	}
}

func TestMigrationRunnerReportsAdvisoryLockReleaseFailure(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t)

	migrationTestState.Lock()
	migrationTestState.releaseResult = 0
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationLockRelease) {
		t.Fatalf("Apply error = %v, want ErrMigrationLockRelease", err)
	}
	operations, _, lockConnection, releaseConnection := migrationTestSnapshot()
	if lockConnection == 0 || lockConnection != releaseConnection || !strings.Contains(strings.Join(operations, "|"), "lock:release") {
		t.Fatalf("release failure did not run against the acquired connection: operations=%#v lock=%d release=%d", operations, lockConnection, releaseConnection)
	}
	closedConnections := migrationTestClosedConnections()
	if len(closedConnections) == 0 || closedConnections[0] != lockConnection {
		t.Fatalf("release failure returned a lock-bearing connection to the pool: closed=%#v lock=%d", closedConnections, lockConnection)
	}
}

func TestMigrationRunnerStopsOnDuplicatePreflight(t *testing.T) {
	runner := migrationTestRunner(t)
	preflight := migrationTestPreflight("DUPLICATE")
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "duplicate", PreflightSQL: preflight, UpSQL: "UP_DUPLICATE", DownSQL: "DOWN_DUPLICATE"})

	migrationTestState.Lock()
	migrationTestState.preflightRows[preflight] = true
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationPreflight) {
		t.Fatalf("Apply error = %v, want ErrMigrationPreflight", err)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if len(ledger) != 0 || strings.Contains(strings.Join(operations, "|"), "sql:UP_DUPLICATE") {
		t.Fatalf("duplicate preflight advanced migration state: operations=%#v ledger=%#v", operations, ledger)
	}
}

func TestMigrationRunnerRollsBackLatestMigrationsInReverseOrder(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t,
		Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"},
		Migration{Version: 2, Name: "second", UpSQL: "UP_SECOND", DownSQL: "DOWN_SECOND"},
	)
	migrations := catalog.Migrations()

	migrationTestSetLedger(
		migrationLedgerEntry{Version: migrations[0].Version, Name: migrations[0].Name, Checksum: migrations[0].Checksum, State: migrationStateApplied},
		migrationLedgerEntry{Version: migrations[1].Version, Name: migrations[1].Name, Checksum: migrations[1].Checksum, State: migrationStateApplied},
	)

	if err := runner.Down(context.Background(), catalog, 2); err != nil {
		t.Fatalf("Down: %v", err)
	}

	operations, ledger, _, _ := migrationTestSnapshot()
	wantTail := []string{"ledger:state:rolling_back:2", "sql:DOWN_SECOND", "ledger:delete:2", "ledger:state:rolling_back:1", "sql:DOWN_FIRST", "ledger:delete:1", "lock:release"}
	if len(operations) < len(wantTail) || strings.Join(operations[len(operations)-len(wantTail):], "|") != strings.Join(wantTail, "|") {
		t.Fatalf("rollback operation tail = %#v, want %#v", operations, wantTail)
	}
	if len(ledger) != 0 {
		t.Fatalf("rollback left ledger entries: %#v", ledger)
	}
}

func TestMigrationRunnerRejectsCatalogTargetBeforeLedgerMutation(t *testing.T) {
	runner := migrationTestRunnerForTarget(t, MigrationTargetAccount)
	catalog := migrationTestCatalogForTarget(t, MigrationTargetShard, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationTarget) {
		t.Fatalf("Apply error = %v, want ErrMigrationTarget", err)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if strings.Join(operations, "|") != "lock:get|lock:release" {
		t.Fatalf("target mismatch touched the selected database: %#v", operations)
	}
	if len(ledger) != 0 {
		t.Fatalf("target mismatch mutated the ledger: %#v", ledger)
	}
}

func TestMigrationRunnerRequiresExactSelectedDatabaseName(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})

	migrationTestState.Lock()
	migrationTestState.databaseName = "sro_account"
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationTarget) {
		t.Fatalf("Apply error = %v, want ErrMigrationTarget", err)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if strings.Join(operations, "|") != "lock:get|database:read|lock:release" {
		t.Fatalf("selected-database mismatch touched the ledger or SQL: %#v", operations)
	}
	if len(ledger) != 0 {
		t.Fatalf("selected-database mismatch mutated the ledger: %#v", ledger)
	}
}

func TestMigrationRunnerBlocksIncompleteLedgerStatesBeforeSQL(t *testing.T) {
	for _, state := range []migrationLedgerState{migrationStateApplying, migrationStateRollingBack} {
		t.Run(string(state), func(t *testing.T) {
			runner := migrationTestRunner(t)
			catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})
			migration := catalog.Migrations()[0]
			migrationTestSetLedger(migrationLedgerEntry{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, State: state})

			err := runner.Apply(context.Background(), catalog)
			if !errors.Is(err, ErrMigrationDirty) {
				t.Fatalf("Apply error = %v, want ErrMigrationDirty", err)
			}
			operations, _, _, _ := migrationTestSnapshot()
			if strings.Contains(strings.Join(operations, "|"), "sql:UP_FIRST") {
				t.Fatalf("dirty state executed migration SQL: %#v", operations)
			}
		})
	}
}

func TestMigrationRunnerLeavesApplyingMarkerWhenFinalizationFails(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})
	wantFailure := errors.New("ledger finalization failed")
	stateUpdateSQL := "UPDATE `" + MigrationLedgerTable + "` SET `state`=? WHERE `version`=? AND `name`=? AND `checksum`=? AND `state`=?"

	migrationTestState.Lock()
	migrationTestState.execErrors[stateUpdateSQL] = wantFailure
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationExecution) || !errors.Is(err, wantFailure) {
		t.Fatalf("Apply error = %v, want ErrMigrationExecution wrapping %v", err, wantFailure)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if !strings.Contains(strings.Join(operations, "|"), "sql:UP_FIRST") {
		t.Fatalf("finalization failure did not reach the completed DDL: %#v", operations)
	}
	if len(ledger) != 1 || ledger[0].State != migrationStateApplying {
		t.Fatalf("finalization failure did not preserve an applying marker: %#v", ledger)
	}

	operationsBeforeRetry := len(operations)
	migrationTestState.Lock()
	delete(migrationTestState.execErrors, stateUpdateSQL)
	migrationTestState.Unlock()
	err = runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationDirty) {
		t.Fatalf("retry error = %v, want ErrMigrationDirty", err)
	}
	operations, _, _, _ = migrationTestSnapshot()
	if strings.Contains(strings.Join(operations[operationsBeforeRetry:], "|"), "sql:UP_FIRST") {
		t.Fatalf("dirty finalization retry executed DDL: %#v", operations[operationsBeforeRetry:])
	}
}

func TestMigrationRunnerLeavesRollingBackMarkerWhenDownSQLFails(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FAIL"})
	migration := catalog.Migrations()[0]
	migrationTestSetLedger(migrationLedgerEntry{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, State: migrationStateApplied})
	wantFailure := errors.New("down ddl failed")

	migrationTestState.Lock()
	migrationTestState.execErrors["DOWN_FAIL"] = wantFailure
	migrationTestState.Unlock()

	err := runner.Down(context.Background(), catalog, 1)
	if !errors.Is(err, ErrMigrationRollback) || !errors.Is(err, wantFailure) {
		t.Fatalf("Down error = %v, want ErrMigrationRollback wrapping %v", err, wantFailure)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if len(ledger) != 1 || ledger[0].State != migrationStateRollingBack {
		t.Fatalf("failed down migration did not preserve a rolling-back marker: %#v", ledger)
	}

	operationsBeforeRetry := len(operations)
	migrationTestState.Lock()
	delete(migrationTestState.execErrors, "DOWN_FAIL")
	migrationTestState.Unlock()
	err = runner.Down(context.Background(), catalog, 1)
	if !errors.Is(err, ErrMigrationDirty) {
		t.Fatalf("retry error = %v, want ErrMigrationDirty", err)
	}
	operations, _, _, _ = migrationTestSnapshot()
	if strings.Contains(strings.Join(operations[operationsBeforeRetry:], "|"), "sql:DOWN_FAIL") {
		t.Fatalf("dirty rollback retry executed DDL: %#v", operations[operationsBeforeRetry:])
	}
}

func TestMigrationRunnerUpgradesLegacyLedgerStateColumn(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})
	migration := catalog.Migrations()[0]

	migrationTestState.Lock()
	migrationTestState.ledgerExists = true
	migrationTestState.ledgerHasStateColumn = false
	migrationTestState.ledger = []migrationLedgerEntry{{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum}}
	migrationTestState.Unlock()

	if err := runner.Apply(context.Background(), catalog); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if !strings.Contains(strings.Join(operations, "|"), "ledger:state-column-upgrade") {
		t.Fatalf("legacy ledger was not upgraded: %#v", operations)
	}
	if len(ledger) != 1 || ledger[0].State != migrationStateApplied {
		t.Fatalf("legacy ledger state = %#v, want applied", ledger)
	}
	if strings.Contains(strings.Join(operations, "|"), "sql:UP_FIRST") {
		t.Fatalf("legacy applied migration re-ran SQL: %#v", operations)
	}
}

func TestMigrationRunnerStopsBeforeDDLWhenApplyingMarkerCommitIsUnknown(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t, Migration{Version: 1, Name: "first", UpSQL: "UP_FIRST", DownSQL: "DOWN_FIRST"})
	wantFailure := errors.New("connection lost while committing marker")

	migrationTestState.Lock()
	migrationTestState.commitError = wantFailure
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, ErrMigrationExecution) || !errors.Is(err, wantFailure) {
		t.Fatalf("Apply error = %v, want ErrMigrationExecution wrapping %v", err, wantFailure)
	}
	operations, ledger, _, _ := migrationTestSnapshot()
	if strings.Contains(strings.Join(operations, "|"), "sql:UP_FIRST") {
		t.Fatalf("unknown marker commit still executed DDL: %#v", operations)
	}
	if len(ledger) != 1 || ledger[0].State != migrationStateApplying {
		t.Fatalf("unknown marker commit did not retain the safe applying state: %#v", ledger)
	}
}

func TestMigrationRunnerDiscardsConnectionWhenReleaseQueryFails(t *testing.T) {
	runner := migrationTestRunner(t)
	catalog := migrationTestCatalog(t)
	wantFailure := errors.New("release transport failure")

	migrationTestState.Lock()
	migrationTestState.queryErrors["SELECT RELEASE_LOCK(?)"] = wantFailure
	migrationTestState.Unlock()

	err := runner.Apply(context.Background(), catalog)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("Apply error = %v, want release failure %v", err, wantFailure)
	}
	_, _, lockConnection, _ := migrationTestSnapshot()
	closedConnections := migrationTestClosedConnections()
	if lockConnection == 0 || len(closedConnections) == 0 || closedConnections[0] != lockConnection {
		t.Fatalf("release query failure returned a lock-bearing connection to the pool: closed=%#v lock=%d", closedConnections, lockConnection)
	}
}
