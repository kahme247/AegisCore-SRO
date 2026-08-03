package lobby

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
)

type deterministicInventoryStore struct {
	inventory model.Inventory
}

func (s deterministicInventoryStore) GetCharacterInventory(context.Context, int64) (model.Inventory, error) {
	return s.inventory, nil
}

func TestWriteInventoryToPacketUsesAscendingSlotOrder(t *testing.T) {
	orders := [][]byte{
		{40, model.SlotPrimaryWeapon, model.SlotHelmet},
		{model.SlotHelmet, 40, model.SlotPrimaryWeapon},
	}
	var packets [][]byte
	for _, order := range orders {
		packet := network.EmptyPacket()
		WriteInventoryToPacket(&packet, deterministicLobbyInventory(order))
		packets = append(packets, append([]byte(nil), packet.Payload()...))
	}

	if !bytes.Equal(packets[0], packets[1]) {
		t.Fatalf("same inventory produced different bytes for different map insertion orders:\nfirst:  %x\nsecond: %x", packets[0], packets[1])
	}
	if got, want := packets[0], expectedSortedInventoryPacket(); !bytes.Equal(got, want) {
		t.Fatalf("inventory packet = %x, want ascending-slot layout %x", got, want)
	}
}

func TestCharacterListAppearanceIsIndependentOfInventoryMapInsertionOrder(t *testing.T) {
	orders := [][]byte{
		{40, model.SlotPrimaryWeapon, model.SlotHelmet},
		{model.SlotHelmet, 40, model.SlotPrimaryWeapon},
	}
	var packets [][]byte
	for _, order := range orders {
		packets = append(packets, serializeCharacterListEntry(t, deterministicLobbyInventory(order)))
	}

	if !bytes.Equal(packets[0], packets[1]) {
		t.Fatalf("same character inventory produced different list bytes for different map insertion orders:\nfirst:  %x\nsecond: %x", packets[0], packets[1])
	}

	const itemCountOffset = 35
	if got, want := packets[0][itemCountOffset], byte(2); got != want {
		t.Fatalf("character list item count = %d, want %d", got, want)
	}
	offset := itemCountOffset + 1
	for _, wantRefID := range []uint32{0x1000, 0x6000} {
		if got := binary.LittleEndian.Uint32(packets[0][offset : offset+4]); got != wantRefID {
			t.Fatalf("character list item at offset %d = %#x, want %#x", offset, got, wantRefID)
		}
		offset += 5 // RefItemID plus the existing Plus byte.
	}
}

func deterministicLobbyInventory(order []byte) model.Inventory {
	itemsBySlot := map[byte]model.Item{
		model.SlotHelmet:        {SRObject: model.SRObject{RefObjectID: 0x1000}},
		model.SlotPrimaryWeapon: {SRObject: model.SRObject{RefObjectID: 0x6000}},
		40:                      {SRObject: model.SRObject{RefObjectID: 0x4000}},
	}
	items := make(map[byte]model.Item, len(order))
	for _, slot := range order {
		items[slot] = itemsBySlot[slot]
	}
	return model.Inventory{Items: items}
}

func expectedSortedInventoryPacket() []byte {
	packet := []byte{model.BaseInventorySize, 3}
	for _, slot := range []byte{model.SlotHelmet, model.SlotPrimaryWeapon, 40} {
		packet = append(packet, slot, 0, 0, 0, 0) // existing RentInfo = uint32(0)
		var refID [4]byte
		item := deterministicLobbyInventory([]byte{slot}).Items[slot]
		binary.LittleEndian.PutUint32(refID[:], item.GetRefObjectID())
		packet = append(packet, refID[:]...)
	}
	return packet
}

func serializeCharacterListEntry(t *testing.T, inventory model.Inventory) []byte {
	t.Helper()
	handler := &CharacterSelectionHandler{inventory: deterministicInventoryStore{inventory: inventory}}
	packet := network.EmptyPacket()
	char := model.Char{
		ID:         1,
		RefObjID:   1907,
		Name:       "A",
		Scale:      1,
		Level:      1,
		Exp:        1,
		Str:        20,
		Int:        20,
		StatPoints: 0,
		HP:         200,
		MP:         200,
	}
	if err := handler.writeCharacterListEntry(context.Background(), &packet, char); err != nil {
		t.Fatalf("writeCharacterListEntry() error = %v", err)
	}
	return append([]byte(nil), packet.Payload()...)
}
