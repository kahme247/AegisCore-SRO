package client

import (
	"net"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/security"
	"github.com/ferdoran/go-sro/framework/security/blowfish"
	"github.com/ferdoran/go-sro/framework/utils"
)

func TestKeyExchangeHandlerFailsClosedOnMalformedAndInvalidPhases(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Client)
		packet network.Packet
	}{
		{
			name:   "truncated initial packet",
			setup:  func(*Client) {},
			packet: network.Packet{},
		},
		{
			name:   "initial encryption setup missing its key",
			setup:  func(*Client) {},
			packet: packetWithByte(t, network.EncodingEncryption),
		},
		{
			name:   "initial handshake with trailing data",
			setup:  func(*Client) {},
			packet: packetWithBytes(t, network.EncodingNone, 0xFF),
		},
		{
			name: "out of order challenge",
			setup: func(client *Client) {
				client.Ctx.HandshakeStarted = true
			},
			packet: packetWithByte(t, network.EncodingKeyChallenge),
		},
		{
			name: "duplicate after completion",
			setup: func(client *Client) {
				client.Ctx.HandshakeStarted = true
				client.Ctx.LocalSetupCompleted = true
				client.Ctx.HandshakeCompleted = true
			},
			packet: packetWithByte(t, network.EncodingKeyChallenge),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, generation, cleanup := attachedKeyExchangeClient(t)
			defer cleanup()
			test.setup(client)

			client.PacketHandlers[0x5000].Handle(test.packet)
			assertGenerationCancelled(t, generation)
		})
	}
}

func TestKeyExchangeHandlerFailsClosedOnInvalidServerSignature(t *testing.T) {
	client, generation, cleanup := attachedKeyExchangeClient(t)
	defer cleanup()

	cipher, err := blowfish.NewCipher([]byte("key-exchange-test"))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	client.Ctx.HandshakeStarted = true
	client.Ctx.LocalSetupCompleted = true
	client.Ctx.CommonSecret = 0x01020304
	client.Ctx.RemotePublic = 0x10203040
	client.Ctx.LocalPublic = 0x55667788
	client.Cipher = cipher

	expected := security.CalculateChallenge(client.Ctx.CommonSecret, client.Ctx.RemotePublic, client.Ctx.LocalPublic)
	cipher.EncryptRev(expected, expected)
	wrongSignature := utils.ByteArrayToUint64(expected) ^ 1
	packet := network.EmptyPacket()
	if err := packet.WriteByte(network.EncodingKeyChallenge); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	if err := packet.WriteUInt64(wrongSignature); err != nil {
		t.Fatalf("WriteUInt64() error = %v", err)
	}

	client.PacketHandlers[0x5000].Handle(packet)
	assertGenerationCancelled(t, generation)
}

func TestKeyExchangeHandlerRejectsTrivialDHValues(t *testing.T) {
	tests := []struct {
		name      string
		generator uint32
		remote    uint32
	}{
		{name: "trivial generator", generator: 1, remote: 2},
		{name: "trivial remote public", generator: 2, remote: 1},
		{name: "upper bound remote public", generator: 2, remote: 16},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, generation, cleanup := attachedKeyExchangeClient(t)
			defer cleanup()

			client.PacketHandlers[0x5000].Handle(keyExchangeInitialPacket(t, test.generator, 17, test.remote))
			assertGenerationCancelled(t, generation)
		})
	}
}

func attachedKeyExchangeClient(t *testing.T) (*Client, *clientGeneration, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	client := NewClient("127.0.0.1", 1, "test")
	generation := newClientGeneration(1, clientConn)
	client.mutex.Lock()
	client.lifecycleRunning = true
	client.generation = generation
	client.Conn = clientConn
	client.mutex.Unlock()

	cleanup := func() {
		generation.stop(ErrClientClosed)
		_ = serverConn.Close()
		_ = clientConn.Close()
	}
	return client, generation, cleanup
}

func assertGenerationCancelled(t *testing.T, generation *clientGeneration) {
	t.Helper()
	select {
	case <-generation.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("invalid key exchange did not cancel the active generation")
	}
}

func packetWithByte(t *testing.T, value byte) network.Packet {
	t.Helper()
	packet := network.EmptyPacket()
	if err := packet.WriteByte(value); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	return packet
}

func packetWithBytes(t *testing.T, values ...byte) network.Packet {
	t.Helper()
	packet := network.EmptyPacket()
	for _, value := range values {
		if err := packet.WriteByte(value); err != nil {
			t.Fatalf("WriteByte() error = %v", err)
		}
	}
	return packet
}

func keyExchangeInitialPacket(t *testing.T, generator, prime, remotePublic uint32) network.Packet {
	t.Helper()
	packet := network.EmptyPacket()
	if err := packet.WriteByte(network.EncodingKeyExchange); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	if err := packet.WriteUInt64(0x0102030405060708); err != nil {
		t.Fatalf("WriteUInt64() error = %v", err)
	}
	if err := packet.WriteUInt32(generator); err != nil {
		t.Fatalf("WriteUInt32(generator) error = %v", err)
	}
	if err := packet.WriteUInt32(prime); err != nil {
		t.Fatalf("WriteUInt32(prime) error = %v", err)
	}
	if err := packet.WriteUInt32(remotePublic); err != nil {
		t.Fatalf("WriteUInt32(remote public) error = %v", err)
	}
	return packet
}
