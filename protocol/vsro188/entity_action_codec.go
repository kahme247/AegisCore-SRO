package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// The stock 0x7074 dispatcher uses these numeric branches. The names are
// descriptive; the binary confirms the widths/order and branch values, not
// every gameplay meaning.
const (
	EntityActionKindTarget         = 4
	EntityActionKindRemove         = 5
	EntityActionTargetModeEntity   = 1
	EntityActionTargetModePosition = 2
)

type EntityActionStopRequest struct{}
type EntityActionTargetRequest struct{ TargetUID uint32 }
type EntityActionSkillTargetRequest struct {
	SkillID   uint32
	TargetUID uint32
}
type EntityActionPositionSkillRequest struct {
	SkillID uint32
	Region  int16
	X       int32
	Z       int32
	Y       int32
}
type EntityActionRemoveBuffRequest struct{ SkillID uint32 }

type EntityActionStopRequestCodec struct{}
type EntityActionTargetRequestCodec struct{}
type EntityActionSkillTargetRequestCodec struct{}
type EntityActionPositionSkillRequestCodec struct{}
type EntityActionRemoveBuffRequestCodec struct{}

func newEntityActionPacket() network.Packet {
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.EntityActionRequest
	return packet
}

func (EntityActionStopRequestCodec) Encode(EntityActionStopRequest) (network.Packet, error) {
	packet := newEntityActionPacket()
	if err := packet.WriteByte(CombatActionPhaseStart); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func (EntityActionTargetRequestCodec) Encode(request EntityActionTargetRequest) (network.Packet, error) {
	packet := newEntityActionPacket()
	for _, write := range []func() error{
		func() error { return packet.WriteByte(CombatActionPhaseStart) },
		func() error { return packet.WriteByte(EntityActionKindTarget) },
		func() error { return packet.WriteUInt32(request.TargetUID) },
		func() error { return packet.WriteByte(0) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}

func (EntityActionSkillTargetRequestCodec) Encode(request EntityActionSkillTargetRequest) (network.Packet, error) {
	packet := newEntityActionPacket()
	for _, write := range []func() error{
		func() error { return packet.WriteByte(CombatActionPhaseStart) },
		func() error { return packet.WriteByte(EntityActionKindTarget) },
		func() error { return packet.WriteUInt32(request.SkillID) },
		func() error { return packet.WriteByte(EntityActionTargetModeEntity) },
		func() error { return packet.WriteUInt32(request.TargetUID) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}

func (EntityActionPositionSkillRequestCodec) Encode(request EntityActionPositionSkillRequest) (network.Packet, error) {
	packet := newEntityActionPacket()
	for _, write := range []func() error{
		func() error { return packet.WriteByte(CombatActionPhaseStart) },
		func() error { return packet.WriteByte(EntityActionKindTarget) },
		func() error { return packet.WriteUInt32(request.SkillID) },
		func() error { return packet.WriteByte(EntityActionTargetModePosition) },
		func() error { return packet.WriteUInt16(uint16(request.Region)) },
		func() error { return packet.WriteUInt32(uint32(request.X)) },
		func() error { return packet.WriteUInt32(uint32(request.Z)) },
		func() error { return packet.WriteUInt32(uint32(request.Y)) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}

func (EntityActionRemoveBuffRequestCodec) Encode(request EntityActionRemoveBuffRequest) (network.Packet, error) {
	packet := newEntityActionPacket()
	for _, write := range []func() error{
		func() error { return packet.WriteByte(CombatActionPhaseStart) },
		func() error { return packet.WriteByte(EntityActionKindRemove) },
		func() error { return packet.WriteUInt32(request.SkillID) },
		func() error { return packet.WriteByte(0) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, fmt.Errorf("write 0x7074 remove-buff request: %w", err)
		}
	}
	return packet, nil
}
