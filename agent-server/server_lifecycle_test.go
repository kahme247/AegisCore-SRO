package main

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/security/auth"
	"github.com/ferdoran/go-sro/framework/server"
)

func TestPlayerSessionClosedReleasesAdmissionBeforeCharacterJoin(t *testing.T) {
	store := auth.NewAdmissionTicketStore()
	ticket := auth.AdmissionTicket{
		QueueID:      1,
		AccountID:    77,
		Username:     "alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     netip.MustParseAddr("127.0.0.1"),
	}
	if err := store.Install(ticket); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := store.Redeem(auth.AdmissionCredentials{
		QueueID:      ticket.QueueID,
		Username:     ticket.Username,
		ContentID:    ticket.ContentID,
		HasContentID: true,
		SourceIP:     ticket.SourceIP,
	}); err != nil {
		t.Fatalf("Redeem() error = %v", err)
	}
	if !store.IsAccountActive(ticket.AccountID) {
		t.Fatal("redeemed account is not active before disconnect")
	}

	agent := AgentServer{AdmissionTickets: store}
	agent.playerSessionClosed(&server.Session{UserContext: server.UserContext{UserID: ticket.AccountID}})
	if store.IsAccountActive(ticket.AccountID) {
		t.Fatal("close before character join left the admission account active")
	}
}

func TestAgentCloseStopsPublicAcceptsBeforeDrainingSessions(t *testing.T) {
	engine := server.NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	listener, err := engine.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	agent := AgentServer{Server: &engine}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := agent.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if replacement, err := net.Listen("tcp4", listener.Addr().String()); err != nil {
		t.Fatalf("public listener remained bound after Agent.Close(): %v", err)
	} else if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close() error = %v", err)
	}
	if _, err := engine.Listen(); err == nil {
		t.Fatal("Agent.Close() permitted the public listener to be reopened")
	}
}

func TestAgentPacketLoopStopsWhenShutdownContextIsCancelled(t *testing.T) {
	engine := server.NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	packetContext, cancelPackets := context.WithCancel(context.Background())
	agent := AgentServer{Server: &engine}
	done := make(chan struct{})
	go func() {
		agent.handlePackets(packetContext)
		close(done)
	}()

	cancelPackets()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("packet loop did not stop after shutdown cancellation")
	}
}
