package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type GuildSiegeAuthorityRequest struct {
	CharacterID uint32
	Authority   byte
}

func DecodeGuildSiegeAuthorityRequest(packet *network.Packet) (GuildSiegeAuthorityRequest, error) {
	if packet == nil || packet.Remaining() != 5 {
		return GuildSiegeAuthorityRequest{}, fmt.Errorf("invalid guild siege authority request")
	}
	characterID, err := packet.ReadUInt32()
	if err != nil {
		return GuildSiegeAuthorityRequest{}, err
	}
	authority, err := packet.ReadByte()
	if err != nil {
		return GuildSiegeAuthorityRequest{}, err
	}
	return GuildSiegeAuthorityRequest{CharacterID: characterID, Authority: authority}, nil
}

// BuildGuildSiegeAuthorityFailure is error-only in the stock GameServer.
// Successful requests are acknowledged asynchronously by 0x3103.
func BuildGuildSiegeAuthorityFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildUpdateSiegeAuthorityResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}

func BuildGuildSiegeAuthorityUpdate(characterID uint32, authority byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityGuildUpdateSiegeAuthority
	p.WriteUInt32(characterID)
	p.WriteByte(authority)
	return p
}
