package server

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestAgentAdmissionPolicyRejectsApplicationTrafficBeforeAuthentication(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()
	session.moduleIdentified.Store(true)
	session.admissionPolicy = AgentAdmissionPolicy(0x6103)
	registry := NewHandlerRegistry()
	var handled atomic.Int32
	registry.Register(0x7025, func(context.Context, *Session, network.Packet) error {
		handled.Add(1)
		return nil
	})
	session.handlers = registry

	packet := network.EmptyClientPacket()
	packet.MessageID = 0x7025
	session.handleIncomingPacket(packet)
	if got := handled.Load(); got != 0 {
		t.Fatalf("unauthenticated application handler calls = %d, want 0", got)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("unauthenticated application packet did not close the session")
	}
}

func TestAgentAdmissionPolicyAllowsAuthenticationThenAuthenticatedTraffic(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()
	session.moduleIdentified.Store(true)
	policy := AgentAdmissionPolicy(0x6103)
	if err := policy(session, 0x6103); err != nil {
		t.Fatalf("authentication opcode rejected: %v", err)
	}
	if err := policy(session, 0x7025); err == nil {
		t.Fatal("application opcode accepted before authentication")
	}
	session.MarkAuthenticated()
	if err := policy(session, 0x7025); err != nil {
		t.Fatalf("application opcode rejected after authentication: %v", err)
	}
}

func TestAllowListedAdmissionPolicyRequiresModuleIdentification(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()
	policy := AllowListedAdmissionPolicy(0x6100)
	if err := policy(session, 0x6100); err == nil {
		t.Fatal("opcode accepted before module identification")
	}
	session.moduleIdentified.Store(true)
	if err := policy(session, 0x6100); err != nil {
		t.Fatalf("allow-listed opcode rejected: %v", err)
	}
	if err := policy(session, 0x7025); err == nil {
		t.Fatal("non-allow-listed opcode accepted")
	}
}

func TestAvailabilityAdmissionPolicyFailsClosedUntilReady(t *testing.T) {
	ready := false
	called := false
	policy := AvailabilityAdmissionPolicy(func() bool { return ready }, func(*Session, uint16) error {
		called = true
		return nil
	})
	if err := policy(nil, 0x6103); err == nil || called {
		t.Fatal("unavailable policy admitted or delegated application traffic")
	}
	ready = true
	if err := policy(nil, 0x6103); err != nil || !called {
		t.Fatalf("ready policy error=%v delegated=%t", err, called)
	}
}
