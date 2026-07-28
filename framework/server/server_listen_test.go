package server

import (
	"net"
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
)

func TestServerListenReservesPublicPortUntilRollback(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	listener, err := engine.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	address := listener.Addr().String()
	if _, err := net.Listen("tcp4", address); err == nil {
		t.Fatal("a second listener bound an Agent-reserved public port")
	}
	if err := engine.CloseListener(); err != nil {
		t.Fatalf("CloseListener() error = %v", err)
	}
	replacement, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("public port remained reserved after rollback: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close() error = %v", err)
	}
}
