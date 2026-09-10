package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type EntitySelectRequest struct{ UniqueID uint32 }
type EntityPlayerSelectResponse struct {
	UniqueID    uint32
	JobType     byte
	JobLevel    byte
	HasJobLevel bool
}
type EntityDeselectRequest struct{ UniqueID uint32 }
type CharacterAttributeIncreaseRequest struct{}

type EntitySelectRequestCodec struct{}
type EntityPlayerSelectResponseCodec struct{}
type EntityDeselectRequestCodec struct{}
type CharacterAttributeIncreaseRequestCodec struct{}

func (EntitySelectRequestCodec) Decode(packet network.Packet) (EntitySelectRequest, error) {
	uid, err := decodeExactUID(packet, "entity selection")
	return EntitySelectRequest{UniqueID: uid}, err
}

// Encode writes the player-specific success body consumed by the stock B045
// handler: accepted, target UID, two player metadata bytes, then job type and,
// for active job states, job level.
func (EntityPlayerSelectResponseCodec) Encode(response EntityPlayerSelectResponse) (network.Packet, error) {
	active := response.JobType >= JobTrader && response.JobType <= JobHunter
	if active != response.HasJobLevel || (!active && response.JobType != JobNone) || (active && response.JobLevel == 0) {
		return network.Packet{}, fmt.Errorf("invalid player selection job state type=%d level=%d hasLevel=%t", response.JobType, response.JobLevel, response.HasJobLevel)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntitySelectResponse
	for _, write := range []func() error{
		func() error { return packet.WriteByte(1) },
		func() error { return packet.WriteUInt32(response.UniqueID) },
		func() error { return packet.WriteByte(1) },
		func() error { return packet.WriteByte(5) },
		func() error { return packet.WriteByte(response.JobType) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, fmt.Errorf("write player selection response: %w", err)
		}
	}
	if response.HasJobLevel {
		if err := packet.WriteByte(response.JobLevel); err != nil {
			return network.Packet{}, fmt.Errorf("write player selection job level: %w", err)
		}
	}
	return packet, nil
}

func (EntityDeselectRequestCodec) Decode(packet network.Packet) (EntityDeselectRequest, error) {
	uid, err := decodeExactUID(packet, "entity deselection")
	return EntityDeselectRequest{UniqueID: uid}, err
}

func (CharacterAttributeIncreaseRequestCodec) Decode(packet network.Packet) (CharacterAttributeIncreaseRequest, error) {
	if packet.Remaining() != 0 {
		return CharacterAttributeIncreaseRequest{}, fmt.Errorf("character attribute request has %d trailing bytes", packet.Remaining())
	}
	return CharacterAttributeIncreaseRequest{}, nil
}

func decodeExactUID(packet network.Packet, name string) (uint32, error) {
	if packet.Remaining() != 4 {
		return 0, fmt.Errorf("%s request payload length %d, want 4", name, packet.Remaining())
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return 0, fmt.Errorf("read %s target: %w", name, err)
	}
	if packet.Remaining() != 0 {
		return 0, fmt.Errorf("%s request has %d trailing bytes", name, packet.Remaining())
	}
	return uid, nil
}
