package character

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/server"
)

type resurrectionPlayersFake struct{ player *model.Player }

func (f resurrectionPlayersFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}

type resurrectionRepositoryFake struct {
	characterID int
	hp, mp      int
	updated     bool
}

func (f *resurrectionRepositoryFake) SaveVitals(context.Context, int, int, int) error { return nil }
func (f *resurrectionRepositoryFake) SpendMP(context.Context, int, int, int) (bool, error) {
	return false, nil
}

func (f *resurrectionRepositoryFake) Resurrect(_ context.Context, characterID, hp, mp int) (bool, error) {
	f.characterID, f.hp, f.mp = characterID, hp, mp
	return f.updated, nil
}

func TestResurrectionRestoresPersistedAndRuntimeVitals(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, clientConn) }()
	session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "resurrection-test")
	t.Cleanup(func() {
		session.Close()
		_ = clientConn.Close()
	})
	session.UserContext.UniqueID = 77
	session.UserContext.CharacterID = 123
	player := &model.Player{
		ID:        123,
		Session:   session,
		LifeState: model.Dead,
		Level:     1,
		BaseStats: model.BaseStats{HP: 0, MP: 50, Str: 20, Int: 20},
	}
	player.SetUniqueID(77)
	repository := &resurrectionRepositoryFake{updated: true}
	handler := &ResurrectionHandler{players: resurrectionPlayersFake{player: player}, repository: repository}
	request := network.EmptyClientPacket()
	if err := request.WriteByte(resurrectAtPresentPoint); err != nil {
		t.Fatalf("build resurrection request: %v", err)
	}

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("resurrection failed: %v", err)
	}
	if repository.characterID != 123 || repository.hp != 200 || repository.mp != 200 {
		t.Fatalf("persisted resurrection = character:%d hp:%d mp:%d", repository.characterID, repository.hp, repository.mp)
	}
	if hp, mp := player.CurrentVitals(); hp != 200 || mp != 200 {
		t.Fatalf("runtime resurrection vitals = %d/%d, want 200/200", hp, mp)
	}
	if player.GetLifeState() != model.Alive {
		t.Fatalf("runtime resurrection state = %d, want alive", player.GetLifeState())
	}
}

func TestResurrectionRejectsUnknownMode(t *testing.T) {
	request := network.EmptyClientPacket()
	if err := request.WriteByte(9); err != nil {
		t.Fatalf("build resurrection request: %v", err)
	}
	handler := &ResurrectionHandler{repository: &resurrectionRepositoryFake{}}
	if err := handler.handlePacket(context.Background(), nil, request); err == nil {
		t.Fatal("unknown resurrection mode succeeded")
	}
}
