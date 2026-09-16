package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// BuildPKPenaltyUpdate mirrors sro_client.exe On30cd at 0x00881090.
func BuildPKPenaltyUpdate(penalty int32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.PKUpdatePenalty
	p.WriteUInt32(uint32(penalty))
	return p
}

// BuildPKDailyUpdate mirrors sro_client.exe On30ce at 0x00881100.
func BuildPKDailyUpdate(daily uint8) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.PKUpdateDaily
	p.WriteByte(daily)
	return p
}

// BuildPKLevelUpdate mirrors sro_client.exe On30d3 at 0x00881170.
func BuildPKLevelUpdate(total int16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.PKUpdateLevel
	p.WriteUInt16(uint16(total))
	return p
}
