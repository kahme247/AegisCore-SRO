package client

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestClientExpiresSilentMassiveHeader(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := NewClient("127.0.0.1", 1, "test")
	generation := newClientGeneration(1, clientConn)
	limits := network.DefaultMassiveLimits()
	limits.MaxAssemblyAge = 40 * time.Millisecond
	codec, err := network.NewMassiveCodec(limits)
	if err != nil {
		t.Fatalf("NewMassiveCodec() error = %v", err)
	}
	generation.massiveAssembler = network.NewMassiveAssembler(codec)
	client.mutex.Lock()
	client.lifecycleRunning = true
	client.generation = generation
	client.Conn = clientConn
	client.mutex.Unlock()

	go client.readerLoop(generation)
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
	frame, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	if err := writeAllClient(serverConn, frame); err != nil {
		t.Fatalf("write massive header: %v", err)
	}

	select {
	case <-generation.ctx.Done():
	case <-time.After(400 * time.Millisecond):
		t.Fatal("silent massive header did not expire client reader")
	}
	generation.errMu.Lock()
	err = generation.err
	generation.errMu.Unlock()
	if !errors.Is(err, network.ErrMassiveAssemblyExpired) {
		t.Fatalf("generation error = %v, want ErrMassiveAssemblyExpired", err)
	}
	select {
	case <-generation.done:
	case <-time.After(time.Second):
		t.Fatal("client reader did not exit after silent massive assembly expiry")
	}
}
