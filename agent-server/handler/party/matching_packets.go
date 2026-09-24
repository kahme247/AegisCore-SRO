package party

import "github.com/Harvi8/AegisCore/framework/network"

func buildMatchingUpsertResult(responseOpcode uint16, matchingID uint32, partySetting, purposeType, levelMin, levelMax byte, title string) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = responseOpcode
	packet.WriteByte(1)
	packet.WriteUInt32(matchingID)
	packet.WriteUInt32(0)
	packet.WriteByte(partySetting)
	packet.WriteByte(purposeType)
	packet.WriteByte(levelMin)
	packet.WriteByte(levelMax)
	packet.WriteString(title)
	return packet
}
