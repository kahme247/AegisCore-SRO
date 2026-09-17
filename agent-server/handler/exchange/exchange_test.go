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
		{name: "cancelled", packet: func() (uint16, []byte) { p := exchangeCancelled(); return p.MessageID, p.Payload() }, wantID: opcode.ExchangeCancelledResponse, payload: nil},
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

	invitation := exchangeInvitation(0x12345678)
	want = []byte{1, 0x78, 0x56, 0x34, 0x12}
	if invitation.MessageID != opcode.ExchangeInvitationResponse || !bytes.Equal(invitation.Payload(), want) {
		t.Fatalf("invitation packet = (0x%04X, % X), want (0x%04X, % X)", invitation.MessageID, invitation.Payload(), opcode.ExchangeInvitationResponse, want)
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

func TestInvitationAcceptanceStartsExchangeForBothPlayers(t *testing.T) {
	requesterSession := &server.Session{UserContext: server.UserContext{UserID: 10, CharacterID: 1, UniqueID: 101}}
	responderSession := &server.Session{UserContext: server.UserContext{UserID: 20, CharacterID: 2, UniqueID: 202}}
	requester := &model.Player{ID: 1, Session: requesterSession, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 101}}
	responder := &model.Player{ID: 2, Session: responderSession, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 202}}
	manager := NewManager(exchangeTestPlayers{101: requester, 202: responder}, &countingExchangeRepository{})
	state := &exchangeSession{
		a: &participant{player: requester, items: make(map[byte]model.ExchangeOfferedItem)},
		b: &participant{player: responder, items: make(map[byte]model.ExchangeOfferedItem)},
	}
	manager.byChar[requester.ID], manager.byChar[responder.ID] = state, state

	type sentPacket struct {
		session *server.Session
		packet  network.Packet
	}
	var sent []sentPacket
	manager.send = func(session *server.Session, packet network.Packet) error {
		sent = append(sent, sentPacket{session: session, packet: packet})
		return nil
	}
	accept := network.EmptyPacket()
	accept.WriteByte(1)
	if err := manager.handleInvitation(context.Background(), responderSession, accept); err != nil {
		t.Fatalf("handleInvitation() error = %v", err)
	}
	if !state.started || len(sent) != 2 {
		t.Fatalf("started = %v, packets = %d; want true, 2", state.started, len(sent))
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
