package model

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestBuildCOSDataGrowthMatchesVSRO188Reader(t *testing.T) {
	cos := &COS{NPC: NPC{SRObject: SRObject{UniqueID: 0x11223344, RefObjectID: 0x55667788}, Mutex: new(sync.Mutex)}, Kind: COSGrowthPet, OwnerUniqueID: 0xAABBCCDD, Level: 12, Health: 90, MaxHealth: 100, Experience: 1234, HGP: 9000, Settings: 1, InventorySlot: 7}
	cos.Rename("Wolf")
	p := BuildCOSData(cos)
	want := []byte{
		0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55,
		90, 0, 0, 0, 100, 0, 0, 0,
		0xD2, 0x04, 0, 0, 0, 0, 0, 0, 12, 0x28, 0x23,
		1, 0, 0, 0, 4, 0, 'W', 'o', 'l', 'f', 0,
		0xDD, 0xCC, 0xBB, 0xAA, 7,
	}
	if !reflect.DeepEqual(p.Payload(), want) {
		t.Fatalf("30C8 payload = %x, want %x", p.Payload(), want)
	}
}

func TestBuildCOSItemStateUpdateMatchesInventoryStateLayout(t *testing.T) {
	p := BuildCOSItemStateUpdate(17, COSItemActive)
	want := []byte{17, 64, byte(COSItemActive)}
	if p.MessageID != opcode.InventoryItemUpdate || !bytes.Equal(p.Payload(), want) {
		t.Fatalf("COS item state packet = id %04X payload % X, want %04X/% X", p.MessageID, p.Payload(), opcode.InventoryItemUpdate, want)
	}
}

func TestGrowthCOSExperienceAdvancesLevelReferenceAndPacket(t *testing.T) {
	levels, err := LoadLevelTable(strings.NewReader("1 100 0 0 0 0\n2 200 0 0 0 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	previous := RefChars
	defer func() { RefChars = previous }()
	RefChars = map[uint32]RefChar{
		100: {RefObject: RefObject{ID: 100, OrgObjCodeName: "COS_TEST", TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}}, Level: 1, MaxHP: 100},
		101: {RefObject: RefObject{ID: 101, OrgObjCodeName: "COS_TEST", TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}}, Level: 2, MaxHP: 150},
	}
	pet := &COS{NPC: NPC{SRObject: SRObject{UniqueID: 7, RefObjectID: 100}, Mutex: new(sync.Mutex)}, Kind: COSGrowthPet, Level: 1, Health: 50, MaxHealth: 100}
	levelsGained, refChanged := pet.AwardExperience(levels, 125)
	if levelsGained != 1 || !refChanged || pet.Level != 2 || pet.Experience != 25 || pet.GetRefObjectID() != 101 || pet.Health != 75 || pet.MaxHealth != 150 {
		t.Fatalf("growth award = levels %d refChanged %t snapshot %+v", levelsGained, refChanged, pet.Snapshot())
	}
	packet := BuildCOSExperienceUpdate(7, 125, 99)
	want := []byte{7, 0, 0, 0, COSUpdateExperience, 125, 0, 0, 0, 0, 0, 0, 0, 99, 0, 0, 0}
	if !reflect.DeepEqual(packet.Payload(), want) {
		t.Fatalf("COS experience packet = % X, want % X", packet.Payload(), want)
	}
}

func TestWriteCOSSpawnTailGrowthMatchesVSRO188Reader(t *testing.T) {
	p := network.EmptyPacket()
	WriteCOSSpawnTail(&p, COSSnapshot{Kind: COSGrowthPet, Name: "Wolf", OwnerName: "Alice", OwnerUniqueID: 0x11223344})
	want := []byte{4, 0, 'W', 'o', 'l', 'f', 5, 0, 'A', 'l', 'i', 'c', 'e', 0, 0, 0x44, 0x33, 0x22, 0x11}
	if !reflect.DeepEqual(p.Payload(), want) {
		t.Fatalf("COS spawn tail = %x, want %x", p.Payload(), want)
	}
	if binary.LittleEndian.Uint32(p.Payload()[len(p.Payload())-4:]) != 0x11223344 {
		t.Fatal("owner UID was not final field")
	}
}

func TestWriteCOSSpawnTailRideHasNoTrailingFields(t *testing.T) {
	p := network.EmptyPacket()
	WriteCOSSpawnTail(&p, COSSnapshot{Kind: COSRideTransport, OwnerUniqueID: 99})
	if len(p.Payload()) != 0 {
		t.Fatalf("ride COS tail = %x, want empty", p.Payload())
	}
}
