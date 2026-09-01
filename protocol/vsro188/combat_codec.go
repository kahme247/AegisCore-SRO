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

// CombatAttackBeginResponse is the exact prefix consumed by the stock B070
// handler. The remainder is delegated to the action-state subsystem; it is
// deliberately retained as opaque bytes until that continuation is fully
// recovered.
type CombatAttackBeginResponse struct {
	Result       byte
	FailureCode  uint16
	ResultFamily uint16
	SkillID      uint32
	SourceUID    uint32
	CastUID      uint32
	TargetUID    uint32
	Remainder    []byte
}

// CombatAttackEndResponse is the exact prefix consumed by the stock B071
// handler. Its success body is delegated to the action-state subsystem.
type CombatAttackEndResponse struct {
	Result      byte
	FailureCode uint16
	CastUID     uint32
	Remainder   []byte
}

func DecodeCombatAttackBeginResponse(packet network.Packet) (CombatAttackBeginResponse, error) {
	result, err := packet.ReadByte()
	if err != nil {
		return CombatAttackBeginResponse{}, fmt.Errorf("read B070 result: %w", err)
	}
	if result != 1 {
		if packet.Remaining() != 2 {
			return CombatAttackBeginResponse{}, fmt.Errorf("B070 failure payload length %d, want 2", packet.Remaining())
		}
		code, err := packet.ReadUInt16()
		if err != nil {
			return CombatAttackBeginResponse{}, fmt.Errorf("read B070 failure code: %w", err)
		}
		return CombatAttackBeginResponse{Result: result, FailureCode: code}, nil
	}
	if packet.Remaining() < 18 {
		return CombatAttackBeginResponse{}, fmt.Errorf("B070 success payload length %d, want at least 18", packet.Remaining())
	}
	family, err := packet.ReadUInt16()
	if err != nil {
		return CombatAttackBeginResponse{}, fmt.Errorf("read B070 result family: %w", err)
	}
	if family != 0x3000 && family != 0x3002 {
		return CombatAttackBeginResponse{}, fmt.Errorf("B070 result family 0x%04X is not 0x3000 or 0x3002", family)
	}
	values := [4]uint32{}
	for index := range values {
		values[index], err = packet.ReadUInt32()
		if err != nil {
			return CombatAttackBeginResponse{}, fmt.Errorf("read B070 direct field %d: %w", index, err)
		}
	}
	remainder, err := packet.ReadBytes(packet.Remaining())
	if err != nil {
		return CombatAttackBeginResponse{}, fmt.Errorf("read B070 delegated remainder: %w", err)
	}
	return CombatAttackBeginResponse{Result: result, ResultFamily: family, SkillID: values[0], SourceUID: values[1], CastUID: values[2], TargetUID: values[3], Remainder: remainder}, nil
}

func DecodeCombatAttackEndResponse(packet network.Packet) (CombatAttackEndResponse, error) {
	result, err := packet.ReadByte()
	if err != nil {
		return CombatAttackEndResponse{}, fmt.Errorf("read B071 result: %w", err)
	}
	if result != 1 {
		if packet.Remaining() < 6 {
			return CombatAttackEndResponse{}, fmt.Errorf("B071 non-success payload length %d, want at least 6", packet.Remaining())
		}
		code, err := packet.ReadUInt16()
		if err != nil {
			return CombatAttackEndResponse{}, fmt.Errorf("read B071 failure code: %w", err)
		}
		cast, err := packet.ReadUInt32()
		if err != nil {
			return CombatAttackEndResponse{}, fmt.Errorf("read B071 action ID: %w", err)
		}
		remainder, _ := packet.ReadBytes(packet.Remaining())
		return CombatAttackEndResponse{Result: result, FailureCode: code, CastUID: cast, Remainder: remainder}, nil
	}
	if packet.Remaining() < 4 {
		return CombatAttackEndResponse{}, fmt.Errorf("B071 success payload length %d, want at least 4", packet.Remaining())
	}
	cast, err := packet.ReadUInt32()
	if err != nil {
		return CombatAttackEndResponse{}, fmt.Errorf("read B071 cast ID: %w", err)
	}
	remainder, _ := packet.ReadBytes(packet.Remaining())
	return CombatAttackEndResponse{Result: result, CastUID: cast, Remainder: remainder}, nil
}

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
