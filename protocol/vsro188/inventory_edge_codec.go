package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type InventoryUnknownRequest struct {
	GroundItemUniqueID uint32
	Slot               byte
}

func DecodeInventoryUnknownRequest(packet *network.Packet) (InventoryUnknownRequest, error) {
	if packet == nil || packet.Remaining() != 5 {
		return InventoryUnknownRequest{}, fmt.Errorf("invalid inventory 703F request")
	}
	groundItemUID, err := packet.ReadUInt32()
	if err != nil {
		return InventoryUnknownRequest{}, err
	}
	slot, err := packet.ReadByte()
	if err != nil || groundItemUID == 0 {
		return InventoryUnknownRequest{}, fmt.Errorf("invalid inventory 703F selector")
	}
	return InventoryUnknownRequest{GroundItemUniqueID: groundItemUID, Slot: slot}, nil
}

func BuildInventoryAmmoUpdate(count uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryUpdateAmmo
	packet.WriteUInt16(count)
	return packet
}

func BuildInventoryUnknownSuccess() network.Packet {
	return buildInventoryEdgeResult(opcode.InventoryUnknownResponse, true, 0)
}

func BuildInventoryUnknownFailure(code uint16) network.Packet {
	return buildInventoryEdgeResult(opcode.InventoryUnknownResponse, false, code)
}

func BuildStorageOpenSuccess() network.Packet {
	return buildInventoryEdgeResult(opcode.InventoryStorageOpenResponse, true, 0)
}

func BuildStorageOpenFailure(code uint16) network.Packet {
	return buildInventoryEdgeResult(opcode.InventoryStorageOpenResponse, false, code)
}

func buildInventoryEdgeResult(messageID uint16, success bool, code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	if success {
		packet.WriteByte(1)
		return packet
	}
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}
