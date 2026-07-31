package party

import (
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

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

func buildMatchingListResult(pageIndex, pageCount byte, parties []*model.Party) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyMatchingListResponse
	packet.WriteByte(1)
	packet.WriteByte(pageIndex)
	packet.WriteByte(pageCount)
	packet.WriteByte(byte(len(parties)))
	for _, party := range parties {
		packet.WriteUInt32(party.Number)
		packet.WriteUInt32(party.MasterJID)
		packet.WriteString(party.MasterName)
		packet.WriteByte(party.CountryType)
		packet.WriteByte(party.MemberCount)
		packet.WriteByte(byte(party.PartySettingsFlag))
		packet.WriteByte(byte(party.PurposeType))
		packet.WriteByte(party.LevelMin)
		packet.WriteByte(party.LevelMax)
		packet.WriteString(party.Title)
	}
	return packet
}
