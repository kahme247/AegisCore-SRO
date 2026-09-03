package character

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestResurrectionItemUseSuccessUsesStockB04CLayout(t *testing.T) {
	packet := buildResurrectionItemUseSuccess(model.ItemUseResult{SourceSlot: 14, NewAmount: 2, ItemType: 0x6EEC})
	if packet.MessageID != 0xB04C {
		t.Fatalf("resurrection item-use opcode = 0x%04X, want 0xB04C", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{1, 14, 2, 0, 0xEC, 0x6E}; !bytes.Equal(got, want) {
		t.Fatalf("resurrection item-use payload = %X, want %X", got, want)
	}
}

type resurrectionPlayersFake struct{ player *model.Player }

func (f resurrectionPlayersFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}

type resurrectionRepositoryFake struct {
	characterID int
	hp, mp      int
	updated     bool
	itemResult  model.ItemUseResult
	itemCalls   int
}

type returnPointResurrectionRepositoryFake struct {
	resurrectionRepositoryFake
	destination model.TeleportDestination
	returnCalls int
}

func (f *returnPointResurrectionRepositoryFake) ResurrectAtReturnPoint(_ context.Context, characterID, hp, mp int) (model.TeleportDestination, bool, error) {
	f.characterID, f.hp, f.mp = characterID, hp, mp
	f.returnCalls++
	return f.destination, f.updated, nil
}

type resurrectionTeleportFake struct {
	prepared, aborted, completed int
	destination                  model.TeleportDestination
}

func (f *resurrectionTeleportFake) PrepareItemReturn(*server.Session, *model.Player) error {
	f.prepared++
	return nil
}
func (f *resurrectionTeleportFake) AbortItemReturn(*server.Session) { f.aborted++ }
func (f *resurrectionTeleportFake) CompleteItemReturn(_ *server.Session, _ *model.Player, destination model.TeleportDestination) error {
	f.completed++
	f.destination = destination
	return nil
}

func (f *resurrectionRepositoryFake) UseResurrectionItem(context.Context, int64, model.ItemUseVitals) (model.ItemUseResult, error) {
	f.itemCalls++
	return f.itemResult, nil
}

func (f *resurrectionRepositoryFake) SaveVitals(context.Context, int, int, int) error { return nil }
func (f *resurrectionRepositoryFake) SpendMP(context.Context, int, int, int) (bool, error) {
	return false, nil
}
func (f *resurrectionRepositoryFake) SpendHP(context.Context, int, int, int) (bool, error) {
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
	if err := request.WriteByte(resurrectAtReturnPoint); err != nil {
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

func TestPresentPointResurrectionConsumesCatalogScroll(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, clientConn) }()
	session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "resurrection-item-test")
	t.Cleanup(func() { session.Close(); _ = clientConn.Close() })
	session.UserContext.UniqueID, session.UserContext.CharacterID = 77, 123
	// A queued potion used to race lethal damage and leave a dead runtime player
	// with non-zero HP. The resurrection request must repair that stale state.
	player := &model.Player{ID: 123, Session: session, LifeState: model.Dead, Level: 1, BaseStats: model.BaseStats{HP: 25, MP: 50, Str: 20, Int: 20}}
	player.SetUniqueID(77)
	repository := &resurrectionRepositoryFake{itemResult: model.ItemUseResult{
		Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 200, MP: 200,
		SourceSlot: 14, NewAmount: 2, ItemType: 0x6EEC,
	}}
	handler := &ResurrectionHandler{players: resurrectionPlayersFake{player: player}, repository: repository, items: repository}
	request := network.EmptyClientPacket()
	request.WriteByte(resurrectAtPresentPoint)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("present-point resurrection failed: %v", err)
	}
	if repository.itemCalls != 1 || player.GetLifeState() != model.Alive {
		t.Fatalf("item calls=%d life=%d", repository.itemCalls, player.GetLifeState())
	}
}

func TestReturnPointResurrectionUsesDesignatedHomeTeleport(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "return-point-resurrection-test")
	t.Cleanup(func() { session.Close(); _ = clientConn.Close() })
	session.UserContext.UniqueID, session.UserContext.CharacterID = 77, 123
	player := &model.Player{ID: 123, Session: session, LifeState: model.Dead, Level: 1, BaseStats: model.BaseStats{HP: 0, MP: 50, Str: 20, Int: 20}}
	player.SetUniqueID(77)
	destination := model.TeleportDestination{ID: 20, Region: 26265, X: 957, Y: -80, Z: 1508, WorldID: 1}
	repository := &returnPointResurrectionRepositoryFake{resurrectionRepositoryFake: resurrectionRepositoryFake{updated: true}, destination: destination}
	teleport := &resurrectionTeleportFake{}
	handler := &ResurrectionHandler{players: resurrectionPlayersFake{player: player}, repository: repository, teleport: teleport}
	request := network.EmptyClientPacket()
	request.WriteByte(resurrectAtReturnPoint)

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("return-point resurrection failed: %v", err)
	}
	if repository.returnCalls != 1 || repository.characterID != 123 || repository.hp != 200 || repository.mp != 200 {
		t.Fatalf("return-point persistence calls=%d character=%d vitals=%d/%d", repository.returnCalls, repository.characterID, repository.hp, repository.mp)
	}
	if teleport.prepared != 1 || teleport.aborted != 0 || teleport.completed != 1 || teleport.destination != destination {
		t.Fatalf("return-point teleport prepared=%d aborted=%d completed=%d destination=%+v", teleport.prepared, teleport.aborted, teleport.completed, teleport.destination)
	}
	if hp, mp := player.CurrentVitals(); hp != 200 || mp != 200 || player.GetLifeState() != model.Alive {
		t.Fatalf("runtime resurrection = life:%d vitals:%d/%d", player.GetLifeState(), hp, mp)
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
