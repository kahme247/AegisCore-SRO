package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestCOSExtensionLayoutsMatchClientParsers(t *testing.T) {
	begin := BuildCOSUnknownTransferBegin(true)
	if begin.MessageID != opcode.COSUnknownUpdate0 || hex.EncodeToString(begin.Payload()) != "01" {
		t.Fatalf("begin = %04x/%x", begin.MessageID, begin.Payload())
	}
	data, err := BuildCOSUnknownTransferData([]byte{1, 2, 3})
	if err != nil || hex.EncodeToString(data.Payload()) != "010203" {
		t.Fatalf("data = %x, %v", data.Payload(), err)
	}
	records, err := BuildCOSUnknownRecords([]COSUnknownRecord{{First: 1, Second: 2, Third: 3, State: 4}})
	if err != nil || hex.EncodeToString(records.Payload()) != "0101000000020000000300000004" {
		t.Fatalf("records = %x, %v", records.Payload(), err)
	}
	state, err := BuildCOSStateUpdate(COSStateUpdate{UniqueID: 0x12345678, Mask: 3, FirstState: 4, SecondState: 5})
	if err != nil || hex.EncodeToString(state.Payload()) != "78563412030405" {
		t.Fatalf("state = %x, %v", state.Payload(), err)
	}
	success := BuildCOSUnknownResponseSuccess(7)
	failure := BuildCOSUnknownResponseFailure(0x1234)
	if hex.EncodeToString(success.Payload()) != "0107" || hex.EncodeToString(failure.Payload()) != "023412" {
		t.Fatalf("response = %x/%x", success.Payload(), failure.Payload())
	}
}

func TestCOSExtensionCodecsRejectUnsupportedLayouts(t *testing.T) {
	if _, err := BuildCOSStateUpdate(COSStateUpdate{UniqueID: 1, Mask: 4}); err == nil {
		t.Fatal("expected unsupported COS state mask")
	}
	if _, err := BuildCOSUnknownTransferData(nil); err == nil {
		t.Fatal("expected empty transfer rejection")
	}
	request := network.EmptyClientPacket()
	request.WriteByte(1)
	if err := DecodeCOSUnknownRequest(&request); err == nil {
		t.Fatal("expected trailing request rejection")
	}
}
