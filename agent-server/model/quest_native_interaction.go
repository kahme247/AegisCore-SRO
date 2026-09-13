package model

import (
	"context"
	"database/sql"
)

const MeetRocInteractionQuestID = 376
const MeetRocProgressItemRefID uint32 = 24175

var NativeInteractionTalkKeys = map[int]string{
	307: "SN_TALK_QNO_TQ_BAEKRYEONG_13_09",
	308: "SN_TALK_QNO_TQ_BAEKRYEONG_14_09",
	310: "SN_TALK_QNO_TQ_BAEKRYEONG_16_09",
}

func NativeInteractionTalkKey(questID int) (string, bool) {
	key, exists := NativeInteractionTalkKeys[questID]
	return key, exists
}

// RegisterNativeInteractionEvents binds all four concrete event-7 overrides.
// The three Baekryoung handlers are wire-only notifications. MeetRoc likewise
// performs no quest SQL mutation; its player adapter republishes inventory-
// derived progress for quests 374/375 after this locked state validation.
func RegisterNativeInteractionEvents(dispatcher *SQLQuestEventDispatcher) error {
	if dispatcher == nil {
		return ErrQuestRuntimeUnavailable
	}
	questIDs := []int{307, 308, 310, MeetRocInteractionQuestID}
	for _, id := range questIDs {
		questID := id
		if err := dispatcher.Register(questID, QuestAuthoredInteraction,
			func(context.Context, *sql.Tx, int, CharacterQuest) error { return nil }); err != nil {
			return err
		}
	}
	return nil
}
