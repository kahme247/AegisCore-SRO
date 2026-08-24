package aggregate

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestAgentOwnershipCatalogCoversEveryRequiredAggregate(t *testing.T) {
	catalog := AgentOwnershipCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := len(catalog), len(allKinds); got != want {
		t.Fatalf("owner count = %d, want %d", got, want)
	}
	delete(catalog, Fortress)
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate accepted a missing fortress owner")
	}
}

func TestCommandRequiresExactAggregateAndIdempotencyIdentity(t *testing.T) {
	valid := Command{Aggregate: ID{Kind: Character, Value: "42"}, ExpectedVersion: 7, IdempotencyKey: "session:1:packet:2", Name: "character.save"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
	tests := []Command{
		{Aggregate: ID{Kind: "other", Value: "42"}, IdempotencyKey: "key", Name: "command"},
		{Aggregate: ID{Kind: Character, Value: ""}, IdempotencyKey: "key", Name: "command"},
		{Aggregate: ID{Kind: Character, Value: "42"}, IdempotencyKey: "bad key", Name: "command"},
		{Aggregate: ID{Kind: Character, Value: "42"}, IdempotencyKey: "key"},
	}
	for index, command := range tests {
		if err := command.Validate(); err == nil {
			t.Fatalf("invalid command %d accepted", index)
		}
	}
}

func TestOutboxMessageRequiresValidJSONAndOccurrenceTime(t *testing.T) {
	message := OutboxMessage{ID: "event-1", Aggregate: ID{Kind: Guild, Value: "9"}, Topic: "guild.updated", Payload: []byte(`{"level":2}`), OccurredAt: time.Now()}
	if err := message.Validate(); err != nil {
		t.Fatalf("valid outbox message rejected: %v", err)
	}
	message.Payload = []byte(`{"level":`)
	if err := message.Validate(); err == nil {
		t.Fatal("invalid JSON payload accepted")
	}
}

func TestClassifyRetrySeparatesConflictsAndTemporaryFailures(t *testing.T) {
	if got := ClassifyRetry(fmt.Errorf("wrapped: %w", ErrStaleVersion)); got != RetryConflict {
		t.Fatalf("stale classification = %s", got)
	}
	if got := ClassifyRetry(testTemporaryError{}); got != RetryTransient {
		t.Fatalf("temporary classification = %s", got)
	}
	if got := ClassifyRetry(&mysql.MySQLError{Number: 1213, Message: "deadlock"}); got != RetryTransient {
		t.Fatalf("deadlock classification = %s", got)
	}
	if got := ClassifyRetry(errors.New("permanent")); got != RetryNone {
		t.Fatalf("permanent classification = %s", got)
	}
}

func TestIdentifierBoundsAreByteExact(t *testing.T) {
	if err := (IdempotencyKey(strings.Repeat("a", 128))).Validate(); err != nil {
		t.Fatalf("128-byte key rejected: %v", err)
	}
	if err := (IdempotencyKey(strings.Repeat("a", 129))).Validate(); err == nil {
		t.Fatal("129-byte key accepted")
	}
}

type testTemporaryError struct{}

func (testTemporaryError) Error() string   { return "temporary" }
func (testTemporaryError) Temporary() bool { return true }
