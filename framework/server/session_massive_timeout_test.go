package server

import (
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSessionExpiresSilentMassiveHeaderBeforeIdleDeadline(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, make(chan PacketChannelData, 1), nil, "test")
	defer session.Close()

	limits := network.DefaultMassiveLimits()
	limits.MaxAssemblyAge = 40 * time.Millisecond
	codec, err := network.NewMassiveCodec(limits)
	if err != nil {
		t.Fatalf("NewMassiveCodec() error = %v", err)
	}
	session.massive.replaceAssembler(network.NewMassiveAssembler(codec))
	session.SetIdleTimeout(time.Second)
	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	session.moduleIdentified.Store(true)

	finished := make(chan struct{})
	go func() {
		session.StartHandling()
		close(finished)
	}()

	header := network.EmptyClientPacket()
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
	if written, err := clientConn.Write(frame); err != nil || written != len(frame) {
		t.Fatalf("write massive header = (%d, %v), want (%d, nil)", written, err, len(frame))
	}

	select {
	case <-session.Done():
	case <-time.After(400 * time.Millisecond):
		t.Fatal("silent massive header waited for the one-second idle timeout instead of assembly expiry")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("StartHandling did not exit after silent massive assembly expiry")
	}
}
