package server

import (
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSessionRejectsKeepAliveInterleavedWithMassiveAssembly(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, make(chan PacketChannelData, 1), nil, "test")
	defer session.Close()
	session.handlers = NewHandlerRegistry()
	registerCoreHandlers(session.handlers)
	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	session.moduleIdentified.Store(true)

	header := network.EmptyClientPacket()
	header.MessageID = network.MassivePacketID
	if err := header.WriteBool(true); err != nil {
		t.Fatalf("WriteBool() error = %v", err)
	}
	if err := header.WriteUInt16(1); err != nil {
		t.Fatalf("WriteUInt16(body count) error = %v", err)
	}
	if err := header.WriteUInt16(0xA100); err != nil {
		t.Fatalf("WriteUInt16(target opcode) error = %v", err)
	}
	session.handleIncomingPacket(header)
	select {
	case <-session.Done():
		t.Fatal("valid massive header closed the session")
	default:
	}

	keepAlive := network.EmptyClientPacket()
	keepAlive.MessageID = 0x2002
	session.handleIncomingPacket(keepAlive)
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("keep-alive interleaved with massive assembly did not close the session")
	}
}
