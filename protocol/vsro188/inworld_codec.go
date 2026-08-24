package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
)

type EntitySelectRequest struct{ UniqueID uint32 }
type EntityDeselectRequest struct{ UniqueID uint32 }
type CharacterAttributeIncreaseRequest struct{}

type EntitySelectRequestCodec struct{}
type EntityDeselectRequestCodec struct{}
type CharacterAttributeIncreaseRequestCodec struct{}

func (EntitySelectRequestCodec) Decode(packet network.Packet) (EntitySelectRequest, error) {
	uid, err := decodeExactUID(packet, "entity selection")
	return EntitySelectRequest{UniqueID: uid}, err
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
