package exchange

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type exchangeTestPlayers map[uint32]*model.Player

func (players exchangeTestPlayers) GetPlayerByUniqueId(id uint32) (*model.Player, error) {
	return players[id], nil
}

type countingExchangeRepository struct{ calls atomic.Int32 }

func (repository *countingExchangeRepository) CommitExchange(_ context.Context, first, second model.ExchangeOffer) (model.ExchangeCommitResult, error) {
	repository.calls.Add(1)
	return model.ExchangeCommitResult{
		FirstGold:       100 - first.Gold + second.Gold,
		SecondGold:      100 - second.Gold + first.Gold,
		FirstInventory:  model.NewInventory(nil),
		SecondInventory: model.NewInventory(nil),
	}, nil
}

func TestExchangeControlPacketsMatchProtocol(t *testing.T) {
	tests := []struct {
		name    string
		packet  func() (uint16, []byte)
		wantID  uint16
		payload []byte
	}{
		{name: "confirm success", packet: func() (uint16, []byte) {
			p := exchangeSuccess(opcode.ExchangeConfirmResponse)
			return p.MessageID, p.Payload()
		}, wantID: opcode.ExchangeConfirmResponse, payload: []byte{1}},
		{name: "approve failure", packet: func() (uint16, []byte) {
			p := exchangeFailure(opcode.ExchangeApproveResponse)
			return p.MessageID, p.Payload()
		}, wantID: opcode.ExchangeApproveResponse, payload: []byte{2, 0, 0}},
		{name: "inventory operation success", packet: func() (uint16, []byte) {
			p := inventoryOperationSuccess(4)
			return p.MessageID, p.Payload()
		}, wantID: opcode.ItemOperationResponse, payload: []byte{1, 4}},
		{name: "inventory operation failure", packet: func() (uint16, []byte) {
			p := inventoryOperationFailure()
			return p.MessageID, p.Payload()
		}, wantID: opcode.ItemOperationResponse, payload: []byte{2, 0}},
		{name: "cancelled", packet: func() (uint16, []byte) { p := exchangeCancelled(); return p.MessageID, p.Payload() }, wantID: opcode.ExchangeCancelledResponse, payload: []byte{0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, payload := test.packet()
			if id != test.wantID || !bytes.Equal(payload, test.payload) {
				t.Fatalf("packet = (0x%04X, % X), want (0x%04X, % X)", id, payload, test.wantID, test.payload)
			}
		})
	}

	start := exchangeStartSuccess(0x12345678)
	want := []byte{1, 0x78, 0x56, 0x34, 0x12}
	if start.MessageID != opcode.ExchangeStartResponse || !bytes.Equal(start.Payload(), want) {
		t.Fatalf("start packet = (0x%04X, % X), want (0x%04X, % X)", start.MessageID, start.Payload(), opcode.ExchangeStartResponse, want)
	}

}

func TestExchangeGoldUpdateMatchesClientHandlerLayout(t *testing.T) {
	packet := buildExchangeGoldUpdate(0x1122334455667788)
	want := []byte{2, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}
	if packet.MessageID != opcode.ExchangeUpdateResponse || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("gold update = (0x%04X, % X), want (0x%04X, % X)", packet.MessageID, packet.Payload(), opcode.ExchangeUpdateResponse, want)
	}
}

func TestBuildItemSnapshotCountsOnlyExistingOfferedItems(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, Inventory: model.NewInventory(nil)}
	owner := &participant{player: player, items: map[byte]model.ExchangeOfferedItem{
		0: {SourceSlot: 13, ExchangeSlot: 0, ItemID: 999},
	}}
	packet := buildItemSnapshot(owner, true)
	payload := packet.Payload()
	if packet.MessageID != opcode.ExchangeUpdateItemsResponse || len(payload) != 5 {
		t.Fatalf("snapshot = (0x%04X, % X), want 5-byte empty snapshot", packet.MessageID, payload)
	}
	if uid := binary.LittleEndian.Uint32(payload[:4]); uid != 77 || payload[4] != 0 {
		t.Fatalf("snapshot header = uid %d count %d, want uid 77 count 0", uid, payload[4])
	}
}

func TestPeerSnapshotProjectsTemporaryEnhancementAndExpiration(t *testing.T) {
	now := time.Now().UTC()
	item := model.Item{
		ID: 9001, OptLevel: 3, Data: 100,
		SRObject: model.SRObject{
			RefObjectID: 7001,
			TypeInfo:    model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2},
		},
	}
	player := &model.Player{
		SRObject:  model.SRObject{UniqueID: 77},
		Inventory: model.NewInventory(map[byte]model.Item{30: item}),
	}
	player.ApplyItemService(model.ItemServiceEffect{
		Kind:                    model.ItemServiceReinforceWeapon,
		TargetSlot:              30,
		TargetSlotSet:           true,
		TargetBaseOptLevel:      3,
		TargetProjectedOptLevel: 5,
		ActivatedAt:             now.Add(-time.Minute),
		ExpiresAt:               now.Add(2 * time.Hour),
	})
	owner := &participant{player: player, items: map[byte]model.ExchangeOfferedItem{
		0: {SourceSlot: 30, ExchangeSlot: 0, ItemID: item.ID},
	}}

	packet := buildItemSnapshot(owner, false)
	payload := packet.Payload()
	if len(payload) < 25 || payload[4] != 1 || payload[5] != 0 {
		t.Fatalf("peer snapshot header = % X", payload)
	}
	if rentType := binary.LittleEndian.Uint32(payload[6:10]); rentType != 1 {
		t.Fatalf("rent type = %d, want temporary period type 1", rentType)
	}
	if expires := binary.LittleEndian.Uint32(payload[16:20]); expires != uint32(now.Add(2*time.Hour).Unix()) {
		t.Fatalf("expiration = %d, want %d", expires, now.Add(2*time.Hour).Unix())
	}
	if refID := binary.LittleEndian.Uint32(payload[20:24]); refID != item.GetRefObjectID() {
		t.Fatalf("reference = %d, want %d", refID, item.GetRefObjectID())
	}
	if level := payload[24]; level != 5 {
		t.Fatalf("projected option level = %d, want 5", level)
	}
	if original := player.Inventory.Snapshot().Items[30]; original.OptLevel != 3 || original.RentInfo.Type != 0 {
		t.Fatalf("persistent item mutated by snapshot: %+v", original)
	}
}

func TestStartCreatesExchangeForBothPlayers(t *testing.T) {
	requesterSession := &server.Session{UserContext: server.UserContext{UserID: 10, CharacterID: 1, UniqueID: 101}}
	responderSession := &server.Session{UserContext: server.UserContext{UserID: 20, CharacterID: 2, UniqueID: 202}}
	requester := &model.Player{ID: 1, Session: requesterSession, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 101}}
	responder := &model.Player{ID: 2, Session: responderSession, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 202}}
	requester.KnownObjectList = model.NewKnownObjectList(requester)
	requester.KnownObjectList.AddObject(responder)
	requester.SetLifeState(model.Alive)
	responder.SetLifeState(model.Alive)
	manager := NewManager(exchangeTestPlayers{101: requester, 202: responder}, &countingExchangeRepository{})

	type sentPacket struct {
		session *server.Session
		packet  network.Packet
	}
	var sent []sentPacket
	manager.send = func(session *server.Session, packet network.Packet) error {
		sent = append(sent, sentPacket{session: session, packet: packet})
		return nil
	}
	request := network.EmptyPacket()
	request.WriteUInt32(responder.GetUniqueID())
	if err := manager.handleStart(context.Background(), requesterSession, request); err != nil {
		t.Fatalf("handleStart() error = %v", err)
	}
	if manager.Busy(requester.ID) == false || manager.Busy(responder.ID) == false || len(sent) != 2 {
		t.Fatalf("exchange active = (%v, %v), packets = %d; want true, true, 2", manager.Busy(requester.ID), manager.Busy(responder.ID), len(sent))
	}
	if sent[0].session != requesterSession || sent[0].packet.MessageID != opcode.ExchangeStartResponse || !bytes.Equal(sent[0].packet.Payload(), []byte{1, 202, 0, 0, 0}) {
		t.Fatalf("requester packet = (0x%04X, % X)", sent[0].packet.MessageID, sent[0].packet.Payload())
	}
	if sent[1].session != responderSession || sent[1].packet.MessageID != opcode.ExchangeStartedResponse || !bytes.Equal(sent[1].packet.Payload(), []byte{101, 0, 0, 0}) {
		t.Fatalf("responder packet = (0x%04X, % X)", sent[1].packet.MessageID, sent[1].packet.Payload())
	}
}

func TestFirstFreeExchangeSlot(t *testing.T) {
	items := make(map[byte]model.ExchangeOfferedItem, maxExchangeItems)
	for slot := byte(0); slot < maxExchangeItems; slot++ {
		items[slot] = model.ExchangeOfferedItem{ExchangeSlot: slot}
	}
	if got := firstFreeExchangeSlot(items); got != 0xff {
		t.Fatalf("firstFreeExchangeSlot(full) = %d, want 255", got)
	}
	delete(items, 4)
	if got := firstFreeExchangeSlot(items); got != 4 {
		t.Fatalf("firstFreeExchangeSlot() = %d, want 4", got)
	}
}

func TestConcurrentApprovalsCommitExactlyOnce(t *testing.T) {
	firstSession := &server.Session{UserContext: server.UserContext{UserID: 10, CharacterID: 1, UniqueID: 101}}
	secondSession := &server.Session{UserContext: server.UserContext{UserID: 20, CharacterID: 2, UniqueID: 202}}
	first := &model.Player{ID: 1, Session: firstSession, Gold: 100, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 101}}
	second := &model.Player{ID: 2, Session: secondSession, Gold: 100, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 202}}
	repository := &countingExchangeRepository{}
	manager := NewManager(exchangeTestPlayers{101: first, 202: second}, repository)
	manager.send = func(*server.Session, network.Packet) error { return nil }
	state := &exchangeSession{
		a:       &participant{player: first, items: make(map[byte]model.ExchangeOfferedItem), confirmed: true},
		b:       &participant{player: second, items: make(map[byte]model.ExchangeOfferedItem), confirmed: true},
		started: true,
	}
	manager.byChar[first.ID], manager.byChar[second.ID] = state, state

	var wait sync.WaitGroup
	wait.Add(2)
	for _, session := range []*server.Session{firstSession, secondSession} {
		go func(session *server.Session) {
			defer wait.Done()
			if err := manager.handleApprove(context.Background(), session, network.EmptyPacket()); err != nil {
				t.Errorf("handleApprove() error = %v", err)
			}
		}(session)
	}
	wait.Wait()

	if calls := repository.calls.Load(); calls != 1 {
		t.Fatalf("CommitExchange() calls = %d, want 1", calls)
	}
	if manager.Busy(first.ID) || manager.Busy(second.ID) {
		t.Fatal("completed exchange remains busy")
	}
	if until := manager.cooldowns[orderedPair(first.ID, second.ID)]; time.Until(until) <= 0 {
		t.Fatal("completed exchange did not start same-player cooldown")
	}
}
