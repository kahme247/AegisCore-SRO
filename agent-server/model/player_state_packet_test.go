package model

import (
	"encoding/binary"
	"testing"

	"github.com/ferdoran/go-sro/framework/network/opcode"
)

func TestJoiningPlayerBodyStateUpdateIsNormal(t *testing.T) {
	packet := BuildBodyStateUpdatePacket(0x11223344, Normal)
	if packet.MessageID != opcode.EntityUpdateMovementState {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntityUpdateMovementState)
	}

	payload := packet.Payload()
	if len(payload) != 6 {
		t.Fatalf("payload length = %d, want 6", len(payload))
	}
	if got := binary.LittleEndian.Uint32(payload[:4]); got != 0x11223344 {
		t.Fatalf("unique ID = 0x%08X, want 0x11223344", got)
	}
	if got := payload[4]; got != 4 {
		t.Fatalf("state category = %d, want body-state category 4", got)
	}
	if got := payload[5]; got != 0 {
		t.Fatalf("body state = %d, want normal state 0; value 2 is untouchable", got)
	}
}
