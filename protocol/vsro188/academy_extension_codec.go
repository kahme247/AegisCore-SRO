package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type AcademyInvitationResponse struct {
	AcademyID uint32
	LeaderID  uint32
	Accepted  bool
}

type AcademyTimedUpdate struct {
	DurationMinutes uint32
	Reference       uint32
	Value           uint32
}

func DecodeAcademyInviteRequest(packet *network.Packet) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid academy invite request")
	}
	return packet.ReadUInt32()
}

func DecodeAcademyMemberActionRequest(packet *network.Packet) (string, error) {
	if packet == nil {
		return "", fmt.Errorf("invalid academy member action request")
	}
	name, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 || ValidateText(name, TextRules{Field: "academy member", MaxRunes: 64, RequireTrimmed: true}) != nil {
		return "", fmt.Errorf("invalid academy member action request")
	}
	return name, nil
}

func DecodeAcademyInvitationResponse(packet *network.Packet) (AcademyInvitationResponse, error) {
	if packet == nil || packet.Remaining() != 9 {
		return AcademyInvitationResponse{}, fmt.Errorf("invalid academy invitation response")
	}
	academyID, err := packet.ReadUInt32()
	if err != nil {
		return AcademyInvitationResponse{}, err
	}
	leaderID, err := packet.ReadUInt32()
	if err != nil {
		return AcademyInvitationResponse{}, err
	}
	decision, err := packet.ReadByte()
	if err != nil || decision > 1 {
		return AcademyInvitationResponse{}, fmt.Errorf("invalid academy invitation decision")
	}
	return AcademyInvitationResponse{AcademyID: academyID, LeaderID: leaderID, Accepted: decision == 1}, nil
}

func DecodeAcademyUnknownOperationRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy operation request")
	}
	return nil
}

func BuildAcademyInvitation(academyID, leaderID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AcademyMatchingResponse
	p.WriteUInt32(academyID)
	p.WriteUInt32(leaderID)
	return p
}

func BuildAcademyExtensionResult(messageID uint16, errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	if errorCode == 0 {
		p.WriteByte(1)
	} else {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
	}
	return p
}

func BuildAcademyTimedOperation(updates []AcademyTimedUpdate, errorCode uint16) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.AcademyUnknownOperationResponse
	if errorCode != 0 {
		p.WriteByte(0x0A)
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
		return p, nil
	}
	if len(updates) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many academy timed updates")
	}
	p.WriteByte(0)
	p.WriteByte(byte(len(updates)))
	for _, update := range updates {
		p.WriteUInt32(update.DurationMinutes)
		p.WriteUInt32(update.Reference)
		p.WriteUInt32(update.Value)
	}
	return p, nil
}

func BuildAcademyUpdate(action byte, characterID uint32, state byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AcademyUpdate
	p.WriteByte(action)
	if action != 1 {
		p.WriteUInt32(characterID)
		p.WriteByte(state)
	}
	return p
}

func BuildAcademyUnknownUpdate3C86(durationMinutes uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AcademyUnknownUpdate3C86
	p.WriteUInt32(durationMinutes)
	return p
}

func BuildAcademyUnknownUpdate3C87(durationMinutes, reference uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AcademyUnknownUpdate3C87
	p.WriteUInt32(durationMinutes)
	p.WriteUInt32(reference)
	return p
}
