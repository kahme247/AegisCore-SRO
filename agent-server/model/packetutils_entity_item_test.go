package model

import (
	"encoding/binary"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/g3n/engine/math32"
)

func TestGroundQuestItemSpawnUsesPersistedCreatorName(t *testing.T) {
	item := &Item{CreatorName: "TraderAlias", SRObject: SRObject{
		UniqueID:    77,
		RefObjectID: 900,
		TypeInfo:    TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 9},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(1, 0, 1),
		},
	}}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntitySingleSpawn
	WriteEntitySpawnData(&packet, item)
	payload := packet.Payload()
	if len(payload) < 6 {
		t.Fatalf("payload too short: % X", payload)
	}
	length := int(binary.LittleEndian.Uint16(payload[4:6]))
	if length != len(item.CreatorName) || string(payload[6:6+length]) != item.CreatorName {
		t.Fatalf("creator = %q length=%d, want %q", payload[6:6+length], length, item.CreatorName)
	}
}

func TestGroundEquipmentItemSpawnUsesPersistedOptionLevel(t *testing.T) {
	item := &Item{OptLevel: 7, SRObject: SRObject{
		UniqueID:    55753,
		RefObjectID: 245,
		TypeInfo:    TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 6},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(1, 0, 1),
		},
	}}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntitySingleSpawn

	WriteEntitySpawnData(&packet, item)

	payload := packet.Payload()
	if len(payload) < 5 || payload[4] != item.OptLevel {
		t.Fatalf("ground equipment spawn payload = % X, want option level %d after ref ID", payload, item.OptLevel)
	}
}
