package vsro188

import (
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
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

func TestEntityPlayerSelectResponseCodecWritesStockBranches(t *testing.T) {
	for _, test := range []struct {
		name     string
		response EntityPlayerSelectResponse
		want     []byte
	}{
		{
			name:     "no job omits level",
			response: EntityPlayerSelectResponse{UniqueID: 0x12345678, JobType: 4},
			want:     []byte{1, 0x78, 0x56, 0x34, 0x12, 1, 5, 4},
		},
		{
			name:     "active job includes level",
			response: EntityPlayerSelectResponse{UniqueID: 0x12345678, JobType: 2, JobLevel: 7, HasJobLevel: true},
			want:     []byte{1, 0x78, 0x56, 0x34, 0x12, 1, 5, 2, 7},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet, err := (EntityPlayerSelectResponseCodec{}).Encode(test.response)
			if err != nil {
				t.Fatal(err)
			}
			if packet.MessageID != opcode.EntitySelectResponse {
				t.Fatalf("message id = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntitySelectResponse)
			}
			if got := packet.Payload(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("payload = %x, want %x", got, test.want)
			}
		})
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
