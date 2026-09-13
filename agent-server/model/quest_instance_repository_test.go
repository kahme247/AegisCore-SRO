package model

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

type questInstanceExecer struct{ *questExecer }

func (*questInstanceExecer) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

func TestQuestInstanceMemberRepositorySavesRecoveredFieldOrder(t *testing.T) {
	database := &questInstanceExecer{questExecer: &questExecer{rows: 1}}
	repository, err := NewQuestInstanceMemberRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := NewGameWorldScope(41, 3)
	opened := time.Unix(100, 0)
	lastEnter := time.Unix(200, 0)
	state := QuestInstanceMemberState{
		CharacterID: 77, DungeonKey: 91, Scope: scope, OpenedAt: opened,
		RegionID: 25001, X: 10, Y: 20, Z: 30, Activated: true,
		EnterCount: 4, LastEnterAt: &lastEnter,
	}
	if err = repository.SaveQuestInstanceMember(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	want := []any{77, uint32(91), uint16(41), uint16(3), opened, int16(25001),
		float32(10), float32(20), float32(30), true, uint32(4), &lastEnter}
	if database.query != saveQuestInstanceMember || !reflect.DeepEqual(database.args, want) {
		t.Fatalf("save query=%q args=%#v", database.query, database.args)
	}
}

func TestQuestInstanceMemberRepositoryDeletesByLegacyCharacterWorldKey(t *testing.T) {
	database := &questInstanceExecer{questExecer: &questExecer{rows: 1}}
	repository, _ := NewQuestInstanceMemberRepository(database)
	if err := repository.DeleteQuestInstanceMember(context.Background(), 77, 41); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(database.args, []any{77, uint16(41)}) {
		t.Fatalf("delete args = %#v", database.args)
	}
	database.rows = 0
	if err := repository.DeleteQuestInstanceMember(context.Background(), 77, 41); !errors.Is(err, ErrQuestInstanceMissing) {
		t.Fatalf("missing delete error = %v", err)
	}
}
