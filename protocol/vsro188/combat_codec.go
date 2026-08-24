package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	CombatActionPhaseStart    byte = 1
	CombatActionTypeCandidate byte = 4
)

// CombatCandidateActionRequest is intentionally semantic-neutral: the same
// five-byte tail can name a basic-attack target or a no-target skill. Reference
// data, not the packet codec, decides which meaning applies.
type CombatCandidateActionRequest struct {
	CandidateID uint32
}

type CombatCandidateActionRequestCodec struct{}

type CombatActionStateResponse struct{ Active bool }
type CombatActionStateResponseCodec struct{}

type CombatAttackBeginSuccessPrefix struct {
	SkillID        uint32
	SourceUniqueID uint32
	CastUniqueID   uint32
	TargetUniqueID uint32
}

type CombatAttackEndSuccessPrefix struct {
	CastUniqueID   uint32
	TargetUniqueID uint32
}

type CombatAttackBeginSuccessPrefixCodec struct{}
type CombatAttackEndSuccessPrefixCodec struct{}

func (CombatCandidateActionRequestCodec) Decode(packet network.Packet) (CombatCandidateActionRequest, error) {
	if packet.Remaining() != 7 {
		return CombatCandidateActionRequest{}, fmt.Errorf("combat candidate payload length %d, want 7", packet.Remaining())
	}
	phase, err := packet.ReadByte()
	if err != nil {
		return CombatCandidateActionRequest{}, fmt.Errorf("read combat action phase: %w", err)
	}
	if phase != CombatActionPhaseStart {
		return CombatCandidateActionRequest{}, fmt.Errorf("combat candidate phase %d, want %d", phase, CombatActionPhaseStart)
	}
	actionType, err := packet.ReadByte()
	if err != nil {
		return CombatCandidateActionRequest{}, fmt.Errorf("read combat action type: %w", err)
	}
	if actionType != CombatActionTypeCandidate {
		return CombatCandidateActionRequest{}, fmt.Errorf("combat candidate action type %d, want %d", actionType, CombatActionTypeCandidate)
	}
	candidateID, err := packet.ReadUInt32()
	if err != nil {
		return CombatCandidateActionRequest{}, fmt.Errorf("read combat candidate ID: %w", err)
	}
	terminator, err := packet.ReadByte()
	if err != nil {
		return CombatCandidateActionRequest{}, fmt.Errorf("read combat candidate terminator: %w", err)
	}
	if candidateID == 0 || terminator != 0 {
		return CombatCandidateActionRequest{}, fmt.Errorf("combat candidate ID or terminator is invalid")
	}
	return CombatCandidateActionRequest{CandidateID: candidateID}, nil
}

func (CombatActionStateResponseCodec) Encode(response CombatActionStateResponse) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityActionResponse
	state, recurring := byte(2), byte(0)
	if response.Active {
		state, recurring = 1, 1
	}
	if err := packet.WriteByte(state); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteByte(recurring); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func (CombatAttackBeginSuccessPrefixCodec) Encode(response CombatAttackBeginSuccessPrefix) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntitySkillCastBegin
	for _, write := range []func() error{
		func() error { return packet.WriteByte(1) },
		func() error { return packet.WriteUInt16(0x3002) },
		func() error { return packet.WriteUInt32(response.SkillID) },
		func() error { return packet.WriteUInt32(response.SourceUniqueID) },
		func() error { return packet.WriteUInt32(response.CastUniqueID) },
		func() error { return packet.WriteUInt32(response.TargetUniqueID) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}

func (CombatAttackEndSuccessPrefixCodec) Encode(response CombatAttackEndSuccessPrefix) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntitySkillCastEnd
	if err := packet.WriteByte(1); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt32(response.CastUniqueID); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt32(response.TargetUniqueID); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}
