package character

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ferdoran/go-sro/framework/network/opcode"
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
	entries := make([]string, 256)
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
