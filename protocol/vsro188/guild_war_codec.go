package vsro188

import (
	"fmt"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// GuildWarInfo is one SERVER_AGENT_GUILD_WAR_INFO (0x3109) record.
// The stock vSRO 1.188 client parser at 0x87FF10 reads this exact layout.
type GuildWarInfo struct {
	WarID, WarPeriodID                      uint32
	VictoryPointID                          byte
	Wager, ChallengerGuildID, TargetGuildID uint32
	ChallengerScore, TargetScore            uint32
}

func BuildGuildWarInfo(records ...GuildWarInfo) (network.Packet, error) {
	if len(records) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many guild war records")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildWarInfo
	p.WriteByte(byte(len(records)))
	for _, record := range records {
		if record.WarID == 0 {
			return network.Packet{}, fmt.Errorf("invalid guild war id")
		}
		p.WriteUInt32(record.WarID)
		p.WriteUInt32(record.WarPeriodID)
		p.WriteByte(record.VictoryPointID)
		p.WriteUInt32(record.Wager)
		p.WriteUInt32(record.ChallengerGuildID)
		p.WriteUInt32(record.TargetGuildID)
		p.WriteUInt32(record.ChallengerScore)
		p.WriteUInt32(record.TargetScore)
	}
	return p, nil
}

const GuildWarMaximumWager uint32 = 500_000_000

const GuildWarUnlimitedPeriod uint32 = math.MaxInt32

// DecodeGuildWarPeriod decodes the stock client's packed day/hour/ten-minute
// duration produced by sro_client.exe sub_71A810.
func DecodeGuildWarPeriod(value uint32) (duration time.Duration, unlimited bool, err error) {
	if value == GuildWarUnlimitedPeriod {
		return 0, true, nil
	}
	if value&0xFC0003FF != 0 {
		return 0, false, fmt.Errorf("invalid guild war period bits")
	}
	days := (value >> 10) & 0x1f
	hours := (value >> 15) & 0x1f
	minutes := (value >> 20) & 0x3f
	if days > 30 || hours > 23 || minutes > 50 || minutes%10 != 0 {
		return 0, false, fmt.Errorf("invalid guild war period")
	}
	duration = (time.Duration(days)*24*time.Hour + time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute)
	if duration == 0 {
		return 0, false, fmt.Errorf("empty guild war period")
	}
	return duration, false, nil
}

// GuildWarProposal is the shared 0x7110 body used by a guild master to make a
// proposal and by the server to present that proposal to the opposing master.
type GuildWarProposal struct {
	GuildName      string
	Mode           byte
	WarPeriodID    uint32
	VictoryPointID byte
	Wager          uint32
}

func DecodeGuildWarProposal(packet *network.Packet) (GuildWarProposal, error) {
	if packet == nil {
		return GuildWarProposal{}, fmt.Errorf("guild war proposal packet is nil")
	}
	name, err := packet.ReadString()
	if err != nil {
		return GuildWarProposal{}, err
	}
	mode, err := packet.ReadByte()
	if err != nil {
		return GuildWarProposal{}, err
	}
	period, err := packet.ReadUInt32()
	if err != nil {
		return GuildWarProposal{}, err
	}
	victoryPoint, err := packet.ReadByte()
	if err != nil {
		return GuildWarProposal{}, err
	}
	wager, err := packet.ReadUInt32()
	if err != nil {
		return GuildWarProposal{}, err
	}
	if _, _, periodErr := DecodeGuildWarPeriod(period); packet.Remaining() != 0 || name == "" || mode != 0 || wager > GuildWarMaximumWager || periodErr != nil {
		return GuildWarProposal{}, fmt.Errorf("invalid guild war proposal")
	}
	return GuildWarProposal{GuildName: name, Mode: mode, WarPeriodID: period, VictoryPointID: victoryPoint, Wager: wager}, nil
}

func BuildGuildWarProposal(proposal GuildWarProposal) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildWarRequest
	packet.WriteString(proposal.GuildName)
	packet.WriteByte(proposal.Mode)
	packet.WriteUInt32(proposal.WarPeriodID)
	packet.WriteByte(proposal.VictoryPointID)
	packet.WriteUInt32(proposal.Wager)
	return packet
}

func BuildGuildWarStartResult(errorCode uint16) network.Packet {
	return buildGuildWarResult(opcode.GuildWarStartResponse, errorCode)
}

func BuildGuildWarEndResult(errorCode uint16) network.Packet {
	return buildGuildWarResult(opcode.GuildWarEndResponse, errorCode)
}

func DecodeGuildWarEndRequest(packet *network.Packet) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid guild war end request")
	}
	warID, err := packet.ReadUInt32()
	if err != nil || warID == 0 || packet.Remaining() != 0 {
		return 0, fmt.Errorf("invalid guild war id")
	}
	return warID, nil
}

func BuildGuildWarEndRequest(warID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildWarEndRequest
	packet.WriteUInt32(warID)
	return packet
}

func DecodeGuildWarRewardRequest(packet *network.Packet) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid guild war reward request")
	}
	warID, err := packet.ReadUInt32()
	if err != nil || warID == 0 || packet.Remaining() != 0 {
		return 0, fmt.Errorf("invalid guild war id")
	}
	return warID, nil
}

func BuildGuildWarRewardRequest(warID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildWarRewardRequest
	p.WriteUInt32(warID)
	return p
}

func BuildGuildWarRewardResult(reward uint32, errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildWarRewardResponse
	if errorCode == 0 {
		p.WriteByte(1)
		p.WriteUInt32(reward)
	} else {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
	}
	return p
}

func DecodeGuildWarCompensationClaimRequest(packet *network.Packet) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid guild war compensation claim request")
	}
	return packet.ReadUInt32()
}

func BuildGuildWarCompensationClaimRequest(reward uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildWarCompensationClaimRequest
	p.WriteUInt32(reward)
	return p
}

func BuildGuildWarCompensationClaimResult(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildWarCompensationClaimResponse
	if errorCode == 0 {
		p.WriteByte(1)
	} else {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
	}
	return p
}

func buildGuildWarResult(messageID, errorCode uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	if errorCode == 0 {
		packet.WriteByte(1)
	} else {
		packet.WriteByte(2)
		packet.WriteUInt16(errorCode)
	}
	return packet
}
