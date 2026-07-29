package inventory

import (
	"context"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

type itemUseRepositoryFake struct{ calls int }

func (f *itemUseRepositoryFake) UseRecoveryItem(context.Context, int64, byte, uint16, model.ItemUseVitals) (model.ItemUseResult, error) {
	f.calls++
	return model.ItemUseResult{}, nil
}

func TestItemUseMalformedPayloadFailsBeforeIdentityOrDatabaseLookup(t *testing.T) {
	lookup := &inventoryHandlerPlayerLookupFake{}
	repository := &itemUseRepositoryFake{}
	h := NewItemUseHandler(lookup, repository)
	var sent []network.Packet
	h.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	packet := network.EmptyClientPacket()
	packet.WriteByte(14)
	packet.WriteUInt16(0x08EC)
	packet.WriteByte(0xFF)
	if err := h.handlePacket(context.Background(), &server.Session{}, packet); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	if lookup.calls != 0 || repository.calls != 0 {
		t.Fatalf("malformed request reached lookup/repository: %d/%d", lookup.calls, repository.calls)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.InventoryItemUseResponse {
		t.Fatalf("failure packets = %+v, want one 0xB04C response", sent)
	}
}

func TestItemUseResponseMatchesExactVSRO188Parser(t *testing.T) {
	success := buildItemUseSuccess(14, 0x08EC, 49)
	result, e1 := success.ReadByte()
	slot, e2 := success.ReadByte()
	amount, e3 := success.ReadUInt16()
	itemType, e4 := success.ReadUInt16()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || result != 1 || slot != 14 || itemType != 0x08EC || amount != 49 || success.Remaining() != 0 {
		t.Fatalf("success response = result %d slot %d itemType 0x%04X amount %d remaining %d errors %v/%v/%v/%v", result, slot, itemType, amount, success.Remaining(), e1, e2, e3, e4)
	}
	failure := buildItemUseFailure()
	result, e1 = failure.ReadByte()
	errorCode, e2 := failure.ReadUInt16()
	if e1 != nil || e2 != nil || result != 2 || errorCode != 0 || failure.Remaining() != 0 {
		t.Fatalf("failure response = result %d errorCode %d remaining %d errors %v/%v", result, errorCode, failure.Remaining(), e1, e2)
	}
}
