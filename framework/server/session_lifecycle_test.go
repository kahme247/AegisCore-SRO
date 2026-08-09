package server

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/security/blowfish"
)

func TestSessionCloseIsIdempotentAndNeverBlocksOnLegacyNotification(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	closed := make(chan *Session) // intentionally unconsumed and unbuffered
	session := NewSession(serverConn, network.EncodingOptions{}, make(chan PacketChannelData), closed, "test")
	var cleanupCalls atomic.Int32
	session.onClose = func(*Session) {
		cleanupCalls.Add(1)
	}

	finished := make(chan struct{})
	go func() {
		session.Close()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Close() blocked on an unconsumed close notification")
	}
	session.Close()
	select {
	case <-session.Done():
	default:
		t.Fatal("Done() is not closed after Close()")
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup callback count = %d, want 1", got)
	}
}

func TestSessionClosesImmediatelyWhenCRCValidationFails(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{EDC: true}, make(chan PacketChannelData), make(chan *Session, 1), "test")
	defer session.Close()

	sequenceSeed := uint32(0x12345678)
	session.Sequence = network.NewMessageSequence(sequenceSeed)
	packetSequenceState := network.NewMessageSequence(sequenceSeed)
	packetSequence := packetSequenceState.Next()
	session.Crc = network.NewMessageCRC(0x42)

	packet := network.EmptyClientPacket()
	packet.MessageID = 0xA100
	packet.Sequence = packetSequence
	if err := packet.WriteMessageID(); err != nil {
		t.Fatalf("WriteMessageID() error = %v", err)
	}
	if err := packet.WriteSize(); err != nil {
		t.Fatalf("WriteSize() error = %v", err)
	}
	if err := packet.WriteSequence(); err != nil {
		t.Fatalf("WriteSequence() error = %v", err)
	}
	packet.CRC = session.Crc.Compute(packet.Buffer[:packet.DataSize+int(network.HeaderSize)]) ^ 0xFF
	if err := packet.WriteCRC(); err != nil {
		t.Fatalf("WriteCRC() error = %v", err)
	}

	session.handleIncomingPacket(packet)
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("invalid CRC did not close the session")
	}
}

func TestKeepAliveCoreHandlerAcceptsRepeatedPackets(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()
	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	session.moduleIdentified.Store(true)

	packet := network.EmptyClientPacket()
	packet.MessageID = 0x2002
	for i := 0; i < 129; i++ {
		if err := handleKeepAlivePacket(context.Background(), session, packet); err != nil {
			t.Fatalf("keep-alive %d rejected: %v", i+1, err)
		}
	}
}

func TestServerSessionRegistryIsSynchronizedAtItsBoundary(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	firstServerConn, firstClientConn := net.Pipe()
	secondServerConn, secondClientConn := net.Pipe()
	defer firstClientConn.Close()
	defer secondClientConn.Close()
	first := NewSession(firstServerConn, network.EncodingOptions{}, nil, nil, "first")
	second := NewSession(secondServerConn, network.EncodingOptions{}, nil, nil, "second")
	defer first.Close()
	defer second.Close()

	engine.AddSession(first)
	engine.AddSession(second)
	if got := engine.SessionCount(); got != 2 {
		t.Fatalf("SessionCount() = %d, want 2", got)
	}
	engine.RemoveSession(first)
	if got := engine.SessionCount(); got != 1 {
		t.Fatalf("SessionCount() after remove = %d, want 1", got)
	}
	if got := len(engine.SnapshotSessions()); got != 1 {
		t.Fatalf("SnapshotSessions() length = %d, want 1", got)
	}
}

func TestServerCloseHookRunsAfterSynchronizedRemoval(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	var called atomic.Int32
	engine.SetSessionClosedHandler(func(closed *Session) {
		if closed != session {
			t.Errorf("close hook received session %p, want %p", closed, session)
		}
		if got := engine.SessionCount(); got != 0 {
			t.Errorf("SessionCount() during close hook = %d, want 0", got)
		}
		called.Add(1)
	})
	session.onClose = engine.closeSession
	session.onDrained = engine.sessionDrained
	engine.AddSession(session)

	session.Close()
	session.Close()
	select {
	case <-session.Drained():
	case <-time.After(time.Second):
		t.Fatal("session did not drain after close")
	}
	if got := called.Load(); got != 1 {
		t.Fatalf("close hook calls = %d, want 1", got)
	}
}

func TestSessionClosesAfterConfiguredIdleTimeout(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, make(chan PacketChannelData), make(chan *Session, 1), "test")
	session.SetIdleTimeout(25 * time.Millisecond)
	if got, want := session.IdleTimeout(), 25*time.Millisecond; got != want {
		t.Fatalf("IdleTimeout() = %v, want %v", got, want)
	}

	finished := make(chan struct{})
	go func() {
		session.StartHandling()
		close(finished)
	}()

	// StartHandling sends the unencrypted setup packet before it begins its
	// read loop. Drain it so the test reaches the configured read deadline.
	setup := make([]byte, int(network.HeaderSize)+1)
	if _, err := io.ReadFull(clientConn, setup); err != nil {
		t.Fatalf("failed to read setup packet: %v", err)
	}

	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("idle session did not close")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("StartHandling did not exit after idle timeout")
	}
}

func TestSessionHandlerRegistryDispatchesInCallerOrderAndFailsClosed(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, make(chan PacketChannelData), make(chan *Session, 1), "test")
	defer session.Close()
	session.securityMu.Lock()
	session.setHandshakePhaseLocked(HandshakeEstablished)
	session.securityMu.Unlock()
	session.moduleIdentified.Store(true)
	registry := NewHandlerRegistry()
	order := make([]byte, 0, 2)
	registry.Register(0xA100, func(_ context.Context, _ *Session, packet network.Packet) error {
		value, err := packet.ReadByte()
		if err != nil {
			return err
		}
		order = append(order, value)
		return nil
	})
	session.handlers = registry

	for _, value := range []byte{0x01, 0x02} {
		packet := network.EmptyClientPacket()
		packet.MessageID = 0xA100
		if err := packet.WriteByte(value); err != nil {
			t.Fatalf("WriteByte() error = %v", err)
		}
		session.handleIncomingPacket(packet)
	}
	if got, want := string(order), string([]byte{0x01, 0x02}); got != want {
		t.Fatalf("handler order = %v, want %v", order, []byte{0x01, 0x02})
	}

	registry.Register(0xA101, func(context.Context, *Session, network.Packet) error {
		return errors.New("malformed request")
	})
	bad := network.EmptyClientPacket()
	bad.MessageID = 0xA101
	session.handleIncomingPacket(bad)
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("handler error did not close the session")
	}
}

func TestSessionSenderEncryptsFramesAfterHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{Encryption: true}, nil, nil, "test")
	defer session.Close()
	cipher, err := blowfish.NewCipher([]byte("session-wire-test-key"))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	session.Cipher = cipher
	session.Context.FinishedHandshake = true
	packet := network.EmptyPacket()
	packet.MessageID = 0xA100
	if err := packet.WriteBytes([]byte{0x10, 0x20, 0x30}); err != nil {
		t.Fatalf("WriteBytes() error = %v", err)
	}

	sent := make(chan error, 1)
	go func() { sent <- session.Send(packet) }()
	wire := make([]byte, int(network.EncryptOffset)+8)
	if _, err := io.ReadFull(clientConn, wire); err != nil {
		t.Fatalf("failed to read encrypted frame: %v", err)
	}
	if err := <-sent; err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	incoming := network.NewPacket(wire)
	if !incoming.Encrypted {
		t.Fatal("outbound frame is not encrypted after handshake")
	}
	decoded, err := incoming.DecryptChecked(cipher)
	if err != nil {
		t.Fatalf("DecryptChecked() error = %v", err)
	}
	if got, want := decoded.Payload(), []byte{0x10, 0x20, 0x30}; string(got) != string(want) {
		t.Fatalf("decoded payload = %X, want %X", got, want)
	}
}

func TestSessionSenderLeavesMassiveWrappersUnencryptedAfterHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{Encryption: true}, nil, nil, "test")
	defer session.Close()
	cipher, err := blowfish.NewCipher([]byte("session-wire-test-key"))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	session.Cipher = cipher
	session.Context.FinishedHandshake = true
	packet := network.EmptyPacket()
	packet.MessageID = 0xA100
	if err := packet.WriteByte(1); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}

	sent := make(chan error, 1)
	go func() { sent <- session.SendMassive(packet) }()
	wire := make([]byte, 19)
	if _, err := io.ReadFull(clientConn, wire); err != nil {
		t.Fatalf("failed to read massive frames: %v", err)
	}
	if err := <-sent; err != nil {
		t.Fatalf("SendMassive() error = %v", err)
	}
	for index, frame := range [][]byte{wire[:11], wire[11:]} {
		incoming := network.NewPacket(frame)
		if incoming.Encrypted {
			t.Fatalf("massive wrapper %d is encrypted", index)
		}
		if incoming.MessageID != network.MassivePacketID {
			t.Fatalf("massive wrapper %d opcode = 0x%04X", index, incoming.MessageID)
		}
	}
}

func TestRepeatedSessionClosesReleaseEverySenderWorker(t *testing.T) {
	const sessions = 128
	created := make([]*Session, 0, sessions)
	clients := make([]net.Conn, 0, sessions)
	for i := 0; i < sessions; i++ {
		serverConn, clientConn := net.Pipe()
		created = append(created, NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test"))
		clients = append(clients, clientConn)
	}
	for _, session := range created {
		session.Close()
	}
	for _, clientConn := range clients {
		_ = clientConn.Close()
	}
	for _, session := range created {
		finished := make(chan struct{})
		go func(sender *Sender) {
			sender.Wait()
			close(finished)
		}(session.Sender)
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("sender worker did not exit after session close")
		}
	}
}

func TestThousandsOfSessionLifecyclesReleaseRegistryAndSenderWorkers(t *testing.T) {
	const sessionCount = 2048
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	sessions := make([]*Session, 0, sessionCount)
	peers := make([]net.Conn, 0, sessionCount)

	for i := 0; i < sessionCount; i++ {
		serverConn, clientConn := net.Pipe()
		session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
		session.onClose = engine.RemoveSession
		engine.AddSession(session)
		sessions = append(sessions, session)
		peers = append(peers, clientConn)
	}
	if got := engine.SessionCount(); got != sessionCount {
		t.Fatalf("SessionCount() after connect = %d, want %d", got, sessionCount)
	}

	for _, session := range sessions {
		session.Close()
	}
	for _, peer := range peers {
		_ = peer.Close()
	}
	if got := engine.SessionCount(); got != 0 {
		t.Fatalf("SessionCount() after disconnect = %d, want 0", got)
	}

	finished := make(chan struct{})
	go func() {
		for _, session := range sessions {
			session.Sender.Wait()
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("sender workers remained after thousands of session closes")
	}
}
