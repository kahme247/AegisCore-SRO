package model

import "testing"

func TestQuestInstanceEntryQuestIDCatalog(t *testing.T) {
	tests := map[string]int{
		"ITEM_QNO_EU_IVY_3_06":        208,
		"ITEM_QNO_CA_GORIA_7_03":      251,
		"ITEM_QNO_TQ_STONEBOARD_4_03": 276,
		"ITEM_QNO_TQ_ORNAMENT_4_04":   287,
	}
	for codeName, want := range tests {
		got, ok := QuestInstanceEntryQuestID(codeName)
		if !ok || got != want {
			t.Fatalf("QuestInstanceEntryQuestID(%q)=(%d,%t), want (%d,true)", codeName, got, ok, want)
		}
	}
	for _, codeName := range []string{"ITEM_QNO_RM_OLDWOMAN_7_03", "ITEM_QNO_TQ_BAEKRYEONG_17_01", "ITEM_ETC_HP_POTION_01"} {
		if questID, ok := QuestInstanceEntryQuestID(codeName); ok {
			t.Fatalf("dialogue/unrelated item %q mapped to quest %d", codeName, questID)
		}
	}
}
