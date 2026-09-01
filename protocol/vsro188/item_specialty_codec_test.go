package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSpecialtyRequestMatchesRecovered7034Layout(t *testing.T) {
	want := SpecialtyRequest{FirstSelector: 3, SecondSelector: 4, Quantity: 0x1234}
	packet := BuildSpecialtyRequest(want)
	if got, expected := packet.Payload(), []byte{0x2B, 3, 4, 0x34, 0x12}; !bytes.Equal(got, expected) {
		t.Fatalf("request payload = % X, want % X", got, expected)
	}
	input := network.EmptyClientPacket()
	_ = input.WriteBytes(packet.Payload())
	decoded, err := DecodeSpecialtyRequest(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != want {
		t.Fatalf("decoded request = %+v, want %+v", decoded, want)
	}
}

func TestSpecialtySuccessMatchesRecoveredB034Envelope(t *testing.T) {
	packet := BuildSpecialtySuccess(SpecialtySuccess{
		FirstSelector:     3,
		SecondSelector:    4,
		DestinationSlots:  []byte{13, 14},
		Quantity:          0x1234,
		ExtraChildCount:   1,
		ExtraChildPayload: []byte{0xAA, 0xBB},
	})
	want := []byte{1, 0x2B, 3, 4, 2, 13, 14, 0x34, 0x12, 1, 0xAA, 0xBB}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("success payload = % X, want % X", got, want)
	}
}
