package character

import (
	"errors"
	"io"
	"net"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

type fakeGameReadyPlayers struct {
	player      *model.Player
	err         error
	lookups     int
	activations int
}

func (f *fakeGameReadyPlayers) GetPlayerByUniqueId(_ uint32) (*model.Player, error) {
	f.lookups++
	if f.err != nil {
		return nil, f.err
	}
	return f.player, nil
}

func (f *fakeGameReadyPlayers) ActivatePlayer(player *model.Player) []service.VisibilityDelta {
	f.activations++
	if player.CurrentHP() == 0 {
		player.SetLifeState(model.Dead)
	} else {
		player.SetLifeState(model.Alive)
	}
	return nil
}

func TestGameReadyRejectsOutOfOrderTrailingAndDuplicateRequests(t *testing.T) {
	session := newGameReadyTestSession(t)
	player := &model.Player{
		ID:        123,
		Session:   session,
		CharName:  "staged-character",
		LifeState: model.Spawning,
		BaseStats: model.BaseStats{HP: 100, MP: 100},
	}
	players := &fakeGameReadyPlayers{player: player}
	handler := NewGameReadyHandler(players)

	empty := network.EmptyClientPacket()
	if err := handler.handleSessionPacket(nil, session, empty); err == nil {
		t.Fatal("out-of-order GameReady succeeded before character loading")
	}
	if got, want := session.LifecyclePhase(), server.SessionLobby; got != want {
		t.Fatalf("out-of-order GameReady changed lifecycle to %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("out-of-order GameReady changed life state to %d, want %d", got, want)
	}
	if players.lookups != 0 {
		t.Fatalf("out-of-order GameReady queried staged player %d times, want 0", players.lookups)
	}

	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	trailing := network.EmptyClientPacket()
	if err := trailing.WriteByte(0xFF); err != nil {
		t.Fatalf("build trailing GameReady packet: %v", err)
	}
	if err := handler.handleSessionPacket(nil, session, trailing); err == nil {
		t.Fatal("GameReady with trailing data succeeded")
	}
	if got, want := session.LifecyclePhase(), server.SessionCharacterLoading; got != want {
		t.Fatalf("trailing GameReady changed lifecycle to %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("trailing GameReady changed life state to %d, want %d", got, want)
	}
	if players.lookups != 0 {
		t.Fatalf("trailing GameReady queried staged player %d times, want 0", players.lookups)
	}

	if err := handler.handleSessionPacket(nil, session, empty); err != nil {
		t.Fatalf("valid GameReady rejected: %v", err)
	}
	if got, want := session.LifecyclePhase(), server.SessionInWorld; got != want {
		t.Fatalf("valid GameReady lifecycle = %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Alive; got != want {
		t.Fatalf("valid GameReady life state = %d, want %d", got, want)
	}
	if players.lookups != 1 {
		t.Fatalf("valid GameReady lookups = %d, want 1", players.lookups)
	}
	if players.activations != 1 {
		t.Fatalf("valid GameReady visibility activations = %d, want 1", players.activations)
	}

	if err := handler.handleSessionPacket(nil, session, empty); err == nil {
		t.Fatal("duplicate GameReady succeeded")
	}
	if got, want := session.LifecyclePhase(), server.SessionInWorld; got != want {
		t.Fatalf("duplicate GameReady changed lifecycle to %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Alive; got != want {
		t.Fatalf("duplicate GameReady changed life state to %d, want %d", got, want)
	}
	if players.lookups != 1 {
		t.Fatalf("duplicate GameReady lookups = %d, want 1", players.lookups)
	}
	if players.activations != 1 {
		t.Fatalf("duplicate GameReady visibility activations = %d, want 1", players.activations)
	}
}

func TestGameReadyPreservesPersistedDeathState(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	player := &model.Player{
		ID:        session.UserContext.CharacterID,
		Session:   session,
		CharName:  session.UserContext.CharName,
		LifeState: model.Spawning,
		BaseStats: model.BaseStats{HP: 0, MP: 75},
	}
	handler := NewGameReadyHandler(&fakeGameReadyPlayers{player: player})

	if err := handler.handleSessionPacket(nil, session, network.EmptyClientPacket()); err != nil {
		t.Fatalf("dead character GameReady rejected: %v", err)
	}
	if player.GetLifeState() != model.Dead {
		t.Fatalf("loaded zero-HP character state = %d, want dead", player.GetLifeState())
	}
}

func TestGameReadyRejectsAStagedCharacterOwnedByAnotherSession(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	otherSession := newGameReadyTestSession(t)
	player := &model.Player{
		ID:        123,
		Session:   otherSession,
		CharName:  "staged-character",
		LifeState: model.Spawning,
	}
	handler := NewGameReadyHandler(&fakeGameReadyPlayers{player: player})

	if err := handler.handleSessionPacket(nil, session, network.EmptyClientPacket()); err == nil {
		t.Fatal("GameReady accepted a player owned by another session")
	}
	if got, want := session.LifecyclePhase(), server.SessionCharacterLoading; got != want {
		t.Fatalf("foreign player GameReady changed lifecycle to %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("foreign player GameReady changed life state to %d, want %d", got, want)
	}
}

func TestGameReadyRejectsMissingStagedPlayer(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	handler := NewGameReadyHandler(&fakeGameReadyPlayers{err: errors.New("not found")})

	if err := handler.handleSessionPacket(nil, session, network.EmptyClientPacket()); err == nil {
		t.Fatal("GameReady accepted a missing staged player")
	}
	if got, want := session.LifecyclePhase(), server.SessionCharacterLoading; got != want {
		t.Fatalf("missing player GameReady changed lifecycle to %s, want %s", got, want)
	}
}

func TestGameReadyRejectsMismatchedStagedCharacterID(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	player := &model.Player{
		ID:        999,
		Session:   session,
		CharName:  session.UserContext.CharName,
		LifeState: model.Spawning,
	}
	handler := NewGameReadyHandler(&fakeGameReadyPlayers{player: player})

	if err := handler.handleSessionPacket(nil, session, network.EmptyClientPacket()); err == nil {
		t.Fatal("GameReady accepted a mismatched staged character ID")
	}
	if got, want := session.LifecyclePhase(), server.SessionCharacterLoading; got != want {
		t.Fatalf("mismatched character ID changed lifecycle to %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("mismatched character ID changed life state to %d, want %d", got, want)
	}
}

func newGameReadyTestSession(t *testing.T) *server.Session {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, clientConn) }()
	session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "game-ready-test")
	t.Cleanup(func() {
		session.Close()
		_ = clientConn.Close()
	})
	session.MarkAuthenticated()
	if !session.EnterLobby() {
		t.Fatal("test session could not enter lobby")
	}
	session.UserContext.UniqueID = 77
	session.UserContext.CharacterID = 123
	session.UserContext.CharName = "staged-character"
	return session
}
