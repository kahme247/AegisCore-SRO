package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type GuildMercenaryAttributeRequest struct {
	NPCUniqueID uint32
	Attribute   byte
}

func DecodeGuildMercenaryTerminateRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild mercenary terminate request")
	}
	return nil
}

func BuildGuildMercenaryTerminateFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildMercenaryTerminateResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}

func DecodeGuildMercenaryAttributeRequest(packet *network.Packet) (GuildMercenaryAttributeRequest, error) {
	if packet == nil || packet.Remaining() != 5 {
		return GuildMercenaryAttributeRequest{}, fmt.Errorf("invalid guild mercenary attribute request")
	}
	npcID, err := packet.ReadUInt32()
	if err != nil {
		return GuildMercenaryAttributeRequest{}, err
	}
	attribute, err := packet.ReadByte()
	if err != nil {
		return GuildMercenaryAttributeRequest{}, err
	}
	return GuildMercenaryAttributeRequest{NPCUniqueID: npcID, Attribute: attribute}, nil
}

func BuildGuildMercenaryAttributeFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildMercenaryAttrResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}

// BuildGuildMercenaryAttributeUpdate matches GameServer 0x5C5600:
// GuildUpdate subtype 5, guild-field mask 0x48, GP, then attribute bits.
func BuildGuildMercenaryAttributeUpdate(guildPoints uint32, attribute byte) network.Packet {
	p := newGuildUpdate()
	p.WriteByte(byte(GuildUpdateGuild))
	p.WriteByte(0x48)
	p.WriteUInt32(guildPoints)
	p.WriteByte(attribute)
	return p
}
