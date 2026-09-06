package lobby

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
)

func TestWriteCharacterQuickSlotConfigUsesVSROLayoutAndSlotOrder(t *testing.T) {
	player := &model.Player{}
	player.SetQuickSlots([]model.CharacterQuickSlot{
		{Sequence: 10, ContentType: 2, ContentID: 0x11223344},
		{Sequence: 1, ContentType: 1, ContentID: 0xAABBCCDD},
	})
	packet := network.EmptyPacket()
	writeCharacterQuickSlotConfig(&packet, player)

	want := []byte{
		7, 2,
		1, 1, 0xDD, 0xCC, 0xBB, 0xAA,
		10, 2, 0x44, 0x33, 0x22, 0x11,
	}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("quick-slot config = %x, want %x", got, want)
	}
}

func TestWriteCharacterQuickSlotConfigWritesEmptySection(t *testing.T) {
	packet := network.EmptyPacket()
	writeCharacterQuickSlotConfig(&packet, &model.Player{})
	if got, want := packet.Payload(), []byte{7, 0}; !bytes.Equal(got, want) {
		t.Fatalf("empty quick-slot config = %x, want %x", got, want)
	}
}

func TestWriteAutoPotionConfigUsesPersistedVSROPayload(t *testing.T) {
	player := &model.Player{}
	want := model.AutoPotionSettings{0x19, 0xDC, 0x1A, 0xD9, 0x13, 0x00, 0x8A}
	player.SetAutoPotionSettings(want, true)
	packet := network.EmptyPacket()
	writeAutoPotionConfig(&packet, player)
	if got := packet.Payload(); !bytes.Equal(got, want[:]) {
		t.Fatalf("auto-potion config = %x, want %x", got, want)
	}
}

func TestWriteAutoPotionConfigUsesStockThreeUInt16AndByteDefault(t *testing.T) {
	packet := network.EmptyPacket()
	writeAutoPotionConfig(&packet, &model.Player{})
	want := []byte{0, 0, 1, 0, 1, 0, 0}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("default auto-potion config = %x, want %x", got, want)
	}
}
