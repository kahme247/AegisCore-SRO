package character

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/g3n/engine/math32"
)

type destinationPositionResolverFake struct {
	global *math32.Vector3
}

func (f *destinationPositionResolverFake) NewPosFromGlobalCoordinates(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	f.global = global.Clone()
	return navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromXAndY(101, 100), Offset: math32.NewVector3(57, global.Y, 489)}, nil
}

func TestParseForwardPacketFixtures(t *testing.T) {
	stock := network.EmptyClientPacket()
	stock.WriteUInt16(2468)
	action, heading, err := parseForwardPacket(&stock, 999)
	if err != nil || action != 1 || heading != 2468 {
		t.Fatalf("unexpected stock forward result action=%d heading=%d err=%v", action, heading, err)
	}

	packet := network.EmptyClientPacket()
	packet.WriteByte(1)
	packet.WriteUInt16(1234)
	action, heading, err = parseForwardPacket(&packet, 999)
	if err != nil || action != 1 || heading != 1234 {
		t.Fatalf("unexpected forward fixture result action=%d heading=%d err=%v", action, heading, err)
	}

	fallbackPacket := network.EmptyClientPacket()
	fallbackPacket.WriteByte(1)
	action, heading, err = parseForwardPacket(&fallbackPacket, 4321)
	if err != nil || action != 1 || heading != 4321 {
		t.Fatalf("unexpected fallback fixture result action=%d heading=%d err=%v", action, heading, err)
	}
}

func TestParseTurnPacketFixtures(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteUInt16(2222)
	heading, err := parseTurnPacket(&packet)
	if err != nil || heading != 2222 {
		t.Fatalf("unexpected turn fixture result heading=%d err=%v", heading, err)
	}

	packetWithSource := network.EmptyClientPacket()
	packetWithSource.WriteByte(1)
	packetWithSource.WriteUInt16(3333)
	heading, err = parseTurnPacket(&packetWithSource)
	if err != nil || heading != 3333 {
		t.Fatalf("unexpected sourced turn fixture result heading=%d err=%v", heading, err)
	}
}

func TestParseDirectionalMovementPacketAcceptsStockAndSourcedForms(t *testing.T) {
	stock := network.EmptyClientPacket()
	stock.WriteUInt16(1234)
	source, heading, err := parseDirectionalMovementPacket(&stock)
	if err != nil || source != 1 || heading != 1234 {
		t.Fatalf("stock direction = source:%d heading:%d err:%v", source, heading, err)
	}

	sourced := network.EmptyClientPacket()
	sourced.WriteByte(0)
	sourced.WriteUInt16(4321)
	source, heading, err = parseDirectionalMovementPacket(&sourced)
	if err != nil || source != 0 || heading != 4321 {
		t.Fatalf("sourced direction = source:%d heading:%d err:%v", source, heading, err)
	}
}

func TestParseDirectionalMovementPacketRejectsOtherLengths(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(1)
	if _, _, err := parseDirectionalMovementPacket(&packet); err == nil {
		t.Fatal("short directional packet was accepted")
	}
}

func TestParseDestinationMovementPacketAcceptsNormalRegions(t *testing.T) {
	normal := network.EmptyClientPacket()
	normal.WriteUInt16(uint16(25000))
	normal.WriteUInt16(150)
	normal.WriteUInt16(13)
	normal.WriteUInt16(250)
	regionID, offset, err := parseDestinationMovementPacket(&normal)
	if err != nil || regionID != 25000 || offset == nil || offset.X != 150 || offset.Y != 13 || offset.Z != 250 {
		t.Fatalf("normal destination = region:%d offset:%v err:%v", regionID, offset, err)
	}
}

func TestResolveDestinationPositionNormalizesCoordinatesAcrossRegionSeam(t *testing.T) {
	resolver := &destinationPositionResolverFake{}
	packetRegion := navmeshv2.NewRegionFromXAndY(100, 100)
	position, err := resolveDestinationPosition(resolver, packetRegion.ID, math32.NewVector3(1977, 12, 489))
	if err != nil {
		t.Fatalf("resolve destination: %v", err)
	}
	if resolver.global == nil {
		t.Fatal("destination was not resolved through global coordinates")
	}
	if resolver.global.X != 193977 || resolver.global.Y != 12 || resolver.global.Z != 192489 {
		t.Fatalf("global destination = %v, want (193977, 12, 192489)", resolver.global)
	}
	if position.Region.X != 101 || position.Region.Y != 100 || position.Offset.X != 57 || position.Offset.Z != 489 {
		t.Fatalf("normalized destination = region(%d,%d) offset=%v", position.Region.X, position.Region.Y, position.Offset)
	}
}

func TestParseDestinationMovementPacketAcceptsDungeonWireAndRejectsInvalidNormalWidth(t *testing.T) {
	normalWithExtendedCoordinates := network.EmptyClientPacket()
	normalWithExtendedCoordinates.WriteUInt16(uint16(25000))
	normalWithExtendedCoordinates.WriteUInt32(1)
	normalWithExtendedCoordinates.WriteUInt32(2)
	normalWithExtendedCoordinates.WriteUInt32(3)
	if _, _, err := parseDestinationMovementPacket(&normalWithExtendedCoordinates); err == nil {
		t.Fatal("normal region accepted extended destination coordinates")
	}

	dungeon := network.EmptyClientPacket()
	dungeon.WriteUInt16(0x8001)
	dungeon.WriteUInt32(1)
	dungeon.WriteUInt32(2)
	dungeon.WriteUInt32(3)
	region, offset, err := parseDestinationMovementPacket(&dungeon)
	if err != nil || region != -32767 || offset.X != 1 || offset.Y != 2 || offset.Z != 3 {
		t.Fatalf("dungeon wire form = region:%d offset:%v err:%v", region, offset, err)
	}
}

func TestMovementPacketParsersRejectEmptyPayloads(t *testing.T) {
	forwardPacket := network.EmptyClientPacket()
	if _, _, err := parseForwardPacket(&forwardPacket, 0); err == nil {
		t.Fatal("empty forward packet was accepted")
	}
	turnPacket := network.EmptyClientPacket()
	if _, err := parseTurnPacket(&turnPacket); err == nil {
		t.Fatal("empty turn packet was accepted")
	}
}
