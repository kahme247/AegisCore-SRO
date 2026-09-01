package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestDurabilityUpdateMatchesMaintained3052Contract(t *testing.T) {
	packet := BuildDurabilityUpdate(DurabilityUpdate{Slot: 0x0D, Value: 0x11223344})
	if packet.MessageID != 0x3052 {
		t.Fatalf("durability opcode = 0x%04X, want 0x3052", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{0x0D, 0x44, 0x33, 0x22, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("durability payload = %X, want %X", got, want)
	}

	input := network.EmptyClientPacket()
	if err := input.WriteBytes(packet.Payload()); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeDurabilityUpdate(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != (DurabilityUpdate{Slot: 0x0D, Value: 0x11223344}) {
		t.Fatalf("decoded 0x3052 = %+v", decoded)
	}
}

func TestDecodeDurabilityUpdateRejectsMalformedPayload(t *testing.T) {
	for _, payload := range [][]byte{{0x0D, 0x01, 0x02}, {0x0D, 0x01, 0x02, 0x03, 0x04, 0x05}} {
		packet := network.EmptyClientPacket()
		if err := packet.WriteBytes(payload); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeDurabilityUpdate(&packet); err == nil {
			t.Fatalf("0x3052 accepted payload %X", payload)
		}
	}
}
