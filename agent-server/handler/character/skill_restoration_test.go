package character

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestRestorationInsufficientGoldUsesBinaryErrorCode(t *testing.T) {
	if got := restorationErrorCode(model.ErrSkillRestorationGold); got != 0x7804 {
		t.Fatalf("restorationErrorCode(insufficient gold) = 0x%04X, want 0x7804", got)
	}
}

func TestBuildRestorationGoldUpdateUsesStockPointPacket(t *testing.T) {
	packet := buildRestorationGoldUpdate(0x0102030405060708)
	want := []byte{1, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	if packet.MessageID != opcode.EntityUpdatePoints || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("gold update = 0x%04X %x, want 0x%04X %x", packet.MessageID, packet.Payload(), opcode.EntityUpdatePoints, want)
	}
}
