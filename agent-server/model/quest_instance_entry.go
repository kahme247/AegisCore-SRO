package model

// QuestInstanceEntryQuestID returns the native item-driven instance producer.
// Dialogue-driven entries (quests 141 and 311) intentionally do not belong in
// this catalog.
func QuestInstanceEntryQuestID(codeName string) (int, bool) {
	questID, ok := map[string]int{
		"ITEM_QNO_EU_IVY_3_06":        208,
		"ITEM_QNO_CA_GORIA_7_03":      251,
		"ITEM_QNO_TQ_STONEBOARD_4_03": 276,
		"ITEM_QNO_TQ_ORNAMENT_4_04":   287,
	}[codeName]
	return questID, ok
}
