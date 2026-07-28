package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/security"
	"github.com/ferdoran/go-sro/framework/security/blowfish"
	"github.com/ferdoran/go-sro/framework/utils"
)

func TestReconnectTerminalCloseIsNilSafe(t *testing.T) {
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = false

	client.Reconnect()

	select {
	case <-client.ConnectionClosed:
	default:
		t.Fatal("terminal reconnect did not publish a close notification")
	}
}

func TestResetSecurityForReconnectUsesFreshPrivateKey(t *testing.T) {
	client := NewClient("127.0.0.1", 1, "test")
	client.Ctx.Private = 0x12345678
	client.Ctx.HandshakeStarted = true
	client.Ctx.HandshakeCompleted = true

	if err := client.resetSecurityForReconnect(); err != nil {
		t.Fatalf("resetSecurityForReconnect() error = %v", err)
	}

	if client.Ctx.Private == 0 {
		t.Fatal("reconnect private key is zero")
	}
	if client.Ctx.Private == 0x12345678 {
		t.Fatal("reconnect reused the previous private key")
	}
	if client.Ctx.HandshakeStarted || client.Ctx.HandshakeCompleted {
		t.Fatal("reconnect retained prior handshake state")
	}
}

func TestReconnectNotificationWaitsForHandshake(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 2)
	acceptErrors := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				acceptErrors <- err
				return
			}
			accepted <- conn
		}
	}()

	address := listener.Addr().(*net.TCPAddr)
	client := NewClient("127.0.0.1", address.Port, "test")
	client.AutoReconnect = true
	client.ReconnectTimeoutInSeconds = 0
	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()

	var firstConn net.Conn
	select {
	case firstConn = <-accepted:
		defer firstConn.Close()
	case err := <-acceptErrors:
		t.Fatalf("Accept() error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("client did not connect")
	}
	defer client.closeConnection()

	completeNoKeyExchangeHandshake(t, firstConn)

	select {
	case <-connectDone:
	case <-time.After(time.Second):
		t.Fatal("Connect() did not return after module identification")
	}
	if err := firstConn.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	var secondConn net.Conn
	select {
	case secondConn = <-accepted:
		defer secondConn.Close()
	case err := <-acceptErrors:
		t.Fatalf("second Accept() error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("client did not reconnect")
	}

	select {
	case <-client.Reconnected:
		t.Fatal("reconnect notification arrived before the second handshake completed")
	case <-time.After(100 * time.Millisecond):
	}
	completeNoKeyExchangeHandshake(t, secondConn)
	select {
	case <-client.Reconnected:
	case <-time.After(time.Second):
		t.Fatal("reconnect notification was not delivered after handshake completion")
	}
}

func TestRepeatedReconnectsUseOneLifecycleAndFreshGenerations(t *testing.T) {
	const generations = 32
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = true
	client.ReconnectTimeoutInSeconds = 0
	serverConnections := make(chan net.Conn, generations)
	var dialCalls atomic.Int32
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		call := dialCalls.Add(1)
		if call > generations {
			return nil, errors.New("unexpected extra generation")
		}
		clientConn, serverConn := net.Pipe()
		serverConnections <- serverConn
		return clientConn, nil
	}
	var finalServerConn net.Conn
	defer func() {
		client.Close()
		if finalServerConn != nil {
			_ = finalServerConn.Close()
		}
		if done := client.Done(); done != nil {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("client lifecycle did not stop after reconnect stress test")
			}
		}
	}()

	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()

	for generation := 0; generation < generations; generation++ {
		var serverConn net.Conn
		select {
		case serverConn = <-serverConnections:
		case <-time.After(time.Second):
			t.Fatalf("generation %d did not dial", generation+1)
		}
		completeNoKeyExchangeHandshake(t, serverConn)

		if generation == 0 {
			select {
			case <-connectDone:
			case <-time.After(time.Second):
				t.Fatal("first Connect() did not return")
			}
		} else {
			select {
			case <-client.Reconnected:
			case <-time.After(time.Second):
				t.Fatalf("generation %d never became ready", generation+1)
			}
		}

		if generation == generations-1 {
			finalServerConn = serverConn
			continue
		}
		if err := serverConn.Close(); err != nil {
			t.Fatalf("close generation %d server connection: %v", generation+1, err)
		}
	}

	if got, want := dialCalls.Load(), int32(generations); got != want {
		t.Fatalf("dial generations = %d, want %d", got, want)
	}
}

func moduleIdentificationFrame(t *testing.T) []byte {
	t.Helper()
	packet := network.EmptyPacket()
	packet.MessageID = 0x2001
	if err := packet.WriteString("test"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := packet.WriteByte(1); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	frame, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return frame
}

func completeNoKeyExchangeHandshake(t *testing.T, serverConn net.Conn) {
	t.Helper()
	initial := network.EmptyPacket()
	initial.MessageID = 0x5000
	if err := initial.WriteByte(network.EncodingNone); err != nil {
		t.Fatalf("write initial handshake options: %v", err)
	}
	initialFrame, err := initial.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal initial handshake: %v", err)
	}
	if err := writeAllClient(serverConn, initialFrame); err != nil {
		t.Fatalf("write initial handshake: %v", err)
	}

	reader := network.NewPacketReader(serverConn)
	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("read module identification: %v", err)
	}
	if got, want := len(packets), 1; got != want {
		t.Fatalf("module identification frame count = %d, want %d", got, want)
	}
	if got, want := packets[0].MessageID, uint16(0x2001); got != want {
		t.Fatalf("module identification opcode = 0x%04X, want 0x%04X", got, want)
	}
	if err := writeAllClient(serverConn, moduleIdentificationFrame(t)); err != nil {
		t.Fatalf("write module identification response: %v", err)
	}
}

func TestCloseCancelsBlockingDial(t *testing.T) {
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = true
	client.DialTimeout = time.Minute
	started := make(chan struct{})
	client.dialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}

	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dial did not start")
	}

	client.Close()
	select {
	case <-connectDone:
	case <-time.After(time.Second):
		t.Fatal("Connect() did not return after Close() cancelled its dial")
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("client lifecycle did not stop after Close()")
	}
	select {
	case <-client.ConnectionClosed:
	case <-time.After(time.Second):
		t.Fatal("Close() did not publish one close notification")
	}
	client.Close()
	select {
	case <-client.ConnectionClosed:
		t.Fatal("idempotent Close() published a duplicate close notification")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestClientCanConnectAgainAfterClose(t *testing.T) {
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = false
	serverConnections := make(chan net.Conn, 2)
	var dialCalls atomic.Int32
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		dialCalls.Add(1)
		clientConn, serverConn := net.Pipe()
		serverConnections <- serverConn
		return clientConn, nil
	}

	connectOnce := func(label string) net.Conn {
		t.Helper()
		connected := make(chan struct{})
		go func() {
			client.Connect()
			close(connected)
		}()
		var serverConn net.Conn
		select {
		case serverConn = <-serverConnections:
		case <-time.After(time.Second):
			t.Fatalf("%s lifecycle did not dial", label)
		}
		completeNoKeyExchangeHandshake(t, serverConn)
		select {
		case <-connected:
		case <-time.After(time.Second):
			t.Fatalf("%s Connect() did not complete", label)
		}
		return serverConn
	}

	firstServerConn := connectOnce("first")
	client.Close()
	_ = firstServerConn.Close()
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("first lifecycle did not stop")
	}

	secondServerConn := connectOnce("second")
	client.Close()
	_ = secondServerConn.Close()
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("second lifecycle did not stop")
	}
	if got, want := dialCalls.Load(), int32(2); got != want {
		t.Fatalf("dial count = %d, want %d", got, want)
	}
}

func TestHandshakeTimeoutClosesGeneration(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = false
	client.HandshakeTimeout = 25 * time.Millisecond
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return clientConn, nil
	}

	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()
	select {
	case <-connectDone:
	case <-time.After(time.Second):
		t.Fatal("Connect() did not return after handshake timeout")
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("timed-out lifecycle did not finish")
	}
	select {
	case <-client.ConnectionClosed:
	default:
		t.Fatal("handshake timeout did not publish a close notification")
	}
}

func TestConcurrentConnectCallsUseOneGeneration(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = false
	var dialCalls atomic.Int32
	dialed := make(chan struct{})
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		if dialCalls.Add(1) != 1 {
			return nil, errors.New("duplicate client lifecycle attempted a second dial")
		}
		close(dialed)
		return clientConn, nil
	}
	defer closeAndWaitClient(t, client)

	const callers = 8
	var callersDone sync.WaitGroup
	callersDone.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer callersDone.Done()
			client.Connect()
		}()
	}
	select {
	case <-dialed:
	case <-time.After(time.Second):
		t.Fatal("shared lifecycle did not dial")
	}
	completeNoKeyExchangeHandshake(t, serverConn)
	callersReturned := make(chan struct{})
	go func() {
		callersDone.Wait()
		close(callersReturned)
	}()
	select {
	case <-callersReturned:
	case <-time.After(time.Second):
		t.Fatal("concurrent Connect callers did not share the ready generation")
	}
	if got, want := dialCalls.Load(), int32(1); got != want {
		t.Fatalf("dial count = %d, want %d", got, want)
	}
}

func TestNoKeyExchangeSendsModuleIdentification(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	client := NewClient("127.0.0.1", 1, "test-module")
	client.AutoReconnect = false
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return clientConn, nil
	}
	defer closeAndWaitClient(t, client)

	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()

	initial := network.EmptyPacket()
	initial.MessageID = 0x5000
	if err := initial.WriteByte(network.EncodingNone); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	initialFrame, err := initial.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	if err := writeAllClient(serverConn, initialFrame); err != nil {
		t.Fatalf("write initial handshake: %v", err)
	}

	reader := network.NewPacketReader(serverConn)
	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("read module identification: %v", err)
	}
	if got, want := len(packets), 1; got != want {
		t.Fatalf("module frame count = %d, want %d", got, want)
	}
	if got, want := packets[0].MessageID, uint16(0x2001); got != want {
		t.Fatalf("module opcode = 0x%04X, want 0x%04X", got, want)
	}
	moduleID, err := packets[0].ReadString()
	if err != nil {
		t.Fatalf("read outbound module id: %v", err)
	}
	if got, want := moduleID, "test-module"; got != want {
		t.Fatalf("outbound module id = %q, want %q", got, want)
	}

	if err := writeAllClient(serverConn, moduleIdentificationFrame(t)); err != nil {
		t.Fatalf("write module identification response: %v", err)
	}
	select {
	case <-connectDone:
	case <-time.After(time.Second):
		t.Fatal("Connect() did not complete after no-key-exchange handshake")
	}
}

func TestKeyExchangeCompletesInStrictFourMessageOrder(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	client := NewClient("127.0.0.1", 1, "test-module")
	client.AutoReconnect = false
	client.securityMu.Lock()
	client.Ctx.Private = 11
	client.securityMu.Unlock()
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return clientConn, nil
	}
	defer closeAndWaitClient(t, client)

	const (
		handshakeKey  = uint64(0x0102030405060708)
		generator     = uint32(2)
		prime         = uint32(7919)
		serverPrivate = uint32(7)
	)
	serverPublic := security.G_pow_X_mod_P(generator, serverPrivate, prime)
	initial := network.EmptyPacket()
	initial.MessageID = 0x5000
	if err := initial.WriteByte(network.EncodingKeyExchange); err != nil {
		t.Fatalf("write key-exchange options: %v", err)
	}
	if err := initial.WriteUInt64(handshakeKey); err != nil {
		t.Fatalf("write handshake key: %v", err)
	}
	if err := initial.WriteUInt32(generator); err != nil {
		t.Fatalf("write generator: %v", err)
	}
	if err := initial.WriteUInt32(prime); err != nil {
		t.Fatalf("write prime: %v", err)
	}
	if err := initial.WriteUInt32(serverPublic); err != nil {
		t.Fatalf("write server public key: %v", err)
	}
	initialFrame, err := initial.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal key-exchange setup: %v", err)
	}

	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()
	if err := writeAllClient(serverConn, initialFrame); err != nil {
		t.Fatalf("write key-exchange setup: %v", err)
	}

	reader := network.NewPacketReader(serverConn)
	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("read client key-exchange response: %v", err)
	}
	if got, want := len(packets), 1; got != want {
		t.Fatalf("client key-exchange frame count = %d, want %d", got, want)
	}
	response := packets[0]
	if got, want := response.MessageID, uint16(0x5000); got != want {
		t.Fatalf("client key-exchange opcode = 0x%04X, want 0x%04X", got, want)
	}
	clientPublic, err := response.ReadUInt32()
	if err != nil {
		t.Fatalf("read client public key: %v", err)
	}
	clientSignature, err := response.ReadUInt64()
	if err != nil {
		t.Fatalf("read client signature: %v", err)
	}
	commonSecret := security.G_pow_X_mod_P(clientPublic, serverPrivate, prime)
	handshakeCipher, err := blowfish.NewCipher(security.CalculateKey(commonSecret, serverPublic, clientPublic))
	if err != nil {
		t.Fatalf("create handshake cipher: %v", err)
	}
	expectedClientSignature := security.CalculateChallenge(commonSecret, clientPublic, serverPublic)
	handshakeCipher.EncryptRev(expectedClientSignature, expectedClientSignature)
	if got, want := clientSignature, utils.ByteArrayToUint64(expectedClientSignature); got != want {
		t.Fatalf("client signature = %d, want %d", got, want)
	}

	serverSignature := security.CalculateChallenge(commonSecret, serverPublic, clientPublic)
	handshakeCipher.EncryptRev(serverSignature, serverSignature)
	challenge := network.EmptyPacket()
	challenge.MessageID = 0x5000
	if err := challenge.WriteByte(network.EncodingKeyChallenge); err != nil {
		t.Fatalf("write challenge option: %v", err)
	}
	if err := challenge.WriteUInt64(utils.ByteArrayToUint64(serverSignature)); err != nil {
		t.Fatalf("write server challenge: %v", err)
	}
	challengeFrame, err := challenge.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal server challenge: %v", err)
	}
	if err := writeAllClient(serverConn, challengeFrame); err != nil {
		t.Fatalf("write server challenge: %v", err)
	}

	packets = nil
	for len(packets) < 2 {
		batch, readErr := reader.ReadPackets()
		if readErr != nil {
			t.Fatalf("read completion and module identification: %v", readErr)
		}
		packets = append(packets, batch...)
	}
	if got, want := packets[0].MessageID, uint16(0x9000); got != want {
		t.Fatalf("key-exchange completion opcode = 0x%04X, want 0x%04X", got, want)
	}
	if got, want := packets[1].MessageID, uint16(0x2001); got != want {
		t.Fatalf("module identification opcode = 0x%04X, want 0x%04X", got, want)
	}
	if err := writeAllClient(serverConn, moduleIdentificationFrame(t)); err != nil {
		t.Fatalf("write module identification response: %v", err)
	}
	select {
	case <-connectDone:
	case <-time.After(time.Second):
		t.Fatal("Connect() did not complete after the strict key exchange")
	}
}

func TestOutboundSenderPreservesOrderAndOwnsFrame(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	wrappedConn := &gatedWriteConn{Conn: clientConn, completed: make(chan struct{}, 4)}
	client := NewClient("127.0.0.1", 1, "test")
	client.AutoReconnect = false
	client.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return wrappedConn, nil
	}
	defer closeAndWaitClient(t, client)

	connectDone := make(chan struct{})
	go func() {
		client.Connect()
		close(connectDone)
	}()
	completeNoKeyExchangeHandshake(t, serverConn)
	select {
	case <-wrappedConn.completed:
	case <-time.After(time.Second):
		t.Fatal("handshake module identification did not finish sending")
	}
	select {
	case <-connectDone:
	case <-time.After(time.Second):
		t.Fatal("client did not connect")
	}

	first := network.NewClientPacket(0xA100)
	if err := first.WriteByte(0x11); err != nil {
		t.Fatalf("first WriteByte() error = %v", err)
	}
	second := network.NewClientPacket(0xA101)
	if err := second.WriteByte(0x22); err != nil {
		t.Fatalf("second WriteByte() error = %v", err)
	}
	started, release := wrappedConn.armNextWrite()
	client.OutgoingPacketChannel <- *first
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not begin first frame write")
	}
	client.OutgoingPacketChannel <- *second
	first.Data[0] = 0x99
	close(release)

	reader := network.NewPacketReader(serverConn)
	var packets []network.Packet
	for len(packets) < 2 {
		batch, err := reader.ReadPackets()
		if err != nil {
			t.Fatalf("read outbound packets: %v", err)
		}
		packets = append(packets, batch...)
	}
	if got, want := packets[0].MessageID, uint16(0xA100); got != want {
		t.Fatalf("first outbound opcode = 0x%04X, want 0x%04X", got, want)
	}
	if got, want := packets[1].MessageID, uint16(0xA101); got != want {
		t.Fatalf("second outbound opcode = 0x%04X, want 0x%04X", got, want)
	}
	if got, want := packets[0].Payload(), []byte{0x11}; string(got) != string(want) {
		t.Fatalf("first outbound payload = %X, want %X", got, want)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-wrappedConn.completed:
		case <-time.After(time.Second):
			t.Fatal("sender did not complete its outbound writes")
		}
	}
}

func TestReconnectSecurityResetFailsClosedWhenCSPRNGFails(t *testing.T) {
	client := NewClient("127.0.0.1", 1, "test")
	previous := client.Ctx.Private
	client.randomBytes = func([]byte) (int, error) {
		return 0, errors.New("CSPRNG unavailable")
	}
	if err := client.resetSecurityForReconnect(); !errors.Is(err, ErrClientPrivateKey) {
		t.Fatalf("resetSecurityForReconnect() error = %v, want ErrClientPrivateKey", err)
	}
	if got := client.Ctx.Private; got != previous {
		t.Fatalf("private key changed after failed reset: got %d, want %d", got, previous)
	}
}

func closeAndWaitClient(t *testing.T, client *Client) {
	t.Helper()
	client.Close()
	if done := client.Done(); done != nil {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("client did not stop during cleanup")
		}
	}
}

type gatedWriteConn struct {
	net.Conn
	mu        sync.Mutex
	started   chan struct{}
	release   chan struct{}
	completed chan struct{}
}

func (c *gatedWriteConn) armNextWrite() (<-chan struct{}, chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started = make(chan struct{})
	c.release = make(chan struct{})
	return c.started, c.release
}

func (c *gatedWriteConn) Write(bytes []byte) (int, error) {
	defer func() {
		if c.completed == nil {
			return
		}
		select {
		case c.completed <- struct{}{}:
		default:
		}
	}()
	c.mu.Lock()
	started := c.started
	release := c.release
	if started != nil {
		c.started = nil
		c.release = nil
	}
	c.mu.Unlock()
	if started != nil {
		close(started)
		<-release
	}
	return c.Conn.Write(bytes)
}
