package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestOperatorPacketLayoutsMatchStockClientParser(t *testing.T) {
	requestPacket := network.EmptyClientPacket()
	requestPacket.WriteUInt16(25)
	requestPacket.WriteBytes([]byte{3, 2, 1})
	request, err := DecodeOperatorRequest(&requestPacket)
	if err != nil || request.Command != 25 || !bytes.Equal(request.Payload, []byte{3, 2, 1}) {
		t.Fatalf("request = %#v, %v", request, err)
	}

	failure := BuildOperatorFailure(0x1234)
	if failure.MessageID != opcode.OperatorResponse || !bytes.Equal(failure.Payload(), []byte{2, 0x34, 0x12}) {
		t.Fatalf("failure = %04x/%x", failure.MessageID, failure.Payload())
	}
	success, err := BuildOperatorSuccess(4, []byte{1, 2})
	if err != nil || !bytes.Equal(success.Payload(), []byte{1, 4, 0, 1, 2}) {
		t.Fatalf("success = %x, %v", success.Payload(), err)
	}
	punishment, err := BuildOperatorPunishment(1, 0x12345678)
	if err != nil || punishment.MessageID != opcode.OperatorPunishment || !bytes.Equal(punishment.Payload(), []byte{1, 0x78, 0x56, 0x34, 0x12}) {
		t.Fatalf("punishment = %04x/%x, %v", punishment.MessageID, punishment.Payload(), err)
	}
}

func TestOperatorCodecsRejectUnsafeInput(t *testing.T) {
	zero := network.EmptyClientPacket()
	zero.WriteUInt16(0)
	if _, err := DecodeOperatorRequest(&zero); err == nil {
		t.Fatal("accepted zero command")
	}
	if _, err := BuildOperatorPunishment(2, 0); err == nil {
		t.Fatal("accepted unknown punishment state")
	}
}
