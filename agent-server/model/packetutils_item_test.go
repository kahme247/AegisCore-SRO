package model

import (
	"bytes"
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
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
