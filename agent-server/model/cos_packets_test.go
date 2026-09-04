package model

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestCOSOwnerSpeedAppliesOnlyToGrowthAndAbilityPets(t *testing.T) {
	for _, test := range []struct {
		name string
		kind COSKind
		want float32
	}{
		{name: "growth", kind: COSGrowthPet, want: 75},
		{name: "ability", kind: COSAbilityPet, want: 75},
		{name: "ride", kind: COSRideTransport, want: 40},
		{name: "trade", kind: COSJobTransport, want: 40},
	} {
		t.Run(test.name, func(t *testing.T) {
			pet := &COS{NPC: NPC{Mutex: new(sync.Mutex), RunSpeed: 40}, Kind: test.kind}
			pet.SetOwnerMovementSpeed(75, true)
			if got := pet.GetMovementSpeed(); got != test.want {
				t.Fatalf("movement speed = %g, want %g", got, test.want)
			}
		})
	}
}

func TestCOSExpireTimedServicesRemovesOnlyExpiredJobs(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	pet := &COS{BaseInventorySize: 5, InventorySize: 28, TimedServices: map[int]COSTimedService{
		1: {Category: 5, JobID: 1, Data1: 28, ExpiresAt: now.Add(-time.Second)},
		2: {JobID: 2, ExpiresAt: now.Add(time.Second)},
	}}
	expired := pet.ExpireTimedServices(now)
	if len(expired) != 1 || expired[0].JobID != 1 || len(pet.TimedServices) != 1 || pet.InventorySize != 5 {
		t.Fatalf("expired services = %#v remaining=%#v inventory-size=%d", expired, pet.TimedServices, pet.InventorySize)
	}
}

func TestCOSApplyCommittedResourcesClampsHealthAndUpdatesHGPWithoutReviving(t *testing.T) {
	pet := &COS{NPC: NPC{Mutex: new(sync.Mutex), LifeState: Alive}, Health: 50, MaxHealth: 100, HGP: 20}
	if !pet.ApplyCommittedResources(500, 30) || pet.Health != 100 || pet.HGP != 30 {
		t.Fatalf("committed resources = health %d hgp %d", pet.Health, pet.HGP)
	}
	pet.MarkDead()
	if pet.ApplyCommittedResources(100, 40) || pet.Health != 0 {
		t.Fatal("resource application revived a dead COS")
	}
}

func TestBuildCOSDataGrowthMatchesVSRO188Reader(t *testing.T) {
	cos := &COS{NPC: NPC{SRObject: SRObject{UniqueID: 0x11223344, RefObjectID: 0x55667788}, Mutex: new(sync.Mutex)}, Kind: COSGrowthPet, OwnerUniqueID: 0xAABBCCDD, Level: 12, Health: 90, MaxHealth: 100, Experience: 1234, HGP: 9000, Settings: 1, InventorySlot: 7}
	cos.Rename("Wolf")
	p, ok := BuildCOSData(cos)
	if !ok {
		t.Fatal("growth COS 0x30C8 layout was rejected")
	}
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

func TestBuildCOSHealthUpdateMatchesClient3057Mask1Layout(t *testing.T) {
	packet := BuildCOSHealthUpdate(0x11223344, 360)
	want := []byte{0x44, 0x33, 0x22, 0x11, 0, 0, 1, 0x68, 0x01, 0, 0}
	if packet.MessageID != opcode.EntityUpdateStatus || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("COS health update = opcode 0x%04X payload %X", packet.MessageID, packet.Payload())
	}
}

func TestBuildCOSHGPUpdateMatchesClient30C9Type4Layout(t *testing.T) {
	packet := BuildCOSHGPUpdate(0x11223344, 9900)
	if packet.MessageID != opcode.COSUpdateResponse || !bytes.Equal(packet.Payload(), []byte{0x44, 0x33, 0x22, 0x11, COSUpdateHGP, 0xAC, 0x26}) {
		t.Fatalf("COS HGP update = opcode 0x%04X payload %X", packet.MessageID, packet.Payload())
	}
}

func TestBuildCOSItemStateUpdateMatchesInventoryStateLayout(t *testing.T) {
	p := BuildCOSItemStateUpdate(17, COSItemActive)
	want := []byte{17, 64, byte(COSItemActive)}
	if p.MessageID != opcode.InventoryItemUpdate || !bytes.Equal(p.Payload(), want) {
		t.Fatalf("COS item state packet = id %04X payload % X, want %04X/% X", p.MessageID, p.Payload(), opcode.InventoryItemUpdate, want)
	}
}

func TestCOSReviveAndMarkDeadUpdateRuntimeLifeState(t *testing.T) {
	cos := &COS{NPC: NPC{Mutex: new(sync.Mutex)}, MaxHealth: 100, Health: 100}
	if !cos.MarkDead() || cos.GetLifeState() != Dead || cos.Snapshot().Health != 0 {
		t.Fatal("MarkDead did not publish dead COS state")
	}
	if !cos.Revive(40) || cos.GetLifeState() != Alive || cos.Snapshot().Health != 40 {
		t.Fatal("Revive did not restore COS state")
	}
}

func TestBuildCOSDataRideTransportMatchesClient30C8Reader(t *testing.T) {
	cos := &COS{NPC: NPC{SRObject: SRObject{UniqueID: 0x11223344, RefObjectID: 0x55667788}, Mutex: new(sync.Mutex)}, Kind: COSRideTransport, Health: 90, MaxHealth: 100, Inventory: NewInventory(nil)}
	packet, ok := BuildCOSData(cos)
	want := []byte{
		0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55,
		90, 0, 0, 0, 100, 0, 0, 0,
		0, // inventory capacity; client 0x009A1900 stops here when it is zero
	}
	if !ok || packet.MessageID != opcode.COSDataResponse || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("ride 30C8 = ok=%t opcode=0x%04X payload=% X, want 0x%04X/% X", ok, packet.MessageID, packet.Payload(), opcode.COSDataResponse, want)
	}
}

func TestBuildCOSDataAbilitySerializesStoredInventory(t *testing.T) {
	item := Item{SRObject: SRObject{RefObjectID: 500, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 0}}, StackSize: 3}
	cos := &COS{
		NPC:           NPC{SRObject: SRObject{UniqueID: 7, RefObjectID: 8}, Mutex: new(sync.Mutex)},
		Kind:          COSAbilityPet,
		Health:        90,
		MaxHealth:     100,
		Settings:      1,
		InventorySize: 5,
		Inventory:     NewInventory(map[byte]Item{2: item}),
		OwnerUniqueID: 9,
		InventorySlot: 10,
	}
	cos.Rename("Raccoon")
	p, ok := BuildCOSData(cos)
	if !ok {
		t.Fatal("ability COS 0x30C8 layout was rejected")
	}
	want := []byte{
		7, 0, 0, 0, 8, 0, 0, 0, 90, 0, 0, 0, 100, 0, 0, 0,
		1, 0, 0, 0, 7, 0, 'R', 'a', 'c', 'c', 'o', 'o', 'n',
		5, 1, 2, 0, 0, 0, 0, 0xF4, 1, 0, 0, 3, 0,
		9, 0, 0, 0, 10,
	}
	if !bytes.Equal(p.Payload(), want) {
		t.Fatalf("ability 30C8 payload = % X, want % X", p.Payload(), want)
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

func TestCOSApplyDamageTransitionsToDead(t *testing.T) {
	pet := &COS{NPC: NPC{Mutex: new(sync.Mutex), LifeState: Alive}, Health: 50, MaxHealth: 100}
	if applied, killed := pet.ApplyDamage(20); applied != 20 || killed || pet.Health != 30 || pet.GetLifeState() != Alive {
		t.Fatalf("first damage = %d/%t health=%d state=%d", applied, killed, pet.Health, pet.GetLifeState())
	}
	if applied, killed := pet.ApplyDamage(99); applied != 30 || !killed || pet.Health != 0 || pet.GetLifeState() != Dead {
		t.Fatalf("lethal damage = %d/%t health=%d state=%d", applied, killed, pet.Health, pet.GetLifeState())
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
