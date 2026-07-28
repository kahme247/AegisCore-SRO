package server

import (
	"net"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
)

func TestUnregisteredApplicationPacketIsForwardedToServerPacketChannel(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	packets := make(chan PacketChannelData, 1)
	session := NewSession(serverConn, network.EncodingOptions{}, packets, nil, "test")
	defer session.Close()
	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	session.moduleIdentified.Store(true)

	packet := network.EmptyClientPacket()
	packet.MessageID = 0xA199
	if err := packet.WriteByte(0x42); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	session.handleIncomingPacket(packet)

	select {
	case received := <-packets:
		if received.Session != session {
			t.Fatalf("forwarded session = %p, want %p", received.Session, session)
		}
		if received.MessageID != packet.MessageID {
			t.Fatalf("forwarded opcode = 0x%04X, want 0x%04X", received.MessageID, packet.MessageID)
		}
	case <-time.After(time.Second):
		t.Fatal("unregistered application packet was not forwarded to the server packet channel")
	}
}
