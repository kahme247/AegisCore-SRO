package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestJobSuitCountdownPacketsMatchClientParsers(t *testing.T) {
	start := BuildJobSuitCountdownStart(0x11223344, JobSuitCountdownEquip, 30)
	if start.MessageID != opcode.InventoryEquipCountdownStart || !bytes.Equal(start.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 2, 1, 30}) {
		t.Fatalf("start = %04X/% X", start.MessageID, start.Payload())
	}
	stop := BuildJobSuitCountdownStop(0x11223344, JobSuitCountdownFinish)
	if stop.MessageID != opcode.InventoryEquipCountdownStop || !bytes.Equal(stop.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 2}) {
		t.Fatalf("stop = %04X/% X", stop.MessageID, stop.Payload())
	}
}
