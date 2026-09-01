package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestGoldPointUpdateMatchesMaintained304ELayout(t *testing.T) {
	update := GoldPointUpdate{Value: 0x1122334455667788, TrailingFlag: 0}
	packet := BuildGoldPointUpdate(update)
	if packet.MessageID != 0x304E {
		t.Fatalf("gold point opcode = 0x%04X, want 0x304E", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{1, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0}; !bytes.Equal(got, want) {
		t.Fatalf("gold point payload = %X, want %X", got, want)
	}
	decoded, err := DecodeGoldPointUpdate(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != update {
		t.Fatalf("decoded gold point update = %+v, want %+v", decoded, update)
	}
}

func TestDecodeGoldPointUpdateRejectsOtherPointKinds(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(2)
	packet.WriteUInt32(18)
	if _, err := DecodeGoldPointUpdate(&packet); err == nil {
		t.Fatal("0x304E decoder accepted non-gold point kind")
	}
}

func TestSkillPointUpdateMatchesMaintained304ELayout(t *testing.T) {
	update := SkillPointUpdate{Value: 18, TrailingFlag: 0}
	packet := BuildSkillPointUpdate(update)
	if got, want := packet.Payload(), []byte{2, 18, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("skill-point payload = %X, want %X", got, want)
	}
	decoded, err := DecodeSkillPointUpdate(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != update {
		t.Fatalf("decoded skill-point update = %+v, want %+v", decoded, update)
	}
}
