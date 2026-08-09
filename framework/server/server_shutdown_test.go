package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestServerShutdownStopsAcceptsAndDrainsRegisteredSessions(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	listener, err := engine.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, engine.PacketChannel, engine.SessionClosed, engine.ModuleID)
	session.onClose = engine.closeSession
	session.onDrained = engine.sessionDrained
	engine.AddSession(session)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case <-session.Drained():
	case <-time.After(time.Second):
		t.Fatal("Shutdown() returned before the registered session drained")
	}
	if got := engine.SessionCount(); got != 0 {
		t.Fatalf("session count after Shutdown() = %d, want 0", got)
	}

	if replacement, err := net.Listen("tcp4", listener.Addr().String()); err != nil {
		t.Fatalf("listener remained bound after Shutdown(): %v", err)
	} else if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close() error = %v", err)
	}
	if _, err := engine.Listen(); err == nil {
		t.Fatal("server accepted a new listener after permanent shutdown")
	}
}
