package model

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestBuildGroundItemOwnershipRelease(t *testing.T) {
	packet := BuildGroundItemOwnershipRelease(0x11223344)
	if packet.MessageID != opcode.EntityRemoveOwnership {
		t.Fatalf("message ID = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntityRemoveOwnership)
	}
	if got := packet.Payload(); len(got) != 4 || got[0] != 0x44 || got[1] != 0x33 || got[2] != 0x22 || got[3] != 0x11 {
		t.Fatalf("payload = % X, want 44 33 22 11", got)
	}
}
