package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// GuildUpdateType is the first-byte discriminator consumed by the vSRO 1.188
// client GuildUpdate (0x38F5) handler.
type GuildUpdateType byte

const (
	GuildUpdateDisband     GuildUpdateType = 0x01
	GuildUpdateMemberJoin  GuildUpdateType = 0x02
	GuildUpdateMemberLeave GuildUpdateType = 0x03
	GuildUpdateGuild       GuildUpdateType = 0x05
	GuildUpdateMember      GuildUpdateType = 0x06
	GuildUpdatePermissions GuildUpdateType = 0x14
	GuildUpdateWarScore    GuildUpdateType = 0x1D
)

func BuildGuildWarScoreUpdate(side byte, warID, points, characterID uint32, characterName string) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateWarScore))
	packet.WriteByte(side)
	packet.WriteUInt32(warID)
	packet.WriteUInt32(points)
	packet.WriteUInt32(characterID)
	packet.WriteString(characterName)
	return packet
}

type GuildMemberLeaveReason byte

const (
	GuildMemberLeft   GuildMemberLeaveReason = 0x01
	GuildMemberKicked GuildMemberLeaveReason = 0x02
)

// GuildMemberUpdate is the member record consumed by GuildUpdate type 0x02.
type GuildMemberUpdate struct {
	CharacterID    uint32
	Name           string
	Class          byte
	Level          byte
	Donation       uint32
	Permissions    uint32
	Contribution   uint32
	Nickname       string
	RefObjectID    uint32
	SiegeAuthority byte
	Online         bool
}

type GuildPermissionUpdate struct {
	CharacterID uint32
	Permissions uint32
}

type GuildUpdateGuildType byte

const (
	GuildUpdateGuildGP      GuildUpdateGuildType = 0x08
	GuildUpdateGuildPromote GuildUpdateGuildType = 0x0C
	GuildUpdateGuildNotice  GuildUpdateGuildType = 0x10
)

type GuildUpdateMemberType byte

const (
	GuildUpdateMemberLevel    GuildUpdateMemberType = 0x01
	GuildUpdateMemberState    GuildUpdateMemberType = 0x02
	GuildUpdateMemberGP       GuildUpdateMemberType = 0x08
	GuildUpdateMemberPosition GuildUpdateMemberType = 0x40
	GuildUpdateMemberZone     GuildUpdateMemberType = 0x80
)

func newGuildUpdate() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildUpdate
	return packet
}

func BuildGuildDisbandUpdate() network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateDisband))
	return packet
}

func BuildGuildMemberJoinUpdate(member GuildMemberUpdate) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateMemberJoin))
	packet.WriteUInt32(member.CharacterID)
	packet.WriteString(member.Name)
	packet.WriteByte(member.Class)
	packet.WriteByte(member.Level)
	packet.WriteUInt32(member.Donation)
	packet.WriteUInt32(member.Permissions)
	packet.WriteUInt32(member.Contribution)
	packet.WriteUInt32(0)
	packet.WriteUInt32(0)
	packet.WriteString(member.Nickname)
	packet.WriteUInt32(member.RefObjectID)
	packet.WriteByte(member.SiegeAuthority)
	if member.Online {
		packet.WriteByte(0)
	} else {
		packet.WriteByte(1)
	}
	return packet
}

func BuildGuildMemberLeaveUpdate(characterID uint32, reason GuildMemberLeaveReason) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateMemberLeave))
	packet.WriteUInt32(characterID)
	packet.WriteByte(byte(reason))
	return packet
}

func BuildGuildPermissionUpdate(updates []GuildPermissionUpdate) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdatePermissions))
	packet.WriteByte(byte(len(updates)))
	for _, update := range updates {
		packet.WriteUInt32(update.CharacterID)
		packet.WriteUInt32(update.Permissions)
	}
	return packet
}

func BuildGuildGPUpdate(guildPoints uint32) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateGuild))
	packet.WriteByte(byte(GuildUpdateGuildGP))
	packet.WriteUInt32(guildPoints)
	return packet
}

func BuildGuildPromoteUpdate(level byte, guildPoints uint32) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateGuild))
	packet.WriteByte(byte(GuildUpdateGuildPromote))
	packet.WriteByte(level)
	packet.WriteUInt32(guildPoints)
	return packet
}

func BuildGuildNoticeUpdate(title, body string) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateGuild))
	packet.WriteByte(byte(GuildUpdateGuildNotice))
	packet.WriteString(title)
	packet.WriteString(body)
	return packet
}

func BuildGuildMemberGPUpdate(characterID, guildPoints uint32) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateMember))
	packet.WriteUInt32(characterID)
	packet.WriteByte(byte(GuildUpdateMemberGP))
	packet.WriteUInt32(guildPoints)
	return packet
}

func BuildGuildMemberLevelUpdate(characterID uint32, level byte) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateMember))
	packet.WriteUInt32(characterID)
	packet.WriteByte(byte(GuildUpdateMemberLevel))
	packet.WriteByte(level)
	return packet
}

func BuildGuildMemberPositionUpdate(characterID uint32, position byte) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateMember))
	packet.WriteUInt32(characterID)
	packet.WriteByte(byte(GuildUpdateMemberPosition))
	packet.WriteByte(position)
	return packet
}

func BuildGuildMemberStateUpdate(characterID uint32, online bool) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(byte(GuildUpdateMember))
	packet.WriteUInt32(characterID)
	packet.WriteByte(byte(GuildUpdateMemberState))
	if online {
		packet.WriteByte(0)
	} else {
		packet.WriteByte(1)
	}
	return packet
}

func BuildGuildLeaderTransferUpdate(previousMasterID, nextMasterID uint32) network.Packet {
	packet := newGuildUpdate()
	packet.WriteByte(0x16)
	packet.WriteByte(0x02)
	packet.WriteByte(0x54)
	packet.WriteUInt32(previousMasterID)
	packet.WriteByte(0x00)
	packet.WriteUInt32(^uint32(0))
	packet.WriteByte(0x01)
	packet.WriteUInt32(nextMasterID)
	packet.WriteByte(0x0A)
	packet.WriteUInt32(0)
	packet.WriteByte(0)
	return packet
}

func BuildGuildEntityNicknameUpdate(guildID uint32, characterName, nickname string) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateGuildNickname
	packet.WriteUInt32(guildID)
	packet.WriteString(characterName)
	packet.WriteString(nickname)
	return packet
}

func BuildGuildEntityUpdate(uniqueID, guildID uint32, guildName, nickname string) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildEntityUpdate
	packet.WriteUInt32(uniqueID)
	packet.WriteUInt32(guildID)
	packet.WriteString(guildName)
	packet.WriteString(nickname)
	packet.WriteUInt32(0)
	packet.WriteUInt32(0)
	packet.WriteUInt32(0)
	packet.WriteByte(0)
	packet.WriteByte(1)
	return packet
}
