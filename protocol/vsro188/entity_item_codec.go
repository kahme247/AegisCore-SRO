package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func buildEntityItemPacket(messageID uint16, uniqueID uint32, slot byte, refItemID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(slot)
	packet.WriteUInt32(refItemID)
	return packet
}

// BuildEntityEquipItem writes the maintained 0x3038 equipment notification.
func BuildEntityEquipItem(uniqueID uint32, slot byte, refItemID uint32, oneHanded bool) network.Packet {
	packet := buildEntityItemPacket(opcode.EntityEquipItem, uniqueID, slot, refItemID)
	packet.WriteBool(oneHanded)
	return packet
}

// BuildEntityUnequipItem writes the maintained 0x3039 equipment notification.
func BuildEntityUnequipItem(uniqueID uint32, slot byte, refItemID uint32) network.Packet {
	return buildEntityItemPacket(opcode.EntityUnequipItem, uniqueID, slot, refItemID)
}

// BuildEntityPickupAnimation writes the maintained 0x3036 pickup animation.
func BuildEntityPickupAnimation(uniqueID uint32, heading byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityAnimationPickup
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(heading)
	return packet
}
