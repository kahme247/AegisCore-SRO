package model

import "testing"

func TestNativeInteractionTalkKeysMatchRecoveredHandlers(t *testing.T) {
	want := map[int]string{
		307: "SN_TALK_QNO_TQ_BAEKRYEONG_13_09",
		308: "SN_TALK_QNO_TQ_BAEKRYEONG_14_09",
		310: "SN_TALK_QNO_TQ_BAEKRYEONG_16_09",
	}
	for questID, key := range want {
		if got, exists := NativeInteractionTalkKey(questID); !exists || got != key {
			t.Fatalf("quest %d key=%q exists=%t", questID, got, exists)
		}
	}
	if _, exists := NativeInteractionTalkKey(MeetRocInteractionQuestID); exists {
		t.Fatal("MeetRoc recomputation incorrectly classified as a talk-only override")
	}
}
