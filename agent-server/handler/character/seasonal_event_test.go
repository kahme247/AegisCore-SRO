package character

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type seasonalEventRepositoryFake struct {
	characterID int64
	result      model.ChristmasExchangeResult
	err         error
}

func (repository *seasonalEventRepositoryFake) ExchangeChristmasSocks(_ context.Context, characterID int64) (model.ChristmasExchangeResult, error) {
	repository.characterID = characterID
	return repository.result, repository.err
}

func TestChristmasEventAtomicallyExchangesSocksAndReturnsSuccess(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharacterID: 20}}
	reward := model.Item{ID: 99, SRObject: model.SRObject{RefObjectID: 3658}, StackSize: 1, Data: 1}
	inventory := model.NewInventory(map[byte]model.Item{13: reward})
	player := &model.Player{ID: 20, Session: session, Inventory: model.NewInventory(nil)}
	repository := &seasonalEventRepositoryFake{result: model.ChristmasExchangeResult{SocksSlot: 13, RewardSlot: 13, RewardRefItemID: 3658, RewardQuantity: 1, RewardItem: reward, Inventory: inventory}}
	var sent []network.Packet
	h := &SeasonalEventHandler{repository: repository, players: gachaWorldFake{player: player}, enabled: true, send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	if err := h.handleChristmas(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if repository.characterID != 20 || len(sent) != 3 || sent[0].MessageID != opcode.ItemOperationResponse || sent[1].MessageID != opcode.ItemOperationResponse || sent[2].MessageID != opcode.ChristmasEventResponse {
		t.Fatalf("exchange character=%d packets=%#v", repository.characterID, sent)
	}
	if got := hex.EncodeToString(sent[2].Payload()); got != "01" {
		t.Fatalf("response = %s", got)
	}
	if item, ok := player.Inventory.Items[13]; !ok || item.ID != 99 {
		t.Fatalf("inventory = %#v", player.Inventory.Items)
	}
}

func TestChristmasEventReportsMissingSocksWithoutClaimingSuccess(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharacterID: 20}}
	player := &model.Player{ID: 20, Session: session}
	repository := &seasonalEventRepositoryFake{err: model.ErrChristmasSocksMissing}
	var sent network.Packet
	h := &SeasonalEventHandler{repository: repository, players: gachaWorldFake{player: player}, enabled: true, send: func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }}
	if err := h.handleChristmas(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(sent.Payload()); got != "0203" {
		t.Fatalf("response = %s", got)
	}
}
