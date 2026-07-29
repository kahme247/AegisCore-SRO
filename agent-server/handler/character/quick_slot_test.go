package character

import (
	"context"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/server"
)

type quickSlotWorldFake struct{ player *model.Player }

func (f quickSlotWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) { return f.player, nil }

type quickSlotRepositoryFake struct {
	characterID int
	slot        model.CharacterQuickSlot
	err         error
}

func (*quickSlotRepositoryFake) LoadQuickSlots(context.Context, int) ([]model.CharacterQuickSlot, error) {
	return nil, nil
}

func (f *quickSlotRepositoryFake) SaveQuickSlot(_ context.Context, characterID int, slot model.CharacterQuickSlot) error {
	f.characterID = characterID
	f.slot = slot
	return f.err
}

func TestQuickSlotUpdatePersistsAndUpdatesPlayerWithoutResponse(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{ID: 42, Session: session}
	repository := &quickSlotRepositoryFake{}
	handler := &QuickSlotHandler{world: quickSlotWorldFake{player: player}, repository: repository}

	packet := network.EmptyClientPacket()
	packet.WriteByte(quickSlotConfigType)
	packet.WriteByte(3)
	packet.WriteByte(2)
	packet.WriteUInt32(0x01020304)
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}

	want := model.CharacterQuickSlot{Sequence: 3, ContentType: 2, ContentID: 0x01020304}
	if repository.characterID != 42 || repository.slot != want {
		t.Fatalf("saved quick slot = character %d %#v, want character 42 %#v", repository.characterID, repository.slot, want)
	}
	slots := player.QuickSlotSnapshot()
	if len(slots) != 1 || slots[0] != want {
		t.Fatalf("runtime quick slots = %#v, want %#v", slots, []model.CharacterQuickSlot{want})
	}
}

func TestQuickSlotClearDeletesRuntimeSlot(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{ID: 42, Session: session}
	player.SetQuickSlots([]model.CharacterQuickSlot{{Sequence: 3, ContentType: 2, ContentID: 9}})
	handler := &QuickSlotHandler{world: quickSlotWorldFake{player: player}, repository: &quickSlotRepositoryFake{}}

	packet := network.EmptyClientPacket()
	packet.WriteByte(quickSlotConfigType)
	packet.WriteByte(3)
	packet.WriteByte(0)
	packet.WriteUInt32(0)
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	if slots := player.QuickSlotSnapshot(); len(slots) != 0 {
		t.Fatalf("runtime quick slots after clear = %#v", slots)
	}
}

func TestQuickSlotUpdateRejectsMalformedPayloadAndOutOfRangeSlot(t *testing.T) {
	handler := &QuickSlotHandler{}
	if err := handler.handlePacket(context.Background(), &server.Session{}, network.EmptyClientPacket()); err == nil {
		t.Fatal("accepted empty config update")
	}

	packet := network.EmptyClientPacket()
	packet.WriteByte(quickSlotConfigType)
	packet.WriteByte(model.MaxCharacterQuickSlots)
	packet.WriteByte(1)
	packet.WriteUInt32(1)
	if err := handler.handlePacket(context.Background(), &server.Session{}, packet); err == nil {
		t.Fatal("accepted out-of-range quick slot")
	}
}
