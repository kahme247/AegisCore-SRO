package server

import (
	"net"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSessionLifecycleRequiresOrderedCharacterTransitions(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "lifecycle-test")
	defer session.Close()

	if got, want := session.LifecyclePhase(), SessionConnected; got != want {
		t.Fatalf("initial lifecycle phase = %s, want %s", got, want)
	}
	if session.EnterLobby() {
		t.Fatal("unauthenticated session entered lobby")
	}
	if session.BeginCharacterLoading() {
		t.Fatal("unauthenticated session began character loading")
	}
	if session.CompleteCharacterLoading() {
		t.Fatal("session completed loading before a character was selected")
	}

	session.markHandshaking()
	if got, want := session.LifecyclePhase(), SessionHandshaking; got != want {
		t.Fatalf("handshake lifecycle phase = %s, want %s", got, want)
	}
	session.MarkAuthenticated()
	if got, want := session.LifecyclePhase(), SessionAuthenticated; got != want {
		t.Fatalf("authenticated lifecycle phase = %s, want %s", got, want)
	}
	if !session.EnterLobby() {
		t.Fatal("authenticated session could not enter lobby")
	}
	if !session.BeginCharacterLoading() {
		t.Fatal("lobby session could not begin character loading")
	}
	if session.BeginCharacterLoading() {
		t.Fatal("duplicate character loading transition succeeded")
	}
	if !session.CompleteCharacterLoading() {
		t.Fatal("valid character loading completion was rejected")
	}
	if session.CompleteCharacterLoading() {
		t.Fatal("duplicate character loading completion succeeded")
	}
	if !session.IsInWorld() {
		t.Fatal("completed lifecycle is not in world")
	}
	if session.AbortCharacterLoading() {
		t.Fatal("in-world session returned to lobby through abort")
	}

	session.Close()
	if got, want := session.LifecyclePhase(), SessionClosing; got != want {
		t.Fatalf("closed lifecycle phase = %s, want %s", got, want)
	}
}

func TestAgentLifecycleAdmissionPolicyRejectsGameplayDuringCharacterLoading(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "lifecycle-policy-test")
	defer session.Close()
	session.moduleIdentified.Store(true)

	policy := AgentLifecycleAdmissionPolicy(
		0x6103,
		[]uint16{0x7001, 0x7007, 0x7450},
		[]uint16{0x3012},
	)
	if err := policy(session, 0x6103); err != nil {
		t.Fatalf("authentication opcode rejected before admission: %v", err)
	}
	if err := policy(session, 0x7001); err == nil {
		t.Fatal("lobby opcode accepted before authentication")
	}

	session.MarkAuthenticated()
	if err := policy(session, 0x7001); err != nil {
		t.Fatalf("lobby opcode rejected after authentication: %v", err)
	}
	if err := policy(session, 0x7021); err == nil {
		t.Fatal("gameplay opcode accepted before lobby/character selection")
	}
	if !session.EnterLobby() || !session.BeginCharacterLoading() {
		t.Fatal("could not enter character loading state")
	}
	if err := policy(session, 0x7021); err == nil {
		t.Fatal("gameplay opcode accepted while character bootstrap is pending")
	}
	if err := policy(session, 0x7007); err == nil {
		t.Fatal("lobby mutation accepted while character bootstrap is pending")
	}
	if err := policy(session, 0x3012); err != nil {
		t.Fatalf("GameReady opcode rejected while character bootstrap is pending: %v", err)
	}
	if !session.CompleteCharacterLoading() {
		t.Fatal("could not complete character loading")
	}
	if err := policy(session, 0x7021); err != nil {
		t.Fatalf("gameplay opcode rejected after entering world: %v", err)
	}
}
