package vsro188

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestItemUseRequestMatchesStock704CForms(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want ItemUseRequest
	}{
		{"ordinary", "0eec08", ItemUseRequest{InventorySlot: 0x0E, ItemType: 0x08EC}},
		{"reverse-map", "0ded290707000000", ItemUseRequest{InventorySlot: 0x0D, ItemType: 0x29ED, HasOption: true, Option: 7, MapID: 7}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire, err := hex.DecodeString(test.wire)
			if err != nil {
				t.Fatal(err)
			}
			packet := network.EmptyClientPacket()
			if err := packet.WriteBytes(wire); err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeItemUseRequest(&packet)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, test.want) {
				t.Fatalf("decoded request = %+v, want %+v", decoded, test.want)
			}
			built, err := BuildItemUseRequest(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(built.Payload()); got != test.wire {
				t.Fatalf("encoded request = %s, want %s", got, test.wire)
			}
		})
	}
}

func TestItemUseRequestPreservesCOSTargetPayload(t *testing.T) {
	want := ItemUseRequest{
		InventorySlot:  19,
		ItemType:       0x29ed,
		ItemData:       []byte{0x78, 0x56, 0x34, 0x12},
		HasCOSTarget:   true,
		TargetObjectID: 0x12345678,
	}
	packet, err := BuildItemUseRequest(want)
	if err != nil {
		t.Fatalf("build COS-target 0x704C: %v", err)
	}
	got, err := DecodeItemUseRequest(&packet)
	if err != nil {
		t.Fatalf("decode COS-target 0x704C: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("COS-target 0x704C = %+v, want %+v", got, want)
	}
}

func TestItemUseRequestPreservesOneByteCOSSlotSelector(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(13)
	packet.WriteUInt16(0x29ED)
	packet.WriteByte(17)
	request, err := DecodeItemUseRequest(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.HasOption || request.HasCOSTarget || !reflect.DeepEqual(request.ItemData, []byte{17}) {
		t.Fatalf("one-byte COS selector = %#v", request)
	}
}

func TestItemUseResponsesMatchStockB04CLayouts(t *testing.T) {
	success := BuildItemUseSuccess(ItemUseSuccess{SourceSlot: 14, NewAmount: 3, ItemType: 0x08EC})
	if got, want := hex.EncodeToString(success.Payload()), "010e0300ec08"; got != want {
		t.Fatalf("success payload = %s, want %s", got, want)
	}
	failure := BuildItemUseFailure(0x183E)
	if got, want := hex.EncodeToString(failure.Payload()), "023e18"; got != want {
		t.Fatalf("failure payload = %s, want %s", got, want)
	}
}
