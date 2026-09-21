package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/framework/internal/textutil"
)

var (
	ErrDefinitionExists   = errors.New("scheduler definition already exists")
	ErrDefinitionNotFound = errors.New("scheduler definition was not found")
	ErrDefinitionChanged  = errors.New("scheduler definition changed")
	ErrRunLeaseLost       = errors.New("scheduler run lease is no longer owned")
)

type database interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type SQLStore struct {
	database database
	now      func() time.Time
}

func NewSQLStore(database database) (*SQLStore, error) {
	if database == nil {
		return nil, errors.New("scheduler database is required")
	}
	return &SQLStore{database: database, now: time.Now}, nil
}

func (store *SQLStore) Create(ctx context.Context, definition Definition) (Definition, error) {
	if store == nil || store.database == nil {
		return Definition{}, errors.New("scheduler store is not configured")
	}
	if definition.Version != 0 || !definition.NextRunAt.IsZero() {
		return Definition{}, errors.New("new scheduler definition cannot supply persisted version or next run")
	}
	if err := definition.Validate(); err != nil {
		return Definition{}, err
	}
	next, available, err := initialRun(definition)
	if err != nil {
		return Definition{}, err
	}
	definition.Version = 1
	definition.NextRunAt = next
	if !available {
		definition.Enabled = false
	}
	ctx = normalizeContext(ctx)
	_, err = store.database.ExecContext(ctx,
		"INSERT INTO `AEGIS_SCHEDULE_DEFINITION` (`SCHEDULE_ID`,`NAME`,`HANDLER`,`PAYLOAD`,`RECURRENCE`,`PHASE`,`SCOPE`,`SHARD_ID`,`TIMEZONE`,`ACTIVE_FROM`,`ACTIVE_UNTIL`,`RUN_AT`,`SECOND_OF_DAY`,`INTERVAL_VALUE`,`WEEKDAY_MASK`,`MISFIRE_POLICY`,`ENABLED`,`VERSION`,`NEXT_RUN_AT`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		definition.ID, definition.Name, definition.Handler, []byte(definition.Payload), definition.Recurrence, definition.Phase, definition.Scope, nullableShard(definition), definition.Timezone,
		nullableDate(definition.ActiveFrom), nullableDate(definition.ActiveUntil), nullableRunAt(definition), nullableSecond(definition), nullableInterval(definition), nullableWeekdays(definition),
		definition.MisfirePolicy, definition.Enabled, definition.Version, nullableTime(definition.NextRunAt),
	)
	if err != nil {
		return Definition{}, fmt.Errorf("create scheduler definition %q: %w", definition.ID, err)
	}
	return cloneDefinition(definition), nil
}

func (store *SQLStore) Get(ctx context.Context, id string) (Definition, error) {
	if store == nil || store.database == nil {
		return Definition{}, errors.New("scheduler store is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return Definition{}, errors.New("scheduler definition ID must contain 1-128 characters")
	}
	rows, err := store.database.QueryContext(normalizeContext(ctx), definitionSelect+" WHERE `SCHEDULE_ID`=?", id)
	if err != nil {
		return Definition{}, fmt.Errorf("query scheduler definition %q: %w", id, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Definition{}, fmt.Errorf("query scheduler definition %q: %w", id, err)
		}
		return Definition{}, ErrDefinitionNotFound
	}
	definition, err := scanDefinition(rows)
	if err != nil {
		return Definition{}, err
	}
	if rows.Next() {
		return Definition{}, fmt.Errorf("scheduler definition %q is duplicated", id)
	}
	return cloneDefinition(definition), nil
}

func (store *SQLStore) List(ctx context.Context, limit int) ([]Definition, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("scheduler store is not configured")
	}
	if limit < 1 || limit > 500 {
		return nil, errors.New("scheduler definition list limit must be between 1 and 500")
	}
	rows, err := store.database.QueryContext(normalizeContext(ctx), definitionSelect+" ORDER BY `SCHEDULE_ID` LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("list scheduler definitions: %w", err)
	}
	defer rows.Close()
	definitions := make([]Definition, 0)
	for rows.Next() {
		definition, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, cloneDefinition(definition))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scheduler definitions: %w", err)
	}
	return definitions, nil
}

func (store *SQLStore) SetEnabled(ctx context.Context, id string, expectedVersion uint64, enabled bool) (Definition, error) {
	if expectedVersion == 0 {
		return Definition{}, errors.New("scheduler expected version is required")
	}
	definition, err := store.Get(ctx, id)
	if err != nil {
		return Definition{}, err
	}
	if definition.Version != expectedVersion {
		return Definition{}, ErrDefinitionChanged
	}
	if definition.Enabled == enabled {
		return definition, nil
	}
	nextRunAt := definition.NextRunAt
	if enabled {
		var available bool
		nextRunAt, available, err = definition.NextAfter(store.now().UTC().Add(-time.Nanosecond))
		if err != nil {
			return Definition{}, err
		}
		if !available {
			return Definition{}, errors.New("scheduler definition has no future occurrence")
		}
	}
	result, err := store.database.ExecContext(normalizeContext(ctx),
		"UPDATE `AEGIS_SCHEDULE_DEFINITION` SET `ENABLED`=?,`NEXT_RUN_AT`=?,`VERSION`=`VERSION`+1 WHERE `SCHEDULE_ID`=? AND `VERSION`=?",
		enabled, nullableTime(nextRunAt), definition.ID, expectedVersion,
	)
	if err != nil {
		return Definition{}, fmt.Errorf("set scheduler definition %q enabled=%t: %w", definition.ID, enabled, err)
	}
	if err := requireOneRow(result, "scheduler definition changed"); err != nil {
		return Definition{}, ErrDefinitionChanged
	}
	definition.Enabled = enabled
	definition.NextRunAt = nextRunAt
	definition.Version++
	return cloneDefinition(definition), nil
}

type RunRecord struct {
	ID              uint64          `json:"id"`
	ScheduleID      string          `json:"scheduleId"`
	ScheduleVersion uint64          `json:"scheduleVersion"`
	ScheduledFor    time.Time       `json:"scheduledFor"`
	Handler         string          `json:"handler"`
	Payload         json.RawMessage `json:"payload"`
	Phase           Phase           `json:"phase"`
	Scope           Scope           `json:"scope"`
	ShardID         uint16          `json:"shardId,omitempty"`
	Status          string          `json:"status"`
	AttemptCount    uint32          `json:"attemptCount"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	FinishedAt      *time.Time      `json:"finishedAt,omitempty"`
	LastError       string          `json:"lastError,omitempty"`
}

func (store *SQLStore) ListRuns(ctx context.Context, scheduleID string, limit int) ([]RunRecord, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("scheduler store is not configured")
	}
	scheduleID = strings.TrimSpace(scheduleID)
	if scheduleID == "" || len(scheduleID) > 128 || limit < 1 || limit > 500 {
		return nil, errors.New("scheduler run-history parameters are invalid")
	}
	rows, err := store.database.QueryContext(normalizeContext(ctx),
		"SELECT `RUN_ID`,`FK_SCHEDULE`,`SCHEDULE_VERSION`,`SCHEDULED_FOR`,`HANDLER`,`PAYLOAD`,`PHASE`,`SCOPE`,COALESCE(`SHARD_ID`,0),`STATUS`,`ATTEMPT_COUNT`,`STARTED_AT`,`FINISHED_AT`,COALESCE(`LAST_ERROR`,'') FROM `AEGIS_SCHEDULE_RUN` WHERE `FK_SCHEDULE`=? ORDER BY `SCHEDULED_FOR` DESC,`RUN_ID` DESC LIMIT ?",
		scheduleID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list scheduler runs for %q: %w", scheduleID, err)
	}
	defer rows.Close()
	records := make([]RunRecord, 0)
	for rows.Next() {
		var record RunRecord
		var payload []byte
		var attempt uint64
		if err := rows.Scan(&record.ID, &record.ScheduleID, &record.ScheduleVersion, &record.ScheduledFor, &record.Handler, &payload, &record.Phase, &record.Scope, &record.ShardID, &record.Status, &attempt, &record.StartedAt, &record.FinishedAt, &record.LastError); err != nil {
			return nil, fmt.Errorf("scan scheduler run history: %w", err)
		}
		if attempt > uint64(^uint32(0)) || !json.Valid(payload) {
			return nil, fmt.Errorf("scheduler run %d contains invalid durable state", record.ID)
		}
		record.AttemptCount = uint32(attempt)
		record.Payload = append(json.RawMessage(nil), payload...)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scheduler run history: %w", err)
	}
	return records, nil
}

type MaterializeResult struct {
	Pending   int
	Skipped   int
	Exhausted int
}

func (store *SQLStore) MaterializeDue(ctx context.Context, now time.Time, limit int) (result MaterializeResult, resultErr error) {
	if store == nil || store.database == nil {
		return result, errors.New("scheduler store is not configured")
	}
	if now.IsZero() || limit < 1 || limit > 1000 {
		return result, errors.New("scheduler materialization requires time and limit between 1 and 1000")
	}
	ctx = normalizeContext(ctx)
	now = now.UTC()
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, fmt.Errorf("begin scheduler materialization: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, definitionSelect+" WHERE `ENABLED`=TRUE AND `NEXT_RUN_AT`<=? ORDER BY `NEXT_RUN_AT`,`SCHEDULE_ID` LIMIT ? FOR UPDATE SKIP LOCKED", now, limit)
	if err != nil {
		return result, fmt.Errorf("select due scheduler definitions: %w", err)
	}
	definitions := make([]Definition, 0)
	for rows.Next() {
		definition, err := scanDefinition(rows)
		if err != nil {
			_ = rows.Close()
			return result, err
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, fmt.Errorf("iterate due scheduler definitions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("close due scheduler definitions: %w", err)
	}
	for _, definition := range definitions {
		due := definition.NextRunAt.UTC()
		status := "pending"
		var finishedAt any
		if definition.MisfirePolicy == SkipMissed && due.Before(now) {
			status = "skipped"
			finishedAt = now
			result.Skipped++
		} else {
			result.Pending++
		}
		_, err := tx.ExecContext(ctx,
			"INSERT INTO `AEGIS_SCHEDULE_RUN` (`FK_SCHEDULE`,`SCHEDULE_VERSION`,`SCHEDULED_FOR`,`HANDLER`,`PAYLOAD`,`PHASE`,`SCOPE`,`SHARD_ID`,`STATUS`,`NEXT_ATTEMPT_AT`,`FINISHED_AT`) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
			definition.ID, definition.Version, due, definition.Handler, []byte(definition.Payload), definition.Phase, definition.Scope, nullableShard(definition), status, now, finishedAt,
		)
		if err != nil {
			return result, fmt.Errorf("materialize scheduler run %q at %s: %w", definition.ID, due.Format(time.RFC3339Nano), err)
		}
		next, available, err := definition.NextAfter(now)
		if err != nil {
			return result, fmt.Errorf("advance scheduler definition %q: %w", definition.ID, err)
		}
		enabled := definition.Enabled && available
		if !available {
			result.Exhausted++
		}
		update, err := tx.ExecContext(ctx,
			"UPDATE `AEGIS_SCHEDULE_DEFINITION` SET `NEXT_RUN_AT`=?,`ENABLED`=? WHERE `SCHEDULE_ID`=? AND `VERSION`=?",
			nullableTime(next), enabled, definition.ID, definition.Version,
		)
		if err != nil {
			return result, fmt.Errorf("persist scheduler advance %q: %w", definition.ID, err)
		}
		if err := requireOneRow(update, "scheduler definition changed during materialization"); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit scheduler materialization: %w", err)
	}
	return result, nil
}

type Run struct {
	ID              uint64
	ScheduleID      string
	ScheduleVersion uint64
	ScheduledFor    time.Time
	Handler         string
	Payload         json.RawMessage
	Phase           Phase
	Scope           Scope
	ShardID         uint16
	Attempt         uint32
	LeaseOwner      string
	LeaseUntil      time.Time
}

func (store *SQLStore) ClaimRuns(ctx context.Context, worker string, now time.Time, limit int, leaseDuration time.Duration) (runs []Run, resultErr error) {
	if store == nil || store.database == nil {
		return nil, errors.New("scheduler store is not configured")
	}
	if err := validateWorker(worker); err != nil {
		return nil, err
	}
	if now.IsZero() || limit < 1 || limit > 1000 || leaseDuration <= 0 || leaseDuration > 24*time.Hour {
		return nil, errors.New("scheduler claim parameters are invalid")
	}
	ctx = normalizeContext(ctx)
	now = now.UTC()
	leaseUntil := now.Add(leaseDuration)
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin scheduler run claim: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx,
		"SELECT `RUN_ID`,`FK_SCHEDULE`,`SCHEDULE_VERSION`,`SCHEDULED_FOR`,`HANDLER`,`PAYLOAD`,`PHASE`,`SCOPE`,COALESCE(`SHARD_ID`,0),`ATTEMPT_COUNT` FROM `AEGIS_SCHEDULE_RUN` WHERE `STATUS`='pending' AND `NEXT_ATTEMPT_AT`<=? ORDER BY `SCHEDULED_FOR`,`RUN_ID` LIMIT ? FOR UPDATE SKIP LOCKED",
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select due scheduler runs: %w", err)
	}
	for rows.Next() {
		var run Run
		var payload []byte
		var attempt uint64
		if err := rows.Scan(&run.ID, &run.ScheduleID, &run.ScheduleVersion, &run.ScheduledFor, &run.Handler, &payload, &run.Phase, &run.Scope, &run.ShardID, &attempt); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan due scheduler run: %w", err)
		}
		if attempt >= uint64(^uint32(0)) || !json.Valid(payload) {
			_ = rows.Close()
			return nil, fmt.Errorf("scheduler run %d contains invalid durable state", run.ID)
		}
		run.Payload = append(json.RawMessage(nil), payload...)
		run.Attempt = uint32(attempt) + 1
		run.LeaseOwner = worker
		run.LeaseUntil = leaseUntil
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate scheduler runs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close scheduler runs: %w", err)
	}
	for _, run := range runs {
		update, err := tx.ExecContext(ctx,
			"UPDATE `AEGIS_SCHEDULE_RUN` SET `STATUS`='processing',`ATTEMPT_COUNT`=`ATTEMPT_COUNT`+1,`LEASE_OWNER`=?,`LEASE_UNTIL`=?,`STARTED_AT`=COALESCE(`STARTED_AT`,?),`LAST_ERROR`=NULL WHERE `RUN_ID`=? AND `STATUS`='pending'",
			worker, leaseUntil, now, run.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("lease scheduler run %d: %w", run.ID, err)
		}
		if err := requireOneRow(update, "scheduler run changed during claim"); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit scheduler run claim: %w", err)
	}
	return runs, nil
}

func (store *SQLStore) MarkSucceeded(ctx context.Context, run Run, finishedAt time.Time) error {
	if store == nil || store.database == nil {
		return errors.New("scheduler store is not configured")
	}
	if err := validateRunLease(run); err != nil {
		return err
	}
	if finishedAt.IsZero() {
		return errors.New("scheduler run completion time is required")
	}
	finishedAt = finishedAt.UTC()
	result, err := store.database.ExecContext(normalizeContext(ctx),
		"UPDATE `AEGIS_SCHEDULE_RUN` SET `STATUS`='succeeded',`FINISHED_AT`=?,`LEASE_OWNER`=NULL,`LEASE_UNTIL`=NULL,`LAST_ERROR`=NULL WHERE `RUN_ID`=? AND `STATUS`='processing' AND `LEASE_OWNER`=? AND `LEASE_UNTIL`>=?",
		finishedAt, run.ID, run.LeaseOwner, finishedAt,
	)
	if err != nil {
		return fmt.Errorf("complete scheduler run %d: %w", run.ID, err)
	}
	if err := requireOneRow(result, "scheduler run lease lost"); err != nil {
		return fmt.Errorf("%w: run %d", ErrRunLeaseLost, run.ID)
	}
	return nil
}

func (store *SQLStore) MarkFailed(ctx context.Context, run Run, runErr error, policy RetryPolicy, failedAt time.Time) (terminal bool, resultErr error) {
	if store == nil || store.database == nil {
		return false, errors.New("scheduler store is not configured")
	}
	if err := validateRunLease(run); err != nil {
		return false, err
	}
	if runErr == nil || failedAt.IsZero() {
		return false, errors.New("scheduler run failure and time are required")
	}
	if err := policy.Validate(); err != nil {
		return false, err
	}
	failedAt = failedAt.UTC()
	status, delay := policy.Decide(run.Attempt, runErr)
	finishedAt := any(nil)
	if status != "pending" {
		terminal = true
		finishedAt = failedAt
	}
	result, err := store.database.ExecContext(normalizeContext(ctx),
		"UPDATE `AEGIS_SCHEDULE_RUN` SET `STATUS`=?,`NEXT_ATTEMPT_AT`=?,`LAST_ERROR`=?,`FINISHED_AT`=?,`LEASE_OWNER`=NULL,`LEASE_UNTIL`=NULL WHERE `RUN_ID`=? AND `STATUS`='processing' AND `LEASE_OWNER`=? AND `LEASE_UNTIL`>=?",
		status, failedAt.Add(delay), textutil.TruncateUTF8(runErr.Error(), 1024), finishedAt, run.ID, run.LeaseOwner, failedAt,
	)
	if err != nil {
		return false, fmt.Errorf("record scheduler run %d failure: %w", run.ID, err)
	}
	if err := requireOneRow(result, "scheduler run lease lost"); err != nil {
		return false, fmt.Errorf("%w: run %d", ErrRunLeaseLost, run.ID)
	}
	return terminal, nil
}

func (store *SQLStore) RecoverExpired(ctx context.Context, now time.Time) (int64, error) {
	if store == nil || store.database == nil {
		return 0, errors.New("scheduler store is not configured")
	}
	if now.IsZero() {
		return 0, errors.New("scheduler recovery time is required")
	}
	now = now.UTC()
	result, err := store.database.ExecContext(normalizeContext(ctx),
		"UPDATE `AEGIS_SCHEDULE_RUN` SET `STATUS`='pending',`NEXT_ATTEMPT_AT`=?,`LAST_ERROR`='execution lease expired',`LEASE_OWNER`=NULL,`LEASE_UNTIL`=NULL WHERE `STATUS`='processing' AND `LEASE_UNTIL`<?",
		now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("recover expired scheduler runs: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect recovered scheduler runs: %w", err)
	}
	return affected, nil
}

type RetryPolicy struct {
	MaxAttempts uint32
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (policy RetryPolicy) Validate() error {
	if policy.MaxAttempts == 0 || policy.BaseDelay <= 0 || policy.MaxDelay < policy.BaseDelay {
		return errors.New("scheduler retry policy is invalid")
	}
	return nil
}

// Decide returns pending for retry, failed for a permanent error, or dead when
// the attempt budget is exhausted.
func (policy RetryPolicy) Decide(attempt uint32, err error) (string, time.Duration) {
	var permanent interface{ Permanent() bool }
	if err != nil && errors.As(err, &permanent) && permanent.Permanent() {
		return "failed", 0
	}
	if attempt >= policy.MaxAttempts || err == nil || policy.Validate() != nil {
		return "dead", 0
	}
	delay := policy.BaseDelay
	for current := uint32(1); current < attempt && delay < policy.MaxDelay; current++ {
		if delay > policy.MaxDelay/2 {
			delay = policy.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}
	return "pending", delay
}

const definitionSelect = "SELECT `SCHEDULE_ID`,`NAME`,`HANDLER`,`PAYLOAD`,`RECURRENCE`,`PHASE`,`SCOPE`,COALESCE(`SHARD_ID`,0),`TIMEZONE`,`ACTIVE_FROM`,`ACTIVE_UNTIL`,`RUN_AT`,COALESCE(`SECOND_OF_DAY`,0),COALESCE(`INTERVAL_VALUE`,0),COALESCE(`WEEKDAY_MASK`,0),`MISFIRE_POLICY`,`ENABLED`,`VERSION`,`NEXT_RUN_AT` FROM `AEGIS_SCHEDULE_DEFINITION`"

type rowScanner interface{ Scan(...any) error }

func scanDefinition(scanner rowScanner) (Definition, error) {
	var definition Definition
	var payload []byte
	var activeFrom, activeUntil, runAt, nextRunAt sql.NullTime
	if err := scanner.Scan(
		&definition.ID, &definition.Name, &definition.Handler, &payload, &definition.Recurrence, &definition.Phase, &definition.Scope, &definition.ShardID, &definition.Timezone,
		&activeFrom, &activeUntil, &runAt, &definition.SecondOfDay, &definition.Interval, &definition.WeekdayMask, &definition.MisfirePolicy, &definition.Enabled, &definition.Version, &nextRunAt,
	); err != nil {
		return Definition{}, fmt.Errorf("scan scheduler definition: %w", err)
	}
	definition.Payload = append(json.RawMessage(nil), payload...)
	if activeFrom.Valid {
		value := dateFromTime(activeFrom.Time)
		definition.ActiveFrom = &value
	}
	if activeUntil.Valid {
		value := dateFromTime(activeUntil.Time)
		definition.ActiveUntil = &value
	}
	if runAt.Valid {
		definition.RunAt = runAt.Time.UTC()
	}
	if nextRunAt.Valid {
		definition.NextRunAt = nextRunAt.Time.UTC()
	}
	if err := definition.Validate(); err != nil {
		return Definition{}, fmt.Errorf("invalid stored scheduler definition %q: %w", definition.ID, err)
	}
	return definition, nil
}

func initialRun(definition Definition) (time.Time, bool, error) {
	if definition.Recurrence == OneTime {
		return definition.RunAt.UTC(), true, nil
	}
	location, err := time.LoadLocation(definition.Timezone)
	if err != nil {
		return time.Time{}, false, err
	}
	start := time.Date(definition.ActiveFrom.Year, definition.ActiveFrom.Month, definition.ActiveFrom.Day, 0, 0, 0, 0, location).UTC().Add(-time.Nanosecond)
	return definition.NextAfter(start)
}

func cloneDefinition(definition Definition) Definition {
	definition.Payload = append(json.RawMessage(nil), definition.Payload...)
	if definition.ActiveFrom != nil {
		value := *definition.ActiveFrom
		definition.ActiveFrom = &value
	}
	if definition.ActiveUntil != nil {
		value := *definition.ActiveUntil
		definition.ActiveUntil = &value
	}
	return definition
}

func nullableShard(definition Definition) any {
	if definition.Scope == AllShards {
		return nil
	}
	return definition.ShardID
}

func nullableDate(date *Date) any {
	if date == nil {
		return nil
	}
	return time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC)
}

func nullableRunAt(definition Definition) any {
	if definition.RunAt.IsZero() {
		return nil
	}
	return definition.RunAt.UTC()
}

func nullableSecond(definition Definition) any {
	if definition.Recurrence == OneTime {
		return nil
	}
	return definition.SecondOfDay
}

func nullableInterval(definition Definition) any {
	if definition.Recurrence == OneTime {
		return nil
	}
	return definition.Interval
}

func nullableWeekdays(definition Definition) any {
	if definition.Recurrence != Weekly {
		return nil
	}
	return definition.WeekdayMask
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func validateRunLease(run Run) error {
	if run.ID == 0 || run.Attempt == 0 || run.LeaseUntil.IsZero() || strings.TrimSpace(run.LeaseOwner) == "" || len(run.LeaseOwner) > 128 {
		return errors.New("scheduler run lease is invalid")
	}
	return nil
}

func validateWorker(worker string) error {
	if worker == "" || len(worker) > 128 || strings.TrimSpace(worker) != worker {
		return errors.New("scheduler worker must be 1-128 trimmed characters")
	}
	return nil
}

func requireOneRow(result sql.Result, reason string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect %s: %w", reason, err)
	}
	if affected != 1 {
		return errors.New(reason)
	}
	return nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
