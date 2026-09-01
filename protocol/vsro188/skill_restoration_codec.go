package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	SkillRestorationSuccess byte = 1
	SkillRestorationFailure byte = 2
)

// SkillRestorationRequest is the common nine-byte request body used by
// 0x7202 and 0x7203. ItemRefID is the ITEMDATA reference ID, not an inventory
// slot or ITEM.ID instance key.
type SkillRestorationRequest struct {
	ItemRefID   uint32
	TargetID    uint32
	TargetLevel byte
}

func DecodeSkillRestorationRequest(packet *network.Packet) (SkillRestorationRequest, error) {
	if packet == nil || packet.Remaining() != 9 {
		remaining := 0
		if packet != nil {
			remaining = packet.Remaining()
		}
		return SkillRestorationRequest{}, fmt.Errorf("skill restoration payload length %d, want 9", remaining)
	}
	itemRefID, err := packet.ReadUInt32()
	if err != nil {
		return SkillRestorationRequest{}, err
	}
	targetID, err := packet.ReadUInt32()
	if err != nil {
		return SkillRestorationRequest{}, err
	}
	targetLevel, err := packet.ReadByte()
	if err != nil {
		return SkillRestorationRequest{}, err
	}
	return SkillRestorationRequest{ItemRefID: itemRefID, TargetID: targetID, TargetLevel: targetLevel}, nil
}

func BuildSkillWithdrawSuccess(resultingSkillID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SkillWithdrawResponse
	packet.WriteByte(SkillRestorationSuccess)
	packet.WriteUInt32(resultingSkillID)
	return packet
}

// DecodeSkillWithdrawSuccess decodes the confirmed success body for 0xB202.
func DecodeSkillWithdrawSuccess(packet *network.Packet) (uint32, error) {
	if packet == nil {
		return 0, fmt.Errorf("0xB202 skill-withdraw packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read 0xB202 result: %w", err)
	}
	if result != SkillRestorationSuccess {
		return 0, fmt.Errorf("0xB202 result is %d, want success result 1", result)
	}
	resultingSkillID, err := packet.ReadUInt32()
	if err != nil {
		return 0, fmt.Errorf("read 0xB202 resulting skill: %w", err)
	}
	if packet.Remaining() != 0 {
		return 0, fmt.Errorf("0xB202 success has %d trailing bytes", packet.Remaining())
	}
	return resultingSkillID, nil
}

func BuildMasteryWithdrawSuccess(masteryID uint32, level byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.MasteryWithdrawResponse
	packet.WriteByte(SkillRestorationSuccess)
	packet.WriteUInt32(masteryID)
	packet.WriteByte(level)
	return packet
}

// DecodeMasteryWithdrawSuccess decodes the confirmed success body for 0xB203.
func DecodeMasteryWithdrawSuccess(packet *network.Packet) (uint32, byte, error) {
	if packet == nil {
		return 0, 0, fmt.Errorf("0xB203 mastery-withdraw packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB203 result: %w", err)
	}
	if result != SkillRestorationSuccess {
		return 0, 0, fmt.Errorf("0xB203 result is %d, want success result 1", result)
	}
	masteryID, err := packet.ReadUInt32()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB203 mastery: %w", err)
	}
	level, err := packet.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB203 mastery level: %w", err)
	}
	if packet.Remaining() != 0 {
		return 0, 0, fmt.Errorf("0xB203 success has %d trailing bytes", packet.Remaining())
	}
	return masteryID, level, nil
}

func BuildSkillRestorationFailure(responseOpcode, code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = responseOpcode
	packet.WriteByte(SkillRestorationFailure)
	packet.WriteUInt16(code)
	return packet
}

// DecodeSkillRestorationFailure decodes the shared confirmed failure body for
// either dedicated restoration response opcode. The caller owns response
// opcode routing; this function owns only the result/code payload.
func DecodeSkillRestorationFailure(packet *network.Packet) (uint16, error) {
	if packet == nil {
		return 0, fmt.Errorf("skill-restoration failure packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read skill-restoration failure result: %w", err)
	}
	if result != SkillRestorationFailure {
		return 0, fmt.Errorf("skill-restoration result is %d, want failure result 2", result)
	}
	code, err := packet.ReadUInt16()
	if err != nil {
		return 0, fmt.Errorf("read skill-restoration failure code: %w", err)
	}
	if packet.Remaining() != 0 {
		return 0, fmt.Errorf("skill-restoration failure has %d trailing bytes", packet.Remaining())
	}
	return code, nil
}
