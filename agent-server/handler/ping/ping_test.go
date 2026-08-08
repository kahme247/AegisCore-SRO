package ping

import (
	"context"
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
)

func TestPingResponseEchoesToken(t *testing.T) {
	for _, token := range []uint32{0, 0x12345678, 0xFFFFFFFF} {
		response, err := buildResponse(token)
		if err != nil {
			t.Fatalf("buildResponse(%08X): %v", token, err)
		}
		if response.MessageID != opcode.AgentPingResponse {
			t.Fatalf("response opcode = %04X, want %04X", response.MessageID, opcode.AgentPingResponse)
		}
		got, err := response.ReadUInt32()
		if err != nil {
			t.Fatalf("read response token: %v", err)
		}
		if got != token {
			t.Fatalf("response token = %08X, want %08X", got, token)
		}
	}
}

func TestPingRequestRequiresExactlyFourBytes(t *testing.T) {
	for _, size := range []int{0, 1, 2, 3, 5} {
		packet := network.EmptyClientPacket()
		for i := 0; i < size; i++ {
			if err := packet.WriteByte(0); err != nil {
				t.Fatalf("write malformed payload: %v", err)
			}
		}
		if _, err := readToken(packet); err == nil {
			t.Fatalf("readToken accepted %d bytes", size)
		}
	}
}

func TestPingRequestRejectsMissingSession(t *testing.T) {
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt32(1); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := handleRequest(context.Background(), nil, packet); err == nil {
		t.Fatal("handleRequest accepted a missing session")
	}
}
