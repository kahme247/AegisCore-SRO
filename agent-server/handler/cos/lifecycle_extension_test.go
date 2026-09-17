package cos

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestCOSUnknownRequestReturnsNativeFailure(t *testing.T) {
	var sent network.Packet
	h := &LifecycleHandler{send: func(_ *server.Session, packet network.Packet) error {
		sent = packet
		return nil
	}}
	if err := h.handleUnknown(context.Background(), &server.Session{}, network.EmptyClientPacket()); err != nil {
		t.Fatalf("unknown COS request: %v", err)
	}
	if sent.MessageID != opcode.COSUnknownResponse || hex.EncodeToString(sent.Payload()) != "020100" {
		t.Fatalf("response = %04x/%x", sent.MessageID, sent.Payload())
	}
}
