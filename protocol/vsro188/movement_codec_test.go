package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
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

func TestMovementResponseCodecsMatchRecoveredClientReaders(t *testing.T) {
	direction, err := (EntityDirectionalMovementResponseCodec{}).Encode(EntityDirectionalMovementResponse{EntityUniqueID: 42, Source: 1, Heading: 12345})
	if err != nil || !bytes.Equal(direction.Payload(), []byte{42, 0, 0, 0, 0, 1, 0x39, 0x30, 0}) {
		t.Fatalf("direction Encode() = %x, %v", direction.Payload(), err)
	}
	destination, err := (EntityDestinationMovementResponseCodec{}).Encode(EntityDestinationMovementResponse{
		EntityUniqueID: 42, TargetRegion: 25000, TargetX: 150, TargetY: 13, TargetZ: 250,
		SourceRegion: 25000, SourceX: 1000, SourceY: 12.5, SourceZ: 2000,
	})
	wantDestination := []byte{42, 0, 0, 0, 1, 0xA8, 0x61, 0x96, 0, 0x0D, 0, 0xFA, 0, 1, 0xA8, 0x61, 0xE8, 3, 0, 0, 0x48, 0x41, 0xD0, 7}
	if err != nil || !bytes.Equal(destination.Payload(), wantDestination) {
		t.Fatalf("destination Encode() = %x, %v", destination.Payload(), err)
	}
	position, err := (EntityPositionUpdateCodec{}).Encode(EntityPositionUpdate{EntityUniqueID: 42, Region: 25000, X: 100, Y: 12.5, Z: 200})
	wantPosition := []byte{42, 0, 0, 0, 0xA8, 0x61, 0, 0, 0xC8, 0x42, 0, 0, 0x48, 0x41, 0, 0, 0x48, 0x43, 0, 0}
	if err != nil || !bytes.Equal(position.Payload(), wantPosition) {
		t.Fatalf("position Encode() = %x, %v", position.Payload(), err)
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

func TestEntityMovementCodecRejectsUnknownAndUnsupportedForms(t *testing.T) {
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
	if _, err := (EntityMovementRequestCodec{}).Decode(dungeon); err == nil {
		t.Fatal("Decode() accepted unsupported dungeon coordinates")
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
