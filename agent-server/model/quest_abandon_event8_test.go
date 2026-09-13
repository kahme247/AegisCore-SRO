package model

import (
	"reflect"
	"testing"
)

func TestNativeQuestAbandonDefinitionsPreserveBinaryCleanup(t *testing.T) {
	tests := []struct {
		questID int
		items   []uint32
		related []int
	}{
		{141, []uint32{5941}, nil},
		{287, []uint32{10564, 10567, 10565}, nil},
		{308, nil, []int{307}},
		{310, nil, []int{309}},
		{366, []uint32{23363, 23361}, nil},
		{96, []uint32{23974, 23975}, []int{92}},
	}
	for _, test := range tests {
		got := nativeQuestAbandonDefinition(test.questID)
		if !reflect.DeepEqual(got.ItemRefIDs, test.items) ||
			!reflect.DeepEqual(got.RelatedQuestIDs, test.related) {
			t.Errorf("quest %d cleanup = %#v", test.questID, got)
		}
	}
}

func TestCommitQuestAbandonResetsRelatedLiveQuest(t *testing.T) {
	player := &Player{Quests: []CharacterQuest{
		{QuestID: 308, Status: QuestActive, AchievementCount: 2, Data1: 3, Data2: 4},
		{QuestID: 307, Status: QuestProgressed, AchievementCount: 5, Data1: 6, Data2: 7},
	}}
	player.CommitQuestAbandon(308, QuestAbandonResult{ResetQuestIDs: []int{307}})
	for _, quest := range player.Quests {
		if quest.Status != QuestAborted || quest.AchievementCount != 0 || quest.Data1 != 0 || quest.Data2 != 0 {
			t.Fatalf("quest was not reset: %#v", quest)
		}
	}
}
