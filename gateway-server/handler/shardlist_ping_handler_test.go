package handler

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/gateway-server/config"
)

func TestBuildShardlistPongWritesDocumentedIPv4Bytes(t *testing.T) {
	response, err := buildShardlistPong(
		config.Farm{ID: 7, Name: "Farm"},
		config.AgentEndpoint{IP: netip.MustParseAddr("203.0.113.42"), Port: 15882},
	)
	if err != nil {
		t.Fatalf("buildShardlistPong() error = %v", err)
	}
	if response.MessageID != opcode.ShardlistPong {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", response.MessageID, opcode.ShardlistPong)
	}
	if got, want := response.Payload(), []byte{1, 7, 203, 0, 113, 42}; !bytes.Equal(got, want) {
		t.Fatalf("response payload = % X, want % X", got, want)
	}
}

func TestBuildShardlistPongRejectsNonIPv4Endpoint(t *testing.T) {
	if _, err := buildShardlistPong(
		config.Farm{ID: 1, Name: "Farm"},
		config.AgentEndpoint{IP: netip.MustParseAddr("2001:db8::1"), Port: 15882},
	); err == nil {
		t.Fatal("buildShardlistPong() error = nil, want non-IPv4 rejection")
	}
}

func TestShardlistPingRejectsUnexpectedRequestPayload(t *testing.T) {
	packet := network.EmptyClientPacket()
	if err := packet.WriteByte(1); err != nil {
		t.Fatal(err)
	}
	if err := validateEmptyGatewayRequest(packet, "shard-list ping"); err == nil {
		t.Fatal("validateEmptyGatewayRequest() error = nil, want unexpected payload rejection")
	}
}
