package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	JobSuitCountdownEquip   byte = 1
	JobSuitCountdownUnequip byte = 2
	JobSuitCountdownCancel  byte = 1
	JobSuitCountdownFinish  byte = 2
)

// BuildJobSuitCountdownStart matches sro_client.exe 0x008822C0:
// entity UID, inventory-countdown family 2, equip direction, seconds.
func BuildJobSuitCountdownStart(uniqueID uint32, direction, seconds byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryEquipCountdownStart
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(2)
	packet.WriteByte(direction)
	packet.WriteByte(seconds)
	return packet
}

// BuildJobSuitCountdownStop matches sro_client.exe 0x008825D0.
func BuildJobSuitCountdownStop(uniqueID uint32, result byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryEquipCountdownStop
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(result)
	return packet
}
