package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// MigrationLedgerTable is created in the database currently being migrated.
	// Run account and shard catalogs against their respective database handles so
	// their histories remain independent.
	MigrationLedgerTable = "go_sro_schema_migrations"

	DefaultMigrationLockName    = "go-sro:migrations"
	DefaultMigrationLockTimeout = 30 * time.Second
	MaxMigrationLockTimeout     = 5 * time.Minute
)

var (
	ErrInvalidMigrationCatalog  = errors.New("invalid migration catalog")
	ErrInvalidMigrationRunner   = errors.New("invalid migration runner")
	ErrMigrationLockUnavailable = errors.New("migration advisory lock unavailable")
	ErrMigrationLockRelease     = errors.New("migration advisory lock release failed")
	ErrMigrationHistory         = errors.New("migration history is inconsistent")
	ErrMigrationChecksum        = errors.New("migration checksum mismatch")
	ErrMigrationPreflight       = errors.New("migration preflight failed")
	ErrMigrationExecution       = errors.New("migration execution failed")
	ErrMigrationRollback        = errors.New("migration rollback failed")
	ErrMigrationTarget          = errors.New("migration target does not match selected database")
	ErrMigrationDirty           = errors.New("migration ledger contains an incomplete transition")
)

var migrationFilePattern = regexp.MustCompile(`^([0-9]+)_([a-z][a-z0-9_-]*)\.(up|down|preflight)\.sql$`)

// Migration is a reversible, versioned database change. Checksum is assigned
// by NewMigrationCatalog and covers the version, name, preflight, up, and down
// SQL exactly as supplied. Migration files therefore must never be edited once
// they have been applied to any environment.
type Migration struct {
	Version uint64
	Name    string

	PreflightSQL string
	UpSQL        string
	DownSQL      string
	Checksum     string
}

// MigrationTarget identifies one of the two vSRO database schemas. A loaded
// catalog carries its target, and a runner must be configured for the same
// target and currently selected database before it is allowed to create a
// ledger or execute SQL. This prevents an account catalog from recording
// history in the shard database while changing account tables (or vice versa).
type MigrationTarget string

const (
	MigrationTargetAccount MigrationTarget = "account"
	MigrationTargetShard   MigrationTarget = "shard"
)

func (t MigrationTarget) databaseName() string {
	switch t {
	case MigrationTargetAccount:
		return "SRO_ACCOUNT"
	case MigrationTargetShard:
		return "SRO_SHARD"
	default:
		return ""
	}
}

func parseMigrationTarget(value string) (MigrationTarget, error) {
	target := MigrationTarget(strings.ToLower(strings.TrimSpace(value)))
	if target.databaseName() == "" {
		return "", fmt.Errorf("%w: unknown target %q", ErrInvalidMigrationCatalog, value)
	}
	return target, nil
}

// MigrationCatalog is an immutable, version-sorted collection of migrations
// for exactly one vSRO database schema. Construct it with
// NewMigrationCatalog or LoadMigrationCatalog.
type MigrationCatalog struct {
	target     MigrationTarget
	migrations []Migration
}

// Migrations returns a defensive copy in ascending version order.
func (c MigrationCatalog) Migrations() []Migration {
	return append([]Migration(nil), c.migrations...)
}

// Target is the schema this catalog may be applied to.
func (c MigrationCatalog) Target() MigrationTarget {
	return c.target
}

// NewMigrationCatalog validates, checksums, and sorts a target-bound set of
// migrations. Every migration must be reversible: both UpSQL and DownSQL are
// required. The target is mandatory so a catalog can never be applied to a
// differently configured database by accident.
func NewMigrationCatalog(target MigrationTarget, migrations []Migration) (MigrationCatalog, error) {
	if target.databaseName() == "" {
		return MigrationCatalog{}, fmt.Errorf("%w: migration target is required", ErrInvalidMigrationCatalog)
	}
	normalized := append([]Migration(nil), migrations...)
	seenVersions := make(map[uint64]struct{}, len(normalized))

	for index := range normalized {
		migration := &normalized[index]
		migration.Name = strings.TrimSpace(migration.Name)
		if migration.Version == 0 {
			return MigrationCatalog{}, fmt.Errorf("%w: migration %q has version 0", ErrInvalidMigrationCatalog, migration.Name)
		}
		if migration.Name == "" {
			return MigrationCatalog{}, fmt.Errorf("%w: migration %d has no name", ErrInvalidMigrationCatalog, migration.Version)
		}
		if _, exists := seenVersions[migration.Version]; exists {
			return MigrationCatalog{}, fmt.Errorf("%w: duplicate version %d", ErrInvalidMigrationCatalog, migration.Version)
		}
		seenVersions[migration.Version] = struct{}{}

		if err := validateMigrationSQL(*migration); err != nil {
			return MigrationCatalog{}, err
		}

		computedChecksum := MigrationChecksum(*migration)
		if migration.Checksum != "" && migration.Checksum != computedChecksum {
			return MigrationCatalog{}, fmt.Errorf("%w: migration %d %q supplied a checksum that does not match its SQL", ErrInvalidMigrationCatalog, migration.Version, migration.Name)
		}
		migration.Checksum = computedChecksum
	}

	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left].Version < normalized[right].Version
	})

	return MigrationCatalog{target: target, migrations: normalized}, nil
}

// LoadMigrationCatalog reads one migration target directory from fileSystem.
// A migration consists of files named:
//
//	000001_normalize_identity.preflight.sql (optional)
//	000001_normalize_identity.up.sql
//	000001_normalize_identity.down.sql
//
// The target directory is deliberately supplied by the caller so account and
// shard schemas cannot accidentally share a migration ledger or connection.
func LoadMigrationCatalog(fileSystem fs.FS, targetDirectory string) (MigrationCatalog, error) {
	if fileSystem == nil {
		return MigrationCatalog{}, fmt.Errorf("%w: migration filesystem is required", ErrInvalidMigrationCatalog)
	}
	targetDirectory = strings.Trim(strings.TrimSpace(targetDirectory), "/")
	if targetDirectory == "" || targetDirectory == "." {
		return MigrationCatalog{}, fmt.Errorf("%w: target directory is required", ErrInvalidMigrationCatalog)
	}
	target, err := parseMigrationTarget(targetDirectory)
	if err != nil {
		return MigrationCatalog{}, err
	}

	entries, err := fs.ReadDir(fileSystem, targetDirectory)
	if err != nil {
		return MigrationCatalog{}, fmt.Errorf("%w: read %q: %v", ErrInvalidMigrationCatalog, targetDirectory, err)
	}

	type parts struct {
		version   uint64
		name      string
		preflight string
		up        string
		down      string
	}
	partsByKey := make(map[string]*parts)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		if !strings.HasSuffix(fileName, ".sql") {
			continue
		}

		matches := migrationFilePattern.FindStringSubmatch(fileName)
		if matches == nil {
			return MigrationCatalog{}, fmt.Errorf("%w: unexpected migration filename %q", ErrInvalidMigrationCatalog, fileName)
		}
		version, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil || version == 0 {
			return MigrationCatalog{}, fmt.Errorf("%w: invalid migration version in %q", ErrInvalidMigrationCatalog, fileName)
		}

		contents, err := fs.ReadFile(fileSystem, path.Join(targetDirectory, fileName))
		if err != nil {
			return MigrationCatalog{}, fmt.Errorf("%w: read %q: %v", ErrInvalidMigrationCatalog, fileName, err)
		}

		key := strconv.FormatUint(version, 10) + ":" + matches[2]
		migrationParts := partsByKey[key]
		if migrationParts == nil {
			migrationParts = &parts{version: version, name: matches[2]}
			partsByKey[key] = migrationParts
		}

		switch matches[3] {
		case "preflight":
			if migrationParts.preflight != "" {
				return MigrationCatalog{}, fmt.Errorf("%w: duplicate preflight SQL for migration %d %q", ErrInvalidMigrationCatalog, version, matches[2])
			}
			migrationParts.preflight = string(contents)
		case "up":
			if migrationParts.up != "" {
				return MigrationCatalog{}, fmt.Errorf("%w: duplicate up SQL for migration %d %q", ErrInvalidMigrationCatalog, version, matches[2])
			}
			migrationParts.up = string(contents)
		case "down":
			if migrationParts.down != "" {
				return MigrationCatalog{}, fmt.Errorf("%w: duplicate down SQL for migration %d %q", ErrInvalidMigrationCatalog, version, matches[2])
			}
			migrationParts.down = string(contents)
		}
	}

	migrations := make([]Migration, 0, len(partsByKey))
	for _, migrationParts := range partsByKey {
		migrations = append(migrations, Migration{
			Version:      migrationParts.version,
			Name:         migrationParts.name,
			PreflightSQL: migrationParts.preflight,
			UpSQL:        migrationParts.up,
			DownSQL:      migrationParts.down,
		})
	}
	return NewMigrationCatalog(target, migrations)
}

// MigrationChecksum returns the SHA-256 checksum stored in the migration
// ledger. NUL separators make the serialized fields unambiguous without
// modifying source SQL or its line endings.
func MigrationChecksum(migration Migration) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(strconv.FormatUint(migration.Version, 10)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(migration.Name))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(migration.PreflightSQL))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(migration.UpSQL))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(migration.DownSQL))
	return hex.EncodeToString(hash.Sum(nil))
}

func validateMigrationSQL(migration Migration) error {
	for _, part := range []struct {
		name string
		sql  string
	}{
		{name: "up", sql: migration.UpSQL},
		{name: "down", sql: migration.DownSQL},
	} {
		if strings.TrimSpace(part.sql) == "" {
			return fmt.Errorf("%w: migration %d %q has no %s SQL", ErrInvalidMigrationCatalog, migration.Version, migration.Name, part.name)
		}
		statements, err := executableSQLStatements(part.sql)
		if err != nil {
			return fmt.Errorf("%w: migration %d %q has invalid %s SQL: %v", ErrInvalidMigrationCatalog, migration.Version, migration.Name, part.name, err)
		}
		if len(statements) == 0 {
			return fmt.Errorf("%w: migration %d %q has no executable %s SQL", ErrInvalidMigrationCatalog, migration.Version, migration.Name, part.name)
		}
	}
	if strings.TrimSpace(migration.PreflightSQL) != "" {
		statements, err := executableSQLStatements(migration.PreflightSQL)
		if err != nil {
			return fmt.Errorf("%w: migration %d %q has invalid preflight SQL: %v", ErrInvalidMigrationCatalog, migration.Version, migration.Name, err)
		}
		for _, statement := range statements {
			if !isReadOnlyPreflightStatement(statement) {
				return fmt.Errorf("%w: migration %d %q has non-SELECT preflight SQL", ErrInvalidMigrationCatalog, migration.Version, migration.Name)
			}
		}
	}
	return nil
}

// MigrationRunnerOptions controls the advisory-lock behavior of a runner.
// LockTimeout is bounded to keep a startup from waiting indefinitely. Zero
// selects DefaultMigrationLockTimeout.
type MigrationRunnerOptions struct {
	// Target must be supplied for a catalog loaded from db/migrations. The
	// runner verifies it against both the catalog and SELECT DATABASE() on its
	// pinned connection before it creates a ledger or runs schema SQL.
	Target MigrationTarget

	LockName    string
	LockTimeout time.Duration
}

// MigrationRunner applies one catalog to one MySQL database handle. It pins a
// single SQL connection while it holds GET_LOCK, because MySQL advisory locks
// are connection-scoped rather than pool-scoped.
type MigrationRunner struct {
	database           *sql.DB
	target             MigrationTarget
	lockName           string
	lockTimeoutSeconds int64
	releaseLockTimeout time.Duration
}

// NewMigrationRunner prepares a runner but does not contact the database.
func NewMigrationRunner(database *sql.DB, options MigrationRunnerOptions) (*MigrationRunner, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: database handle is required", ErrInvalidMigrationRunner)
	}
	if options.Target.databaseName() == "" {
		return nil, fmt.Errorf("%w: migration target is required", ErrInvalidMigrationRunner)
	}

	lockName := strings.TrimSpace(options.LockName)
	if lockName == "" {
		lockName = DefaultMigrationLockName
	}
	// MySQL limits GET_LOCK names to 64 characters. The function accepts UTF-8
	// strings, but bytes are what the wire protocol and server limit.
	if len(lockName) > 64 {
		return nil, fmt.Errorf("%w: advisory lock name exceeds 64 bytes", ErrInvalidMigrationRunner)
	}

	lockTimeout := options.LockTimeout
	if lockTimeout == 0 {
		lockTimeout = DefaultMigrationLockTimeout
	}
	if lockTimeout < 0 || lockTimeout > MaxMigrationLockTimeout {
		return nil, fmt.Errorf("%w: lock timeout must be between 0 and %s", ErrInvalidMigrationRunner, MaxMigrationLockTimeout)
	}

	lockTimeoutSeconds := int64((lockTimeout + time.Second - 1) / time.Second)
	if lockTimeoutSeconds < 1 {
		lockTimeoutSeconds = 1
	}
	return &MigrationRunner{
		database:           database,
		target:             options.Target,
		lockName:           lockName,
		lockTimeoutSeconds: lockTimeoutSeconds,
		releaseLockTimeout: DefaultMigrationLockTimeout,
	}, nil
}

// Apply verifies the entire recorded history, runs each pending preflight, and
// then records each successful up migration in the version/checksum ledger.
func (r *MigrationRunner) Apply(ctx context.Context, catalog MigrationCatalog) error {
	if r == nil {
		return fmt.Errorf("%w: runner is nil", ErrInvalidMigrationRunner)
	}
	return r.withLock(ctx, func(ctx context.Context, connection *sql.Conn) error {
		if err := r.validateCatalogTarget(ctx, connection, catalog); err != nil {
			return err
		}
		if err := ensureMigrationLedger(ctx, connection); err != nil {
			return err
		}
		applied, err := readMigrationLedger(ctx, connection)
		if err != nil {
			return err
		}
		plan, err := planPendingMigrations(catalog, applied)
		if err != nil {
			return err
		}

		for _, migration := range plan.migrations {
			if err := runMigrationPreflight(ctx, connection, migration); err != nil {
				return err
			}
			// MySQL DDL can commit implicitly, so it cannot share an atomic
			// transaction with the ledger. Persist a durable applying marker
			// before the first statement. Any execution or finalization failure
			// leaves that marker behind and blocks an unsafe automatic retry.
			if err := beginMigrationApplication(ctx, connection, migration); err != nil {
				return err
			}
			if err := executeMigrationSQL(ctx, connection, migration, "up", migration.UpSQL, ErrMigrationExecution); err != nil {
				return err
			}
			if err := markMigrationApplied(ctx, connection, migration); err != nil {
				return err
			}
		}
		return nil
	})
}

// Rollback runs the requested number of most-recent recorded migrations in
// descending version order. A down migration is only removed from the ledger
// after its SQL succeeds.
func (r *MigrationRunner) Rollback(ctx context.Context, catalog MigrationCatalog, steps int) error {
	if r == nil {
		return fmt.Errorf("%w: runner is nil", ErrInvalidMigrationRunner)
	}
	if steps < 1 {
		return fmt.Errorf("%w: rollback steps must be positive", ErrMigrationRollback)
	}
	return r.withLock(ctx, func(ctx context.Context, connection *sql.Conn) error {
		if err := r.validateCatalogTarget(ctx, connection, catalog); err != nil {
			return err
		}
		if err := ensureMigrationLedger(ctx, connection); err != nil {
			return err
		}
		applied, err := readMigrationLedger(ctx, connection)
		if err != nil {
			return err
		}
		plan, err := planRollbackMigrations(catalog, applied, steps)
		if err != nil {
			return err
		}

		for _, migration := range plan.migrations {
			// Persist the rollback transition before any down SQL. If the DDL
			// outcome becomes uncertain, the dirty ledger row deliberately blocks
			// an automatic retry until an operator reconciles it.
			if err := beginMigrationRollback(ctx, connection, migration); err != nil {
				return err
			}
			if err := executeMigrationSQL(ctx, connection, migration, "down", migration.DownSQL, ErrMigrationRollback); err != nil {
				return err
			}
			if err := finishMigrationRollback(ctx, connection, migration); err != nil {
				return err
			}
		}
		return nil
	})
}

// Down is a readable alias for Rollback.
func (r *MigrationRunner) Down(ctx context.Context, catalog MigrationCatalog, steps int) error {
	return r.Rollback(ctx, catalog, steps)
}

// MigrationError identifies the migration and phase that failed while keeping
// errors.Is/errors.As support through its wrapped error.
type MigrationError struct {
	Operation string
	Version   uint64
	Name      string
	Err       error
}

func (e *MigrationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	identifier := "migration"
	if e.Version != 0 {
		identifier = fmt.Sprintf("migration %d", e.Version)
	}
	if e.Name != "" {
		identifier += " " + e.Name
	}
	if e.Err == nil {
		return fmt.Sprintf("%s %s failed", identifier, e.Operation)
	}
	return fmt.Sprintf("%s %s failed: %v", identifier, e.Operation, e.Err)
}

func (e *MigrationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func migrationError(operation string, migration Migration, err error) error {
	return &MigrationError{
		Operation: operation,
		Version:   migration.Version,
		Name:      migration.Name,
		Err:       err,
	}
}

type migrationLedgerEntry struct {
	Version  uint64
	Name     string
	Checksum string
	State    migrationLedgerState
}

type migrationLedgerState string

const (
	migrationStateApplying    migrationLedgerState = "applying"
	migrationStateApplied     migrationLedgerState = "applied"
	migrationStateRollingBack migrationLedgerState = "rolling_back"
)

func beginMigrationApplication(ctx context.Context, connection *sql.Conn, migration Migration) error {
	return transitionMigrationLedger(ctx, connection, "record applying", migration, ErrMigrationExecution,
		"INSERT INTO `"+MigrationLedgerTable+"` (`version`, `name`, `checksum`, `state`) VALUES (?, ?, ?, ?)",
		migration.Version, migration.Name, migration.Checksum, migrationStateApplying,
	)
}

func markMigrationApplied(ctx context.Context, connection *sql.Conn, migration Migration) error {
	return transitionMigrationLedger(ctx, connection, "mark applied", migration, ErrMigrationExecution,
		"UPDATE `"+MigrationLedgerTable+"` SET `state`=? WHERE `version`=? AND `name`=? AND `checksum`=? AND `state`=?",
		migrationStateApplied, migration.Version, migration.Name, migration.Checksum, migrationStateApplying,
	)
}

func beginMigrationRollback(ctx context.Context, connection *sql.Conn, migration Migration) error {
	return transitionMigrationLedger(ctx, connection, "record rollback", migration, ErrMigrationRollback,
		"UPDATE `"+MigrationLedgerTable+"` SET `state`=? WHERE `version`=? AND `name`=? AND `checksum`=? AND `state`=?",
		migrationStateRollingBack, migration.Version, migration.Name, migration.Checksum, migrationStateApplied,
	)
}

func finishMigrationRollback(ctx context.Context, connection *sql.Conn, migration Migration) error {
	return transitionMigrationLedger(ctx, connection, "remove rollback ledger entry", migration, ErrMigrationRollback,
		"DELETE FROM `"+MigrationLedgerTable+"` WHERE `version`=? AND `name`=? AND `checksum`=? AND `state`=?",
		migration.Version, migration.Name, migration.Checksum, migrationStateRollingBack,
	)
}

// transitionMigrationLedger commits a single state transition before or after
// DDL. The transition must not depend on a pooled connection's autocommit
// setting: another consumer may previously have used SET autocommit=0. Keep
// this transaction deliberately short and never run migration DDL inside it,
// because MySQL DDL may commit implicitly.
func transitionMigrationLedger(ctx context.Context, connection *sql.Conn, operation string, migration Migration, sentinel error, statement string, arguments ...any) (resultErr error) {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return migrationError(operation, migration, fmt.Errorf("%w: begin ledger transition: %w", sentinel, err))
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, migrationError(operation, migration, fmt.Errorf("%w: rollback ledger transition: %w", sentinel, rollbackErr)))
		}
	}()

	result, err := transaction.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return migrationError(operation, migration, fmt.Errorf("%w: %w", sentinel, err))
	}
	if err := requireMigrationTransition(result, operation, migration, sentinel); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		// Do not continue after a commit error. Its outcome can be unknown: a
		// durable marker is safe, while executing DDL without one is not.
		return migrationError(operation, migration, fmt.Errorf("%w: commit ledger transition: %w", sentinel, err))
	}
	committed = true
	return nil
}

func requireMigrationTransition(result sql.Result, operation string, migration Migration, sentinel error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return migrationError(operation, migration, fmt.Errorf("%w: %w", sentinel, err))
	}
	if affected != 1 {
		return migrationError(operation, migration, fmt.Errorf("%w: expected one ledger row, affected %d", errors.Join(sentinel, ErrMigrationHistory), affected))
	}
	return nil
}

func (coordinator migrationLockCoordinator) run(ctx context.Context, operation func(context.Context, *sql.Conn) error) (resultErr error) {
	r := coordinator.runner
	if r == nil || r.database == nil {
		return fmt.Errorf("%w: database handle is required", ErrInvalidMigrationRunner)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	connection, err := r.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migration connection: %w", err)
	}
	discarded := false
	discard := func() {
		if discarded {
			return
		}
		discarded = true
		if discardErr := discardMigrationConnection(connection); discardErr != nil {
			resultErr = errors.Join(resultErr, discardErr)
		}
	}
	defer func() {
		if discarded {
			return
		}
		if closeErr := connection.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("migration connection close: %w", closeErr))
		}
	}()

	if err := r.acquireLock(ctx, connection); err != nil {
		// A transport/scan error can occur after MySQL processed GET_LOCK. Do
		// not return that server session to the pool unless we know no lock was
		// acquired; closing it through driver.ErrBadConn releases any
		// connection-scoped advisory lock at the server.
		if !errors.Is(err, ErrMigrationLockUnavailable) {
			discard()
		}
		return err
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.Background(), r.releaseLockTimeout)
		defer cancel()
		if releaseErr := r.releaseLock(releaseContext, connection); releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
			// sql.Conn.Close normally returns this connection to database/sql's
			// pool; it does not promise to terminate the MySQL server session.
			// Discard on every uncertain release result so GET_LOCK cannot leak
			// into a later migration run on the reused session.
			discard()
		}
	}()

	return operation(ctx, connection)
}

// discardMigrationConnection tells database/sql that a pinned session is no
// longer reusable. driver.ErrBadConn causes Conn.Raw to discard the underlying
// physical connection rather than return it to the pool, which is required for
// a MySQL connection-scoped GET_LOCK whose release result is uncertain.
func discardMigrationConnection(connection *sql.Conn) error {
	if connection == nil {
		return nil
	}
	err := connection.Raw(func(any) error { return driver.ErrBadConn })
	if err == nil || errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return fmt.Errorf("discard migration advisory-lock connection: %w", err)
}

func (r *MigrationRunner) validateCatalogTarget(ctx context.Context, connection *sql.Conn, catalog MigrationCatalog) error {
	if catalog.target.databaseName() == "" {
		return fmt.Errorf("%w: catalog target is required", ErrMigrationTarget)
	}
	if r == nil || r.target != catalog.target {
		var runnerTarget MigrationTarget
		if r != nil {
			runnerTarget = r.target
		}
		return fmt.Errorf("%w: catalog target %q, runner target %q", ErrMigrationTarget, catalog.target, runnerTarget)
	}

	var selectedDatabase sql.NullString
	if err := connection.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selectedDatabase); err != nil {
		return fmt.Errorf("%w: query selected database: %v", ErrMigrationTarget, err)
	}
	expectedDatabase := catalog.target.databaseName()
	// MySQL schema names can be case-sensitive on Linux. Require the exact
	// selected schema, not a case-folded match, because the migration SQL is
	// deliberately unqualified and must affect the ledger's schema too.
	if !selectedDatabase.Valid || strings.TrimSpace(selectedDatabase.String) != expectedDatabase {
		return fmt.Errorf("%w: selected database %q, expected %q", ErrMigrationTarget, selectedDatabase.String, expectedDatabase)
	}
	return nil
}

func (r *MigrationRunner) acquireLock(ctx context.Context, connection *sql.Conn) error {
	var lockResult sql.NullInt64
	if err := connection.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", r.lockName, r.lockTimeoutSeconds).Scan(&lockResult); err != nil {
		return fmt.Errorf("migration advisory lock: %w", err)
	}
	if !lockResult.Valid || lockResult.Int64 == 0 {
		return ErrMigrationLockUnavailable
	}
	if lockResult.Int64 != 1 {
		return fmt.Errorf("migration advisory lock: %w (unexpected result %d)", ErrMigrationLockUnavailable, lockResult.Int64)
	}
	return nil
}

func (r *MigrationRunner) releaseLock(ctx context.Context, connection *sql.Conn) error {
	var lockResult sql.NullInt64
	if err := connection.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", r.lockName).Scan(&lockResult); err != nil {
		return fmt.Errorf("migration advisory lock release: %w", err)
	}
	if !lockResult.Valid || lockResult.Int64 != 1 {
		return ErrMigrationLockRelease
	}
	return nil
}

func ensureMigrationLedger(ctx context.Context, connection *sql.Conn) error {
	const createLedgerSQL = "CREATE TABLE IF NOT EXISTS `go_sro_schema_migrations` (" +
		"`version` BIGINT UNSIGNED NOT NULL, " +
		"`name` VARCHAR(255) NOT NULL, " +
		"`checksum` CHAR(64) NOT NULL, " +
		"`state` VARCHAR(16) NOT NULL, " +
		"`applied_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), " +
		"PRIMARY KEY (`version`)" +
		") ENGINE=InnoDB"
	if _, err := connection.ExecContext(ctx, createLedgerSQL); err != nil {
		return fmt.Errorf("migration ledger create: %w", err)
	}

	// The first version of this runner recorded only version/name/checksum.
	// Upgrade that ledger in place under the same advisory lock so its completed
	// rows become explicitly applied. This is deliberately separate from game
	// schema migrations: a legacy ledger has only rows written after successful
	// DDL, so "applied" is the only safe historical state to infer.
	var stateColumnCount int
	if err := connection.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		MigrationLedgerTable, "state",
	).Scan(&stateColumnCount); err != nil {
		return fmt.Errorf("migration ledger state-column check: %w", err)
	}
	if stateColumnCount == 0 {
		if _, err := connection.ExecContext(ctx,
			"ALTER TABLE `"+MigrationLedgerTable+"` ADD COLUMN `state` VARCHAR(16) NOT NULL DEFAULT 'applied'",
		); err != nil {
			return fmt.Errorf("migration ledger state-column upgrade: %w", err)
		}
	}
	return nil
}

func readMigrationLedger(ctx context.Context, connection *sql.Conn) ([]migrationLedgerEntry, error) {
	rows, err := connection.QueryContext(ctx, "SELECT `version`, `name`, `checksum`, `state` FROM `"+MigrationLedgerTable+"` ORDER BY `version` ASC")
	if err != nil {
		return nil, fmt.Errorf("migration ledger read: %w", err)
	}
	defer rows.Close()

	entries := make([]migrationLedgerEntry, 0)
	for rows.Next() {
		var entry migrationLedgerEntry
		if err := rows.Scan(&entry.Version, &entry.Name, &entry.Checksum, &entry.State); err != nil {
			return nil, fmt.Errorf("migration ledger scan: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migration ledger rows: %w", err)
	}
	return entries, nil
}

func verifyMigrationHistory(catalog MigrationCatalog, applied []migrationLedgerEntry) (map[uint64]migrationLedgerEntry, error) {
	byVersion := make(map[uint64]Migration, len(catalog.migrations))
	for _, migration := range catalog.migrations {
		byVersion[migration.Version] = migration
	}

	appliedByVersion := make(map[uint64]migrationLedgerEntry, len(applied))
	for _, entry := range applied {
		if _, duplicate := appliedByVersion[entry.Version]; duplicate {
			return nil, fmt.Errorf("%w: duplicate ledger version %d", ErrMigrationHistory, entry.Version)
		}
		if entry.State != migrationStateApplied {
			return nil, fmt.Errorf("%w: migration %d is %q", ErrMigrationDirty, entry.Version, entry.State)
		}
		migration, exists := byVersion[entry.Version]
		if !exists {
			return nil, fmt.Errorf("%w: ledger version %d is not present in the catalog", ErrMigrationHistory, entry.Version)
		}
		if entry.Name != migration.Name {
			return nil, migrationError("verify name", migration, fmt.Errorf("%w: ledger name %q", ErrMigrationHistory, entry.Name))
		}
		if entry.Checksum != migration.Checksum {
			return nil, migrationError("verify checksum", migration, fmt.Errorf("%w: recorded %s, catalog %s", ErrMigrationChecksum, entry.Checksum, migration.Checksum))
		}
		appliedByVersion[entry.Version] = entry
	}

	missingMigrationSeen := false
	for _, migration := range catalog.migrations {
		_, isApplied := appliedByVersion[migration.Version]
		if !isApplied {
			missingMigrationSeen = true
			continue
		}
		if missingMigrationSeen {
			return nil, fmt.Errorf("%w: migration %d is applied after an earlier catalog migration is missing", ErrMigrationHistory, migration.Version)
		}
	}
	return appliedByVersion, nil
}

func runMigrationPreflight(ctx context.Context, connection *sql.Conn, migration Migration) error {
	if strings.TrimSpace(migration.PreflightSQL) == "" {
		return nil
	}
	statements, err := executableSQLStatements(migration.PreflightSQL)
	if err != nil {
		return migrationError("parse preflight", migration, fmt.Errorf("%w: %w", ErrMigrationPreflight, err))
	}
	for _, statement := range statements {
		if !isReadOnlyPreflightStatement(statement) {
			return migrationError("parse preflight", migration, fmt.Errorf("%w: preflight statement is not SELECT-only", ErrMigrationPreflight))
		}
		rows, err := connection.QueryContext(ctx, statement)
		if err != nil {
			return migrationError("preflight", migration, fmt.Errorf("%w: %w", ErrMigrationPreflight, err))
		}
		hasViolations := rows.Next()
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if hasViolations {
			return migrationError("preflight", migration, ErrMigrationPreflight)
		}
		if rowsErr != nil {
			return migrationError("preflight", migration, fmt.Errorf("%w: %w", ErrMigrationPreflight, rowsErr))
		}
		if closeErr != nil {
			return migrationError("preflight", migration, fmt.Errorf("%w: %w", ErrMigrationPreflight, closeErr))
		}
	}
	return nil
}

func executeMigrationSQL(ctx context.Context, connection *sql.Conn, migration Migration, operation string, sqlText string, sentinel error) error {
	statements, err := executableSQLStatements(sqlText)
	if err != nil {
		return migrationError("parse "+operation, migration, fmt.Errorf("%w: %w", sentinel, err))
	}
	if len(statements) == 0 {
		return migrationError("parse "+operation, migration, fmt.Errorf("%w: no executable SQL", sentinel))
	}
	for _, statement := range statements {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			return migrationError(operation, migration, fmt.Errorf("%w: %w", sentinel, err))
		}
	}
	return nil
}

// executableSQLStatements removes comment-only fragments from the parser
// output. Comments are retained in statements that also contain SQL so driver
// diagnostics still reference the authored migration text.
func executableSQLStatements(sqlText string) ([]string, error) {
	statements, err := splitSQLStatements(sqlText)
	if err != nil {
		return nil, err
	}
	executable := make([]string, 0, len(statements))
	for _, statement := range statements {
		if sqlStatementBody(statement) == "" {
			continue
		}
		executable = append(executable, statement)
	}
	return executable, nil
}

// sqlStatementBody removes only leading whitespace and comments, enough to
// determine whether a parser fragment contains executable SQL and to classify
// a preflight's first keyword. splitSQLStatements has already verified that
// quotes and block comments are balanced.
func sqlStatementBody(statement string) string {
	body := strings.TrimSpace(statement)
	for body != "" {
		switch {
		case strings.HasPrefix(body, "--") && (len(body) == 2 || body[2] == ' ' || body[2] == '\t' || body[2] == '\r' || body[2] == '\n'):
			newline := strings.IndexByte(body, '\n')
			if newline < 0 {
				return ""
			}
			body = strings.TrimSpace(body[newline+1:])
		case strings.HasPrefix(body, "#"):
			newline := strings.IndexByte(body, '\n')
			if newline < 0 {
				return ""
			}
			body = strings.TrimSpace(body[newline+1:])
		case strings.HasPrefix(body, "/*"):
			end := strings.Index(body[2:], "*/")
			if end < 0 {
				return body
			}
			body = strings.TrimSpace(body[end+4:])
		default:
			return body
		}
	}
	return ""
}

// isReadOnlyPreflightStatement deliberately accepts only a leading SELECT.
// MySQL's QueryContext can execute a DML statement and report an empty result
// set, so allowing generic QueryContext here would turn a typo in a duplicate
// preflight into a destructive mutation. WITH is not accepted because MySQL
// permits data-modifying statements after a CTE.
func isReadOnlyPreflightStatement(statement string) bool {
	body := sqlStatementBody(statement)
	if body == "" {
		return false
	}
	keywordEnd := 0
	for keywordEnd < len(body) {
		character := body[keywordEnd]
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			break
		}
		keywordEnd++
	}
	return keywordEnd == len("SELECT") && strings.EqualFold(body[:keywordEnd], "SELECT")
}

// splitSQLStatements handles the quoting and comment forms used by the MySQL
// migration files. It intentionally does not implement DELIMITER/routine
// syntax; schema migrations should be ordinary SQL statements so they remain
// inspectable and portable through database/sql.
func splitSQLStatements(sqlText string) ([]string, error) {
	const (
		normal = iota
		singleQuoted
		doubleQuoted
		backtickQuoted
		lineComment
		blockComment
	)

	state := normal
	var current strings.Builder
	statements := make([]string, 0, 1)
	appendCurrent := func() {
		statement := strings.TrimSpace(current.String())
		if statement != "" {
			statements = append(statements, statement)
		}
		current.Reset()
	}

	for index := 0; index < len(sqlText); index++ {
		character := sqlText[index]
		next := byte(0)
		if index+1 < len(sqlText) {
			next = sqlText[index+1]
		}

		switch state {
		case normal:
			switch {
			case character == '\'':
				state = singleQuoted
				current.WriteByte(character)
			case character == '"':
				state = doubleQuoted
				current.WriteByte(character)
			case character == '`':
				state = backtickQuoted
				current.WriteByte(character)
			case character == '-' && next == '-' && (index+2 == len(sqlText) || sqlText[index+2] == ' ' || sqlText[index+2] == '\t' || sqlText[index+2] == '\r' || sqlText[index+2] == '\n'):
				state = lineComment
				current.WriteByte(character)
				current.WriteByte(next)
				index++
			case character == '#':
				state = lineComment
				current.WriteByte(character)
			case character == '/' && next == '*':
				state = blockComment
				current.WriteByte(character)
				current.WriteByte(next)
				index++
			case character == ';':
				appendCurrent()
			default:
				current.WriteByte(character)
			}
		case singleQuoted:
			current.WriteByte(character)
			if character == '\\' && index+1 < len(sqlText) {
				current.WriteByte(next)
				index++
			} else if character == '\'' {
				if next == '\'' {
					current.WriteByte(next)
					index++
				} else {
					state = normal
				}
			}
		case doubleQuoted:
			current.WriteByte(character)
			if character == '\\' && index+1 < len(sqlText) {
				current.WriteByte(next)
				index++
			} else if character == '"' {
				if next == '"' {
					current.WriteByte(next)
					index++
				} else {
					state = normal
				}
			}
		case backtickQuoted:
			current.WriteByte(character)
			if character == '`' {
				if next == '`' {
					current.WriteByte(next)
					index++
				} else {
					state = normal
				}
			}
		case lineComment:
			current.WriteByte(character)
			if character == '\n' {
				state = normal
			}
		case blockComment:
			current.WriteByte(character)
			if character == '*' && next == '/' {
				current.WriteByte(next)
				index++
				state = normal
			}
		}
	}

	if state == singleQuoted || state == doubleQuoted || state == backtickQuoted || state == blockComment {
		return nil, errors.New("unterminated SQL quote or comment")
	}
	appendCurrent()
	return statements, nil
}
