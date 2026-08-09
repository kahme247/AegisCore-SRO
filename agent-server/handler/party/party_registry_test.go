package party

import (
	"errors"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestSendPacketRejectsSerializationErrorBeforeSend(t *testing.T) {
	packet := network.EmptyPacket()
	if err := packet.WriteString(strings.Repeat("x", 1<<16)); err == nil {
		t.Fatal("oversized string unexpectedly serialized")
	}

	called := false
	handler := &PartyHandler{send: func(_ *server.Session, _ network.Packet) error {
		called = true
		return nil
	}}
	if err := handler.sendPacket(nil, packet); !errors.Is(err, network.ErrPacketStringTooLong) {
		t.Fatalf("sendPacket() error = %v, want ErrPacketStringTooLong", err)
	}
	if called {
		t.Fatal("send callback ran for an invalid packet")
	}
}
