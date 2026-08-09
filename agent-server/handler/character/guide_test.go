package character

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestGuideRequestConsumesFlagWithoutGuessedResponse(t *testing.T) {
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt64(uint64(Quest)); err != nil {
		t.Fatalf("write guide flag: %v", err)
	}

	// A session without a Sender proves the handler remains wire-neutral. The
	// old guessed B0EA path would return server.ErrSenderClosed here.
	if err := handleGuideRequest(context.Background(), &server.Session{}, packet); err != nil {
		t.Fatalf("handleGuideRequest() error = %v", err)
	}
}

func TestGuideRequestRejectsMalformedPayload(t *testing.T) {
	if err := handleGuideRequest(context.Background(), &server.Session{}, network.EmptyClientPacket()); err == nil {
		t.Fatal("handleGuideRequest accepted a malformed payload")
	}
}
