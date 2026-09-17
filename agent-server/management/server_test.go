package management

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/scheduler"
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

type schedulerSourceStub struct {
	definitions []scheduler.Definition
	created     scheduler.Definition
	handlers    map[string]bool
}

func (source *schedulerSourceStub) Create(_ context.Context, definition scheduler.Definition) (scheduler.Definition, error) {
	source.created = definition
	definition.Version = 1
	definition.NextRunAt = definition.RunAt
	source.definitions = append(source.definitions, definition)
	return definition, nil
}

func (source *schedulerSourceStub) List(context.Context, int) ([]scheduler.Definition, error) {
	return append([]scheduler.Definition(nil), source.definitions...), nil
}

func (source *schedulerSourceStub) Get(_ context.Context, id string) (scheduler.Definition, error) {
	for _, definition := range source.definitions {
		if definition.ID == id {
			return definition, nil
		}
	}
	return scheduler.Definition{}, scheduler.ErrDefinitionNotFound
}

func (source *schedulerSourceStub) HasHandler(name string) bool { return source.handlers[name] }

func (source *schedulerSourceStub) SetEnabled(_ context.Context, id string, expectedVersion uint64, enabled bool) (scheduler.Definition, error) {
	for index := range source.definitions {
		if source.definitions[index].ID == id && source.definitions[index].Version == expectedVersion {
			source.definitions[index].Enabled = enabled
			source.definitions[index].Version++
			return source.definitions[index], nil
		}
	}
	return scheduler.Definition{}, scheduler.ErrDefinitionChanged
}

func (*schedulerSourceStub) ListRuns(context.Context, string, int) ([]scheduler.RunRecord, error) {
	return []scheduler.RunRecord{}, nil
}

func TestScheduleManagementCreatesListsAndGetsDefinition(t *testing.T) {
	source := &schedulerSourceStub{handlers: map[string]bool{"server.notice": true}}
	handler := NewHandlerWithScheduler(fixedPlayers{}, source)
	runAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	body := fmt.Sprintf(`{"id":"notice-1","name":"Maintenance warning","handler":"server.notice","recurrence":"one_time","phase":"warning","scope":"all_shards","timezone":"UTC","runAt":%q,"misfirePolicy":"run_once","enabled":true,"payload":{"message":"Maintenance soon"}}`, runAt.Format(time.RFC3339))
	create := httptest.NewRequest(http.MethodPost, SchedulesPath, bytes.NewBufferString(body))
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated || source.created.ID != "notice-1" {
		t.Fatalf("create response=%d %s definition=%+v", createResponse.Code, createResponse.Body.String(), source.created)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, SchedulesPath, nil))
	if listResponse.Code != http.StatusOK || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"id":"notice-1"`)) {
		t.Fatalf("list response=%d %s", listResponse.Code, listResponse.Body.String())
	}

	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, SchedulesPath+"/notice-1", nil))
	if getResponse.Code != http.StatusOK || !bytes.Contains(getResponse.Body.Bytes(), []byte(`"handler":"server.notice"`)) {
		t.Fatalf("get response=%d %s", getResponse.Code, getResponse.Body.String())
	}
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, httptest.NewRequest(http.MethodPatch, SchedulesPath+"/notice-1", bytes.NewBufferString(`{"expectedVersion":1,"enabled":false}`)))
	if updateResponse.Code != http.StatusOK || !bytes.Contains(updateResponse.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatalf("update response=%d %s", updateResponse.Code, updateResponse.Body.String())
	}
	runsResponse := httptest.NewRecorder()
	handler.ServeHTTP(runsResponse, httptest.NewRequest(http.MethodGet, SchedulesPath+"/notice-1/runs", nil))
	if runsResponse.Code != http.StatusOK || !bytes.Contains(runsResponse.Body.Bytes(), []byte(`"runs":[]`)) {
		t.Fatalf("runs response=%d %s", runsResponse.Code, runsResponse.Body.String())
	}
}

func TestScheduleManagementRejectsUnknownHandler(t *testing.T) {
	source := &schedulerSourceStub{handlers: map[string]bool{}}
	handler := NewHandlerWithScheduler(fixedPlayers{}, source)
	body := `{"id":"bad","name":"Bad","handler":"missing","recurrence":"one_time","phase":"start","scope":"all_shards","timezone":"UTC","runAt":"2027-01-01T00:00:00Z","misfirePolicy":"run_once","enabled":true,"payload":{}}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, SchedulesPath, bytes.NewBufferString(body)))
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("unknown_schedule_handler")) {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}
