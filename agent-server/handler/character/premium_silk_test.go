package character

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type premiumSilkRepositoryFake struct{ entries []model.ItemMallHistoryEntry }

func (f premiumSilkRepositoryFake) SilkHistory(context.Context, uint32, model.ItemMallCurrency, int) ([]model.ItemMallHistoryEntry, error) {
	return f.entries, nil
}

type premiumSilkPlayersFake struct{ player *model.Player }

func (f premiumSilkPlayersFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}

type premiumDecorationRepositoryFake struct {
	characterID int
	state       model.PremiumDecorationState
}

func (f *premiumDecorationRepositoryFake) LoadPremiumDecoration(context.Context, int) (model.PremiumDecorationState, error) {
	return f.state, nil
}

func (f *premiumDecorationRepositoryFake) SavePremiumDecoration(_ context.Context, characterID int, state model.PremiumDecorationState) error {
	f.characterID, f.state = characterID, state
	return nil
}

func TestPremiumSilkHistoryAndDecorationAreOperational(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UserID: 4, UniqueID: 7}}
	player := &model.Player{ID: 9, Session: session, SRObject: model.SRObject{UniqueID: 7}}
	decorations := &premiumDecorationRepositoryFake{}
	handler := &PremiumSilkHandler{
		repository:  premiumSilkRepositoryFake{entries: []model.ItemMallHistoryEntry{{Timestamp: time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC), Amount: 25}}},
		decorations: decorations,
		players:     premiumSilkPlayersFake{player: player},
	}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	history := network.EmptyClientPacket()
	history.WriteByte(1)
	history.WriteByte(1)
	if err := handler.handleSilkHistory(context.Background(), session, history); err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.SilkHistoryResponse {
		t.Fatalf("history packets = %#v", sent)
	}
	deco := network.EmptyClientPacket()
	deco.WriteUInt32(10)
	deco.WriteUInt32(20)
	if err := handler.handlePremiumDecoration(context.Background(), session, deco); err != nil {
		t.Fatalf("decoration: %v", err)
	}
	if state := player.PremiumDecoration(); state.Token != 10 || state.Reference != 20 || state.Value != 1 {
		t.Fatalf("decoration state = %#v", state)
	}
	if decorations.characterID != player.ID || decorations.state != player.PremiumDecoration() {
		t.Fatalf("persisted decoration = character %d, state %#v", decorations.characterID, decorations.state)
	}
	if len(sent) != 3 || sent[1].MessageID != opcode.PremiumDeco || sent[2].MessageID != opcode.PremiumDecoUseResponse {
		t.Fatalf("decoration packets = %#v", sent)
	}
	pause := network.EmptyClientPacket()
	pause.WriteByte(1)
	pause.WriteByte(0)
	if err := handler.handlePremiumPause(context.Background(), session, pause); err != nil || !player.PremiumCategoryPaused(1) {
		t.Fatalf("pause state = %t, %v", player.PremiumCategoryPaused(1), err)
	}
}
