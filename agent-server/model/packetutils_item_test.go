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

func TestWriteGoldItemMatchesStock008BE4F0AmountBranch(t *testing.T) {
	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 5, TypeID4: 0}},
		Data:      0x11223344,
		StackSize: 9,
	})
	if got, want := packet.Payload(), []byte{0x44, 0x33, 0x22, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("gold payload = % X, want % X", got, want)
	}
}

func TestWriteMagicStoneItemMatchesStock008BE4F0AssimilationBranch(t *testing.T) {
	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 11, TypeID4: 1}},
		StackSize: 9,
		Data:      77,
	})
	if got, want := packet.Payload(), []byte{9, 0, 77}; !bytes.Equal(got, want) {
		t.Fatalf("magic-stone payload = % X, want % X", got, want)
	}
}

func TestWriteRentInfoType3MatchesStock008BB130ReadOrder(t *testing.T) {
	packet := network.EmptyPacket()
	WriteRentInfo(&packet, Item{RentInfo: RentInfo{
		Type:            3,
		CanDelete:       0x1122,
		CanRecharge:     0xCCDD,
		PeriodBeginTime: 0x33445566,
		PeriodEndTime:   0x778899AA,
		PackingTime:     0xEEFF0011,
	}})
	want := []byte{
		0x03, 0x00, 0x00, 0x00,
		0x22, 0x11,
		0x66, 0x55, 0x44, 0x33,
		0xAA, 0x99, 0x88, 0x77,
		0xDD, 0xCC,
		0x11, 0x00, 0xFF, 0xEE,
	}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("type-3 rent payload = % X, want % X", got, want)
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

func TestWriteExpendableItemEncodesNormalGachaCardAsQuantityOnly(t *testing.T) {
	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 14, TypeID4: 1}},
		StackSize: 1,
	})
	if got, want := packet.Payload(), []byte{1, 0}; !bytes.Equal(got, want) {
		t.Fatalf("normal gacha card payload = % X, want % X", got, want)
	}
}

func TestWriteExpendableItemEncodesGachaCouponMetadata(t *testing.T) {
	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 14, TypeID4: 2}},
		StackSize: 1,
	})
	if got, want := packet.Payload(), []byte{1, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("gacha coupon payload = % X, want % X", got, want)
	}
}

func TestWriteExpendableItemEncodesGachaMagicOptions(t *testing.T) {
	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:     SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 14, TypeID4: 2}},
		StackSize:    1,
		MagicOptions: []MagicOption{{Type: 0x11223344, Value: 0x55667788}},
	})
	want := []byte{1, 0, 1, 0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("gacha metadata payload = % X, want % X", got, want)
	}
}

func TestGachaCouponRewardEncodingStaysServerSide(t *testing.T) {
	encoded := encodeGachaCouponReward(4044, 3)
	refID, quantity := decodeGachaCouponReward(encoded)
	if refID != 4044 || quantity != 3 {
		t.Fatalf("decoded reward = %d x%d, want 4044 x3", refID, quantity)
	}

	packet := network.EmptyPacket()
	WriteExpendableItem(&packet, Item{
		SRObject:  SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 14, TypeID4: 2}},
		StackSize: 1,
		Variance:  encoded,
	})
	if got, want := packet.Payload(), []byte{1, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("coupon wire payload = % X, want % X", got, want)
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

func TestWriteMagicCubeItemEncodesStoredElixirQuantity(t *testing.T) {
	packet := network.EmptyPacket()
	WriteContainerItem(&packet, Item{
		SRObject: SRObject{RefObjectID: 20200, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 3}},
		Data:     0x11223344,
	})
	if got, want := packet.Payload(), []byte{0x44, 0x33, 0x22, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("magic-cube payload = % X, want % X", got, want)
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
	want := []byte{
		12,
		9, 0, 0, 0, 0, 0, 0, 0,
		100, 0, 0, 0,
		2,
		212, 0, 0, 0, 3, 0, 0, 0,
		5, 0, 0, 0, 7, 0, 0, 0,
		1, 1, 1, 0xE8, 3, 0, 0, 2, 0, 0, 0,
		2, 1, 0, 0x12, 0x65, 0, 0, 1, 0, 0, 0,
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("equipment payload = % X, want % X", payload, want)
	}
}
