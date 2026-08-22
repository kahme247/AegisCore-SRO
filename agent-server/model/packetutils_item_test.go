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

func TestWriteSummonedGrowthCOSItemIncludesIdentityLevelAndState(t *testing.T) {
	packet := network.EmptyPacket()
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 1}},
		COS:      COSItemInfo{Present: true, State: COSItemSummoned, RefObjectID: 9384, Name: "Wolf", Level: 12},
	})
	want := []byte{2, 0xA8, 0x24, 0, 0, 4, 0, 'W', 'o', 'l', 'f', 12, 0}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("summoned growth item = % X, want % X", got, want)
	}
}

func TestWriteActiveAbilityCOSItemIncludesNonzeroRentalEnd(t *testing.T) {
	packet := network.EmptyPacket()
	endsAt := time.Now().Add(28 * 24 * time.Hour)
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 2}},
		COS:      COSItemInfo{Present: true, State: COSItemActive, RefObjectID: 23857, RentEndsAt: endsAt},
	})
	payload := packet.Payload()
	if len(payload) != 22 || payload[0] != byte(COSItemActive) || binary.LittleEndian.Uint32(payload[1:5]) != 23857 {
		t.Fatalf("active ability item payload = % X", payload)
	}
	if rentalType := binary.LittleEndian.Uint32(payload[7:11]); rentalType != 1 {
		t.Fatalf("ability pet rental type = %d, want fixed-period type 1", rentalType)
	}
	if canDelete := binary.LittleEndian.Uint16(payload[11:13]); canDelete != 1 {
		t.Fatalf("ability pet can-delete = %d, want 1", canDelete)
	}
	if begin, end := binary.LittleEndian.Uint32(payload[13:17]), binary.LittleEndian.Uint32(payload[17:21]); begin == 0 || end == 0 || begin == end {
		t.Fatalf("ability pet rental timestamps = %08X/%08X, want distinct nonzero values", begin, end)
	}
	if payload[21] != 0 {
		t.Fatalf("ability pet buff count = %d, want 0", payload[21])
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
