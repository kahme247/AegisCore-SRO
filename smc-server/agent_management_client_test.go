package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
