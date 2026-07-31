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

const migrationTestDriverName = "aegiscore-migration-test"

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
		LockName:    "aegiscore:migrations:test",
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
	if len(migrations) != 4 {
		t.Fatalf("account migration count = %d, want 4", len(migrations))
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
	balanceMigration := migrations[1]
	if balanceMigration.Version != 2 || balanceMigration.Name != "item_mall_balances" ||
		!strings.Contains(balanceMigration.UpSQL, "GIFT_SILK") || !strings.Contains(balanceMigration.DownSQL, "DROP COLUMN") {
		t.Fatalf("balance migration = %#v, want reversible version 2 item_mall_balances", balanceMigration)
	}
	patchMigration := migrations[3]
	if patchMigration.Version != 4 || patchMigration.Name != "patch_release_catalog" ||
		!strings.Contains(patchMigration.UpSQL, "PATCH_RELEASE_FILE") || !strings.Contains(patchMigration.DownSQL, "DROP TABLE `PATCH_RELEASE`") {
		t.Fatalf("patch migration = %#v, want reversible version 4 patch_release_catalog", patchMigration)
	}
	characterLimitMigration := migrations[2]
	if characterLimitMigration.Version != 3 || characterLimitMigration.Name != "shard_character_limit" ||
		!strings.Contains(characterLimitMigration.UpSQL, "SHARD_CHARACTER_LIMIT") ||
		!strings.Contains(characterLimitMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("character limit migration = %#v, want reversible version 3 shard_character_limit", characterLimitMigration)
	}

	shardCatalog, err := LoadMigrationCatalog(os.DirFS(migrationsRoot), "shard")
	if err != nil {
		t.Fatalf("LoadMigrationCatalog shard: %v", err)
	}
	shardMigrations := shardCatalog.Migrations()
	if shardCatalog.Target() != MigrationTargetShard {
		t.Fatalf("shard catalog target = %q, want %q", shardCatalog.Target(), MigrationTargetShard)
	}
	if len(shardMigrations) != 149 {
		t.Fatalf("shard migration count = %d, want 149", len(shardMigrations))
	}
	shardMigrationByVersion := make(map[uint64]Migration, len(shardMigrations))
	for _, migration := range shardMigrations {
		shardMigrationByVersion[migration.Version] = migration
	}
	mediaRegistryMigration := shardMigrationByVersion[136]
	if mediaRegistryMigration.Version != 136 || mediaRegistryMigration.Name != "media_catalog_registry" ||
		!strings.Contains(mediaRegistryMigration.UpSQL, "MEDIA_CATALOG_MAPPING") ||
		!strings.Contains(mediaRegistryMigration.UpSQL, "MEDIA_TELEPORTDATA_DUNGEON") ||
		!strings.Contains(mediaRegistryMigration.DownSQL, "DROP TABLE IF EXISTS `SRO_SHARD`.`MEDIA_CATALOG_MAPPING`") {
		t.Fatalf("Media registry migration = %#v, want reversible version 136 registry and views", mediaRegistryMigration)
	}
	soOkMigration := shardMigrationByVersion[137]
	if soOkMigration.Version != 137 || soOkMigration.Name != "so_ok_event_npc_spawn" ||
		!strings.Contains(soOkMigration.UpSQL, "`RefObjID`=3861") ||
		!strings.Contains(soOkMigration.UpSQL, "9000006") ||
		!strings.Contains(soOkMigration.DownSQL, "9000001 AND 9000006") {
		t.Fatalf("So-Ok migration = %#v, want reversible fallback event-NPC spawns", soOkMigration)
	}
	expendableMigration := shardMigrationByVersion[138]
	if expendableMigration.Version != 138 || expendableMigration.Name != "expendable_effect_config" ||
		!strings.Contains(expendableMigration.UpSQL, "EXPENDABLE_EFFECT_CONFIG") ||
		!strings.Contains(expendableMigration.UpSQL, "36771") ||
		!strings.Contains(expendableMigration.UpSQL, "39366") ||
		!strings.Contains(expendableMigration.DownSQL, "DROP TABLE IF EXISTS `SRO_SHARD`.`EXPENDABLE_EFFECT_CONFIG`") {
		t.Fatalf("expendable migration = %#v, want reversible configured custom progression effects", expendableMigration)
	}
	summonMigration := shardMigrationByVersion[139]
	if summonMigration.Version != 139 || summonMigration.Name != "item_summon_monster_catalog" ||
		!strings.Contains(summonMigration.UpSQL, "REF_ITEM_SUMMON_MONSTER") ||
		!strings.Contains(summonMigration.UpSQL, "(9318,1954,10,24") ||
		!strings.Contains(summonMigration.UpSQL, "(9319,38214,100,120") ||
		!strings.Contains(summonMigration.DownSQL, "DROP TABLE IF EXISTS `SRO_SHARD`.`REF_ITEM_SUMMON_MONSTER`") {
		t.Fatalf("summon migration = %#v, want reversible level-banded random monster catalog", summonMigration)
	}
	nonUniqueSummonMigration := shardMigrationByVersion[140]
	if nonUniqueSummonMigration.Version != 140 || nonUniqueSummonMigration.Name != "non_unique_item_summons" ||
		!strings.Contains(nonUniqueSummonMigration.UpSQL, "RUNTIME_RARITY") ||
		!strings.Contains(nonUniqueSummonMigration.UpSQL, "(9318,3921,0,10,24") ||
		!strings.Contains(nonUniqueSummonMigration.UpSQL, "(9319,38191,20,100,120") ||
		!strings.Contains(nonUniqueSummonMigration.DownSQL, "DROP COLUMN `RUNTIME_RARITY`") {
		t.Fatalf("non-unique summon migration = %#v, want reversible strong/giant/party monster catalog", nonUniqueSummonMigration)
	}
	specialReturnMigration := shardMigrationByVersion[141]
	if specialReturnMigration.Version != 141 || specialReturnMigration.Name != "special_return_teleport_point" ||
		!strings.Contains(specialReturnMigration.UpSQL, "'STORE_TD_GATE',2197,24758,641,-3,728") ||
		!strings.Contains(specialReturnMigration.DownSQL, "`ID`=2197") {
		t.Fatalf("special-return migration = %#v, want reversible Bandit Den destination", specialReturnMigration)
	}
	expendableTargetMigration := shardMigrationByVersion[142]
	if expendableTargetMigration.Version != 142 || expendableTargetMigration.Name != "expendable_target_and_cooldown" ||
		!strings.Contains(expendableTargetMigration.UpSQL, "COOLDOWN_SECONDS") ||
		!strings.Contains(expendableTargetMigration.UpSQL, "`TypeID_3`=13,`TypeID_4`=11") ||
		!strings.Contains(expendableTargetMigration.DownSQL, "DROP COLUMN `COOLDOWN_SECONDS`") {
		t.Fatalf("expendable target migration = %#v", expendableTargetMigration)
	}
	alchemyStacksMigration := shardMigrationByVersion[144]
	if alchemyStacksMigration.Version != 144 || alchemyStacksMigration.Name != "alchemy_material_stacks" ||
		!strings.Contains(alchemyStacksMigration.UpSQL, "SET `Stack_Size` = 50") ||
		!strings.Contains(alchemyStacksMigration.UpSQL, "`TypeID_4` IN (1, 2, 7, 8)") ||
		!strings.Contains(alchemyStacksMigration.DownSQL, "SET `Stack_Size` = 1") {
		t.Fatalf("alchemy stack migration = %#v, want reversible normal material stack limits", alchemyStacksMigration)
	}
	alchemyStacks5000Migration := shardMigrationByVersion[145]
	if alchemyStacks5000Migration.Version != 145 || alchemyStacks5000Migration.Name != "alchemy_material_stack_5000" ||
		!strings.Contains(alchemyStacks5000Migration.UpSQL, "SET `Stack_Size` = 5000") ||
		!strings.Contains(alchemyStacks5000Migration.UpSQL, "`TypeID_4` IN (1, 2, 7, 8)") ||
		!strings.Contains(alchemyStacks5000Migration.DownSQL, "SET `Stack_Size` = 50") {
		t.Fatalf("5000 alchemy stack migration = %#v", alchemyStacks5000Migration)
	}

	magicCubeMigration := shardMigrationByVersion[121]
	if magicCubeMigration.Version != 121 || magicCubeMigration.Name != "magic_cube_storage" ||
		!strings.Contains(magicCubeMigration.UpSQL, "MAGIC_CUBE_INVENTORY") ||
		!strings.Contains(magicCubeMigration.UpSQL, "UQ_MAGIC_CUBE_INVENTORY_ITEM") ||
		!strings.Contains(magicCubeMigration.UpSQL, "ON DELETE RESTRICT") ||
		!strings.Contains(magicCubeMigration.DownSQL, "DROP TABLE IF EXISTS `SRO_SHARD`.`MAGIC_CUBE_INVENTORY`") {
		t.Fatalf("magic cube migration = %#v, want reversible version 121 linked storage", magicCubeMigration)
	}
	itemLinkMigration := shardMigrationByVersion[123]
	if itemLinkMigration.Version != 123 || itemLinkMigration.Name != "vsro_item_common_link" ||
		!strings.Contains(itemLinkMigration.PreflightSQL, "JSON_EXTRACT(common.`RAW_DATA`, '$.Link')") ||
		!strings.Contains(itemLinkMigration.PreflightSQL, "REF_OBJ_STRUCT_VSRO") ||
		!strings.Contains(itemLinkMigration.PreflightSQL, "common.`TYPE_ID_1` = 4") ||
		!strings.Contains(itemLinkMigration.PreflightSQL, "HAVING COUNT(*) > 1") ||
		strings.Contains(itemLinkMigration.PreflightSQL, "WHERE common.`ID` IS NULL\nWHERE") ||
		!strings.Contains(itemLinkMigration.UpSQL, "ADD COLUMN `LINK` INT") ||
		!strings.Contains(itemLinkMigration.UpSQL, "DROP FOREIGN KEY `fk_ref_obj_item_vsro_common`") ||
		!strings.Contains(itemLinkMigration.DownSQL, "DROP COLUMN `LINK`") {
		t.Fatalf("item-link migration = %#v, want reversible version 123 authoritative common Link", itemLinkMigration)
	}
	dimensionHoleWorldMigration := shardMigrationByVersion[128]
	if dimensionHoleWorldMigration.Version != 128 || dimensionHoleWorldMigration.Name != "dimension_hole_world_lifecycle" ||
		!strings.Contains(dimensionHoleWorldMigration.UpSQL, "WORLD_ENTRY_TYPE") ||
		!strings.Contains(dimensionHoleWorldMigration.UpSQL, "WORLD_DURATION_MS") ||
		!strings.Contains(dimensionHoleWorldMigration.UpSQL, "7200000") ||
		!strings.Contains(dimensionHoleWorldMigration.UpSQL, "600000") ||
		!strings.Contains(dimensionHoleWorldMigration.DownSQL, "DROP COLUMN `WORLD_ENTRY_TYPE`") {
		t.Fatalf("Dimension Hole world migration = %#v, want reversible version 128 lifecycle authority", dimensionHoleWorldMigration)
	}
	dimensionHoleWorldRegionMigration := shardMigrationByVersion[130]
	if dimensionHoleWorldRegionMigration.Version != 130 || dimensionHoleWorldRegionMigration.Name != "dimension_hole_world_regions" ||
		!strings.Contains(dimensionHoleWorldRegionMigration.UpSQL, "REF_GAME_WORLD_REGION") ||
		!strings.Contains(dimensionHoleWorldRegionMigration.UpSQL, "PRIMARY KEY (`WORLD_ID`, `REGION_ID`)") ||
		!strings.Contains(dimensionHoleWorldRegionMigration.UpSQL, "FOREIGN KEY (`WORLD_ID`)") ||
		!strings.Contains(dimensionHoleWorldRegionMigration.DownSQL, "DROP TABLE `SRO_SHARD`.`REF_GAME_WORLD_REGION`") {
		t.Fatalf("Dimension Hole world-region migration = %#v, want reversible version 130 region authority", dimensionHoleWorldRegionMigration)
	}
	dimensionHoleStartMigration := shardMigrationByVersion[131]
	if dimensionHoleStartMigration.Version != 131 || dimensionHoleStartMigration.Name != "dimension_hole_start_positions" ||
		!strings.Contains(dimensionHoleStartMigration.UpSQL, "REF_INSTANCE_WORLD_START_POSITION") ||
		!strings.Contains(dimensionHoleStartMigration.UpSQL, "PRIMARY KEY (`WORLD_ID`, `START_INDEX`)") ||
		!strings.Contains(dimensionHoleStartMigration.UpSQL, "`REGION_ID` SMALLINT NOT NULL") ||
		!strings.Contains(dimensionHoleStartMigration.UpSQL, "`POS_X` INT NOT NULL") ||
		!strings.Contains(dimensionHoleStartMigration.DownSQL, "DROP TABLE `SRO_SHARD`.`REF_INSTANCE_WORLD_START_POSITION`") {
		t.Fatalf("Dimension Hole start-position migration = %#v, want reversible version 131 stock starts", dimensionHoleStartMigration)
	}
	guildWarMigration := shardMigrationByVersion[111]
	if guildWarMigration.Version != 111 || guildWarMigration.Name != "guild_war_runtime" ||
		!strings.Contains(guildWarMigration.UpSQL, "GUILD_WAR_RUNTIME") ||
		!strings.Contains(guildWarMigration.UpSQL, "CHALLENGER_MASTER") ||
		!strings.Contains(guildWarMigration.UpSQL, "SURRENDERED_GUILD") ||
		!strings.Contains(guildWarMigration.UpSQL, "DUE_AT") ||
		!strings.Contains(guildWarMigration.UpSQL, "CHALLENGER_SCORE") ||
		!strings.Contains(guildWarMigration.UpSQL, "REWARD_CLAIMED_AT") ||
		!strings.Contains(guildWarMigration.DownSQL, "DROP TABLE `SRO_SHARD`.`GUILD_WAR_RUNTIME`") {
		t.Fatalf("guild war migration = %#v, want reversible version 111 guild_war_runtime", guildWarMigration)
	}
	guildElectionMigration := shardMigrationByVersion[112]
	if guildElectionMigration.Version != 112 || guildElectionMigration.Name != "guild_election_runtime" ||
		!strings.Contains(guildElectionMigration.UpSQL, "GUILD_ELECTION_CANDIDATE") ||
		!strings.Contains(guildElectionMigration.UpSQL, "GUILD_ELECTION_VOTE") ||
		!strings.Contains(guildElectionMigration.UpSQL, "PREVIOUS_MASTER") ||
		!strings.Contains(guildElectionMigration.DownSQL, "DROP TABLE IF EXISTS `GUILD_ELECTION`") {
		t.Fatalf("guild election migration = %#v, want reversible version 112 guild_election_runtime", guildElectionMigration)
	}
	fortressMigration := shardMigrationByVersion[113]
	if fortressMigration.Version != 113 || fortressMigration.Name != "fortress_expenditure_runtime" ||
		!strings.Contains(fortressMigration.UpSQL, "FORTRESS_EXPENDITURE") ||
		!strings.Contains(fortressMigration.UpSQL, "TREASURY_GOLD") ||
		!strings.Contains(fortressMigration.UpSQL, "FORTRESS_BIJEOKDAN") ||
		!strings.Contains(fortressMigration.UpSQL, "CK_FORTRESS_EXPENDITURE_REASON") ||
		!strings.Contains(fortressMigration.DownSQL, "DROP TABLE IF EXISTS `FORTRESS`") {
		t.Fatalf("fortress migration = %#v, want reversible version 113 fortress_expenditure_runtime", fortressMigration)
	}
	communityFriendMigration := shardMigrationByVersion[114]
	if communityFriendMigration.Version != 114 || communityFriendMigration.Name != "community_friend_runtime" ||
		!strings.Contains(communityFriendMigration.UpSQL, "COMMUNITY_FRIEND_REQUEST") ||
		!strings.Contains(communityFriendMigration.UpSQL, "COMMUNITY_FRIEND") ||
		!strings.Contains(communityFriendMigration.UpSQL, "CK_COMMUNITY_FRIEND_REQUEST_DIRECTION") ||
		!strings.Contains(communityFriendMigration.DownSQL, "DROP TABLE IF EXISTS `COMMUNITY_FRIEND_REQUEST`") {
		t.Fatalf("community friend migration = %#v, want reversible version 114 community_friend_runtime", communityFriendMigration)
	}
	communityBlockMigration := shardMigrationByVersion[115]
	if communityBlockMigration.Version != 115 || communityBlockMigration.Name != "community_block_runtime" ||
		!strings.Contains(communityBlockMigration.UpSQL, "COMMUNITY_BLOCK") ||
		!strings.Contains(communityBlockMigration.UpSQL, "CK_COMMUNITY_BLOCK_DISTINCT") ||
		!strings.Contains(communityBlockMigration.DownSQL, "DROP TABLE IF EXISTS `COMMUNITY_BLOCK`") {
		t.Fatalf("community block migration = %#v, want reversible version 115 community_block_runtime", communityBlockMigration)
	}
	communityMemoMigration := shardMigrationByVersion[116]
	if communityMemoMigration.Version != 116 || communityMemoMigration.Name != "community_memo_runtime" ||
		!strings.Contains(communityMemoMigration.UpSQL, "COMMUNITY_MEMO") ||
		!strings.Contains(communityMemoMigration.UpSQL, "IX_COMMUNITY_MEMO_INBOX") ||
		!strings.Contains(communityMemoMigration.DownSQL, "DROP TABLE IF EXISTS `COMMUNITY_MEMO`") {
		t.Fatalf("community memo migration = %#v, want reversible version 116 community_memo_runtime", communityMemoMigration)
	}
	mediaCatalogMigration := shardMigrationByVersion[81]
	if mediaCatalogMigration.Version != 81 || mediaCatalogMigration.Name != "media_text_catalog" ||
		!strings.Contains(mediaCatalogMigration.UpSQL, "REF_MEDIA_TEXT_CATALOG_ROW") ||
		!strings.Contains(mediaCatalogMigration.DownSQL, "DROP TABLE `SRO_SHARD`.`REF_MEDIA_TEXT_CATALOG`") {
		t.Fatalf("Media text catalog migration = %#v", mediaCatalogMigration)
	}
	alexandriaArrowMigration := shardMigrationByVersion[82]
	if alexandriaArrowMigration.Version != 82 || alexandriaArrowMigration.Name != "alexandria_arrow_shop_mapping" ||
		!strings.Contains(alexandriaArrowMigration.UpSQL, "`FK_REF_ITEM` = 62") ||
		!strings.Contains(alexandriaArrowMigration.DownSQL, "`FK_REF_ITEM` = 10383") {
		t.Fatalf("Alexandria arrow mapping migration = %#v", alexandriaArrowMigration)
	}
	potionStackMigration := shardMigrationByVersion[84]
	if potionStackMigration.Version != 84 || potionStackMigration.Name != "player_potion_stack_limit" ||
		!strings.Contains(potionStackMigration.UpSQL, "`Stack_Size` = 50000") ||
		!strings.Contains(potionStackMigration.DownSQL, "`Stack_Size` = 50") ||
		!strings.Contains(potionStackMigration.UpSQL, "ITEM\\_ETC\\_HP\\_POTION") ||
		!strings.Contains(potionStackMigration.UpSQL, "ITEM\\_ETC\\_MP\\_POTION") {
		t.Fatalf("player potion stack migration = %#v", potionStackMigration)
	}
	cosPolicyMigration := shardMigrationByVersion[85]
	if cosPolicyMigration.Version != 85 || cosPolicyMigration.Name != "cos_gameplay_policy" ||
		!strings.Contains(cosPolicyMigration.UpSQL, "GAMEPLAY_COS_SETTINGS") ||
		!strings.Contains(cosPolicyMigration.UpSQL, "GAMEPLAY_COS_KIND_POLICY") ||
		!strings.Contains(cosPolicyMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("COS gameplay policy migration = %#v", cosPolicyMigration)
	}
	triJobMigration := shardMigrationByVersion[86]
	if triJobMigration.Version != 86 || triJobMigration.Name != "trijob_core" ||
		!strings.Contains(triJobMigration.UpSQL, "REF_JOB_LEVEL_EXPERIENCE") ||
		!strings.Contains(triJobMigration.UpSQL, "PREVIOUS_JOB_TYPE") ||
		!strings.Contains(triJobMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("tri-job core migration = %#v", triJobMigration)
	}
	safeTradeMigration := shardMigrationByVersion[87]
	if safeTradeMigration.Version != 87 || safeTradeMigration.Name != "safe_trade_core" ||
		!strings.Contains(safeTradeMigration.UpSQL, "SAFE_TRADE_MARKET_GOOD") ||
		!strings.Contains(safeTradeMigration.UpSQL, "CHARACTER_SAFE_TRADE") ||
		!strings.Contains(safeTradeMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("safe-trade core migration = %#v", safeTradeMigration)
	}
	stockSpecialtyMigration := shardMigrationByVersion[88]
	if stockSpecialtyMigration.Version != 88 || stockSpecialtyMigration.Name != "stock_specialty_goods" ||
		!strings.Contains(stockSpecialtyMigration.UpSQL, "ITEM_QUOTATION") ||
		!strings.Contains(stockSpecialtyMigration.UpSQL, "CREATOR_NAME") ||
		!strings.Contains(stockSpecialtyMigration.UpSQL, "CUR_STOCK_AMOUNT") ||
		!strings.Contains(stockSpecialtyMigration.DownSQL, "DROP COLUMN `CREATOR_NAME`") {
		t.Fatalf("stock specialty-goods migration = %#v", stockSpecialtyMigration)
	}
	triJobRewardsMigration := shardMigrationByVersion[89]
	if triJobRewardsMigration.Version != 89 || triJobRewardsMigration.Name != "stock_trijob_rewards" ||
		!strings.Contains(triJobRewardsMigration.UpSQL, "TRIJOB_REWARDS") ||
		!strings.Contains(triJobRewardsMigration.UpSQL, "(2, 0)") ||
		!strings.Contains(triJobRewardsMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("stock tri-job rewards migration = %#v", triJobRewardsMigration)
	}
	levelJobFieldsMigration := shardMigrationByVersion[93]
	if levelJobFieldsMigration.Version != 93 || levelJobFieldsMigration.Name != "vsro_level_job_fields" ||
		!strings.Contains(levelJobFieldsMigration.UpSQL, "JOB_ACTIVITY_FACTOR") ||
		!strings.Contains(levelJobFieldsMigration.UpSQL, "'$[8]'") ||
		!strings.Contains(levelJobFieldsMigration.DownSQL, "DROP COLUMN `JOB_ACTIVITY_FACTOR`") {
		t.Fatalf("vSRO level/job fields migration = %#v", levelJobFieldsMigration)
	}
	jobLevelCapMigration := shardMigrationByVersion[94]
	if jobLevelCapMigration.Version != 94 || jobLevelCapMigration.Name != "vsro_job_level_cap" ||
		!strings.Contains(jobLevelCapMigration.UpSQL, "`JOB_LEVEL` > 7") ||
		!strings.Contains(jobLevelCapMigration.UpSQL, "BETWEEN 1 AND 7") ||
		!strings.Contains(jobLevelCapMigration.DownSQL, "BETWEEN 1 AND 10") {
		t.Fatalf("vSRO job-level cap migration = %#v", jobLevelCapMigration)
	}
	guildRenameMigration := shardMigrationByVersion[51]
	if guildRenameMigration.Version != 51 || guildRenameMigration.Name != "guild_forced_rename" ||
		!strings.Contains(guildRenameMigration.UpSQL, "RENAME_REQUIRED") ||
		!strings.Contains(guildRenameMigration.DownSQL, "DROP COLUMN") {
		t.Fatalf("guild rename migration = %#v, want reversible forced-rename state", guildRenameMigration)
	}
	deletionMigration := shardMigrationByVersion[52]
	if deletionMigration.Version != 52 || deletionMigration.Name != "character_deletion_lifecycle" ||
		!strings.Contains(deletionMigration.UpSQL, "DELETION_REQUESTED_AT") ||
		!strings.Contains(deletionMigration.UpSQL, "ON DELETE SET NULL") ||
		!strings.Contains(deletionMigration.DownSQL, "DROP COLUMN") {
		t.Fatalf("character deletion migration = %#v, want reversible deletion clock and audit retention", deletionMigration)
	}
	pkMigration := shardMigrationByVersion[53]
	if pkMigration.Version != 53 || pkMigration.Name != "character_pk_state" ||
		!strings.Contains(pkMigration.UpSQL, "`DAILY_PK` TINYINT UNSIGNED") ||
		!strings.Contains(pkMigration.UpSQL, "`TOTAL_PK` SMALLINT") ||
		!strings.Contains(pkMigration.UpSQL, "`PK_PENALTY_POINT` INT") ||
		!strings.Contains(pkMigration.DownSQL, "DROP COLUMN `DAILY_PK`") {
		t.Fatalf("character PK migration = %#v, want reversible recovered PK state", pkMigration)
	}
	questReferenceMigration := shardMigrationByVersion[54]
	if questReferenceMigration.Version != 54 || questReferenceMigration.Name != "quest_reference_catalog" ||
		!strings.Contains(questReferenceMigration.UpSQL, "REF_QUEST_REWARD_ITEM") ||
		!strings.Contains(questReferenceMigration.DownSQL, "DROP TABLE `SRO_SHARD`.`REF_QUEST`") {
		t.Fatalf("quest reference migration = %#v, want reversible native quest catalogs", questReferenceMigration)
	}
	questScriptMigration := shardMigrationByVersion[55]
	if questScriptMigration.Version != 55 || questScriptMigration.Name != "quest_script_declarations" ||
		!strings.Contains(questScriptMigration.UpSQL, "REF_QUEST_SCRIPT_DECLARATION") ||
		!strings.Contains(questScriptMigration.UpSQL, "SCRIPT_SHA256") ||
		!strings.Contains(questScriptMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest script migration = %#v, want reversible decoded declaration catalog", questScriptMigration)
	}
	questKillMigration := shardMigrationByVersion[56]
	if questKillMigration.Version != 56 || questKillMigration.Name != "quest_kill_objectives" ||
		!strings.Contains(questKillMigration.UpSQL, "REF_QUEST_KILL_OBJECTIVE") ||
		!strings.Contains(questKillMigration.UpSQL, "RUNTIME_ENABLED") ||
		!strings.Contains(questKillMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest kill migration = %#v, want reversible typed kill-objective catalog", questKillMigration)
	}
	questDeliverMigration := shardMigrationByVersion[57]
	if questDeliverMigration.Version != 57 || questDeliverMigration.Name != "quest_deliver_item_ex" ||
		!strings.Contains(questDeliverMigration.UpSQL, "REF_QUEST_DELIVER_ITEM_EX") ||
		!strings.Contains(questDeliverMigration.UpSQL, "ITEM_REF_OBJECT_ID") ||
		!strings.Contains(questDeliverMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest deliver migration = %#v, want reversible typed deliver-item EX catalog", questDeliverMigration)
	}
	questRewardMigration := shardMigrationByVersion[58]
	if questRewardMigration.Version != 58 || questRewardMigration.Name != "quest_script_rewards" ||
		!strings.Contains(questRewardMigration.UpSQL, "REF_QUEST_SCRIPT_REWARD_ITEM") ||
		!strings.Contains(questRewardMigration.UpSQL, "SCRIPT_SHA256") ||
		!strings.Contains(questRewardMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest reward migration = %#v, want reversible script reward catalog", questRewardMigration)
	}
	questDeliverObjectivesMigration := shardMigrationByVersion[59]
	if questDeliverObjectivesMigration.Version != 59 || questDeliverObjectivesMigration.Name != "quest_deliver_item_objectives" ||
		!strings.Contains(questDeliverObjectivesMigration.UpSQL, "REF_QUEST_DELIVER_ITEM_ENTRY") ||
		!strings.Contains(questDeliverObjectivesMigration.UpSQL, "ITEM_COUNT") ||
		!strings.Contains(questDeliverObjectivesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest deliver objectives migration = %#v, want reversible standard deliver-item catalog", questDeliverObjectivesMigration)
	}
	questDialogObjectivesMigration := shardMigrationByVersion[60]
	if questDialogObjectivesMigration.Version != 60 || questDialogObjectivesMigration.Name != "quest_dialog_objectives" ||
		!strings.Contains(questDialogObjectivesMigration.UpSQL, "REF_QUEST_DIALOG_OBJECTIVE") ||
		!strings.Contains(questDialogObjectivesMigration.UpSQL, "DIALOGUE_KEY") ||
		!strings.Contains(questDialogObjectivesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest dialog objectives migration = %#v, want reversible dialog-objective catalog", questDialogObjectivesMigration)
	}
	questKillPlayerObjectivesMigration := shardMigrationByVersion[61]
	if questKillPlayerObjectivesMigration.Version != 61 || questKillPlayerObjectivesMigration.Name != "quest_kill_player_objectives" ||
		!strings.Contains(questKillPlayerObjectivesMigration.UpSQL, "REF_QUEST_KILL_PLAYER_JOB_STATE") ||
		!strings.Contains(questKillPlayerObjectivesMigration.UpSQL, "REGION_VALUE") ||
		!strings.Contains(questKillPlayerObjectivesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest kill-player objectives migration = %#v, want reversible filtered kill-player catalog", questKillPlayerObjectivesMigration)
	}
	questCollectionObjectivesMigration := shardMigrationByVersion[62]
	if questCollectionObjectivesMigration.Version != 62 || questCollectionObjectivesMigration.Name != "quest_collection_item_objectives" ||
		!strings.Contains(questCollectionObjectivesMigration.UpSQL, "REF_QUEST_COLLECTION_ITEM_SOURCE") ||
		!strings.Contains(questCollectionObjectivesMigration.UpSQL, "SOURCE_PARAMETER") ||
		!strings.Contains(questCollectionObjectivesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest collection objectives migration = %#v, want reversible monster-item collection catalog", questCollectionObjectivesMigration)
	}
	questExtendedKillMigration := shardMigrationByVersion[63]
	if questExtendedKillMigration.Version != 63 || questExtendedKillMigration.Name != "quest_extended_kill_objectives" ||
		!strings.Contains(questExtendedKillMigration.UpSQL, "REF_QUEST_EXTENDED_KILL_TARGET") ||
		!strings.Contains(questExtendedKillMigration.UpSQL, "multi_target") ||
		!strings.Contains(questExtendedKillMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest extended kill migration = %#v, want reversible classified/multi-target kill catalog", questExtendedKillMigration)
	}
	questTalismanMigration := shardMigrationByVersion[64]
	if questTalismanMigration.Version != 64 || questTalismanMigration.Name != "quest_talisman_objectives" ||
		!strings.Contains(questTalismanMigration.UpSQL, "REF_QUEST_TALISMAN_OBJECTIVE") ||
		!strings.Contains(questTalismanMigration.UpSQL, "THEME_CODE") ||
		!strings.Contains(questTalismanMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest talisman migration = %#v, want reversible talisman-collection catalog", questTalismanMigration)
	}
	collectionBookMigration := shardMigrationByVersion[96]
	if collectionBookMigration.Version != 96 || collectionBookMigration.Name != "collection_book_talismans" ||
		!strings.Contains(collectionBookMigration.UpSQL, "CHAR_COLLECTION_BOOK") ||
		!strings.Contains(collectionBookMigration.UpSQL, "REF_COLLECTION_BOOK_ITEM") ||
		!strings.Contains(collectionBookMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("collection-book migration = %#v, want reversible durable talisman catalog", collectionBookMigration)
	}
	questNamedPointMigration := shardMigrationByVersion[97]
	if questNamedPointMigration.Version != 97 || questNamedPointMigration.Name != "quest_named_points" ||
		!strings.Contains(questNamedPointMigration.UpSQL, "CHAR_QUEST_NAMED_POINT") ||
		!strings.Contains(questNamedPointMigration.UpSQL, "TEMPLE_ANUBIS") ||
		!strings.Contains(questNamedPointMigration.UpSQL, "TEMPLE_HAROERIS") ||
		!strings.Contains(questNamedPointMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest named-point migration = %#v, want reversible durable temple-point balances", questNamedPointMigration)
	}
	questCountdownMigration := shardMigrationByVersion[98]
	if questCountdownMigration.Version != 98 || questCountdownMigration.Name != "quest_countdown_remaining" ||
		!strings.Contains(questCountdownMigration.UpSQL, "TIMER_REMAINING") ||
		!strings.Contains(questCountdownMigration.UpSQL, "BETWEEN 1 AND 255") ||
		!strings.Contains(questCountdownMigration.DownSQL, "DROP COLUMN") {
		t.Fatalf("quest countdown migration = %#v, want reversible countdown checkpoints", questCountdownMigration)
	}
	questInstanceMigration := shardMigrationByVersion[99]
	if questInstanceMigration.Version != 99 || questInstanceMigration.Name != "quest_instance_members" ||
		!strings.Contains(questInstanceMigration.UpSQL, "CHAR_QUEST_INSTANCE_WORLD") ||
		!strings.Contains(questInstanceMigration.UpSQL, "DUNGEON_KEY_ID") ||
		!strings.Contains(questInstanceMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest instance migration = %#v, want reversible durable instance membership", questInstanceMigration)
	}
	dropProfileMigration := shardMigrationByVersion[100]
	if dropProfileMigration.Version != 100 || dropProfileMigration.Name != "drop_profile_catalog" ||
		!strings.Contains(dropProfileMigration.UpSQL, "REF_DROP_CLASS_SELECTION") ||
		!strings.Contains(dropProfileMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("drop profile migration = %#v, want reversible drop profile catalog", dropProfileMigration)
	}
	questInstanceWorldMigration := shardMigrationByVersion[101]
	if questInstanceWorldMigration.Version != 101 || questInstanceWorldMigration.Name != "quest_instance_world_catalog" ||
		!strings.Contains(questInstanceWorldMigration.UpSQL, "REF_GAME_WORLD") ||
		!strings.Contains(questInstanceWorldMigration.UpSQL, "QUEST_INSTANCE_SCOPE_RESERVATION") ||
		!strings.Contains(questInstanceWorldMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest instance world migration = %#v, want reversible catalog and scope reservations", questInstanceWorldMigration)
	}
	questStartMigration := shardMigrationByVersion[65]
	if questStartMigration.Version != 65 || questStartMigration.Name != "quest_start_requirements" ||
		!strings.Contains(questStartMigration.UpSQL, "REF_QUEST_START_CONDITION") ||
		!strings.Contains(questStartMigration.UpSQL, "REF_QUEST_START_NPC") ||
		!strings.Contains(questStartMigration.UpSQL, "REF_QUEST_DEPENDENCY_ENTRY") ||
		!strings.Contains(questStartMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest start migration = %#v, want reversible qualification, routing and dependency catalogs", questStartMigration)
	}
	questPolicyMigration := shardMigrationByVersion[66]
	if questPolicyMigration.Version != 66 || questPolicyMigration.Name != "quest_policy_and_points" ||
		!strings.Contains(questPolicyMigration.UpSQL, "REF_QUEST_COMPLETION_POLICY") ||
		!strings.Contains(questPolicyMigration.UpSQL, "REF_QUEST_JOB_START_CONDITION") ||
		!strings.Contains(questPolicyMigration.UpSQL, "REF_QUEST_SCRIPT_POINT_REWARD") ||
		!strings.Contains(questPolicyMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quest policy migration = %#v, want reversible completion, qualification and point-reward catalogs", questPolicyMigration)
	}
	buybackMigration := shardMigrationByVersion[38]
	if buybackMigration.Version != 38 || buybackMigration.Name != "character_buyback" ||
		!strings.Contains(buybackMigration.UpSQL, "CREATE TABLE `CHAR_BUYBACK`") ||
		!strings.Contains(buybackMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("buy-back migration = %#v, want reversible version 38 character_buyback", buybackMigration)
	}
	experienceScrollMigration := shardMigrationByVersion[39]
	if experienceScrollMigration.Version != 39 || experienceScrollMigration.Name != "experience_scroll_services" ||
		!strings.Contains(experienceScrollMigration.UpSQL, "SERVICE_KIND` IN (1, 2, 3, 4)") ||
		!strings.Contains(experienceScrollMigration.DownSQL, "DELETE FROM `CHAR_ITEM_SERVICE`") {
		t.Fatalf("experience-scroll migration = %#v, want reversible version 39 experience_scroll_services", experienceScrollMigration)
	}
	reverseReturnMigration := shardMigrationByVersion[40]
	if reverseReturnMigration.Version != 40 || reverseReturnMigration.Name != "reverse_return_position" ||
		!strings.Contains(reverseReturnMigration.UpSQL, "CREATE TABLE `CHAR_REVERSE_RETURN_POSITION`") ||
		!strings.Contains(reverseReturnMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("reverse-return migration = %#v, want reversible version 40 reverse_return_position", reverseReturnMigration)
	}
	reverseDestinationsMigration := shardMigrationByVersion[67]
	if reverseDestinationsMigration.Version != 67 || reverseDestinationsMigration.Name != "reverse_return_destinations" ||
		!strings.Contains(reverseDestinationsMigration.UpSQL, "CREATE TABLE `CHAR_DEATH_POSITION`") ||
		!strings.Contains(reverseDestinationsMigration.UpSQL, "CREATE TABLE `REF_OPTIONAL_TELEPORT`") ||
		!strings.Contains(reverseDestinationsMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("reverse-return destinations migration = %#v", reverseDestinationsMigration)
	}
	sdMigration := shardMigrationByVersion[69]
	if sdMigration.Version != 69 || sdMigration.Name != "enable_sd_continent" ||
		!strings.Contains(sdMigration.UpSQL, "VALUES ('SD', 1)") ||
		!strings.Contains(sdMigration.DownSQL, "`Continent_Name` = 'SD'") {
		t.Fatalf("SD continent migration = %#v", sdMigration)
	}
	allRegionsMigration := shardMigrationByVersion[70]
	if allRegionsMigration.Version != 70 || allRegionsMigration.Name != "enable_all_regions" ||
		!strings.Contains(allRegionsMigration.UpSQL, "('KingsValley',1)") ||
		!strings.Contains(allRegionsMigration.UpSQL, "('TQ',1)") ||
		!strings.Contains(allRegionsMigration.DownSQL, "'SIEGE_DUNGEON'") {
		t.Fatalf("all-regions migration = %#v", allRegionsMigration)
	}
	partyExperienceMigration := shardMigrationByVersion[75]
	if partyExperienceMigration.Version != 75 || partyExperienceMigration.Name != "party_experience_rate" ||
		!strings.Contains(partyExperienceMigration.UpSQL, "PARTY_EXPERIENCE_RATE_PERCENT") ||
		!strings.Contains(partyExperienceMigration.UpSQL, "SET `PARTY_EXPERIENCE_RATE_PERCENT` = `EXPERIENCE_RATE_PERCENT`") ||
		!strings.Contains(partyExperienceMigration.DownSQL, "DROP COLUMN `PARTY_EXPERIENCE_RATE_PERCENT`") {
		t.Fatalf("party experience migration = %#v", partyExperienceMigration)
	}
	itemBuffMigration := shardMigrationByVersion[41]
	if itemBuffMigration.Version != 41 || itemBuffMigration.Name != "character_item_buffs" ||
		!strings.Contains(itemBuffMigration.UpSQL, "CREATE TABLE `CHAR_ITEM_BUFF`") ||
		!strings.Contains(itemBuffMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("item-buff migration = %#v, want reversible version 41 character_item_buffs", itemBuffMigration)
	}
	equipmentMigration := shardMigrationByVersion[43]
	if equipmentMigration.Version != 43 || equipmentMigration.Name != "equipment_systems" ||
		!strings.Contains(equipmentMigration.UpSQL, "CREATE TABLE `AVATAR_INVENTORY`") ||
		!strings.Contains(equipmentMigration.UpSQL, "CREATE TABLE `ITEM_MAGIC_OPTION`") ||
		!strings.Contains(equipmentMigration.DownSQL, "DROP TABLE IF EXISTS `ITEM_MAGIC_OPTION`") {
		t.Fatalf("equipment migration = %#v, want reversible version 43 equipment_systems", equipmentMigration)
	}
	threePageInventoryMigration := shardMigrationByVersion[46]
	if threePageInventoryMigration.Version != 46 || threePageInventoryMigration.Name != "three_page_player_inventory" ||
		!strings.Contains(threePageInventoryMigration.UpSQL, "INVENTORY_SIZE` = 109") ||
		!strings.Contains(threePageInventoryMigration.DownSQL, "inv.`SLOT` >= 45") {
		t.Fatalf("inventory migration = %#v, want reversible version 46 three_page_player_inventory", threePageInventoryMigration)
	}
	socialMigration := shardMigrationByVersion[47]
	if socialMigration.Version != 47 || socialMigration.Name != "social_systems" ||
		!strings.Contains(socialMigration.UpSQL, "CREATE TABLE `SRO_SHARD`.`ACADEMY`") ||
		!strings.Contains(socialMigration.UpSQL, "CREATE TABLE `SRO_SHARD`.`CHARACTER_JOB`") ||
		!strings.Contains(socialMigration.DownSQL, "DROP TABLE IF EXISTS `SRO_SHARD`.`CHARACTER_JOB`") {
		t.Fatalf("social migration = %#v, want reversible version 47 social_systems", socialMigration)
	}
	returnCityMigration := shardMigrationByVersion[23]
	if returnCityMigration.Version != 23 || returnCityMigration.Name != "character_return_city" ||
		!strings.Contains(returnCityMigration.UpSQL, "RETURN_TELEPORT_ID") || !strings.Contains(returnCityMigration.DownSQL, "DROP COLUMN") {
		t.Fatalf("return city migration = %#v, want reversible version 23 character_return_city", returnCityMigration)
	}
	progressionRatesMigration := shardMigrationByVersion[24]
	if progressionRatesMigration.Version != 24 || progressionRatesMigration.Name != "gameplay_progression_rates" ||
		!strings.Contains(progressionRatesMigration.UpSQL, "GAMEPLAY_PROGRESSION_RATES") ||
		!strings.Contains(progressionRatesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("progression rates migration = %#v, want reversible version 24 gameplay_progression_rates", progressionRatesMigration)
	}
	mobRatesMigration := shardMigrationByVersion[25]
	if mobRatesMigration.Version != 25 || mobRatesMigration.Name != "gameplay_mob_rates" ||
		!strings.Contains(mobRatesMigration.UpSQL, "GAMEPLAY_MOB_RATES") ||
		!strings.Contains(mobRatesMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("mob rates migration = %#v, want reversible version 25 gameplay_mob_rates", mobRatesMigration)
	}
	quickSlotsMigration := shardMigrationByVersion[26]
	if quickSlotsMigration.Version != 26 || quickSlotsMigration.Name != "character_quick_slots" ||
		!strings.Contains(quickSlotsMigration.UpSQL, "CHAR_QUICK_SLOT") ||
		!strings.Contains(quickSlotsMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("quick-slot migration = %#v, want reversible version 26 character_quick_slots", quickSlotsMigration)
	}
	speedEffectMigration := shardMigrationByVersion[27]
	if speedEffectMigration.Version != 27 || speedEffectMigration.Name != "character_speed_effect" ||
		!strings.Contains(speedEffectMigration.UpSQL, "CHAR_SPEED_EFFECT") ||
		!strings.Contains(speedEffectMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("speed-effect migration = %#v, want reversible version 27 character_speed_effect", speedEffectMigration)
	}
	uniqueCharacterMigration := shardMigrationByVersion[28]
	if uniqueCharacterMigration.Version != 28 || uniqueCharacterMigration.Name != "unique_character_id" ||
		!strings.Contains(uniqueCharacterMigration.PreflightSQL, "GROUP BY `id`") ||
		!strings.Contains(uniqueCharacterMigration.UpSQL, "UX_CHAR_ID") ||
		!strings.Contains(uniqueCharacterMigration.DownSQL, "DROP INDEX") {
		t.Fatalf("unique-character migration = %#v, want reversible version 28 unique_character_id", uniqueCharacterMigration)
	}
	continentCompatibilityMigration := shardMigrationByVersion[29]
	if continentCompatibilityMigration.Version != 29 || continentCompatibilityMigration.Name != "drop_nonstandard_continent_fk" ||
		!strings.Contains(continentCompatibilityMigration.UpSQL, "DROP FOREIGN KEY `FK_Continent_Name`") ||
		!strings.Contains(continentCompatibilityMigration.DownSQL, "ADD CONSTRAINT `FK_Continent_Name`") {
		t.Fatalf("continent compatibility migration = %#v, want reversible version 29 drop_nonstandard_continent_fk", continentCompatibilityMigration)
	}
	itemMallMigration := shardMigrationByVersion[30]
	if itemMallMigration.Version != 30 || itemMallMigration.Name != "item_mall" ||
		!strings.Contains(itemMallMigration.UpSQL, "ITEM_MALL_GOODS") ||
		!strings.Contains(itemMallMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("item mall migration = %#v, want reversible version 30 item_mall", itemMallMigration)
	}
	itemServiceMigration := shardMigrationByVersion[31]
	if itemServiceMigration.Version != 31 || itemServiceMigration.Name != "character_item_services" ||
		!strings.Contains(itemServiceMigration.UpSQL, "CHAR_ITEM_SERVICE") ||
		!strings.Contains(itemServiceMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("item service migration = %#v, want reversible version 31 character_item_services", itemServiceMigration)
	}
	creationPolicyMigration := shardMigrationByVersion[32]
	if creationPolicyMigration.Version != 32 || creationPolicyMigration.Name != "character_creation_policy" ||
		!strings.Contains(creationPolicyMigration.UpSQL, "CHARACTER_CREATION_SETTING") ||
		!strings.Contains(creationPolicyMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("creation policy migration = %#v, want reversible version 32 character_creation_policy", creationPolicyMigration)
	}
	europeanCreationMigration := shardMigrationByVersion[33]
	if europeanCreationMigration.Version != 33 || europeanCreationMigration.Name != "european_character_creation" ||
		!strings.Contains(europeanCreationMigration.UpSQL, "(14875, 2, 1") ||
		!strings.Contains(europeanCreationMigration.UpSQL, "(14900, 2, 0") ||
		!strings.Contains(europeanCreationMigration.UpSQL, "item.`TypeID_4` BETWEEN 7 AND 15") ||
		!strings.Contains(europeanCreationMigration.UpSQL, "item.`TypeID_3` IN (9, 10, 11)") ||
		!strings.Contains(europeanCreationMigration.UpSQL, "item.`Req_Level_Type_1` IN (1, 513, 514)") ||
		!strings.Contains(europeanCreationMigration.DownSQL, "BETWEEN 14875 AND 14900") {
		t.Fatalf("European creation migration = %#v, want reversible version 33 european_character_creation", europeanCreationMigration)
	}
	constantinopleMigration := shardMigrationByVersion[34]
	if constantinopleMigration.Version != 34 || constantinopleMigration.Name != "enable_constantinople" ||
		!strings.Contains(constantinopleMigration.UpSQL, "('Constantinople', 1)") ||
		!strings.Contains(constantinopleMigration.DownSQL, "`Continent_Name` = 'Constantinople'") {
		t.Fatalf("Constantinople migration = %#v, want reversible version 34 enable_constantinople", constantinopleMigration)
	}
	samarkandMigration := shardMigrationByVersion[35]
	if samarkandMigration.Version != 35 || samarkandMigration.Name != "enable_samarkand" ||
		!strings.Contains(samarkandMigration.UpSQL, "('Samarkand', 1)") ||
		!strings.Contains(samarkandMigration.DownSQL, "`Continent_Name` = 'Samarkand'") {
		t.Fatalf("Samarkand migration = %#v, want reversible version 35 enable_samarkand", samarkandMigration)
	}
	alexandriaMigration := shardMigrationByVersion[36]
	if alexandriaMigration.Version != 36 || alexandriaMigration.Name != "enable_alexandria" ||
		!strings.Contains(alexandriaMigration.UpSQL, "('DELTA', 1)") ||
		!strings.Contains(alexandriaMigration.UpSQL, "`MIN_LEVEL`") ||
		!strings.Contains(alexandriaMigration.UpSQL, "(175,'GATE_SD1',27753") ||
		!strings.Contains(alexandriaMigration.DownSQL, "`Continent_Name` = 'DELTA'") {
		t.Fatalf("Alexandria migration = %#v, want reversible version 36 enable_alexandria", alexandriaMigration)
	}
	premiumDailyMigration := shardMigrationByVersion[37]
	if premiumDailyMigration.Version != 37 || premiumDailyMigration.Name != "premium_daily_activation" ||
		!strings.Contains(premiumDailyMigration.UpSQL, "CHAR_ITEM_SERVICE_DAILY") ||
		!strings.Contains(premiumDailyMigration.DownSQL, "DROP TABLE") {
		t.Fatalf("Premium daily migration = %#v, want reversible version 37 premium_daily_activation", premiumDailyMigration)
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
