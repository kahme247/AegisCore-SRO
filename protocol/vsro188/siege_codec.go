package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	SiegeActionApplicationApply  byte = 7
	SiegeActionApplicationCancel byte = 8

	SiegeRequestAttack byte = 1
	SiegeRequestDefend byte = 2

	SiegeUpdateApplicationApply  byte = 12
	SiegeUpdateApplicationCancel byte = 13
	SiegeUpdateBattleBegin       byte = 2
	SiegeUpdateBattleEnded       byte = 6
	SiegeUpdateOccupied          byte = 8
	SiegeUpdateStructureState    byte = 11
	SiegeUpdateBattleRecord      byte = 17
	SiegeUpdateTaxPeriodBegin    byte = 49
	SiegeUpdateTaxPeriodEnd      byte = 50
	SiegeUpdateApplyPeriodBegin  byte = 51
	SiegeUpdateApplyPeriodEnd    byte = 52
)

type SiegeApplicationRequest struct {
	Action      byte
	RequestType byte
}

// DecodeSiegeApplicationRequest accepts the two application mutations whose
// layouts are established by the vSRO 1.188 GameServer. Other siege actions
// remain fail-closed until their action-specific tails are recovered.
func DecodeSiegeApplicationRequest(packet *network.Packet) (SiegeApplicationRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return SiegeApplicationRequest{}, fmt.Errorf("invalid siege action request")
	}
	action, err := packet.ReadByte()
	if err != nil {
		return SiegeApplicationRequest{}, err
	}
	request := SiegeApplicationRequest{Action: action}
	switch action {
	case SiegeActionApplicationApply:
		if packet.Remaining() != 1 {
			return SiegeApplicationRequest{}, fmt.Errorf("invalid siege application request")
		}
		request.RequestType, err = packet.ReadByte()
		if err != nil {
			return SiegeApplicationRequest{}, err
		}
		if request.RequestType != SiegeRequestAttack && request.RequestType != SiegeRequestDefend {
			return SiegeApplicationRequest{}, fmt.Errorf("invalid siege request type %d", request.RequestType)
		}
	case SiegeActionApplicationCancel:
		if packet.Remaining() != 0 {
			return SiegeApplicationRequest{}, fmt.Errorf("invalid siege cancellation request")
		}
	default:
		return SiegeApplicationRequest{}, fmt.Errorf("unsupported siege action %d", action)
	}
	return request, nil
}

func BuildSiegeActionFailure(action byte, errorCode uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SiegeActionResponse
	packet.WriteByte(2)
	packet.WriteByte(action)
	packet.WriteUInt16(errorCode)
	return packet
}

func BuildSiegeApplicationUpdate(fortressID uint32, requestType byte, cancelled bool) (network.Packet, error) {
	if fortressID == 0 || (requestType != SiegeRequestAttack && requestType != SiegeRequestDefend) {
		return network.Packet{}, fmt.Errorf("invalid siege application update")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SiegeUpdate
	if cancelled {
		packet.WriteByte(SiegeUpdateApplicationCancel)
	} else {
		packet.WriteByte(SiegeUpdateApplicationApply)
	}
	packet.WriteUInt32(fortressID)
	packet.WriteByte(requestType)
	return packet, nil
}

func BuildSiegePeriodUpdate(kind byte) (network.Packet, error) {
	switch kind {
	case SiegeUpdateBattleBegin, SiegeUpdateBattleEnded, SiegeUpdateTaxPeriodBegin, SiegeUpdateTaxPeriodEnd, SiegeUpdateApplyPeriodBegin, SiegeUpdateApplyPeriodEnd:
	default:
		return network.Packet{}, fmt.Errorf("invalid siege period update %d", kind)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SiegeUpdate
	packet.WriteByte(kind)
	return packet, nil
}

func BuildSiegeStructureStateUpdate(fortressID, uniqueID, refEventStructureID uint32, state uint16, heart bool, occupyingGuild string) (network.Packet, error) {
	if fortressID == 0 || uniqueID == 0 || refEventStructureID == 0 || state > 2 || (!heart && occupyingGuild != "") {
		return network.Packet{}, fmt.Errorf("invalid siege structure update")
	}
	if err := ValidateText(occupyingGuild, TextRules{Field: "occupying guild", MaxRunes: 20, RequireTrimmed: true, AllowEmpty: true}); err != nil {
		return network.Packet{}, err
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SiegeUpdate
	packet.WriteByte(SiegeUpdateStructureState)
	packet.WriteUInt32(fortressID)
	packet.WriteUInt32(uniqueID)
	packet.WriteUInt32(refEventStructureID)
	packet.WriteUInt16(state)
	if heart {
		packet.WriteString(occupyingGuild)
	}
	return packet, nil
}

func BuildSiegeBattleRecordUpdate(fortressID, kills, deaths uint32) (network.Packet, error) {
	if fortressID == 0 {
		return network.Packet{}, fmt.Errorf("invalid siege battle record")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SiegeUpdate
	packet.WriteByte(SiegeUpdateBattleRecord)
	packet.WriteUInt32(fortressID)
	packet.WriteUInt32(kills)
	packet.WriteUInt32(deaths)
	return packet, nil
}

type SiegeOccupation struct {
	FortressID      uint32
	GuildName       string
	GuildMasterName string
	Introduction    string
	GuildID         uint32
	GuildCrest      uint32
	UnionID         uint32
	UnionCrest      uint32
}

func BuildSiegeOccupiedUpdate(value SiegeOccupation) (network.Packet, error) {
	if value.FortressID == 0 || value.GuildID == 0 {
		return network.Packet{}, fmt.Errorf("invalid siege occupation")
	}
	for field, text := range map[string]string{"guild name": value.GuildName, "guild master": value.GuildMasterName, "fortress introduction": value.Introduction} {
		if err := ValidateText(text, TextRules{Field: field, MaxRunes: 128, RequireTrimmed: true, AllowEmpty: field == "fortress introduction"}); err != nil {
			return network.Packet{}, err
		}
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SiegeUpdate
	packet.WriteByte(SiegeUpdateOccupied)
	packet.WriteUInt32(value.FortressID)
	packet.WriteString(value.GuildName)
	packet.WriteString(value.GuildMasterName)
	packet.WriteString(value.Introduction)
	packet.WriteUInt32(value.GuildID)
	packet.WriteUInt32(value.GuildCrest)
	packet.WriteUInt32(value.UnionID)
	packet.WriteUInt32(value.UnionCrest)
	return packet, nil
}
