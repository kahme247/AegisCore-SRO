package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const ForgottenWorldRecallLimit = 20

type RecallChallenge struct {
	ChallengeID uint32
	Left        uint32
	Right       uint32
}

type RecallSolveRequest struct {
	ChallengeID uint32
	Answer      uint32
	Mode        byte
}

type ForgottenWorldRecallResponse struct {
	RecallID uint32
	MemberID uint32
	Accepted bool
}

type ForgottenWorldRecallListEntry struct {
	MemberID uint32
	State    byte
	Level    byte
	RegionID uint16
}

type ForgottenWorldUpdate struct {
	MemberID uint32
	State    byte
}

func DecodeRecallSolveRequest(packet *network.Packet) (RecallSolveRequest, error) {
	if packet == nil || packet.Remaining() != 9 {
		return RecallSolveRequest{}, fmt.Errorf("invalid recall solve request")
	}
	challengeID, err := packet.ReadUInt32()
	if err != nil {
		return RecallSolveRequest{}, err
	}
	answer, err := packet.ReadUInt32()
	if err != nil {
		return RecallSolveRequest{}, err
	}
	mode, err := packet.ReadByte()
	if err != nil {
		return RecallSolveRequest{}, err
	}
	return RecallSolveRequest{ChallengeID: challengeID, Answer: answer, Mode: mode}, nil
}

func DecodeForgottenWorldRecallListRequest(packet *network.Packet) (uint32, error) {
	return decodeForgottenWorldUint32Request(packet, "recall list")
}

func DecodeForgottenWorldRecallMemberRequest(packet *network.Packet) (uint32, error) {
	return decodeForgottenWorldUint32Request(packet, "recall member")
}

func DecodeForgottenWorldExitRequest(packet *network.Packet) (uint32, error) {
	return decodeForgottenWorldUint32Request(packet, "exit")
}

func decodeForgottenWorldUint32Request(packet *network.Packet, name string) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid forgotten world %s request", name)
	}
	return packet.ReadUInt32()
}

func DecodeForgottenWorldRecallResponse(packet *network.Packet) (ForgottenWorldRecallResponse, error) {
	if packet == nil || packet.Remaining() != 9 {
		return ForgottenWorldRecallResponse{}, fmt.Errorf("invalid forgotten world recall response")
	}
	recallID, err := packet.ReadUInt32()
	if err != nil {
		return ForgottenWorldRecallResponse{}, err
	}
	memberID, err := packet.ReadUInt32()
	if err != nil {
		return ForgottenWorldRecallResponse{}, err
	}
	decision, err := packet.ReadByte()
	if err != nil || decision > 1 {
		return ForgottenWorldRecallResponse{}, fmt.Errorf("invalid forgotten world recall decision")
	}
	return ForgottenWorldRecallResponse{RecallID: recallID, MemberID: memberID, Accepted: decision == 1}, nil
}

func BuildRecallChallenge(challenge RecallChallenge) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.RecallChallenge
	p.WriteUInt32(challenge.ChallengeID)
	p.WriteUInt32(challenge.Left)
	p.WriteUInt32(challenge.Right)
	return p
}

func BuildForgottenWorldUpdate(update ForgottenWorldUpdate) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.FGWUpdate
	if update.MemberID == 0 || update.State > 1 {
		return p, fmt.Errorf("invalid forgotten world update")
	}
	p.WriteUInt32(update.MemberID)
	p.WriteByte(0x10)
	p.WriteByte(update.State)
	return p, nil
}

func BuildForgottenWorldRecallInvitation(recallID, memberID, worldID uint32, inviterName string) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.FGWRecallRequest
	if recallID == 0 || memberID == 0 || worldID == 0 {
		return p, fmt.Errorf("invalid forgotten world recall invitation")
	}
	if err := ValidateText(inviterName, TextRules{Field: "forgotten world inviter", MaxRunes: 64, RequireTrimmed: true}); err != nil {
		return p, err
	}
	p.WriteUInt32(recallID)
	p.WriteUInt32(memberID)
	p.WriteUInt32(worldID)
	p.WriteString(inviterName)
	return p, nil
}

func BuildRecallResult(messageID uint16, errorCode uint16) network.Packet {
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

func BuildForgottenWorldRecallList(entries []ForgottenWorldRecallListEntry, errorCode uint16) (network.Packet, error) {
	p := BuildRecallResult(opcode.FGWRecallListResponse, errorCode)
	if errorCode != 0 {
		return p, nil
	}
	if len(entries) > ForgottenWorldRecallLimit || len(entries) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many forgotten world recall members")
	}
	p.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		if entry.MemberID == 0 {
			return network.Packet{}, fmt.Errorf("invalid forgotten world recall member")
		}
		p.WriteUInt32(entry.MemberID)
		p.WriteByte(entry.State)
		p.WriteByte(entry.Level)
		p.WriteUInt16(entry.RegionID)
	}
	return p, nil
}
