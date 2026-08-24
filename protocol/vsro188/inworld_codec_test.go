package vsro188

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestEntityInteractionCodecsRequireOneExactUID(t *testing.T) {
	valid := network.EmptyClientPacket()
	valid.WriteUInt32(0x12345678)
	selected, err := (EntitySelectRequestCodec{}).Decode(valid)
	if err != nil || selected.UniqueID != 0x12345678 {
		t.Fatalf("select Decode() = %+v, %v", selected, err)
	}
	deselected, err := (EntityDeselectRequestCodec{}).Decode(valid)
	if err != nil || deselected.UniqueID != 0x12345678 {
		t.Fatalf("deselect Decode() = %+v, %v", deselected, err)
	}
	for _, size := range []int{0, 1, 2, 3, 5, 32} {
		packet := network.EmptyClientPacket()
		for index := 0; index < size; index++ {
			packet.WriteByte(0)
		}
		if _, err := (EntitySelectRequestCodec{}).Decode(packet); err == nil {
			t.Fatalf("select Decode() accepted %d bytes", size)
		}
		if _, err := (EntityDeselectRequestCodec{}).Decode(packet); err == nil {
			t.Fatalf("deselect Decode() accepted %d bytes", size)
		}
	}
}

func TestCharacterAttributeIncreaseCodecRequiresEmptyPayload(t *testing.T) {
	empty := network.EmptyClientPacket()
	if _, err := (CharacterAttributeIncreaseRequestCodec{}).Decode(empty); err != nil {
		t.Fatal(err)
	}
	empty.WriteByte(0)
	if _, err := (CharacterAttributeIncreaseRequestCodec{}).Decode(empty); err == nil {
		t.Fatal("Decode() accepted trailing data")
	}
}
