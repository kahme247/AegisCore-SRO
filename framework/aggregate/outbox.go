package aggregate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrOutboxLeaseLost = errors.New("outbox lease is no longer owned")

type LeasedMessage struct {
	OutboxMessage
	AggregateVersion Version
	Attempt          uint32
	LeaseOwner       string
	LeaseUntil       time.Time
}

type OutboxStore interface {
	Claim(context.Context, string, int, time.Duration) ([]LeasedMessage, error)
	MarkPublished(context.Context, LeasedMessage, time.Time) error
	MarkFailed(context.Context, LeasedMessage, error, FailurePolicy, time.Time) (bool, error)
	RecoverExpired(context.Context, time.Time) (int64, error)
}

type outboxDatabase interface {
	transactionBeginner
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type SQLOutboxStore struct {
	database outboxDatabase
	now      func() time.Time
}

func NewSQLOutboxStore(database outboxDatabase) (*SQLOutboxStore, error) {
	if database == nil {
		return nil, errors.New("outbox database is required")
	}
	return &SQLOutboxStore{database: database, now: time.Now}, nil
}

func (store *SQLOutboxStore) Claim(ctx context.Context, worker string, limit int, leaseDuration time.Duration) (messages []LeasedMessage, resultErr error) {
	if store == nil || store.database == nil {
		return nil, errors.New("outbox store is not configured")
	}
	if err := validateWorker(worker); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, errors.New("outbox claim limit must be between 1 and 1000")
	}
	if leaseDuration <= 0 || leaseDuration > 24*time.Hour {
		return nil, errors.New("outbox lease duration must be between 1ns and 24h")
	}
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := store.now().UTC()
	leaseUntil := now.Add(leaseDuration)
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx,
		"SELECT `MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`ATTEMPT_COUNT`,`OCCURRED_AT` FROM `AEGIS_OUTBOX_MESSAGE` WHERE `STATUS`='pending' AND `NEXT_ATTEMPT_AT`<=? ORDER BY `OCCURRED_AT`,`MESSAGE_ID` LIMIT ? FOR UPDATE SKIP LOCKED",
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select due outbox messages: %w", err)
	}
	for rows.Next() {
		var (
			message LeasedMessage
			kind    string
			version uint64
			attempt uint64
			payload []byte
		)
		if err := rows.Scan(&message.ID, &kind, &message.Aggregate.Value, &version, &message.Topic, &payload, &attempt, &message.OccurredAt); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan due outbox message: %w", err)
		}
		message.Aggregate.Kind = Kind(kind)
		message.AggregateVersion = Version(version)
		if attempt >= uint64(^uint32(0)) {
			_ = rows.Close()
			return nil, fmt.Errorf("outbox message %q attempt count overflow", message.ID)
		}
		message.Attempt = uint32(attempt) + 1
		message.Payload = append(json.RawMessage(nil), payload...)
		message.LeaseOwner = worker
		message.LeaseUntil = leaseUntil
		if err := validateLeasedMessage(message); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("invalid stored outbox message %q: %w", message.ID, err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate due outbox messages: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close due outbox rows: %w", err)
	}

	for _, message := range messages {
		result, err := tx.ExecContext(ctx,
			"UPDATE `AEGIS_OUTBOX_MESSAGE` SET `STATUS`='processing',`ATTEMPT_COUNT`=`ATTEMPT_COUNT`+1,`LEASE_OWNER`=?,`LEASE_UNTIL`=?,`LAST_ERROR`=NULL WHERE `MESSAGE_ID`=? AND `STATUS`='pending'",
			worker, leaseUntil, message.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("lease outbox message %q: %w", message.ID, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("inspect outbox lease %q: %w", message.ID, err)
		}
		if affected != 1 {
			return nil, fmt.Errorf("%w: message %q changed during claim", ErrOutboxLeaseLost, message.ID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit outbox claim: %w", err)
	}
	return messages, nil
}

func (store *SQLOutboxStore) MarkPublished(ctx context.Context, message LeasedMessage, publishedAt time.Time) error {
	if store == nil || store.database == nil {
		return errors.New("outbox store is not configured")
	}
	if err := validateLeasedMessage(message); err != nil {
		return err
	}
	if publishedAt.IsZero() {
		return errors.New("outbox publication time is required")
	}
	ctx = normalizeContext(ctx)
	publishedAt = publishedAt.UTC()
	result, err := store.database.ExecContext(ctx,
		"UPDATE `AEGIS_OUTBOX_MESSAGE` SET `STATUS`='published',`PUBLISHED_AT`=?,`LEASE_OWNER`=NULL,`LEASE_UNTIL`=NULL,`LAST_ERROR`=NULL WHERE `MESSAGE_ID`=? AND `STATUS`='processing' AND `LEASE_OWNER`=? AND `LEASE_UNTIL`>=?",
		publishedAt, message.ID, message.LeaseOwner, publishedAt,
	)
	if err != nil {
		return fmt.Errorf("publish outbox message %q: %w", message.ID, err)
	}
	return requireOneLeaseRow(result, message.ID)
}

// MarkFailed returns true when the message is dead-lettered and false when it
// is scheduled for another attempt.
func (store *SQLOutboxStore) MarkFailed(ctx context.Context, message LeasedMessage, publishErr error, policy FailurePolicy, failedAt time.Time) (bool, error) {
	if store == nil || store.database == nil {
		return false, errors.New("outbox store is not configured")
	}
	if err := validateLeasedMessage(message); err != nil {
		return false, err
	}
	if publishErr == nil {
		return false, errors.New("outbox publication failure is required")
	}
	if err := policy.Validate(); err != nil {
		return false, err
	}
	if failedAt.IsZero() {
		return false, errors.New("outbox failure time is required")
	}
	ctx = normalizeContext(ctx)
	failedAt = failedAt.UTC()
	dead, delay := policy.Decide(message.Attempt, publishErr)
	status := "pending"
	nextAttempt := failedAt.Add(delay)
	if dead {
		status = "dead"
		nextAttempt = failedAt
	}
	result, err := store.database.ExecContext(ctx,
		"UPDATE `AEGIS_OUTBOX_MESSAGE` SET `STATUS`=?,`NEXT_ATTEMPT_AT`=?,`LAST_ERROR`=?,`LEASE_OWNER`=NULL,`LEASE_UNTIL`=NULL WHERE `MESSAGE_ID`=? AND `STATUS`='processing' AND `LEASE_OWNER`=? AND `LEASE_UNTIL`>=?",
		status, nextAttempt, truncateUTF8(publishErr.Error(), 1024), message.ID, message.LeaseOwner, failedAt,
	)
	if err != nil {
		return false, fmt.Errorf("record outbox failure %q: %w", message.ID, err)
	}
	if err := requireOneLeaseRow(result, message.ID); err != nil {
		return false, err
	}
	return dead, nil
}

func (store *SQLOutboxStore) RecoverExpired(ctx context.Context, now time.Time) (int64, error) {
	if store == nil || store.database == nil {
		return 0, errors.New("outbox store is not configured")
	}
	if now.IsZero() {
		return 0, errors.New("outbox recovery time is required")
	}
	ctx = normalizeContext(ctx)
	now = now.UTC()
	result, err := store.database.ExecContext(ctx,
		"UPDATE `AEGIS_OUTBOX_MESSAGE` SET `STATUS`='pending',`NEXT_ATTEMPT_AT`=?,`LAST_ERROR`='dispatch lease expired',`LEASE_OWNER`=NULL,`LEASE_UNTIL`=NULL WHERE `STATUS`='processing' AND `LEASE_UNTIL`<?",
		now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("recover expired outbox leases: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect recovered outbox leases: %w", err)
	}
	return affected, nil
}

type FailurePolicy struct {
	MaxAttempts uint32
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (policy FailurePolicy) Validate() error {
	if policy.MaxAttempts == 0 {
		return errors.New("outbox maximum attempts must be positive")
	}
	if policy.BaseDelay <= 0 || policy.MaxDelay < policy.BaseDelay {
		return errors.New("outbox retry delays are invalid")
	}
	return nil
}

func (policy FailurePolicy) Decide(attempt uint32, err error) (dead bool, delay time.Duration) {
	if err == nil || policy.Validate() != nil {
		return true, 0
	}
	var permanent interface{ Permanent() bool }
	if errors.As(err, &permanent) && permanent.Permanent() {
		return true, 0
	}
	if attempt >= policy.MaxAttempts {
		return true, 0
	}
	delay = policy.BaseDelay
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
	return false, delay
}

type OutboxPublisher interface {
	Publish(context.Context, LeasedMessage) error
}

type BatchResult struct {
	Claimed   int
	Published int
	Retried   int
	Dead      int
}

type OutboxProcessor struct {
	Store         OutboxStore
	Publisher     OutboxPublisher
	Worker        string
	BatchSize     int
	LeaseDuration time.Duration
	FailurePolicy FailurePolicy
	Now           func() time.Time
}

func (processor OutboxProcessor) RunBatch(ctx context.Context) (BatchResult, error) {
	if processor.Store == nil || processor.Publisher == nil {
		return BatchResult{}, errors.New("outbox processor store and publisher are required")
	}
	if err := validateWorker(processor.Worker); err != nil {
		return BatchResult{}, err
	}
	if err := processor.FailurePolicy.Validate(); err != nil {
		return BatchResult{}, err
	}
	if processor.Now == nil {
		processor.Now = time.Now
	}
	ctx = normalizeContext(ctx)
	messages, err := processor.Store.Claim(ctx, processor.Worker, processor.BatchSize, processor.LeaseDuration)
	if err != nil {
		return BatchResult{}, err
	}
	result := BatchResult{Claimed: len(messages)}
	var processingErrors []error
	for _, message := range messages {
		if err := ctxErr(ctx); err != nil {
			processingErrors = append(processingErrors, err)
			break
		}
		publishErr := processor.Publisher.Publish(ctx, message)
		now := processor.Now().UTC()
		if publishErr == nil {
			if err := processor.Store.MarkPublished(ctx, message, now); err != nil {
				processingErrors = append(processingErrors, err)
				continue
			}
			result.Published++
			continue
		}
		dead, err := processor.Store.MarkFailed(ctx, message, publishErr, processor.FailurePolicy, now)
		if err != nil {
			processingErrors = append(processingErrors, err)
			continue
		}
		if dead {
			result.Dead++
		} else {
			result.Retried++
		}
	}
	return result, errors.Join(processingErrors...)
}

func validateLeasedMessage(message LeasedMessage) error {
	if err := message.OutboxMessage.Validate(); err != nil {
		return err
	}
	if message.Attempt == 0 || message.LeaseUntil.IsZero() {
		return errors.New("outbox lease attempt and expiry are required")
	}
	return validateWorker(message.LeaseOwner)
}

func validateWorker(worker string) error {
	if worker == "" || len(worker) > 128 || strings.TrimSpace(worker) != worker {
		return errors.New("outbox worker must be 1-128 trimmed characters")
	}
	return nil
}

func requireOneLeaseRow(result sql.Result, messageID string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect outbox lease %q: %w", messageID, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: message %q", ErrOutboxLeaseLost, messageID)
	}
	return nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
