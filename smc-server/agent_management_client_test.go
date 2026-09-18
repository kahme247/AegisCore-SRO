package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/scheduler"
)

func TestAgentManagementClientDisconnectAcknowledgesMatchingCommand(t *testing.T) {
	var received PlayerDisconnectCommand
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != agentPlayerDisconnectsPath || request.Method != http.MethodPost {
			http.NotFound(response, request)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			http.Error(response, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(response).Encode(PlayerDisconnectResult{Disconnected: true, CharacterID: received.CharacterID, UniqueID: received.ExpectedUniqueID, Name: "Alice"})
	}))
	defer server.Close()
	client := &AgentManagementClient{endpoint: server.URL + agentOnlinePlayersPath, client: server.Client()}
	command := PlayerDisconnectCommand{CharacterID: 7, ExpectedUniqueID: 100, Reason: "Operator request"}
	result, err := client.DisconnectPlayer(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if received != command || !result.Disconnected || result.Name != "Alice" {
		t.Fatalf("received=%#v result=%#v", received, result)
	}
}

func TestAgentManagementClientCreatesAndListsSchedules(t *testing.T) {
	runAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	definition := scheduler.Definition{ID: "notice-1", Name: "Notice", Handler: "server.notice", Recurrence: scheduler.OneTime, Phase: scheduler.Warning, Scope: scheduler.AllShards, Timezone: "UTC", RunAt: runAt, MisfirePolicy: scheduler.RunOnce, Enabled: true, Payload: []byte(`{"message":"hello"}`)}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != agentSchedulesPath {
			http.NotFound(response, request)
			return
		}
		switch request.Method {
		case http.MethodPost:
			var received scheduler.Definition
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil || received.ID != definition.ID {
				http.Error(response, "bad request", http.StatusBadRequest)
				return
			}
			definition.Version = 1
			definition.NextRunAt = runAt
			response.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(response).Encode(definition)
		case http.MethodGet:
			_ = json.NewEncoder(response).Encode(map[string]any{"schedules": []scheduler.Definition{definition}})
		}
	}))
	defer server.Close()
	client := &AgentManagementClient{endpoint: server.URL + agentOnlinePlayersPath, client: server.Client()}
	created, err := client.CreateSchedule(context.Background(), definition)
	if err != nil || created.Version != 1 {
		t.Fatalf("CreateSchedule() = %+v, %v", created, err)
	}
	definitions, err := client.ListSchedules(context.Background())
	if err != nil || len(definitions) != 1 || definitions[0].ID != "notice-1" {
		t.Fatalf("ListSchedules() = %+v, %v", definitions, err)
	}
}

func TestAgentManagementClientClassifiesStaleIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	client := &AgentManagementClient{endpoint: server.URL + agentOnlinePlayersPath, client: server.Client()}
	_, err := client.DisconnectPlayer(context.Background(), PlayerDisconnectCommand{CharacterID: 7, ExpectedUniqueID: 100, Reason: "Operator request"})
	if !errors.Is(err, ErrAgentPlayerIdentityChanged) {
		t.Fatalf("error = %v, want ErrAgentPlayerIdentityChanged", err)
	}
}

func TestAgentManagementClientBroadcastAcknowledgesCommand(t *testing.T) {
	var received BroadcastCommand
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != agentBroadcastsPath || request.Method != http.MethodPost {
			http.NotFound(response, request)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			http.Error(response, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(response).Encode(BroadcastResult{Broadcast: true, RecipientCount: 2})
	}))
	defer server.Close()
	client := &AgentManagementClient{endpoint: server.URL + agentOnlinePlayersPath, client: server.Client()}
	result, err := client.Broadcast(context.Background(), BroadcastCommand{Message: "Hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if received.Message != "Hello world" || !result.Broadcast || result.RecipientCount != 2 {
		t.Fatalf("received=%#v result=%#v", received, result)
	}
}
