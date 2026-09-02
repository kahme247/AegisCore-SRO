package character

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestGuideResponseMatchesRecoveredGameServerLayout(t *testing.T) {
	packet := buildGuideResponse(uint64(Quest))
	if packet.MessageID != opcode.GuideResponse || !bytes.Equal(packet.Payload(), []byte{1, 0, 0, 0x10, 0, 0, 0, 0, 0}) {
		t.Fatalf("guide response = opcode 0x%04X payload % X", packet.MessageID, packet.Payload())
	}
}

func TestGuideRequestRejectsMalformedPayload(t *testing.T) {
	if err := handleGuideRequest(context.Background(), &server.Session{}, network.EmptyClientPacket()); err == nil {
		t.Fatal("handleGuideRequest accepted a malformed payload")
	}
}
