package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCoreHandlersRejectReorderedHandshakePackets(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{KeyExchange: true}, nil, nil, "test")
	defer session.Close()

	packet := network.EmptyClientPacket()
	packet.MessageID = 0x9000
	if err := handleKeyExchangeCompletedPacket(context.Background(), session, packet); err == nil {
		t.Fatal("premature key exchange completion was accepted")
	}
	if got := session.HandshakePhase(); got != HandshakeNew {
		t.Fatalf("HandshakePhase = %v, want %v", got, HandshakeNew)
	}
}

func TestKeyExchangeCompletionAdvancesOnlyOnce(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{KeyExchange: true}, nil, nil, "test")
	defer session.Close()

	session.securityMu.Lock()
	session.Context.StartedHandshake = true
	session.Context.CompletedLocalSetup = true
	session.Context.CompletedRemoteSetup = true
	session.setHandshakePhaseLocked(HandshakeAwaitingCompletion)
	session.securityMu.Unlock()

	packet := network.EmptyClientPacket()
	packet.MessageID = 0x9000
	if err := handleKeyExchangeCompletedPacket(context.Background(), session, packet); err != nil {
		t.Fatalf("first key exchange completion error = %v", err)
	}
	if !session.Context.FinishedHandshake || session.HandshakePhase() != HandshakeEstablished {
		t.Fatalf("handshake state = %v finished=%t, want established", session.HandshakePhase(), session.Context.FinishedHandshake)
	}
	if err := handleKeyExchangeCompletedPacket(context.Background(), session, packet); err == nil {
		t.Fatal("duplicate key exchange completion was accepted")
	}
}

func TestKeyExchangeRejectsTrivialPublicValues(t *testing.T) {
	tests := []struct {
		name      string
		generator uint32
		local     uint32
		remote    uint32
	}{
		{name: "trivial generator", generator: 1, local: 2, remote: 2},
		{name: "trivial local public", generator: 2, local: 1, remote: 2},
		{name: "trivial remote public", generator: 2, local: 2, remote: 1},
		{name: "upper bound remote public", generator: 2, local: 2, remote: 16},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()
			session := NewSession(serverConn, network.EncodingOptions{KeyExchange: true}, nil, nil, "test")
			defer session.Close()

			session.securityMu.Lock()
			session.Context.Generator = test.generator
			session.Context.Prime = 17
			session.Context.Private = 5
			session.Context.LocalPublic = test.local
			session.setHandshakePhaseLocked(HandshakeChallengeSent)
			session.securityMu.Unlock()

			packet := network.EmptyClientPacket()
			packet.MessageID = 0x5000
			if err := packet.WriteUInt32(test.remote); err != nil {
				t.Fatalf("WriteUInt32() error = %v", err)
			}
			if err := packet.WriteUInt64(0); err != nil {
				t.Fatalf("WriteUInt64() error = %v", err)
			}
			if err := handleKeyExchangePacket(context.Background(), session, packet); err == nil {
				t.Fatal("weak key exchange public value was accepted")
			}
		})
	}
}

func TestModuleIdentificationRequiresEstablishedHandshakeAndExactPayload(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()

	premature := network.EmptyClientPacket()
	premature.MessageID = 0x2001
	if err := premature.WriteString("client"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := premature.WriteByte(0); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	if err := handleModuleIdentificationPacket(context.Background(), session, premature); err == nil {
		t.Fatal("premature module identification was accepted")
	}

	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	trailing := network.EmptyClientPacket()
	trailing.MessageID = 0x2001
	if err := trailing.WriteString("client"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := trailing.WriteByte(0); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	if err := trailing.WriteByte(0xFF); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	if err := handleModuleIdentificationPacket(context.Background(), session, trailing); err == nil {
		t.Fatal("module identification with trailing data was accepted")
	}
}

func TestSessionRejectsApplicationTrafficUntilModuleIdentification(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()

	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	application := network.EmptyClientPacket()
	application.MessageID = 0x6102
	session.handleIncomingPacket(application)
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("application packet before module identification did not close session")
	}
}

func TestKeepAliveRequiresModuleIdentification(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()

	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	keepAlive := network.EmptyClientPacket()
	keepAlive.MessageID = 0x2002
	if err := handleKeepAlivePacket(context.Background(), session, keepAlive); err == nil {
		t.Fatal("keep-alive before module identification was accepted")
	}
}
