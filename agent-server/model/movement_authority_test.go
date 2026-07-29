package model

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/g3n/engine/math32"
)

func testPosition(regionID int16, x, y, z float32) navmeshv2.RtNavmeshPosition {
	region := navmeshv2.Region{ID: regionID}
	return navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(x, y, z)}
}

func TestMovementGenerationRejectsStaleCommit(t *testing.T) {
	object := &SRObject{RtNavmeshPosition: testPosition(1, 10, 0, 10)}
	now := time.Now()
	first := object.BeginMovement(MovementData{StartTime: now, UpdateTime: now, HasDestination: true, TargetPosition: testPosition(1, 20, 0, 20)})
	second := object.BeginMovement(MovementData{StartTime: now, UpdateTime: now, HasDestination: true, TargetPosition: testPosition(1, 30, 0, 30)})
	if second.Generation != first.Generation+1 {
		t.Fatalf("generation did not advance: first=%d second=%d", first.Generation, second.Generation)
	}
	if object.FinalizeMovement(first.Generation, first.TargetPosition, "stale") {
		t.Fatal("stale movement generation committed")
	}
	if !object.FinalizeMovement(second.Generation, second.TargetPosition, "arrived") {
		t.Fatal("current movement generation did not commit")
	}
	if object.GetMovementData() != nil {
		t.Fatal("completed movement state was not cleared")
	}
	if object.GetLastMovementStopReason() != "arrived" {
		t.Fatalf("stop reason was not recorded: %q", object.GetLastMovementStopReason())
	}
	position := object.GetNavmeshPosition()
	if position.Offset.X != 30 || position.Offset.Z != 30 {
		t.Fatalf("unexpected committed position: %s", position.String())
	}
}

func TestHeadingConversions(t *testing.T) {
	heading := RadiansToHeading(math.Pi)
	if math.Abs(float64(heading-32768)) > 1 {
		t.Fatalf("pi converted to unexpected heading: %f", heading)
	}
	radians := HeadingToRadians(32768)
	if math.Abs(float64(radians-math.Pi)) > 0.0001 {
		t.Fatalf("heading converted to unexpected radians: %f", radians)
	}
	if got := RadiansToHeading(2 * math.Pi); got != 0 {
		t.Fatalf("full rotation should normalize to zero, got %f", got)
	}
}

func TestMovementPacketFixtures(t *testing.T) {
	start := testPosition(25000, 100, 12.5, 200)
	player := &Player{SRObject: SRObject{RtNavmeshPosition: start, UniqueID: 42}}
	target := testPosition(25000, 150, 13, 250)
	movement := MovementData{HasDestination: true, TargetPosition: target}
	packet := BuildMovementUpdatePacket(player, movement, start)
	if packet.MessageID != opcode.EntityMovementResponse || packet.DataSize != 24 {
		t.Fatalf("unexpected destination packet metadata: opcode=%04X size=%d", packet.MessageID, packet.DataSize)
	}
	if got, want := packet.Payload(), []byte{
		0x2A, 0x00, 0x00, 0x00, // unique ID
		0x01,       // destination movement
		0xA8, 0x61, // target region 25000
		0x96, 0x00, // target X is a signed 16-bit coordinate
		0x0D, 0x00, // target Y
		0xFA, 0x00, // target Z is a signed 16-bit coordinate
		0x01,       // source-position marker
		0xA8, 0x61, // source region 25000
		0xE8, 0x03, // source X * 10
		0x00, 0x00, 0x48, 0x41, // source Y 12.5
		0xD0, 0x07, // source Z * 10
	}; !bytes.Equal(got, want) {
		t.Fatalf("destination packet payload = % X, want % X", got, want)
	}
	uid, _ := packet.ReadUInt32()
	hasDestination, _ := packet.ReadBool()
	region, _ := packet.ReadUInt16()
	x, _ := packet.ReadUInt16()
	y, _ := packet.ReadUInt16()
	z, _ := packet.ReadUInt16()
	if uid != 42 || !hasDestination || region != uint16(target.Region.ID) || x != uint16(int16(target.Offset.X)) || y != uint16(target.Offset.Y) || z != uint16(int16(target.Offset.Z)) {
		t.Fatalf("destination packet fixture mismatch uid=%d destination=%t region=%d xyz=(%d,%d,%d)", uid, hasDestination, region, x, y, z)
	}

	directionPacket := BuildMovementUpdatePacket(player, MovementData{DirectionAngle: 12345}, start)
	if directionPacket.DataSize != 9 {
		t.Fatalf("unexpected direction packet size: %d", directionPacket.DataSize)
	}
	_, _ = directionPacket.ReadUInt32()
	directionHasDestination, _ := directionPacket.ReadBool()
	source, _ := directionPacket.ReadByte()
	headingValue, _ := directionPacket.ReadUInt16()
	control, _ := directionPacket.ReadByte()
	if directionHasDestination || source != 1 || headingValue != 12345 || control != 0 {
		t.Fatalf("direction packet fixture mismatch destination=%t source=%d heading=%d control=%d", directionHasDestination, source, headingValue, control)
	}
	if got, want := directionPacket.Payload(), []byte{0x2A, 0, 0, 0, 0, 1, 0x39, 0x30, 0}; !bytes.Equal(got, want) {
		t.Fatalf("direction packet payload = % X, want % X", got, want)
	}

	positionPacket := BuildPositionUpdatePacket(player)
	if positionPacket.MessageID != opcode.MovementPositionUpdate || positionPacket.DataSize != 20 {
		t.Fatalf("unexpected position packet metadata: opcode=%04X size=%d", positionPacket.MessageID, positionPacket.DataSize)
	}
	if got, want := positionPacket.Payload(), []byte{
		0x2A, 0, 0, 0,
		0xA8, 0x61,
		0x00, 0x00, 0xC8, 0x42,
		0x00, 0x00, 0x48, 0x41,
		0x00, 0x00, 0x48, 0x43,
		0x00, 0x00,
	}; !bytes.Equal(got, want) {
		t.Fatalf("position packet payload = % X, want % X", got, want)
	}
}
