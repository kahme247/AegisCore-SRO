package client

import (
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
)

func TestModuleIdentificationHandlerFailsClosedOnMalformedPayload(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
	}{
		{
			name:   "missing string length",
			packet: network.EmptyPacket(),
		},
		{
			name:   "declared string exceeds payload",
			packet: packetWithBytes(t, 0x02, 0x00, 'x'),
		},
		{
			name: "trailing payload",
			packet: func() network.Packet {
				packet := network.EmptyPacket()
				if err := packet.WriteString("module"); err != nil {
					t.Fatalf("WriteString() error = %v", err)
				}
				if err := packet.WriteByte(0); err != nil {
					t.Fatalf("WriteByte() error = %v", err)
				}
				if err := packet.WriteByte(0xFF); err != nil {
					t.Fatalf("WriteByte() error = %v", err)
				}
				return packet
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, generation, cleanup := attachedKeyExchangeClient(t)
			defer cleanup()

			client.PacketHandlers[0x2001].Handle(test.packet)
			assertGenerationCancelled(t, generation)
		})
	}
}

func TestClientRejectsPacketsOutsideItsHandshakePhase(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Client, *clientGeneration)
		id    uint16
	}{
		{
			name:  "application packet before handshake",
			setup: func(*Client, *clientGeneration) {},
			id:    0xA100,
		},
		{
			name:  "keepalive before handshake",
			setup: func(*Client, *clientGeneration) {},
			id:    0x2002,
		},
		{
			name:  "module identification before handshake",
			setup: func(*Client, *clientGeneration) {},
			id:    0x2001,
		},
		{
			name: "duplicate module identification",
			setup: func(client *Client, generation *clientGeneration) {
				client.Ctx.HandshakeCompleted = true
				generation.readyOnce.Do(func() { close(generation.ready) })
			},
			id: 0x2001,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, generation, cleanup := attachedKeyExchangeClient(t)
			defer cleanup()
			test.setup(client, generation)

			packet := network.EmptyPacket()
			packet.MessageID = test.id
			if client.handleIncomingPacketForGeneration(generation, packet) {
				t.Fatal("packet outside the handshake phase was accepted")
			}
			assertGenerationCancelled(t, generation)
		})
	}
}
