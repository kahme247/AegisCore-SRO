package questwire

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestBuildUpdatePreservesEveryClassifiedMission(t *testing.T) {
	quest := model.CharacterQuest{
		QuestID: 900, Status: model.QuestProgressed, TaskCount: 3,
		Data1:         2 | 7<<16 | 1<<32,
		ObjectiveKeys: [4]string{"SN_NORMAL", "SN_CHAMPION", "SN_GIANT"},
	}
	packet, err := BuildUpdate(quest, vsro188.QuestUpdateDelta)
	if err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	if len(payload) < 10 || payload[7] != 0x18 || payload[9] != 3 {
		t.Fatalf("classified update prefix = % X", payload)
	}
	for _, entry := range []struct {
		key   string
		value byte
	}{{"SN_NORMAL", 2}, {"SN_CHAMPION", 7}, {"SN_GIANT", 1}} {
		if bytes.Count(payload, []byte(entry.key)) != 1 || bytes.Count(payload, []byte{entry.value, 0, 0, 0}) != 1 {
			t.Fatalf("classified update is missing %s=%d: % X", entry.key, entry.value, payload)
		}
	}
}

func TestBuildUpdatePreservesZeroMissionFerry(t *testing.T) {
	quest := model.CharacterQuest{QuestID: 88, QuestCodeName: "QNO_WC_FERRY4_1", Status: model.QuestRewardReady}
	packet, err := BuildUpdate(quest, vsro188.QuestUpdateDelta)
	if err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	if len(payload) != 10 || payload[9] != 0 {
		t.Fatalf("zero-mission update = % X", payload)
	}
}
