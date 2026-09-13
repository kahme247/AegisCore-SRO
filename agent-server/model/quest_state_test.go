package model

import (
	"errors"
	"reflect"
	"testing"
)

func TestTransitionQuestConfirmedMatrix(t *testing.T) {
	tests := []struct {
		name       string
		status     QuestStatus
		transition QuestTransition
		want       QuestStatus
	}{
		{"activate inactive", QuestInactive, QuestTransitionActivate, QuestActive},
		{"reactivate aborted", QuestAborted, QuestTransitionActivate, QuestActive},
		{"first progress", QuestActive, QuestTransitionProgress, QuestProgressed},
		{"later progress", QuestProgressed, QuestTransitionProgress, QuestProgressed},
		{"active reward ready", QuestActive, QuestTransitionRewardReady, QuestRewardReady},
		{"progressed reward ready", QuestProgressed, QuestTransitionRewardReady, QuestRewardReady},
		{"close", QuestRewardReady, QuestTransitionClose, QuestClosed},
		{"achieve", QuestRewardReady, QuestTransitionAchieve, QuestInactive},
		{"abort active", QuestActive, QuestTransitionAbort, QuestAborted},
		{"abort progressed", QuestProgressed, QuestTransitionAbort, QuestAborted},
		{"abort reward ready", QuestRewardReady, QuestTransitionAbort, QuestAborted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TransitionQuest(test.status, test.transition)
			if err != nil || got != test.want {
				t.Fatalf("TransitionQuest(%d, %d) = %d, %v; want %d", test.status, test.transition, got, err, test.want)
			}
		})
	}
}

func TestCommitQuestAbandonRemovesLiveInventorySlotsAndMarksQuest(t *testing.T) {
	player := &Player{
		Inventory: NewInventory(map[byte]Item{14: {ID: 140}, 19: {ID: 190}, 20: {ID: 200}}),
		Quests:    []CharacterQuest{{QuestID: 101, Status: QuestProgressed}, {QuestID: 102, Status: QuestActive}},
	}
	player.CommitQuestAbandon(101, QuestAbandonResult{RemovedSlots: []byte{14, 19}})
	if _, exists := player.Inventory.Items[14]; exists {
		t.Fatal("removed quest item remains in slot 14")
	}
	if _, exists := player.Inventory.Items[19]; exists {
		t.Fatal("removed quest item remains in slot 19")
	}
	if _, exists := player.Inventory.Items[20]; !exists {
		t.Fatal("unrelated inventory slot was removed")
	}
	if player.Quests[0].Status != QuestAborted || player.Quests[1].Status != QuestActive {
		t.Fatalf("quest snapshot = %#v", player.Quests)
	}
}

func TestTransitionQuestRejectsEveryUnconfirmedEdge(t *testing.T) {
	allowed := map[[2]byte]struct{}{
		{byte(QuestInactive), byte(QuestTransitionActivate)}:      {},
		{byte(QuestAborted), byte(QuestTransitionActivate)}:       {},
		{byte(QuestActive), byte(QuestTransitionProgress)}:        {},
		{byte(QuestProgressed), byte(QuestTransitionProgress)}:    {},
		{byte(QuestActive), byte(QuestTransitionRewardReady)}:     {},
		{byte(QuestProgressed), byte(QuestTransitionRewardReady)}: {},
		{byte(QuestRewardReady), byte(QuestTransitionClose)}:      {},
		{byte(QuestRewardReady), byte(QuestTransitionAchieve)}:    {},
		{byte(QuestActive), byte(QuestTransitionAbort)}:           {},
		{byte(QuestProgressed), byte(QuestTransitionAbort)}:       {},
		{byte(QuestRewardReady), byte(QuestTransitionAbort)}:      {},
	}
	for status := QuestActive; status <= QuestRewardReady; status++ {
		for transition := QuestTransitionActivate; transition <= QuestTransitionAbort; transition++ {
			if _, ok := allowed[[2]byte{byte(status), byte(transition)}]; ok {
				continue
			}
			if _, err := TransitionQuest(status, transition); !errors.Is(err, ErrQuestStateConflict) {
				t.Fatalf("TransitionQuest(%d, %d) error = %v, want ErrQuestStateConflict", status, transition, err)
			}
		}
	}
}

func TestQuestTransitionSourcesAreExact(t *testing.T) {
	want := map[QuestTransition][]QuestStatus{
		QuestTransitionActivate:    {QuestInactive, QuestAborted},
		QuestTransitionProgress:    {QuestActive, QuestProgressed},
		QuestTransitionRewardReady: {QuestActive, QuestProgressed},
		QuestTransitionClose:       {QuestRewardReady},
		QuestTransitionAchieve:     {QuestRewardReady},
		QuestTransitionAbort:       {QuestActive, QuestProgressed, QuestRewardReady},
	}
	for transition, statuses := range want {
		if got := QuestTransitionSources(transition); !reflect.DeepEqual(got, statuses) {
			t.Fatalf("QuestTransitionSources(%d) = %v, want %v", transition, got, statuses)
		}
	}
}
