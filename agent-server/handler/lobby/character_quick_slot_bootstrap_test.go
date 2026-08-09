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
