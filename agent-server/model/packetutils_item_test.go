package model

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestWriteExpendableItemDoesNotEncodeMPPotionAsGachaCard(t *testing.T) {
	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 2}},
		StackSize: 49,
	})

	if got, want := packet.Payload(), []byte{49, 0}; !bytes.Equal(got, want) {
		t.Fatalf("MP potion payload = % X, want % X", got, want)
	}
}

func TestWriteSummonedGrowthCOSItemMatchesStockVSRO188Reader(t *testing.T) {
	packet := network.EmptyPacket()
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 1}},
		COS:      COSItemInfo{Present: true, Kind: COSGrowthPet, State: COSItemSummoned, RefObjectID: 9384, Name: "Wolf", Level: 12},
	})
	// The stock reader consumes state, RefCharID, name, then buff count. It
	// derives the growth pet level from RefChar rather than reading a level byte.
	want := []byte{2, 0xA8, 0x24, 0, 0, 4, 0, 'W', 'o', 'l', 'f', 0}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("summoned growth item = % X, want % X", got, want)
	}
}

func TestWriteActiveAbilityCOSItemMatchesStockVSRO188Reader(t *testing.T) {
	packet := network.EmptyPacket()
	now := time.Date(2026, time.July, 19, 6, 0, 0, 0, time.UTC)
	endsAt := now.Add(28 * 24 * time.Hour)
	packet.WriteByte(byte(COSItemActive))
	packet.WriteUInt32(23857)
	packet.WriteString("")
	writeCOSAbilityPetLifetime(&packet, endsAt, now)
	packet.WriteByte(0)
	want := []byte{3, 0x31, 0x5D, 0, 0, 0, 0, 0x00, 0xEA, 0x24, 0x00, 0}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("active ability item = % X, want % X", got, want)
	}
}

func TestWriteContainerActiveAbilityCOSItemUsesRemainingLifetime(t *testing.T) {
	packet := network.EmptyPacket()
	endsAt := time.Now().Add(28 * 24 * time.Hour)
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 2}},
		COS:      COSItemInfo{Present: true, Kind: COSAbilityPet, State: COSItemActive, RefObjectID: 23857, RentEndsAt: endsAt},
	})
	payload := packet.Payload()
	if len(payload) != 12 || payload[0] != byte(COSItemActive) || binary.LittleEndian.Uint32(payload[1:5]) != 23857 {
		t.Fatalf("active ability item payload = % X", payload)
	}
	if remaining := binary.LittleEndian.Uint32(payload[7:11]); remaining < 28*24*60*60-1 || remaining > 28*24*60*60 {
		t.Fatalf("ability pet remaining lifetime = %d, want approximately 28 days", remaining)
	}
	if payload[11] != 0 {
		t.Fatalf("ability pet buff count = %d, want 0", payload[11])
	}
}

func TestWriteExpendableItemEncodesGachaMetadataOnlyForGachaType(t *testing.T) {
	for _, typeID4 := range []int{1, 2} {
		packet := network.EmptyPacket()
		WriteExpendableItem(&packet, Item{
			SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 14, TypeID4: typeID4}},
			StackSize: 1,
		})
		if got, want := packet.Payload(), []byte{1, 0, 0}; !bytes.Equal(got, want) {
			t.Fatalf("gacha TypeID4 %d payload = % X, want % X", typeID4, got, want)
		}
	}
}

func TestWriteMonsterMaskItemEncodesCapturedMonsterReference(t *testing.T) {
	packet := network.EmptyPacket()
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{RefObjectID: 10371, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 2}},
		Data:     4242,
	})
	if got, want := packet.Payload(), []byte{0x92, 0x10, 0x00, 0x00}; !bytes.Equal(got, want) {
		t.Fatalf("monster mask payload = % X, want % X", got, want)
	}
}

func TestWriteInactiveCOSItemOmitsActivePetTail(t *testing.T) {
	packet := network.EmptyPacket()
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{
			RefObjectID: 9279,
			Name:        "ITEM_COS_P_FLUTE_WHITE",
			TypeInfo:    TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		},
	})
	if got, want := packet.Payload(), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("inactive COS item payload = % X, want % X", got, want)
	}
}

func TestWriteEquipmentItemIncludesBluesSocketsAndAdvancedElixir(t *testing.T) {
	packet := network.EmptyPacket()
	WriteEquipmentItem(&packet, Item{
		OptLevel: 12, Variance: 9, Data: 100,
		MagicOptions:    []MagicOption{{Type: 212, Value: 3}, {Type: 5, Value: 7}},
		SocketOptions:   []BindingOption{{Slot: 1, ID: 1000, Value: 2}},
		AdvancedElixirs: []BindingOption{{Slot: 0, ID: 25874, Value: 1}},
	})
	payload := packet.Payload()
	if payload[0] != 12 || payload[13] != 2 {
		t.Fatalf("equipment plus/blues = %d/%d payload=% X", payload[0], payload[13], payload)
	}
	if !bytes.Contains(payload, []byte{1, 1, 0xE8, 3, 0, 0, 2, 0, 0, 0}) {
		t.Fatalf("socket option missing from payload: % X", payload)
	}
	if !bytes.Contains(payload, []byte{2, 1, 0, 0x12, 0x65, 0, 0, 1, 0, 0, 0}) {
		t.Fatalf("advanced elixir missing from payload: % X", payload)
	}
}
