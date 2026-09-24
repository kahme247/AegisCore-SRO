package model

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/g3n/engine/math32"
)

func TestNormalNPCSpawnAdvertisesCompleteInteractionMask(t *testing.T) {
	npc := &NPC{SRObject: SRObject{
		RefObjectID: 1,
		UniqueID:    2,
		Name:        "NPC_CH_FERRY",
		TypeInfo:    TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000),
			Offset: math32.NewVector3(10, 0, 20),
		},
	}}
	packet := network.EmptyPacket()
	WriteEntitySpawnData(&packet, npc)
	payload := packet.Payload()
	if len(payload) == 0 {
		t.Fatalf("spawn payload too short: %x", payload)
	}
	if len(payload) < 2 {
		t.Fatalf("spawn payload too short for talk data: %x", payload)
	}
	if got := payload[len(payload)-7:]; !bytes.Equal(got, []byte{2, 5, 0, 2, 8, 0, 0}) {
		t.Fatalf("normal NPC talk data = %x, want 02050002080000", got)
	}
}

func TestMobSpawnKeepsNeutralTalkFlagBeforeRarity(t *testing.T) {
	mob := &NPC{SRObject: SRObject{
		RefObjectID: 1,
		UniqueID:    2,
		Name:        "MOB_CH_MANGNYANG",
		TypeInfo:    TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000),
			Offset: math32.NewVector3(10, 0, 20),
		},
	}}
	packet := network.EmptyPacket()
	WriteEntitySpawnData(&packet, mob)
	payload := packet.Payload()
	if len(payload) < 2 {
		t.Fatalf("mob spawn payload too short: %x", payload)
	}
	if got := payload[len(payload)-2:]; !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("mob talk flag/rarity = %x, want 0000 for a normal mob", got)
	}
}

func TestGroundGoldSpawnUsesItemDataAmount(t *testing.T) {
	item := &Item{SRObject: SRObject{
		RefObjectID: 1,
		UniqueID:    2,
		TypeInfo:    TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 5, TypeID4: 0},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000),
			Offset: math32.NewVector3(10, 0, 20),
		},
	}, Data: 54321}
	packet := network.EmptyPacket()
	WriteEntitySpawnData(&packet, item)
	payload := packet.Payload()
	if len(payload) < 8 || binary.LittleEndian.Uint32(payload[4:8]) != 54321 {
		t.Fatalf("gold spawn payload = %x, want amount 54321 at item-data position", payload)
	}
}

func TestMovingMobSpawnUsesStationarySnapshot(t *testing.T) {
	mob := &NPC{SRObject: SRObject{
		RefObjectID: 1,
		UniqueID:    2,
		TypeInfo:    TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region:  navmeshv2.NewRegionFromInt16(25000),
			Offset:  math32.NewVector3(10, 0, 20),
			Heading: 123,
		},
	}}
	mob.BeginMovement(MovementData{
		HasDestination: true,
		TargetPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000),
			Offset: math32.NewVector3(30, 0, 40),
		},
	})

	packet := network.EmptyPacket()
	WriteEntitySpawnData(&packet, mob)
	payload := packet.Payload()
	const movementOffset = 24
	if got, want := payload[movementOffset:movementOffset+5], []byte{0, 1, 1, 123, 0}; !bytes.Equal(got, want) {
		t.Fatalf("moving mob inline movement = %x, want stationary snapshot %x", got, want)
	}
}

func TestWritePositionMapsNavmeshAxesToVSRO188WireOrder(t *testing.T) {
	packet := network.EmptyPacket()
	WritePosition(&packet, navmeshv2.RtNavmeshPosition{
		Region:  navmeshv2.NewRegionFromInt16(25000),
		Offset:  math32.NewVector3(111, -22, 333),
		Heading: 444,
	})
	payload := packet.Payload()
	if got := int16(binary.LittleEndian.Uint16(payload[0:2])); got != 25000 {
		t.Fatalf("region = %d, want 25000", got)
	}
	for _, field := range []struct {
		name   string
		offset int
		want   float32
	}{
		{name: "X", offset: 2, want: 111},
		{name: "vertical", offset: 6, want: -22},
		{name: "horizontal Z", offset: 10, want: 333},
	} {
		got := math.Float32frombits(binary.LittleEndian.Uint32(payload[field.offset : field.offset+4]))
		if got != field.want {
			t.Fatalf("%s = %g, want %g", field.name, got, field.want)
		}
	}
	if got := binary.LittleEndian.Uint16(payload[14:16]); got != 444 {
		t.Fatalf("heading = %d, want 444", got)
	}
}
