// Package aggregate defines the shared transaction boundary for authoritative
// Agent mutations. It is intentionally independent from gameplay packets and
// legacy SQL procedure names.
package aggregate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/go-sql-driver/mysql"
)

type Kind string

const (
	Character Kind = "character"
	Inventory Kind = "inventory"
	Storage   Kind = "storage"
	COS       Kind = "cos"
	Guild     Kind = "guild"
	Academy   Kind = "academy"
	Job       Kind = "job"
	Quest     Kind = "quest"
	Fortress  Kind = "fortress"
	Market    Kind = "market"
	Event     Kind = "event"
)

var allKinds = []Kind{Character, Inventory, Storage, COS, Guild, Academy, Job, Quest, Fortress, Market, Event}

type Owner string

const AgentOwner Owner = "agent"

type ID struct {
	Kind  Kind
	Value string
}

func (id ID) Validate() error {
	if !validKind(id.Kind) {
		return fmt.Errorf("invalid aggregate kind %q", id.Kind)
	}
	value := strings.TrimSpace(id.Value)
	if value == "" || len(value) > 128 || value != id.Value {
		return errors.New("aggregate ID must be 1-128 trimmed characters")
	}
	return nil
}

type Version uint64

type IdempotencyKey string

func (key IdempotencyKey) Validate() error {
	value := string(key)
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return errors.New("idempotency key must be 1-128 trimmed characters")
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("-_.:", character)) {
			return errors.New("idempotency key contains an unsupported character")
		}
	}
	return nil
}

type Command struct {
	Aggregate       ID
	ExpectedVersion Version
	IdempotencyKey  IdempotencyKey
	Name            string
}

func (command Command) Validate() error {
	if err := command.Aggregate.Validate(); err != nil {
		return err
	}
	if err := command.IdempotencyKey.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(command.Name) == "" || len(command.Name) > 128 {
		return errors.New("command name must be 1-128 characters")
	}
	return nil
}

type OutboxMessage struct {
	ID         string
	Aggregate  ID
	Topic      string
	Payload    json.RawMessage
	OccurredAt time.Time
}

func (message OutboxMessage) Validate() error {
	if strings.TrimSpace(message.ID) == "" || len(message.ID) > 128 {
		return errors.New("outbox message ID must be 1-128 characters")
	}
	if err := message.Aggregate.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(message.Topic) == "" || len(message.Topic) > 128 {
		return errors.New("outbox topic must be 1-128 characters")
	}
	if len(message.Payload) == 0 || !json.Valid(message.Payload) {
		return errors.New("outbox payload must be one valid JSON value")
	}
	if message.OccurredAt.IsZero() {
		return errors.New("outbox occurrence time is required")
	}
	return nil
}

// Transaction exposes the one database transaction and outbox collector owned
// by a Coordinator. Repositories must use DB and must not call BeginTx.
type Transaction interface {
	DB() frameworkdb.DBTX
	CurrentVersion() Version
	Enqueue(OutboxMessage) error
}

type Mutation func(context.Context, Transaction) (json.RawMessage, error)

type Result struct {
	Version   Version
	Payload   json.RawMessage
	Duplicate bool
}

type Coordinator interface {
	Execute(context.Context, Command, Mutation) (Result, error)
}

// LatestCoordinator is used for terminal state snapshots whose caller cannot
// safely retain an aggregate version across the lifetime of a network
// session. Implementations still lock and advance the version atomically;
// only the version lookup is moved inside the transaction.
type LatestCoordinator interface {
	ExecuteLatest(context.Context, Command, Mutation) (Result, error)
}

var (
	ErrStaleVersion          = errors.New("stale aggregate version")
	ErrIdempotencyConflict   = errors.New("idempotency key is bound to another command")
	ErrIdempotencyInProgress = errors.New("idempotent command is still in progress")
	ErrInvalidResult         = errors.New("aggregate mutation returned an invalid result")
	ErrRetryExhausted        = errors.New("aggregate retry budget exhausted")
)

type RetryClass string

const (
	RetryNone      RetryClass = "none"
	RetryConflict  RetryClass = "conflict"
	RetryTransient RetryClass = "transient"
)

type temporaryError interface {
	Temporary() bool
}

func ClassifyRetry(err error) RetryClass {
	if err == nil {
		return RetryNone
	}
	if errors.Is(err, ErrStaleVersion) || errors.Is(err, ErrIdempotencyConflict) {
		return RetryConflict
	}
	if errors.Is(err, ErrIdempotencyInProgress) {
		return RetryTransient
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && (mysqlError.Number == 1205 || mysqlError.Number == 1213) {
		return RetryTransient
	}
	var temporary temporaryError
	if errors.As(err, &temporary) && temporary.Temporary() {
		return RetryTransient
	}
	return RetryNone
}

// OwnershipCatalog defines a single authoritative writer for every aggregate
// family in the consolidated runtime.
type OwnershipCatalog map[Kind]Owner

func AgentOwnershipCatalog() OwnershipCatalog {
	catalog := make(OwnershipCatalog, len(allKinds))
	for _, kind := range allKinds {
		catalog[kind] = AgentOwner
	}
	return catalog
}

func (catalog OwnershipCatalog) Validate() error {
	for _, kind := range allKinds {
		owner, exists := catalog[kind]
		if !exists || strings.TrimSpace(string(owner)) == "" {
			return fmt.Errorf("aggregate kind %q has no transactional owner", kind)
		}
	}
	for kind := range catalog {
		if !validKind(kind) {
			return fmt.Errorf("ownership catalog contains unknown aggregate kind %q", kind)
		}
	}
	return nil
}

func validKind(kind Kind) bool {
	for _, candidate := range allKinds {
		if kind == candidate {
			return true
		}
	}
	return false
}
