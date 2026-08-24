package aggregate

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSQLOutboxStoreClaimsDueMessagesWithLease(t *testing.T) {
	now := time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC)
	occurred := now.Add(-time.Minute)
	storeState, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		queryStep("FOR UPDATE SKIP LOCKED", []driver.Value{"message-1", "character", "42", uint64(7), "character.saved", []byte(`{"ok":true}`), uint64(2), occurred}),
		execStep("SET `STATUS`='processing'", 1),
	})
	store, err := NewSQLOutboxStore(database)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	messages, err := store.Claim(context.Background(), "worker-1", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	message := messages[0]
	if message.Attempt != 3 || message.AggregateVersion != 7 || message.LeaseOwner != "worker-1" || !message.LeaseUntil.Equal(now.Add(30*time.Second)) {
		t.Fatalf("leased message = %+v", message)
	}
	storeState.assertFinished(t, 1, 0)
}

func TestSQLOutboxStoreRollsBackInvalidStoredPayload(t *testing.T) {
	now := time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC)
	storeState, database := newCoordinatorTestDatabase(t, []coordinatorStep{
		queryStep("FOR UPDATE SKIP LOCKED", []driver.Value{"message-1", "character", "42", uint64(7), "character.saved", []byte(`{"broken":`), uint64(0), now}),
	})
	store, _ := NewSQLOutboxStore(database)
	store.now = func() time.Time { return now }
	if _, err := store.Claim(context.Background(), "worker-1", 1, time.Minute); err == nil {
		t.Fatal("Claim accepted invalid stored JSON")
	}
	storeState.assertFinished(t, 0, 1)
}

func TestSQLOutboxStoreRejectsStalePublicationLease(t *testing.T) {
	storeState, database := newCoordinatorTestDatabase(t, []coordinatorStep{execStep("SET `STATUS`='published'", 0)})
	store, _ := NewSQLOutboxStore(database)
	message := testLeasedMessage("message-1", 1)
	if err := store.MarkPublished(context.Background(), message, message.LeaseUntil.Add(-time.Second)); !errors.Is(err, ErrOutboxLeaseLost) {
		t.Fatalf("MarkPublished() error = %v, want ErrOutboxLeaseLost", err)
	}
	storeState.assertFinished(t, 0, 0)
}

func TestSQLOutboxStoreRecoversExpiredLeases(t *testing.T) {
	storeState, database := newCoordinatorTestDatabase(t, []coordinatorStep{execStep("dispatch lease expired", 3)})
	store, _ := NewSQLOutboxStore(database)
	recovered, err := store.RecoverExpired(context.Background(), time.Now())
	if err != nil || recovered != 3 {
		t.Fatalf("RecoverExpired() recovered=%d error=%v", recovered, err)
	}
	storeState.assertFinished(t, 0, 0)
}

func TestFailurePolicyUsesBoundedExponentialBackoffAndDeadLetters(t *testing.T) {
	policy := FailurePolicy{MaxAttempts: 4, BaseDelay: time.Second, MaxDelay: 2500 * time.Millisecond}
	tests := []struct {
		attempt uint32
		err     error
		dead    bool
		delay   time.Duration
	}{
		{1, errors.New("temporary"), false, time.Second},
		{2, errors.New("temporary"), false, 2 * time.Second},
		{3, errors.New("temporary"), false, 2500 * time.Millisecond},
		{4, errors.New("temporary"), true, 0},
		{1, permanentPublishError{}, true, 0},
	}
	for _, test := range tests {
		dead, delay := policy.Decide(test.attempt, test.err)
		if dead != test.dead || delay != test.delay {
			t.Fatalf("Decide(%d,%T) = dead %t delay %s, want %t %s", test.attempt, test.err, dead, delay, test.dead, test.delay)
		}
	}
}

func TestOutboxProcessorRecordsPublishedRetryAndDeadOutcomes(t *testing.T) {
	messages := []LeasedMessage{testLeasedMessage("publish", 1), testLeasedMessage("retry", 1), testLeasedMessage("dead", 1)}
	store := &processorTestStore{messages: messages}
	publisher := processorTestPublisher{errors: map[string]error{"retry": errors.New("network unavailable"), "dead": permanentPublishError{}}}
	processor := OutboxProcessor{
		Store: store, Publisher: publisher, Worker: "worker-1", BatchSize: 3, LeaseDuration: time.Minute,
		FailurePolicy: FailurePolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Minute},
		Now:           func() time.Time { return time.Date(2026, 7, 19, 1, 0, 0, 0, time.UTC) },
	}
	result, err := processor.RunBatch(context.Background())
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if result != (BatchResult{Claimed: 3, Published: 1, Retried: 1, Dead: 1}) {
		t.Fatalf("batch result = %+v", result)
	}
	if len(store.published) != 1 || len(store.failed) != 2 {
		t.Fatalf("published=%v failed=%v", store.published, store.failed)
	}
}

func testLeasedMessage(id string, attempt uint32) LeasedMessage {
	now := time.Now().UTC()
	return LeasedMessage{
		OutboxMessage: OutboxMessage{
			ID: id, Aggregate: ID{Kind: Character, Value: "42"}, Topic: "character.saved",
			Payload: []byte(`{"ok":true}`), OccurredAt: now.Add(-time.Minute),
		},
		AggregateVersion: 1, Attempt: attempt, LeaseOwner: "worker-1", LeaseUntil: now.Add(time.Minute),
	}
}

type permanentPublishError struct{}

func (permanentPublishError) Error() string   { return "permanent" }
func (permanentPublishError) Permanent() bool { return true }

type processorTestStore struct {
	messages  []LeasedMessage
	published []string
	failed    []string
}

func (store *processorTestStore) Claim(context.Context, string, int, time.Duration) ([]LeasedMessage, error) {
	return append([]LeasedMessage(nil), store.messages...), nil
}
func (store *processorTestStore) MarkPublished(_ context.Context, message LeasedMessage, _ time.Time) error {
	store.published = append(store.published, message.ID)
	return nil
}
func (store *processorTestStore) MarkFailed(_ context.Context, message LeasedMessage, publishErr error, policy FailurePolicy, _ time.Time) (bool, error) {
	store.failed = append(store.failed, fmt.Sprintf("%s:%s", message.ID, publishErr))
	dead, _ := policy.Decide(message.Attempt, publishErr)
	return dead, nil
}
func (*processorTestStore) RecoverExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type processorTestPublisher struct{ errors map[string]error }

func (publisher processorTestPublisher) Publish(_ context.Context, message LeasedMessage) error {
	return publisher.errors[message.ID]
}
