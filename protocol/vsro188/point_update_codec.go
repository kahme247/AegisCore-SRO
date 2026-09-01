package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// GoldPointUpdate is the maintained gold variant of entity opcode 0x304E.
// The current client-facing contract writes kind=1, a u64 value, and one
// trailing flag byte. Other 0x304E kinds are intentionally not folded into
// this type because their widths and meanings differ.
type GoldPointUpdate struct {
	Value        uint64
	TrailingFlag byte
}

// SkillPointUpdate is the maintained skill-point variant of 0x304E. It uses
// kind=2, a u32 value, and the same trailing flag byte as the gold variant.
type SkillPointUpdate struct {
	Value        uint32
	TrailingFlag byte
}

func BuildGoldPointUpdate(update GoldPointUpdate) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdatePoints
	packet.WriteByte(1)
	packet.WriteUInt64(update.Value)
	packet.WriteByte(update.TrailingFlag)
	return packet
}

func DecodeGoldPointUpdate(packet *network.Packet) (GoldPointUpdate, error) {
	if packet == nil {
		return GoldPointUpdate{}, fmt.Errorf("0x304E gold packet is nil")
	}
	kind, err := packet.ReadByte()
	if err != nil {
		return GoldPointUpdate{}, fmt.Errorf("read 0x304E gold kind: %w", err)
	}
	if kind != 1 {
		return GoldPointUpdate{}, fmt.Errorf("0x304E kind is %d, want gold kind 1", kind)
	}
	value, err := packet.ReadUInt64()
	if err != nil {
		return GoldPointUpdate{}, fmt.Errorf("read 0x304E gold value: %w", err)
	}
	flag, err := packet.ReadByte()
	if err != nil {
		return GoldPointUpdate{}, fmt.Errorf("read 0x304E gold trailing flag: %w", err)
	}
	if packet.Remaining() != 0 {
		return GoldPointUpdate{}, fmt.Errorf("0x304E gold packet has %d trailing bytes", packet.Remaining())
	}
	return GoldPointUpdate{Value: value, TrailingFlag: flag}, nil
}

func BuildSkillPointUpdate(update SkillPointUpdate) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdatePoints
	packet.WriteByte(2)
	packet.WriteUInt32(update.Value)
	packet.WriteByte(update.TrailingFlag)
	return packet
}

func DecodeSkillPointUpdate(packet *network.Packet) (SkillPointUpdate, error) {
	if packet == nil {
		return SkillPointUpdate{}, fmt.Errorf("0x304E skill-point packet is nil")
	}
	kind, err := packet.ReadByte()
	if err != nil {
		return SkillPointUpdate{}, fmt.Errorf("read 0x304E skill-point kind: %w", err)
	}
	if kind != 2 {
		return SkillPointUpdate{}, fmt.Errorf("0x304E kind is %d, want skill-point kind 2", kind)
	}
	value, err := packet.ReadUInt32()
	if err != nil {
		return SkillPointUpdate{}, fmt.Errorf("read 0x304E skill-point value: %w", err)
	}
	flag, err := packet.ReadByte()
	if err != nil {
		return SkillPointUpdate{}, fmt.Errorf("read 0x304E skill-point trailing flag: %w", err)
	}
	if packet.Remaining() != 0 {
		return SkillPointUpdate{}, fmt.Errorf("0x304E skill-point packet has %d trailing bytes", packet.Remaining())
	}
	return SkillPointUpdate{Value: value, TrailingFlag: flag}, nil
}
