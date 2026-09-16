package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestInventoryEdgeLayoutsMatchClientParsers(t *testing.T) {
	requestPacket := network.EmptyClientPacket()
	requestPacket.WriteUInt32(0x12345678)
	requestPacket.WriteByte(9)
	request, err := DecodeInventoryUnknownRequest(&requestPacket)
	if err != nil || request != (InventoryUnknownRequest{GroundItemUniqueID: 0x12345678, Slot: 9}) {
		t.Fatalf("request = %#v, %v", request, err)
	}

	tests := []struct {
		packet network.Packet
		opcode uint16
		want   string
	}{
		{BuildInventoryAmmoUpdate(0x1234), opcode.InventoryUpdateAmmo, "3412"},
		{BuildInventoryUnknownSuccess(), opcode.InventoryUnknownResponse, "01"},
		{BuildInventoryUnknownFailure(0x7182), opcode.InventoryUnknownResponse, "028271"},
		{BuildStorageOpenSuccess(), opcode.InventoryStorageOpenResponse, "01"},
		{BuildStorageOpenFailure(0x7182), opcode.InventoryStorageOpenResponse, "028271"},
	}
	for _, test := range tests {
		if got := hex.EncodeToString(test.packet.Payload()); test.packet.MessageID != test.opcode || got != test.want {
			t.Fatalf("packet = %04x/%s, want %04x/%s", test.packet.MessageID, got, test.opcode, test.want)
		}
	}
}

func TestDecodeInventoryUnknownRequestRejectsInvalidShapes(t *testing.T) {
	zero := network.EmptyClientPacket()
	zero.WriteUInt32(0)
	zero.WriteByte(1)
	if _, err := DecodeInventoryUnknownRequest(&zero); err == nil {
		t.Fatal("accepted zero NPC UID")
	}
	trailing := network.EmptyClientPacket()
	trailing.WriteUInt32(1)
	trailing.WriteByte(2)
	trailing.WriteByte(3)
	if _, err := DecodeInventoryUnknownRequest(&trailing); err == nil {
		t.Fatal("accepted trailing byte")
	}
}
