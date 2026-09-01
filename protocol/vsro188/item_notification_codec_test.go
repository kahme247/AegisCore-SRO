package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestItemUsedNotificationMatchesStock305CLayout(t *testing.T) {
	packet := BuildItemUsedNotification(ItemUsedNotification{FirstValue: 0x11223344, SecondValue: 0xAABBCCDD})
	if packet.MessageID != 0x305C {
		t.Fatalf("item-used opcode = 0x%04X, want 0x305C", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 0xDD, 0xCC, 0xBB, 0xAA}; !bytes.Equal(got, want) {
		t.Fatalf("item-used payload = %X, want %X", got, want)
	}

	input := network.EmptyClientPacket()
	input.WriteBytes(packet.Payload())
	decoded, err := DecodeItemUsedNotification(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.FirstValue != 0x11223344 || decoded.SecondValue != 0xAABBCCDD {
		t.Fatalf("decoded item-used notification = %+v", decoded)
	}
}

func TestDecodeItemUsedNotificationRejectsTrailingBytes(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteBytes([]byte{1, 0, 0, 0, 2, 0, 0, 0, 3})
	if _, err := DecodeItemUsedNotification(&packet); err == nil {
		t.Fatal("0x305C decoder accepted trailing bytes")
	}
}
