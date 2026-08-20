package management

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	frameworkserver "github.com/Harvi8/AegisCore/framework/server"
)

type fixedPlayers struct {
	players          []*model.Player
	broadcastMessage string
}

func activeManagementPlayer(t *testing.T, characterID int, uniqueID uint32) (*model.Player, *frameworkserver.Session) {
	t.Helper()
	client, serverConnection := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	session := frameworkserver.NewSession(serverConnection, network.EncodingOptions{}, nil, nil, "management-test")
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("could not prepare in-world session")
	}
	player := &model.Player{ID: characterID, CharName: "Alice", Session: session}
	player.SetUniqueID(uniqueID)
	return player, session
}

func TestDisconnectCommandClosesOnlyMatchingRuntimeIdentity(t *testing.T) {
	player, session := activeManagementPlayer(t, 7, 100)
	handler := NewHandler(fixedPlayers{players: []*model.Player{player}})

	staleRequest := httptest.NewRequest(http.MethodPost, PlayerDisconnectsPath, bytes.NewBufferString(`{"characterId":7,"expectedUniqueId":99,"reason":"operator test"}`))
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, staleRequest)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, want %d", staleResponse.Code, http.StatusConflict)
	}
	select {
	case <-session.Done():
		t.Fatal("stale runtime identity closed the session")
	default:
	}

	disconnectRequest := httptest.NewRequest(http.MethodPost, PlayerDisconnectsPath, bytes.NewBufferString(`{"characterId":7,"expectedUniqueId":100,"reason":"operator test"}`))
	disconnectResponse := httptest.NewRecorder()
	handler.ServeHTTP(disconnectResponse, disconnectRequest)
	if disconnectResponse.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d, want %d; body=%s", disconnectResponse.Code, http.StatusOK, disconnectResponse.Body.String())
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("matching disconnect did not close the session")
	}
}

func (f fixedPlayers) GetActivePlayers() []*model.Player { return f.players }

func (f fixedPlayers) BroadcastNotice(string) (int, error) { return len(f.players), nil }

func (f fixedPlayers) GetPlayerByCharacterID(characterID int) (*model.Player, error) {
	for _, player := range f.players {
		if player != nil && player.ID == characterID {
			return player, nil
		}
	}
	return nil, errors.New("not found")
}

func TestOnlinePlayersHandlerReturnsDetachedRuntimeSnapshots(t *testing.T) {
	player := &model.Player{ID: 7, CharName: "Alice", Level: 42, Gold: 99, IsGm: true}
	player.SetUniqueID(100)
	request := httptest.NewRequest(http.MethodGet, OnlinePlayersPath, nil)
	response := httptest.NewRecorder()
	NewHandler(fixedPlayers{players: []*model.Player{player}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload struct {
		Players []model.PlayerManagementSnapshot `json:"players"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Players) != 1 || payload.Players[0].CharacterID != 7 || payload.Players[0].UniqueID != 100 || payload.Players[0].Name != "Alice" {
		t.Fatalf("players = %#v", payload.Players)
	}
}

func TestServerRequiresTLSAndPlayerSource(t *testing.T) {
	if _, err := NewServer("127.0.0.1:0", nil, fixedPlayers{}); err == nil {
		t.Fatal("NewServer accepted missing TLS")
	}
	if _, err := NewServer("127.0.0.1:0", &tls.Config{}, fixedPlayers{}); err == nil {
		t.Fatal("NewServer accepted TLS without verified client certificates")
	}
}

type recordingPlayers struct {
	fixedPlayers
	message string
}

func (f *recordingPlayers) BroadcastNotice(message string) (int, error) {
	f.message = message
	return 3, nil
}

func TestBroadcastCommandValidatesAndSendsNotice(t *testing.T) {
	players := &recordingPlayers{}
	handler := NewHandler(players)
	request := httptest.NewRequest(http.MethodPost, BroadcastsPath, bytes.NewBufferString(`{"message":"Server maintenance soon"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || players.message != "Server maintenance soon" {
		t.Fatalf("response=%d %s message=%q", response.Code, response.Body.String(), players.message)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"recipientCount":3`)) {
		t.Fatalf("response body = %s", response.Body.String())
	}

	invalid := httptest.NewRequest(http.MethodPost, BroadcastsPath, bytes.NewBufferString(`{"message":"   "}`))
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}
}
