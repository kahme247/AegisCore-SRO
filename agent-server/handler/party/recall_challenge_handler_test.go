package party

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestRecallChallengeIsBoundOneTimeAndRejectsReplay(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	handler := &PartyHandler{pendingChallenges: make(map[uint32]recallChallengeState), now: func() time.Time { return now }}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77}}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	accepted := 0
	if err := handler.IssueRecallChallenge(session, 900, func(context.Context) error { accepted++; return nil }); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.RecallResponse || sent[1].MessageID != opcode.RecallChallenge {
		t.Fatalf("issued packets = %#v", sent)
	}
	challenge := sent[1]
	id, _ := challenge.ReadUInt32()
	token, _ := challenge.ReadUInt32()
	answer := func() network.Packet {
		packet := network.EmptyClientPacket()
		packet.WriteUInt32(id)
		packet.WriteUInt32(token)
		packet.WriteByte(1)
		return packet
	}
	first := answer()
	if err := handler.handleRecallSolve(context.Background(), session, first); err != nil {
		t.Fatalf("solve: %v", err)
	}
	if accepted != 1 || sent[len(sent)-1].Payload()[0] != 1 {
		t.Fatalf("accepted=%d response=%x", accepted, sent[len(sent)-1].Payload())
	}
	replay := answer()
	if err := handler.handleRecallSolve(context.Background(), session, replay); err != nil {
		t.Fatalf("replay response: %v", err)
	}
	if accepted != 1 || sent[len(sent)-1].Payload()[0] != 2 {
		t.Fatalf("replay accepted=%d response=%x", accepted, sent[len(sent)-1].Payload())
	}
}

func TestRecallChallengeExpiresBeforeCallback(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	handler := &PartyHandler{pendingChallenges: make(map[uint32]recallChallengeState), now: func() time.Time { return now }}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 88}}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	accepted := false
	if err := handler.IssueRecallChallenge(session, 901, func(context.Context) error { accepted = true; return nil }); err != nil {
		t.Fatal(err)
	}
	challenge := sent[1]
	id, _ := challenge.ReadUInt32()
	token, _ := challenge.ReadUInt32()
	now = now.Add(recallChallengeTTL)
	answer := network.EmptyClientPacket()
	answer.WriteUInt32(id)
	answer.WriteUInt32(token)
	answer.WriteByte(1)
	if err := handler.handleRecallSolve(context.Background(), session, answer); err != nil {
		t.Fatal(err)
	}
	if accepted || sent[len(sent)-1].Payload()[0] != 2 {
		t.Fatalf("expired accepted=%t response=%x", accepted, sent[len(sent)-1].Payload())
	}
}
