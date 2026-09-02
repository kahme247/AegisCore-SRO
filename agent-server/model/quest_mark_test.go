package model

import (
	"reflect"
	"testing"
)

func TestPlayerQuestMarkLifecycleUsesOpaqueStableHandles(t *testing.T) {
	player := &Player{}
	first, err := player.AddQuestMark(QuestMark{QuestID: 10, Kind: 1, X: -1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := player.AddQuestMark(QuestMark{QuestID: 20, Flags: 2, TargetID: 99})
	if err != nil {
		t.Fatal(err)
	}
	third, err := player.AddQuestMark(QuestMark{QuestID: 10, Kind: 3})
	if err != nil {
		t.Fatal(err)
	}
	if first.RuntimeID == 0 || second.RuntimeID != first.RuntimeID+1 || third.RuntimeID != second.RuntimeID+1 {
		t.Fatalf("runtime IDs = %d, %d, %d", first.RuntimeID, second.RuntimeID, third.RuntimeID)
	}
	if got := player.QuestMarksSnapshot(); !reflect.DeepEqual(got, []QuestMark{first, second, third}) {
		t.Fatalf("snapshot = %#v", got)
	}
	removed := player.RemoveQuestMarksByQuest(10)
	if !reflect.DeepEqual(removed, []QuestMark{first, third}) {
		t.Fatalf("removed = %#v", removed)
	}
	if got := player.QuestMarksSnapshot(); !reflect.DeepEqual(got, []QuestMark{second}) {
		t.Fatalf("remaining = %#v", got)
	}
	if mark, exists := player.RemoveQuestMark(second.RuntimeID); !exists || mark != second {
		t.Fatalf("removed mark = %#v, exists=%t", mark, exists)
	}
	if _, exists := player.RemoveQuestMark(second.RuntimeID); exists {
		t.Fatal("duplicate removal succeeded")
	}
}

func TestPlayerQuestMarkRejectsInvalidWireState(t *testing.T) {
	player := &Player{}
	for _, mark := range []QuestMark{
		{},
		{QuestID: 1, RuntimeID: 9},
		{QuestID: 1, TargetID: 9},
	} {
		if _, err := player.AddQuestMark(mark); err == nil {
			t.Fatalf("AddQuestMark accepted %#v", mark)
		}
	}
}

func TestPlayerQuestMarkSkipsZeroAfterHandleWrap(t *testing.T) {
	player := &Player{nextQuestMarkID: ^uint32(0)}
	mark, err := player.AddQuestMark(QuestMark{QuestID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if mark.RuntimeID != 1 {
		t.Fatalf("runtime ID = %d, want 1", mark.RuntimeID)
	}
}
