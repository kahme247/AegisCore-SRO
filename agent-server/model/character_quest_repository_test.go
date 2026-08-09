package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"time"
)

type questExecer struct {
	query string
	args  []any
	rows  int64
}

func (e *questExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.query = query
	e.args = append([]any(nil), args...)
	return driver.RowsAffected(e.rows), nil
}

func TestCharacterQuestRepositorySavesActiveQuest(t *testing.T) {
	database := &questExecer{rows: 1}
	repository, err := NewCharacterQuestRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterQuestRepository: %v", err)
	}
	quest := CharacterQuest{QuestID: 101, Status: QuestActive, AchievementCount: 2, Data1: 3, Data2: 4}
	if err := repository.Save(context.Background(), 77, quest); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if database.query != upsertCharacterQuest {
		t.Fatalf("query = %q", database.query)
	}
	want := []any{77, 101, QuestActive, uint16(2), (*time.Time)(nil), uint64(3), uint64(4)}
	if !reflect.DeepEqual(database.args, want) {
		t.Fatalf("args = %#v, want %#v", database.args, want)
	}
}

func TestCharacterQuestRepositoryRequiresCompletionTime(t *testing.T) {
	repository, err := NewCharacterQuestRepository(&questExecer{rows: 1})
	if err != nil {
		t.Fatalf("NewCharacterQuestRepository: %v", err)
	}
	if err := repository.Save(context.Background(), 77, CharacterQuest{QuestID: 101, Status: QuestCompleted}); err == nil {
		t.Fatal("Save accepted completed quest without completion time")
	}
}

func TestCharacterQuestRepositoryActivatesNewOrAbandonedQuest(t *testing.T) {
	for _, affected := range []int64{1, 2} {
		database := &questExecer{rows: affected}
		repository, err := NewCharacterQuestRepository(database)
		if err != nil {
			t.Fatalf("NewCharacterQuestRepository: %v", err)
		}
		quest := CharacterQuest{QuestID: 3, Status: QuestActive, AchievementCount: 1}
		activated, err := repository.Activate(context.Background(), 77, quest)
		if err != nil {
			t.Fatalf("Activate affected=%d: %v", affected, err)
		}
		if database.query != activateCharacterQuest {
			t.Fatalf("query = %q", database.query)
		}
		if activated.QuestID != 3 || activated.Status != QuestActive || activated.StartedAt.IsZero() {
			t.Fatalf("activated quest = %#v", activated)
		}
	}
}

func TestCharacterQuestRepositoryDoesNotOverwriteUnavailableQuest(t *testing.T) {
	repository, err := NewCharacterQuestRepository(&questExecer{rows: 0})
	if err != nil {
		t.Fatalf("NewCharacterQuestRepository: %v", err)
	}
	_, err = repository.Activate(context.Background(), 77, CharacterQuest{QuestID: 3, Status: QuestActive, AchievementCount: 1})
	if !errors.Is(err, ErrCharacterQuestNotAvailable) {
		t.Fatalf("Activate error = %v, want ErrCharacterQuestNotAvailable", err)
	}
}

func TestCharacterQuestRepositoryAbandonIsCompareAndSwap(t *testing.T) {
	database := &questExecer{rows: 1}
	repository, err := NewCharacterQuestRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterQuestRepository: %v", err)
	}
	if err := repository.Abandon(context.Background(), 77, 101); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	if database.query != abandonCharacterQuest {
		t.Fatalf("query = %q", database.query)
	}
	want := []any{QuestAbandoned, 77, 101, QuestActive, QuestReadyToTurnIn, QuestStartedByUser, QuestReadyToTurnInByUser}
	if !reflect.DeepEqual(database.args, want) {
		t.Fatalf("args = %#v, want %#v", database.args, want)
	}
}

func TestCharacterQuestRepositoryAbandonRejectsInactiveQuest(t *testing.T) {
	repository, err := NewCharacterQuestRepository(&questExecer{rows: 0})
	if err != nil {
		t.Fatalf("NewCharacterQuestRepository: %v", err)
	}
	if err := repository.Abandon(context.Background(), 77, 101); !errors.Is(err, ErrCharacterQuestNotActive) {
		t.Fatalf("Abandon error = %v, want ErrCharacterQuestNotActive", err)
	}
}
