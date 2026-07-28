package client

import (
	"errors"
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
)

func TestClientRejectsKeepAliveInterleavedWithMassiveAssembly(t *testing.T) {
	client, generation, cleanup := attachedKeyExchangeClient(t)
	defer cleanup()
	client.securityMu.Lock()
	client.Ctx.HandshakeCompleted = true
	client.securityMu.Unlock()
	generation.readyOnce.Do(func() { close(generation.ready) })

	header := network.EmptyPacket()
	header.MessageID = network.MassivePacketID
	if err := header.WriteBool(true); err != nil {
		t.Fatalf("WriteBool() error = %v", err)
	}
	if err := header.WriteUInt16(1); err != nil {
		t.Fatalf("WriteUInt16(body count) error = %v", err)
	}
	if err := header.WriteUInt16(0xA100); err != nil {
		t.Fatalf("WriteUInt16(target opcode) error = %v", err)
	}
	if !client.handleIncomingPacketForGeneration(generation, header) {
		t.Fatal("valid massive header was rejected")
	}

	keepAlive := network.EmptyPacket()
	keepAlive.MessageID = 0x2002
	if client.handleIncomingPacketForGeneration(generation, keepAlive) {
		t.Fatal("keep-alive interleaved with massive assembly was accepted")
	}
	assertGenerationCancelled(t, generation)
	generation.errMu.Lock()
	err := generation.err
	generation.errMu.Unlock()
	if !errors.Is(err, network.ErrMassiveInterleavedFrame) {
		t.Fatalf("generation error = %v, want ErrMassiveInterleavedFrame", err)
	}
}
