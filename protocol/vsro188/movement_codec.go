package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type EntityDirectionalMovement struct {
	Source  byte
	Heading uint16
}

type EntityDestinationMovement struct {
	RegionID int16
	X        int32
	Y        int32
	Z        int32
	Extended bool
}

type EntityMovementRequest struct {
	HasDestination bool
	Directional    EntityDirectionalMovement
	Destination    EntityDestinationMovement
}

type EntityMovementRequestCodec struct{}
type EntityDirectionalMovementCodec struct{}
type EntityDestinationMovementCodec struct{}

type EntityMovementHeadingRequest struct {
	Heading uint16
}

type EntityMovementHeadingRequestCodec struct {
	Opcode uint16
}

type EntityDirectionalMovementResponse struct {
	EntityUniqueID uint32
	Source         byte
	Heading        uint16
}

type EntityDestinationMovementResponse struct {
	EntityUniqueID uint32
	TargetRegion   uint16
	TargetX        int32
	TargetY        int32
	TargetZ        int32
	// Source* are retained for callers that keep server-side movement context.
	// They are not serialized unless a separate source-bearing response form is
	// added; the final byte below is therefore always hasSource=false here.
	SourceRegion uint16
	SourceX      uint16
	SourceY      float32
	SourceZ      uint16
}

type EntityPositionUpdate struct {
	EntityUniqueID uint32
	Region         uint16
	X              float32
	Y              float32
	Z              float32
	Heading        uint16
}

type EntityTurnResponse struct {
	EntityUniqueID uint32
	Heading        uint16
}

type EntityDirectionalMovementResponseCodec struct{}
type EntityDestinationMovementResponseCodec struct{}
type EntityPositionUpdateCodec struct{}
type EntityTurnResponseCodec struct{}

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

// Encode emits the compact stock 0x7021 form. The client-side constructor
// calls the movement union writer without the optional sourced-compatibility
// byte, so Directional.Source is intentionally not serialized here.
func (EntityMovementRequestCodec) Encode(request EntityMovementRequest) (network.Packet, error) {
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.EntityMovementRequest
	if request.HasDestination {
		if err := packet.WriteByte(1); err != nil {
			return network.Packet{}, err
		}
		if request.Destination.Extended {
			if request.Destination.RegionID >= 0 {
				return network.Packet{}, fmt.Errorf("extended movement region %d must have its high bit set", request.Destination.RegionID)
			}
			body := network.EmptyClientPacket()
			body.MessageID = opcode.EntityMovementRequest
			if err := body.WriteByte(1); err != nil {
				return network.Packet{}, err
			}
			if err := body.WriteUInt16(uint16(request.Destination.RegionID)); err != nil {
				return network.Packet{}, err
			}
			for _, value := range []int32{request.Destination.X, request.Destination.Y, request.Destination.Z} {
				if err := body.WriteUInt32(uint32(value)); err != nil {
					return network.Packet{}, err
				}
			}
			return body, nil
		}
		if request.Destination.RegionID < 0 {
			return network.Packet{}, fmt.Errorf("normal movement region %d is negative; set Extended for the high-bit form", request.Destination.RegionID)
		}
		// The destination writer emits region followed by three 16-bit words.
		// Coordinates are preserved bit-for-bit; the decoder sign-extends them.
		body := network.EmptyClientPacket()
		body.MessageID = opcode.EntityMovementRequest
		if err := body.WriteByte(1); err != nil {
			return network.Packet{}, err
		}
		if err := body.WriteUInt16(uint16(request.Destination.RegionID)); err != nil {
			return network.Packet{}, err
		}
		for _, value := range []int32{request.Destination.X, request.Destination.Y, request.Destination.Z} {
			if err := body.WriteUInt16(uint16(value)); err != nil {
				return network.Packet{}, err
			}
		}
		return body, nil
	}
	if err := packet.WriteByte(0); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt16(request.Directional.Heading); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
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

func (EntityDirectionalMovementCodec) Encode(movement EntityDirectionalMovement) (network.Packet, error) {
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.EntityMovementRequest
	if err := packet.WriteUInt16(movement.Heading); err != nil {
		return network.Packet{}, fmt.Errorf("write directional movement heading: %w", err)
	}
	return packet, nil
}

func (EntityDestinationMovementCodec) Encode(movement EntityDestinationMovement) (network.Packet, error) {
	request, err := (EntityMovementRequestCodec{}).Encode(EntityMovementRequest{HasDestination: true, Destination: movement})
	if err != nil {
		return network.Packet{}, err
	}
	// This codec represents the union body after the 0x7021 destination flag.
	body := network.EmptyClientPacket()
	body.MessageID = opcode.EntityMovementRequest
	if err := body.WriteBytes(request.Payload()[1:]); err != nil {
		return network.Packet{}, err
	}
	return body, nil
}

func (codec EntityMovementHeadingRequestCodec) Encode(request EntityMovementHeadingRequest) (network.Packet, error) {
	if codec.Opcode != opcode.EntityMovementForwardRequest && codec.Opcode != opcode.EntityMovementTurnRequest {
		return network.Packet{}, fmt.Errorf("unsupported movement-heading opcode 0x%04X", codec.Opcode)
	}
	packet := network.EmptyClientPacket()
	packet.MessageID = codec.Opcode
	if err := packet.WriteUInt16(request.Heading); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func (EntityDestinationMovementCodec) Decode(packet network.Packet) (EntityDestinationMovement, error) {
	regionID, err := packet.ReadInt16()
	if err != nil {
		return EntityDestinationMovement{}, fmt.Errorf("read destination movement region: %w", err)
	}
	if regionID < 0 {
		if packet.Remaining() != 12 {
			return EntityDestinationMovement{}, fmt.Errorf("extended region %d has destination payload length %d, want 12", regionID, packet.Remaining())
		}
		x, err := packet.ReadUInt32()
		if err != nil {
			return EntityDestinationMovement{}, fmt.Errorf("read extended destination movement X: %w", err)
		}
		y, err := packet.ReadUInt32()
		if err != nil {
			return EntityDestinationMovement{}, fmt.Errorf("read extended destination movement Y: %w", err)
		}
		z, err := packet.ReadUInt32()
		if err != nil {
			return EntityDestinationMovement{}, fmt.Errorf("read extended destination movement Z: %w", err)
		}
		return EntityDestinationMovement{RegionID: regionID, X: int32(x), Y: int32(y), Z: int32(z), Extended: true}, nil
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
	return EntityDestinationMovement{RegionID: regionID, X: int32(x), Y: int32(y), Z: int32(z)}, nil
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
	writes := []func() error{
		func() error { return packet.WriteUInt32(response.EntityUniqueID) },
		func() error { return packet.WriteByte(1) },
		func() error { return packet.WriteUInt16(response.TargetRegion) },
	}
	if int16(response.TargetRegion) < 0 {
		writes = append(writes,
			func() error { return packet.WriteUInt32(uint32(response.TargetX)) },
			func() error { return packet.WriteUInt32(uint32(response.TargetY)) },
			func() error { return packet.WriteUInt32(uint32(response.TargetZ)) },
		)
	} else {
		for _, coordinate := range []int32{response.TargetX, response.TargetY, response.TargetZ} {
			if coordinate < math.MinInt16 || coordinate > math.MaxInt16 {
				return network.Packet{}, fmt.Errorf("normal movement coordinate %d exceeds int16 wire range", coordinate)
			}
		}
		writes = append(writes,
			func() error { return packet.WriteUInt16(uint16(int16(response.TargetX))) },
			func() error { return packet.WriteUInt16(uint16(int16(response.TargetY))) },
			func() error { return packet.WriteUInt16(uint16(int16(response.TargetZ))) },
		)
	}
	writes = append(writes,
		// The trailing byte is hasSource. A value of 1 makes the stock client
		// read the omitted source region/position fields and assert at EOF.
		func() error { return packet.WriteByte(0) },
	)
	for _, write := range writes {
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

func (EntityTurnResponseCodec) Encode(response EntityTurnResponse) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityMovementTurnResponse
	if err := packet.WriteUInt32(response.EntityUniqueID); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt16(response.Heading); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}
