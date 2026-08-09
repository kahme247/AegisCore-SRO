package inventory

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type itemUseRepositoryFake struct {
	calls  int
	result model.ItemUseResult
}

func (f *itemUseRepositoryFake) UseRecoveryItem(context.Context, int64, byte, uint16, model.ItemUseVitals) (model.ItemUseResult, error) {
	f.calls++
	return f.result, nil
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

func TestSpeedItemPublishesTimedBuffIcon(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7, Level: 10,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		WalkSpeed: 15, RunSpeed: 30, Session: session,
	}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		SourceSlot: 33, ItemType: 0x0EEC, NewAmount: 9,
		EffectSkillID: 3977, SpeedPercent: 100, EffectDuration: 1800000, EffectExpiresAt: time.Now().Add(30 * time.Minute),
	}}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.ConfigureTimedBuffDuration(true)
	var published []network.Packet
	var expire func()
	h.send = func(_ *server.Session, packet network.Packet) error { return nil }
	h.publish = func(_ *model.Player, packet *network.Packet) { published = append(published, *packet) }
	h.schedule = func(delay time.Duration, callback func()) {
		if delay != 30*time.Minute {
			t.Fatalf("speed effect duration = %v, want 30m", delay)
		}
		expire = callback
	}
	request := network.EmptyClientPacket()
	request.WriteByte(33)
	request.WriteUInt16(0x0EEC)
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("speed item use failed: %v", err)
	}
	if len(published) != 1 || published[0].MessageID != opcode.EntityBuffAdd || expire == nil {
		t.Fatalf("published packets = %+v, expiry scheduled = %v", published, expire != nil)
	}
	for range 3 {
		if _, err := published[0].ReadUInt32(); err != nil {
			t.Fatal(err)
		}
	}
	duration, err := published[0].ReadUInt32()
	if err != nil || duration != 1800000 || published[0].Remaining() != 0 {
		t.Fatalf("published speed duration = %d remaining %d: %v", duration, published[0].Remaining(), err)
	}
	expire()
	if len(published) != 2 || published[1].MessageID != opcode.EntityBuffRemove {
		t.Fatalf("expiration packets = %+v, want buff removal", published)
	}
}
