package character

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type fakeGameReadyPlayers struct {
	player          *model.Player
	err             error
	lookups         int
	activations     int
	jobAtActivation model.PlayerJob
	npcs            []*model.NPC
}

func (f *fakeGameReadyPlayers) GetPlayerByUniqueId(_ uint32) (*model.Player, error) {
	f.lookups++
	if f.err != nil {
		return nil, f.err
	}
	return f.player, nil
}

func (f *fakeGameReadyPlayers) GetNPCs() []*model.NPC {
	return append([]*model.NPC(nil), f.npcs...)
}

func (f *fakeGameReadyPlayers) ActivatePlayer(player *model.Player) []service.VisibilityDelta {
	f.activations++
	f.jobAtActivation = player.GetJob()
	if player.CurrentHP() == 0 {
		player.SetLifeState(model.Dead)
	} else {
		player.SetLifeState(model.Alive)
	}
	return nil
}

type fakeGameReadySocial struct {
	prepared     int
	bootstrapped int
}

type fakeGameReadyPK struct {
	state model.CharacterPKState
	name  string
}

func (f *fakeGameReadyPK) LoadByName(_ context.Context, name string) (model.CharacterPKState, error) {
	f.name = name
	return f.state, nil
}

func (f *fakeGameReadyPK) EditByName(context.Context, string, model.CharacterPKState) error {
	return nil
}

func (f *fakeGameReadyPK) RecordKill(context.Context, int) (model.CharacterPKState, error) {
	return f.state, nil
}

func (f *fakeGameReadySocial) PreparePlayer(_ context.Context, player *model.Player) error {
	f.prepared++
	player.SetJob(model.PlayerJob{Type: model.PlayerJobTrader, Level: 4, Alias: "Aegis"})
	return nil
}

func (f *fakeGameReadySocial) Bootstrap(context.Context, *server.Session) error {
	f.bootstrapped++
	return nil
}

func TestGameReadyPreparesSpawnStateBeforeVisibilityActivation(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	player := &model.Player{
		ID: session.UserContext.CharacterID, Session: session,
		CharName: session.UserContext.CharName, LifeState: model.Spawning,
		BaseStats: model.BaseStats{HP: 100, MP: 100},
	}
	players := &fakeGameReadyPlayers{player: player}
	social := &fakeGameReadySocial{}
	handler := NewGameReadyHandler(players)
	handler.ConfigureSocial(social)
	if err := handler.handleSessionPacket(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatalf("GameReady rejected: %v", err)
	}
	if social.prepared != 1 || social.bootstrapped != 1 {
		t.Fatalf("social calls = prepare %d, bootstrap %d", social.prepared, social.bootstrapped)
	}
	if got := players.jobAtActivation; got.Type != model.PlayerJobTrader || got.Level != 4 || got.Alias != "Aegis" {
		t.Fatalf("job at visibility activation = %+v", got)
	}
}

func TestGameReadyLoadsAndPublishesPersistedPKState(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	player := &model.Player{
		ID: session.UserContext.CharacterID, Session: session,
		CharName: session.UserContext.CharName, LifeState: model.Spawning,
		BaseStats: model.BaseStats{HP: 100, MP: 100},
	}
	repository := &fakeGameReadyPK{state: model.CharacterPKState{DailyPK: 7, TotalPK: -12, PKPenaltyPoint: 345}}
	handler := NewGameReadyHandler(&fakeGameReadyPlayers{player: player})
	handler.ConfigurePKRepository(repository)
	if err := handler.handleSessionPacket(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatalf("GameReady rejected: %v", err)
	}
	if repository.name != player.CharName {
		t.Fatalf("loaded PK name = %q", repository.name)
	}
	if got := player.PKCounters(); got != repository.state {
		t.Fatalf("PK state = %+v, want %+v", got, repository.state)
	}
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

func TestGameReadyRejectsActivationThatDoesNotCompleteLifeState(t *testing.T) {
	session := newGameReadyTestSession(t)
	if !session.BeginCharacterLoading() {
		t.Fatal("test session could not enter character loading")
	}
	player := &model.Player{
		ID:        session.UserContext.CharacterID,
		Session:   session,
		CharName:  session.UserContext.CharName,
		LifeState: model.Spawning,
		BaseStats: model.BaseStats{HP: 100, MP: 75},
	}
	handler := NewGameReadyHandler(&incompleteGameReadyActivation{player: player})

	err := handler.handleSessionPacket(nil, session, network.EmptyClientPacket())
	if err == nil {
		t.Fatal("GameReady accepted an activation that left the client-facing life state spawning")
	}
}

type incompleteGameReadyActivation struct {
	player *model.Player
}

func (f *incompleteGameReadyActivation) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}

func (*incompleteGameReadyActivation) ActivatePlayer(*model.Player) []service.VisibilityDelta {
	return nil
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

type fakeQuestIndicatorRepository struct {
	indicators  []model.QuestNPCIndicator
	characterID int
}

func (f *fakeQuestIndicatorRepository) ListAvailableIndicators(_ context.Context, characterID int) ([]model.QuestNPCIndicator, error) {
	f.characterID = characterID
	return append([]model.QuestNPCIndicator(nil), f.indicators...), nil
}

func TestGameReadyPublishesAvailableNPCQuestIndicators(t *testing.T) {
	repository := &fakeQuestIndicatorRepository{indicators: []model.QuestNPCIndicator{
		{QuestID: 58, NPCRefObjectID: 2003},
	}}
	player := &model.Player{ID: 123}
	npc := &model.NPC{SRObject: model.SRObject{UniqueID: 0x11223344, RefObjectID: 2003}}
	world := &fakeGameReadyPlayers{npcs: []*model.NPC{npc}}
	var sent []network.Packet
	err := publishAvailableQuestIndicators(context.Background(), repository, world, player, func(packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.characterID != player.ID || len(sent) != 1 || sent[0].MessageID != opcode.QuestMarkAdd {
		t.Fatalf("indicator publication character=%d packets=%#v", repository.characterID, sent)
	}
	payload := sent[0].Payload()
	if len(payload) != 24 || payload[4] != 0x02 || payload[5] != 1 ||
		binary.LittleEndian.Uint32(payload[20:24]) != npc.GetUniqueID() {
		t.Fatalf("quest indicator payload = %x", payload)
	}
	marks := player.QuestMarksSnapshot()
	if len(marks) != 1 || marks[0].QuestID != 58 || marks[0].TargetID != npc.GetUniqueID() {
		t.Fatalf("registered quest indicators = %+v", marks)
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
