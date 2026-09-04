package model

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// BuildGroundItemOwnershipRelease matches the recovered 0x304D body:
// exactly the runtime object UID, with no status byte or item payload.
func BuildGroundItemOwnershipRelease(uniqueID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityRemoveOwnership
	packet.WriteUInt32(uniqueID)
	return packet
}
