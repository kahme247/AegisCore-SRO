package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestEntityMovementCodecMatchesRecoveredStockLayouts(t *testing.T) {
	direction := network.EmptyClientPacket()
	direction.WriteByte(0)
	direction.WriteUInt16(1234)
	request, err := (EntityMovementRequestCodec{}).Decode(direction)
	if err != nil || request.HasDestination || request.Directional.Source != 1 || request.Directional.Heading != 1234 {
		t.Fatalf("direction Decode() = %+v, %v", request, err)
	}
	destination := network.EmptyClientPacket()
	destination.WriteByte(1)
	destination.WriteUInt16(25000)
	destination.WriteUInt16(150)
	destination.WriteUInt16(13)
	destination.WriteUInt16(250)
	request, err = (EntityMovementRequestCodec{}).Decode(destination)
	if err != nil || !request.HasDestination || request.Destination != (EntityDestinationMovement{RegionID: 25000, X: 150, Y: 13, Z: 250}) {
		t.Fatalf("destination Decode() = %+v, %v", request, err)
	}
}

func TestEntityMovementRequestEncodersMatchRecoveredStockConstructors(t *testing.T) {
	directional, err := (EntityMovementRequestCodec{}).Encode(EntityMovementRequest{
		Directional: EntityDirectionalMovement{Source: 0, Heading: 0x1234},
	})
	if err != nil || directional.MessageID != opcode.EntityMovementRequest || !bytes.Equal(directional.Payload(), []byte{0, 0x34, 0x12}) {
		t.Fatalf("directional Encode() = %x, %v", directional.Payload(), err)
	}

	destination, err := (EntityMovementRequestCodec{}).Encode(EntityMovementRequest{
		HasDestination: true,
		Destination:    EntityDestinationMovement{RegionID: 25000, X: 150, Y: 13, Z: 250},
	})
	wantDestination := []byte{1, 0xA8, 0x61, 0x96, 0, 0x0D, 0, 0xFA, 0}
	if err != nil || !bytes.Equal(destination.Payload(), wantDestination) {
		t.Fatalf("destination Encode() = %x, want %x, err=%v", destination.Payload(), wantDestination, err)
	}

	extended, err := (EntityDestinationMovementCodec{}).Encode(EntityDestinationMovement{
		RegionID: -32767, X: 1, Y: 2, Z: 3, Extended: true,
	})
	// EntityDestinationMovementCodec encodes the union body after the outer
	// destination flag; the signed region remains little-endian.
	wantExtended := []byte{0x01, 0x80, 0x01, 0, 0, 0, 0x02, 0, 0, 0, 0x03, 0, 0, 0}
	if err != nil || !bytes.Equal(extended.Payload(), wantExtended) {
		t.Fatalf("extended Encode() = %x, want %x, err=%v", extended.Payload(), wantExtended, err)
	}

	for _, test := range []struct {
		name   string
		opcode uint16
		want   []byte
	}{
		{name: "forward", opcode: opcode.EntityMovementForwardRequest, want: []byte{0x78, 0x56}},
		{name: "turn", opcode: opcode.EntityMovementTurnRequest, want: []byte{0x78, 0x56}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet, err := (EntityMovementHeadingRequestCodec{Opcode: test.opcode}).Encode(EntityMovementHeadingRequest{Heading: 0x5678})
			if err != nil || packet.MessageID != test.opcode || !bytes.Equal(packet.Payload(), test.want) {
				t.Fatalf("Encode() = opcode 0x%04X payload %x, err=%v", packet.MessageID, packet.Payload(), err)
			}
		})
	}
}

func TestMovementResponseCodecsMatchRecoveredClientReaders(t *testing.T) {
	direction, err := (EntityDirectionalMovementResponseCodec{}).Encode(EntityDirectionalMovementResponse{EntityUniqueID: 42, Source: 1, Heading: 12345})
	if err != nil || !bytes.Equal(direction.Payload(), []byte{42, 0, 0, 0, 0, 1, 0x39, 0x30, 0}) {
		t.Fatalf("direction Encode() = %x, %v", direction.Payload(), err)
	}
	destination, err := (EntityDestinationMovementResponseCodec{}).Encode(EntityDestinationMovementResponse{
		EntityUniqueID: 42, TargetRegion: 25000, TargetX: 150, TargetY: 13, TargetZ: 250,
		SourceRegion: 25000, SourceX: 1000, SourceY: 12.5, SourceZ: 2000,
	})
	// Source fields are intentionally not serialized, so hasSource must be 0.
	wantDestination := []byte{42, 0, 0, 0, 1, 0xA8, 0x61, 0x96, 0, 0x0D, 0, 0xFA, 0, 0}
	if err != nil || !bytes.Equal(destination.Payload(), wantDestination) {
		t.Fatalf("destination Encode() = %x, %v", destination.Payload(), err)
	}
	position, err := (EntityPositionUpdateCodec{}).Encode(EntityPositionUpdate{EntityUniqueID: 42, Region: 25000, X: 100, Y: 12.5, Z: 200})
	wantPosition := []byte{42, 0, 0, 0, 0xA8, 0x61, 0, 0, 0xC8, 0x42, 0, 0, 0x48, 0x41, 0, 0, 0x48, 0x43, 0, 0}
	if err != nil || !bytes.Equal(position.Payload(), wantPosition) {
		t.Fatalf("position Encode() = %x, %v", position.Payload(), err)
	}
	turn, err := (EntityTurnResponseCodec{}).Encode(EntityTurnResponse{EntityUniqueID: 42, Heading: 0x1234})
	if err != nil || !bytes.Equal(turn.Payload(), []byte{42, 0, 0, 0, 0x34, 0x12}) {
		t.Fatalf("turn Encode() = %x, %v", turn.Payload(), err)
	}
}

func TestEntityMovementCodecKeepsSourcedCompatibilitySeparate(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(0)
	packet.WriteByte(0)
	packet.WriteUInt16(4321)
	request, err := (EntityMovementRequestCodec{}).Decode(packet)
	if err != nil || request.Directional.Source != 0 || request.Directional.Heading != 4321 {
		t.Fatalf("Decode() = %+v, %v", request, err)
	}
}

func TestEntityMovementCodecRejectsUnknownAndInvalidWidths(t *testing.T) {
	invalidFlag := network.EmptyClientPacket()
	invalidFlag.WriteByte(2)
	invalidFlag.WriteUInt16(1)
	if _, err := (EntityMovementRequestCodec{}).Decode(invalidFlag); err == nil {
		t.Fatal("Decode() accepted an unknown destination flag")
	}
	dungeon := network.EmptyClientPacket()
	dungeon.WriteByte(1)
	dungeon.WriteUInt16(0x8001)
	dungeon.WriteUInt32(1)
	dungeon.WriteUInt32(2)
	dungeon.WriteUInt32(3)
	request, err := (EntityMovementRequestCodec{}).Decode(dungeon)
	if err != nil || !request.Destination.Extended || request.Destination.RegionID != -32767 || request.Destination.X != 1 || request.Destination.Y != 2 || request.Destination.Z != 3 {
		t.Fatalf("extended Decode() = %+v, %v", request, err)
	}
	extended := network.EmptyClientPacket()
	extended.WriteByte(1)
	extended.WriteUInt16(25000)
	extended.WriteUInt32(1)
	extended.WriteUInt32(2)
	extended.WriteUInt32(3)
	if _, err := (EntityMovementRequestCodec{}).Decode(extended); err == nil {
		t.Fatal("Decode() accepted unsupported extended coordinates")
	}
}
