package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestInventorySizeUpdateMatchesStock3092TwoByteLayout(t *testing.T) {
	packet := BuildInventorySizeUpdate(InventorySizeUpdate{FirstByte: 0x2A, SecondByte: 0x01})
	if packet.MessageID != 0x3092 {
		t.Fatalf("inventory-size opcode = 0x%04X, want 0x3092", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{0x2A, 0x01}; !bytes.Equal(got, want) {
		t.Fatalf("inventory-size payload = %X, want %X", got, want)
	}

	input := network.EmptyClientPacket()
	if err := input.WriteBytes(packet.Payload()); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInventorySizeUpdate(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != (InventorySizeUpdate{FirstByte: 0x2A, SecondByte: 0x01}) {
		t.Fatalf("decoded 0x3092 = %+v", decoded)
	}
}

func TestDecodeInventorySizeUpdateRejectsMalformedPayload(t *testing.T) {
	for _, payload := range [][]byte{{0x01}, {0x01, 0x02, 0x03}} {
		packet := network.EmptyClientPacket()
		if err := packet.WriteBytes(payload); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeInventorySizeUpdate(&packet); err == nil {
			t.Fatalf("0x3092 accepted payload %X", payload)
		}
	}
}
