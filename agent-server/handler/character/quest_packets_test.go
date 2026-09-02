package character

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestBuildQuestTalkPacketBaseLayout(t *testing.T) {
	packet, err := buildQuestTalkPacket(2, "NPC_A", nil)
	if err != nil {
		t.Fatalf("buildQuestTalkPacket: %v", err)
	}
	if packet.MessageID != opcode.QuestTalk {
		t.Fatalf("opcode = 0x%04X", packet.MessageID)
	}
	want := []byte{2, 5, 0, 'N', 'P', 'C', '_', 'A'}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestBuildQuestTalkPacketExtendedLayout(t *testing.T) {
	packet, err := buildQuestTalkPacket(4, "QUEST_A", []string{"ONE", "TWO"})
	if err != nil {
		t.Fatalf("buildQuestTalkPacket: %v", err)
	}
	want := []byte{
		4, 7, 0, 'Q', 'U', 'E', 'S', 'T', '_', 'A',
		2,
		3, 0, 'O', 'N', 'E',
		3, 0, 'T', 'W', 'O',
	}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestBuildQuestTalkPacketRejectsInvalidShape(t *testing.T) {
	if _, err := buildQuestTalkPacket(3, "QUEST_A", []string{"unexpected"}); err == nil {
		t.Fatal("non-extended type accepted repeated strings")
	}
	entries := make([]string, vsro188.QuestDialogueSelectionLimit+1)
	if _, err := buildQuestTalkPacket(5, "QUEST_A", entries); err == nil {
		t.Fatal("extended type accepted more than 255 repeated strings")
	}
}

func TestBuildQuestUpdatePacketLayoutAndRange(t *testing.T) {
	packet, err := buildQuestUpdatePacket(3, 0x78563412)
	if err != nil {
		t.Fatalf("buildQuestUpdatePacket: %v", err)
	}
	if packet.MessageID != opcode.QuestUpdate {
		t.Fatalf("opcode = 0x%04X", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{3, 0x12, 0x34, 0x56, 0x78}; !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
	for _, invalid := range []byte{0, 5, 255} {
		if _, err := buildQuestUpdatePacket(invalid, 1); err == nil {
			t.Fatalf("type %d was accepted", invalid)
		}
	}
}

func TestBuildQuestAddAndStatePacketsPreserveActiveRecordAlignment(t *testing.T) {
	quest := model.CharacterQuest{QuestID: model.QuestChulsanAngerID, Status: model.QuestProgressed, AchievementCount: 1, Data1: 7}
	active := []byte{1, 0, 0x18, byte(model.QuestProgressed), 1, 1, 1, 21, 0}
	active = append(active, "SN_CON_QNO_CH_SMITH_2"...)
	active = append(active, 1, 7, 0, 0, 0)
	for _, test := range []struct {
		name       string
		build      func(model.CharacterQuest) (network.Packet, error)
		updateType byte
	}{
		{name: "add", build: buildQuestAddPacket, updateType: 1},
		{name: "state", build: buildQuestStatePacket, updateType: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet, err := test.build(quest)
			if err != nil {
				t.Fatal(err)
			}
			want := []byte{test.updateType, byte(quest.QuestID), 0, 0, 0}
			want = append(want, active...)
			if got := packet.Payload(); !bytes.Equal(got, want) {
				t.Fatalf("payload = % X, want % X", got, want)
			}
		})
	}
}

func TestBuildQuestScriptPacketLayout(t *testing.T) {
	packet, err := buildQuestScriptPacket("SCRIPT", 0x04030201)
	if err != nil {
		t.Fatalf("buildQuestScriptPacket: %v", err)
	}
	if packet.MessageID != opcode.QuestScript {
		t.Fatalf("opcode = 0x%04X", packet.MessageID)
	}
	want := []byte{6, 0, 'S', 'C', 'R', 'I', 'P', 'T', 1, 2, 3, 4}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestQuestPacketBuildersRejectOversizedStrings(t *testing.T) {
	oversized := strings.Repeat("x", 1<<16)
	if _, err := buildQuestTalkPacket(1, oversized, nil); err == nil {
		t.Fatal("quest talk accepted an oversized string")
	}
	if _, err := buildQuestScriptPacket(oversized, 0); err == nil {
		t.Fatal("quest script accepted an oversized string")
	}
}
