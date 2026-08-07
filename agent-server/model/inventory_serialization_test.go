package model

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/g3n/engine/math32"
)

func TestSortedInventorySlotsUsesAscendingNumericOrder(t *testing.T) {
	items := map[byte]Item{
		SlotPrimaryWeapon: {SRObject: SRObject{RefObjectID: 0x6000}},
		40:                {SRObject: SRObject{RefObjectID: 0x4000}},
		SlotHelmet:        {SRObject: SRObject{RefObjectID: 0x1000}},
	}

	if got, want := SortedInventorySlots(items), []byte{SlotHelmet, SlotPrimaryWeapon, 40}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SortedInventorySlots() = %v, want %v", got, want)
	}
}

func TestEntitySpawnInventorySerializationIsIndependentOfMapInsertionOrder(t *testing.T) {
	orders := [][]byte{
		{40, SlotPrimaryWeapon, SlotHelmet},
		{SlotHelmet, 40, SlotPrimaryWeapon},
	}
	serializers := []struct {
		name      string
		serialize func(Inventory) []byte
	}{
		{
			name: "legacy player spawn",
			serialize: func(inventory Inventory) []byte {
				packet := network.EmptyPacket()
				WriteEntitySpawn(&packet, deterministicInventorySerializationPlayer(inventory))
				return append([]byte(nil), packet.Payload()...)
			},
		},
		{
			name: "typed entity spawn",
			serialize: func(inventory Inventory) []byte {
				packet := network.EmptyPacket()
				WriteEntitySpawnData(&packet, deterministicInventorySerializationPlayer(inventory))
				return append([]byte(nil), packet.Payload()...)
			},
		},
	}

	for _, serializer := range serializers {
		t.Run(serializer.name, func(t *testing.T) {
			first := serializer.serialize(deterministicInventoryForSerialization(orders[0]))
			second := serializer.serialize(deterministicInventoryForSerialization(orders[1]))
			if !bytes.Equal(first, second) {
				t.Fatalf("same inventory produced different bytes for different map insertion orders:\nfirst:  %x\nsecond: %x", first, second)
			}
		})
	}
}

func TestEntitySpawnSerializesPersistedInventorySizeEquippedSlotsAndOptLevels(t *testing.T) {
	inventory := deterministicInventoryForSerialization([]byte{40, SlotPrimaryWeapon, SlotHelmet})
	helmet := inventory.Items[SlotHelmet]
	helmet.OptLevel = 3
	inventory.Items[SlotHelmet] = helmet
	weapon := inventory.Items[SlotPrimaryWeapon]
	weapon.OptLevel = 7
	inventory.Items[SlotPrimaryWeapon] = weapon
	player := deterministicInventorySerializationPlayer(inventory)
	player.InventorySize = 60

	for _, serialize := range []func(*network.Packet, *Player){
		func(packet *network.Packet, player *Player) { WriteEntitySpawn(packet, player) },
		func(packet *network.Packet, player *Player) { WriteEntitySpawnData(packet, player) },
	} {
		packet := network.EmptyPacket()
		serialize(&packet, player)
		payload := packet.Payload()
		if len(payload) < 20 {
			t.Fatalf("spawn payload is too short: %x", payload)
		}
		if payload[8] != 60 || payload[9] != 2 {
			t.Fatalf("inventory size/count = %d/%d, want 60/2", payload[8], payload[9])
		}
		if got := payload[10:15]; !bytes.Equal(got, []byte{0x00, 0x10, 0x00, 0x00, 3}) {
			t.Fatalf("helmet appearance = %x, want ref 0x1000 opt 3", got)
		}
		if got := payload[15:20]; !bytes.Equal(got, []byte{0x00, 0x60, 0x00, 0x00, 7}) {
			t.Fatalf("weapon appearance = %x, want ref 0x6000 opt 7", got)
		}
	}
}

func deterministicInventoryForSerialization(order []byte) Inventory {
	itemsBySlot := map[byte]Item{
		SlotHelmet: {
			SRObject: SRObject{
				RefObjectID: 0x1000,
				TypeInfo:    TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable},
			},
		},
		SlotPrimaryWeapon: {
			SRObject: SRObject{
				RefObjectID: 0x6000,
				TypeInfo:    TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable},
			},
		},
		40: {
			SRObject: SRObject{
				RefObjectID: 0x4000,
				TypeInfo:    TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable},
			},
		},
	}
	items := make(map[byte]Item, len(order))
	for _, slot := range order {
		items[slot] = itemsBySlot[slot]
	}
	return Inventory{Items: items}
}

func deterministicInventorySerializationPlayer(inventory Inventory) *Player {
	return &Player{
		SRObject: SRObject{
			RefObjectID: 1907,
			UniqueID:    100,
			TypeInfo:    TypeInfo{TypeID1: 1, TypeID2: 1},
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
				Region: navmeshv2.NewRegionFromInt16(25000),
				Offset: math32.NewVector3(100, 0, 200),
			},
		},
		CharName:    "inventory-order",
		Inventory:   inventory,
		LifeState:   Alive,
		MotionState: Stand,
		BodyState:   Normal,
		WalkSpeed:   16,
		RunSpeed:    32,
		HwanSpeed:   100,
	}
}
