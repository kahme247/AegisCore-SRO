package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type GachaPlayRequest struct {
	NPCUniqueID uint32
	EntryID     uint32
	CardSlot    byte
}

func DecodeGachaPlayRequest(packet *network.Packet) (GachaPlayRequest, error) {
	if packet == nil || packet.Remaining() != 9 {
		return GachaPlayRequest{}, fmt.Errorf("invalid Gacha play request")
	}
	npc, err := packet.ReadUInt32()
	if err != nil {
		return GachaPlayRequest{}, err
	}
	entryID, err := packet.ReadUInt32()
	if err != nil {
		return GachaPlayRequest{}, err
	}
	cardSlot, err := packet.ReadByte()
	if err != nil || npc == 0 || entryID == 0 {
		return GachaPlayRequest{}, fmt.Errorf("invalid Gacha play selector")
	}
	return GachaPlayRequest{NPCUniqueID: npc, EntryID: entryID, CardSlot: cardSlot}, nil
}

func DecodeGachaExchangeRequest(packet *network.Packet) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid Gacha exchange request")
	}
	value, err := packet.ReadUInt32()
	if err != nil || value == 0 {
		return 0, fmt.Errorf("invalid Gacha exchange NPC")
	}
	return value, nil
}

func BuildGachaPlayResponse(result, detail byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GachaPlayResponse
	p.WriteByte(result)
	p.WriteByte(detail)
	return p
}

func BuildGachaExchangeResponse(result byte, detail uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GachaExchangeResponse
	p.WriteByte(result)
	p.WriteUInt16(detail)
	return p
}

func BuildGachaAnnouncement(itemReference uint32, count uint16) (network.Packet, error) {
	if itemReference == 0 || count == 0 {
		return network.Packet{}, fmt.Errorf("invalid Gacha announcement")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GachaAnnounce
	p.WriteUInt32(itemReference)
	p.WriteUInt16(count)
	return p, nil
}
