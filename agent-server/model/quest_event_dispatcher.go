package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrQuestEventDispatcherUnavailable = errors.New("quest event dispatcher is unavailable")
	ErrQuestEventAlreadyRegistered     = errors.New("quest event handler is already registered")
)

type QuestEventMutation func(context.Context, *sql.Tx, int, CharacterQuest) error

type questEventRegistration struct {
	mutation QuestEventMutation
	statuses map[QuestStatus]struct{}
}

type questEventKey struct {
	questID int
	event   QuestAuthoredEvent
}

// SQLQuestEventDispatcher locks, validates, mutates, and commits one authored
// quest event as a single transaction. It intentionally has no default/no-op
// handler: an unimplemented authored event must fail instead of receiving a
// legacy success acknowledgement.
type SQLQuestEventDispatcher struct {
	shard *sql.DB
	mu    sync.RWMutex
	items map[questEventKey]questEventRegistration
}

func NewQuestEventDispatcher(shard *sql.DB) (*SQLQuestEventDispatcher, error) {
	if shard == nil {
		return nil, ErrQuestEventDispatcherUnavailable
	}
	return &SQLQuestEventDispatcher{shard: shard, items: make(map[questEventKey]questEventRegistration)}, nil
}

func (d *SQLQuestEventDispatcher) Register(questID int, event QuestAuthoredEvent, mutation QuestEventMutation) error {
	return d.register(questID, event, nil, mutation)
}

// RegisterForStatuses overrides the generic event status policy for one
// authored quest handler. This keeps event 0x07 transient-only by default
// while admitting native handlers that explicitly validate other states.
func (d *SQLQuestEventDispatcher) RegisterForStatuses(questID int, event QuestAuthoredEvent, statuses []QuestStatus, mutation QuestEventMutation) error {
	if len(statuses) == 0 {
		return fmt.Errorf("invalid quest event status policy")
	}
	allowed := make(map[QuestStatus]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	return d.register(questID, event, allowed, mutation)
}

func (d *SQLQuestEventDispatcher) register(questID int, event QuestAuthoredEvent, statuses map[QuestStatus]struct{}, mutation QuestEventMutation) error {
	if d == nil || d.shard == nil {
		return ErrQuestEventDispatcherUnavailable
	}
	if questID <= 0 || !supportedQuestEvent(event) || mutation == nil {
		return fmt.Errorf("invalid quest event registration")
	}
	key := questEventKey{questID: questID, event: event}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.items[key]; exists {
		return fmt.Errorf("quest %d event %d: %w", questID, event, ErrQuestEventAlreadyRegistered)
	}
	d.items[key] = questEventRegistration{mutation: mutation, statuses: statuses}
	return nil
}

func (d *SQLQuestEventDispatcher) DispatchQuestEvent(ctx context.Context, characterID, questID int, event QuestAuthoredEvent) error {
	if d == nil || d.shard == nil {
		return ErrQuestEventDispatcherUnavailable
	}
	if characterID <= 0 || questID <= 0 || !supportedQuestEvent(event) {
		return ErrQuestStateConflict
	}
	key := questEventKey{questID: questID, event: event}
	d.mu.RLock()
	registration, exists := d.items[key]
	d.mu.RUnlock()
	if !exists || registration.mutation == nil {
		return ErrQuestEventNotFound
	}

	tx, err := d.shard.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrQuestEventNotFound
	}
	if err != nil {
		return err
	}
	if !registration.accepts(event, quest.Status) {
		return ErrQuestStateConflict
	}
	if err = registration.mutation(ctx, tx, characterID, quest); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return nil
}

func (r questEventRegistration) accepts(event QuestAuthoredEvent, status QuestStatus) bool {
	if r.statuses == nil {
		return questEventAcceptsStatus(event, status)
	}
	_, ok := r.statuses[status]
	return ok
}

func supportedQuestEvent(event QuestAuthoredEvent) bool {
	switch event {
	case QuestAuthoredInteraction, QuestAuthoredLocalAction, QuestAuthoredCountdown, QuestAuthoredMode2Timer:
		return true
	default:
		return false
	}
}

func questEventAcceptsStatus(event QuestAuthoredEvent, status QuestStatus) bool {
	switch event {
	case QuestAuthoredInteraction:
		return status == QuestTransient2
	case QuestAuthoredLocalAction:
		return status.ClientActive()
	case QuestAuthoredCountdown, QuestAuthoredMode2Timer:
		return status.ClientActive()
	default:
		return false
	}
}
