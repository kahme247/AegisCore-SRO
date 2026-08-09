package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestLoginAttemptConfigParsingFailsClosedForInvalidNumbers(t *testing.T) {
	if got := positiveUint32(-1); got != 0 {
		t.Fatalf("positiveUint32(-1) = %d, want 0 so tracker applies its safe default", got)
	}
	if got := positiveUint32(0); got != 0 {
		t.Fatalf("positiveUint32(0) = %d, want 0", got)
	}
	if got := positiveUint32(7); got != 7 {
		t.Fatalf("positiveUint32(7) = %d, want 7", got)
	}
	if got := positiveSecondsDuration(-1); got != 0 {
		t.Fatalf("positiveSecondsDuration(-1) = %s, want 0", got)
	}
	if got := positiveSecondsDuration(15); got != 15*time.Second {
		t.Fatalf("positiveSecondsDuration(15) = %s, want 15s", got)
	}
}

func TestGatewayStartReturnsAndShutdownReleasesPublicListener(t *testing.T) {
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve listener: %v", err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatalf("release reservation: %v", err)
	}

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", address, err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("LookupPort(%q): %v", portText, err)
	}
	packetContext, cancelPackets := context.WithCancel(context.Background())
	defer cancelPackets()
	engine := server.NewEngine(host, port, network.EncodingOptions{})
	gateway := GatewayServer{
		Server:               &engine,
		packetContext:        packetContext,
		cancelPacketHandling: cancelPackets,
	}

	started := time.Now()
	gateway.Start()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Start() blocked for %s", elapsed)
	}

	deadline := time.Now().Add(time.Second)
	var client net.Conn
	for time.Now().Before(deadline) {
		client, err = net.DialTimeout("tcp4", address, 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Gateway did not begin accepting: %v", err)
	}
	if client != nil {
		defer client.Close()
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := gateway.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if replacement, err := net.Listen("tcp4", address); err != nil {
		t.Fatalf("Gateway listener remained bound after Shutdown(): %v", err)
	} else if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close() error = %v", err)
	}
}
