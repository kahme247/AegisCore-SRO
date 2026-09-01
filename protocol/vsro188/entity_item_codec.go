package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// BuildEntityEquipItem writes the maintained 0x3038 equipment notification.
func BuildEntityEquipItem(uniqueID uint32, slot byte, refItemID uint32, oneHanded bool) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityEquipItem
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(slot)
	packet.WriteUInt32(refItemID)
	packet.WriteBool(oneHanded)
	return packet
}

// BuildEntityUnequipItem writes the maintained 0x3039 equipment notification.
func BuildEntityUnequipItem(uniqueID uint32, slot byte, refItemID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUnequipItem
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(slot)
	packet.WriteUInt32(refItemID)
	return packet
}

// BuildEntityPickupAnimation writes the maintained 0x3036 pickup animation.
func BuildEntityPickupAnimation(uniqueID uint32, heading byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityAnimationPickup
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(heading)
	return packet
}
