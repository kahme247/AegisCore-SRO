package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type EntityDirectionalMovement struct {
	Source  byte
	Heading uint16
}

type EntityDestinationMovement struct {
	RegionID int16
	X        int16
	Y        int16
	Z        int16
}

type EntityMovementRequest struct {
	HasDestination bool
	Directional    EntityDirectionalMovement
	Destination    EntityDestinationMovement
}

type EntityMovementRequestCodec struct{}
type EntityDirectionalMovementCodec struct{}
type EntityDestinationMovementCodec struct{}

type EntityDirectionalMovementResponse struct {
	EntityUniqueID uint32
	Source         byte
	Heading        uint16
}

type EntityDestinationMovementResponse struct {
	EntityUniqueID uint32
	TargetRegion   uint16
	TargetX        int16
	TargetY        int16
	TargetZ        int16
	SourceRegion   uint16
	SourceX        uint16
	SourceY        float32
	SourceZ        uint16
}

type EntityPositionUpdate struct {
	EntityUniqueID uint32
	Region         uint16
	X              float32
	Y              float32
	Z              float32
	Heading        uint16
}

type EntityDirectionalMovementResponseCodec struct{}
type EntityDestinationMovementResponseCodec struct{}
type EntityPositionUpdateCodec struct{}

func (EntityMovementRequestCodec) Decode(packet network.Packet) (EntityMovementRequest, error) {
	flag, err := packet.ReadByte()
	if err != nil {
		return EntityMovementRequest{}, fmt.Errorf("read movement destination flag: %w", err)
	}
	if flag > 1 {
		return EntityMovementRequest{}, fmt.Errorf("invalid movement destination flag %d", flag)
	}
	if flag == 0 {
		directional, err := (EntityDirectionalMovementCodec{}).Decode(packet)
		return EntityMovementRequest{Directional: directional}, err
	}
	destination, err := (EntityDestinationMovementCodec{}).Decode(packet)
	return EntityMovementRequest{HasDestination: true, Destination: destination}, err
}

// Decode accepts the pinned stock `heading:uint16` tail and the explicitly
// retained emulator compatibility `source:byte | heading:uint16` tail.
func (EntityDirectionalMovementCodec) Decode(packet network.Packet) (EntityDirectionalMovement, error) {
	request := EntityDirectionalMovement{Source: 1}
	switch packet.Remaining() {
	case 2:
	case 3:
		source, err := packet.ReadByte()
		if err != nil {
			return EntityDirectionalMovement{}, fmt.Errorf("read directional movement source: %w", err)
		}
		request.Source = source
	default:
		return EntityDirectionalMovement{}, fmt.Errorf("directional movement payload length %d, want 2 or 3", packet.Remaining())
	}
	heading, err := packet.ReadUInt16()
	if err != nil {
		return EntityDirectionalMovement{}, fmt.Errorf("read directional movement heading: %w", err)
	}
	request.Heading = heading
	return request, nil
}

func (EntityDestinationMovementCodec) Decode(packet network.Packet) (EntityDestinationMovement, error) {
	regionID, err := packet.ReadInt16()
	if err != nil {
		return EntityDestinationMovement{}, fmt.Errorf("read destination movement region: %w", err)
	}
	if regionID < 0 {
		return EntityDestinationMovement{}, fmt.Errorf("dungeon region %d is unsupported", regionID)
	}
	if packet.Remaining() != 6 {
		return EntityDestinationMovement{}, fmt.Errorf("region %d has destination payload length %d, want 6", regionID, packet.Remaining())
	}
	x, err := packet.ReadInt16()
	if err != nil {
		return EntityDestinationMovement{}, fmt.Errorf("read destination movement X: %w", err)
	}
	y, err := packet.ReadInt16()
	if err != nil {
		return EntityDestinationMovement{}, fmt.Errorf("read destination movement Y: %w", err)
	}
	z, err := packet.ReadInt16()
	if err != nil {
		return EntityDestinationMovement{}, fmt.Errorf("read destination movement Z: %w", err)
	}
	return EntityDestinationMovement{RegionID: regionID, X: x, Y: y, Z: z}, nil
}

func (EntityDirectionalMovementResponseCodec) Encode(response EntityDirectionalMovementResponse) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityMovementResponse
	for _, write := range []func() error{
		func() error { return packet.WriteUInt32(response.EntityUniqueID) },
		func() error { return packet.WriteByte(0) },
		func() error { return packet.WriteByte(response.Source) },
		func() error { return packet.WriteUInt16(response.Heading) },
		func() error { return packet.WriteByte(0) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}

func (EntityDestinationMovementResponseCodec) Encode(response EntityDestinationMovementResponse) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityMovementResponse
	for _, write := range []func() error{
		func() error { return packet.WriteUInt32(response.EntityUniqueID) },
		func() error { return packet.WriteByte(1) },
		func() error { return packet.WriteUInt16(response.TargetRegion) },
		func() error { return packet.WriteUInt16(uint16(response.TargetX)) },
		func() error { return packet.WriteUInt16(uint16(response.TargetY)) },
		func() error { return packet.WriteUInt16(uint16(response.TargetZ)) },
		func() error { return packet.WriteByte(1) },
		func() error { return packet.WriteUInt16(response.SourceRegion) },
		func() error { return packet.WriteUInt16(response.SourceX) },
		func() error { return packet.WriteFloat32(response.SourceY) },
		func() error { return packet.WriteUInt16(response.SourceZ) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}

func (EntityPositionUpdateCodec) Encode(response EntityPositionUpdate) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.MovementPositionUpdate
	for _, write := range []func() error{
		func() error { return packet.WriteUInt32(response.EntityUniqueID) },
		func() error { return packet.WriteUInt16(response.Region) },
		func() error { return packet.WriteFloat32(response.X) },
		func() error { return packet.WriteFloat32(response.Y) },
		func() error { return packet.WriteFloat32(response.Z) },
		func() error { return packet.WriteUInt16(response.Heading) },
	} {
		if err := write(); err != nil {
			return network.Packet{}, err
		}
	}
	return packet, nil
}
