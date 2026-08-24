package aggregate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/go-sql-driver/mysql"
)

type transactionBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// SQLCoordinator atomically owns idempotency, optimistic aggregate versioning,
// repository writes, and outbox publication records.
type SQLCoordinator struct {
	database transactionBeginner
	now      func() time.Time
}

func NewSQLCoordinator(database transactionBeginner) (*SQLCoordinator, error) {
	if database == nil {
		return nil, errors.New("aggregate coordinator database is required")
	}
	return &SQLCoordinator{database: database, now: time.Now}, nil
}

func (coordinator *SQLCoordinator) Execute(ctx context.Context, command Command, mutation Mutation) (result Result, resultErr error) {
	return coordinator.execute(ctx, command, mutation, false)
}

// ExecuteLatest executes against the version locked by this transaction. It
// is intended for terminal snapshots, where the deterministic idempotency key
// is more important than making a disconnected caller carry a stale version.
func (coordinator *SQLCoordinator) ExecuteLatest(ctx context.Context, command Command, mutation Mutation) (result Result, resultErr error) {
	return coordinator.execute(ctx, command, mutation, true)
}

func (coordinator *SQLCoordinator) execute(ctx context.Context, command Command, mutation Mutation, useLatest bool) (result Result, resultErr error) {
	if coordinator == nil || coordinator.database == nil {
		return Result{}, errors.New("aggregate coordinator is not configured")
	}
	if err := command.Validate(); err != nil {
		return Result{}, err
	}
	if mutation == nil {
		return Result{}, errors.New("aggregate mutation is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	tx, err := coordinator.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Result{}, fmt.Errorf("begin aggregate transaction: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx,
		"INSERT IGNORE INTO `AEGIS_AGGREGATE_VERSION` (`AGGREGATE_KIND`,`AGGREGATE_ID`,`VERSION`) VALUES (?,?,0)",
		command.Aggregate.Kind, command.Aggregate.Value,
	); err != nil {
		return Result{}, fmt.Errorf("ensure aggregate version row: %w", err)
	}

	var current uint64
	if err := tx.QueryRowContext(ctx,
		"SELECT `VERSION` FROM `AEGIS_AGGREGATE_VERSION` WHERE `AGGREGATE_KIND`=? AND `AGGREGATE_ID`=? FOR UPDATE",
		command.Aggregate.Kind, command.Aggregate.Value,
	).Scan(&current); err != nil {
		return Result{}, fmt.Errorf("lock aggregate version: %w", err)
	}
	if useLatest {
		command.ExpectedVersion = Version(current)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY` (`IDEMPOTENCY_KEY`,`COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`EXPECTED_VERSION`,`STATUS`) VALUES (?,?,?,?,?,'in_progress')",
		command.IdempotencyKey, command.Name, command.Aggregate.Kind, command.Aggregate.Value, command.ExpectedVersion,
	)
	if err != nil {
		if !isDuplicateKey(err) {
			return Result{}, fmt.Errorf("claim aggregate idempotency key: %w", err)
		}
		duplicate, duplicateErr := loadDuplicateResult(ctx, tx, command, useLatest)
		if duplicateErr != nil {
			return Result{}, duplicateErr
		}
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			return Result{}, fmt.Errorf("finish duplicate aggregate command: %w", err)
		}
		return duplicate, nil
	}

	if current != uint64(command.ExpectedVersion) {
		return Result{}, fmt.Errorf("%w: current=%d expected=%d", ErrStaleVersion, current, command.ExpectedVersion)
	}
	if current == math.MaxUint64 {
		return Result{}, errors.New("aggregate version overflow")
	}

	transaction := &sqlTransaction{tx: tx, aggregate: command.Aggregate, current: Version(current)}
	payload, err := mutation(ctx, transaction)
	if err != nil {
		return Result{}, err
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return Result{}, ErrInvalidResult
	}
	next := current + 1
	update, err := tx.ExecContext(ctx,
		"UPDATE `AEGIS_AGGREGATE_VERSION` SET `VERSION`=? WHERE `AGGREGATE_KIND`=? AND `AGGREGATE_ID`=? AND `VERSION`=?",
		next, command.Aggregate.Kind, command.Aggregate.Value, current,
	)
	if err != nil {
		return Result{}, fmt.Errorf("advance aggregate version: %w", err)
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return Result{}, fmt.Errorf("inspect aggregate version advance: %w", err)
	}
	if affected != 1 {
		return Result{}, fmt.Errorf("%w: aggregate version changed during mutation", ErrStaleVersion)
	}

	for _, message := range transaction.messages {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO `AEGIS_OUTBOX_MESSAGE` (`MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`OCCURRED_AT`) VALUES (?,?,?,?,?,?,?)",
			message.ID, message.Aggregate.Kind, message.Aggregate.Value, next, message.Topic, []byte(message.Payload), message.OccurredAt.UTC(),
		); err != nil {
			return Result{}, fmt.Errorf("insert aggregate outbox message %q: %w", message.ID, err)
		}
	}

	completedAt := coordinator.now().UTC()
	completion, err := tx.ExecContext(ctx,
		"UPDATE `AEGIS_COMMAND_IDEMPOTENCY` SET `STATUS`='completed',`RESULT_VERSION`=?,`RESULT_PAYLOAD`=?,`COMPLETED_AT`=? WHERE `IDEMPOTENCY_KEY`=? AND `STATUS`='in_progress'",
		next, []byte(payload), completedAt, command.IdempotencyKey,
	)
	if err != nil {
		return Result{}, fmt.Errorf("complete aggregate idempotency record: %w", err)
	}
	affected, err = completion.RowsAffected()
	if err != nil {
		return Result{}, fmt.Errorf("inspect aggregate idempotency completion: %w", err)
	}
	if affected != 1 {
		return Result{}, ErrIdempotencyConflict
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit aggregate transaction: %w", err)
	}
	return Result{Version: Version(next), Payload: append(json.RawMessage(nil), payload...)}, nil
}

type sqlTransaction struct {
	tx        *sql.Tx
	aggregate ID
	current   Version
	messages  []OutboxMessage
}

func (transaction *sqlTransaction) DB() frameworkdb.DBTX    { return transaction.tx }
func (transaction *sqlTransaction) CurrentVersion() Version { return transaction.current }

func (transaction *sqlTransaction) Enqueue(message OutboxMessage) error {
	if transaction == nil || transaction.tx == nil {
		return errors.New("aggregate transaction is not configured")
	}
	if err := message.Validate(); err != nil {
		return err
	}
	if message.Aggregate != transaction.aggregate {
		return errors.New("outbox message aggregate does not match transaction owner")
	}
	for _, existing := range transaction.messages {
		if existing.ID == message.ID {
			return errors.New("duplicate outbox message ID in aggregate transaction")
		}
	}
	message.Payload = append(json.RawMessage(nil), message.Payload...)
	transaction.messages = append(transaction.messages, message)
	return nil
}

func loadDuplicateResult(ctx context.Context, tx *sql.Tx, command Command, ignoreExpectedVersion bool) (Result, error) {
	var (
		name, kind, aggregateID, status string
		expected, resultVersion         uint64
		payload                         []byte
	)
	err := tx.QueryRowContext(ctx,
		"SELECT `COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`EXPECTED_VERSION`,`STATUS`,COALESCE(`RESULT_VERSION`,0),`RESULT_PAYLOAD` FROM `AEGIS_COMMAND_IDEMPOTENCY` WHERE `IDEMPOTENCY_KEY`=? FOR UPDATE",
		command.IdempotencyKey,
	).Scan(&name, &kind, &aggregateID, &expected, &status, &resultVersion, &payload)
	if err != nil {
		return Result{}, fmt.Errorf("load duplicate aggregate command: %w", err)
	}
	if name != command.Name || kind != string(command.Aggregate.Kind) || aggregateID != command.Aggregate.Value || (!ignoreExpectedVersion && expected != uint64(command.ExpectedVersion)) {
		return Result{}, ErrIdempotencyConflict
	}
	if status != "completed" {
		return Result{}, ErrIdempotencyInProgress
	}
	if !json.Valid(payload) {
		return Result{}, fmt.Errorf("%w: stored duplicate result is invalid", ErrInvalidResult)
	}
	return Result{Version: Version(resultVersion), Payload: append(json.RawMessage(nil), payload...), Duplicate: true}, nil
}

func isDuplicateKey(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
