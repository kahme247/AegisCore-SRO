package service

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestBuildServerNoticePacketUsesDebugConsoleNoticeWire(t *testing.T) {
	packet, err := buildServerNoticePacket("GM notice")
	if err != nil {
		t.Fatal(err)
	}
	if packet.MessageID != opcode.ChatUpdate {
		t.Fatalf("message ID = 0x%04X, want 0x%04X", packet.MessageID, opcode.ChatUpdate)
	}
	want := []byte{managementNoticeChatType, 9, 0, 'G', 'M', ' ', 'n', 'o', 't', 'i', 'c', 'e'}
	if !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("notice payload = % X, want % X", packet.Payload(), want)
	}
}

func TestBuildServerNoticePacketRejectsUntrimmedText(t *testing.T) {
	if _, err := buildServerNoticePacket(" GM notice "); err == nil {
		t.Fatal("untrimmed notice unexpectedly accepted")
	}
}
