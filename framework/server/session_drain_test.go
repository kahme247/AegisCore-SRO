package server

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

// drainingGateConn deliberately keeps an in-flight Write blocked even after
// Close. It lets the lifecycle test prove that domain cleanup waits for the
// sender worker itself rather than assuming Conn.Close has already stopped it.
type drainingGateConn struct {
	writeStarted chan struct{}
	releaseWrite chan struct{}
	closeOnce    sync.Once
	closed       atomic.Bool
}

func newDrainingGateConn() *drainingGateConn {
	return &drainingGateConn{
		writeStarted: make(chan struct{}, 1),
		releaseWrite: make(chan struct{}),
	}
}

func (c *drainingGateConn) Read([]byte) (int, error) { return 0, io.EOF }

func (c *drainingGateConn) Write(bytes []byte) (int, error) {
	select {
	case c.writeStarted <- struct{}{}:
	default:
	}
	<-c.releaseWrite
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return len(bytes), nil
}

func (c *drainingGateConn) Close() error {
	c.closeOnce.Do(func() { c.closed.Store(true) })
	return nil
}

func (*drainingGateConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*drainingGateConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*drainingGateConn) SetDeadline(time.Time) error      { return nil }
func (*drainingGateConn) SetReadDeadline(time.Time) error  { return nil }
func (*drainingGateConn) SetWriteDeadline(time.Time) error { return nil }

func TestSessionDefersTerminalHookUntilWorkersAndSenderDrain(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	conn := newDrainingGateConn()
	session := NewSession(conn, network.EncodingOptions{}, nil, nil, "test")
	session.onClose = engine.closeSession
	session.onDrained = engine.sessionDrained
	engine.AddSession(session)

	callback := make(chan UserContext, 1)
	var callbackCount atomic.Int32
	engine.SetSessionClosedHandler(func(closed *Session) {
		callbackCount.Add(1)
		callback <- closed.UserContext
	})

	// Reserve a packet handler that is still updating domain state when the
	// socket fails. Close must remove the registry entry immediately, but it
	// must not invoke the terminal hook until this worker is finished.
	session.lifetime.addWorkerForTest()
	workerStarted := make(chan struct{})
	releaseWorker := make(chan struct{})
	go func() {
		defer session.lifetime.workerDone()
		session.UserContext.UserID = 77
		close(workerStarted)
		<-releaseWorker
		session.UserContext.UniqueID = 9001
		session.UserContext.CharName = "terminal-state"
	}()
	<-workerStarted

	packet := network.EmptyPacket()
	packet.MessageID = 0xA100
	sendFinished := make(chan error, 1)
	go func() { sendFinished <- session.Send(packet) }()
	select {
	case <-conn.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("sender did not begin its blocked write")
	}

	session.Close()
	if got := engine.SessionCount(); got != 0 {
		t.Fatalf("SessionCount() immediately after Close = %d, want 0", got)
	}
	select {
	case state := <-callback:
		t.Fatalf("terminal hook ran before workers drained: %+v", state)
	default:
	}

	close(releaseWorker)
	select {
	case state := <-callback:
		t.Fatalf("terminal hook ran before sender drained: %+v", state)
	case <-time.After(50 * time.Millisecond):
	}

	close(conn.releaseWrite)
	select {
	case state := <-callback:
		if state.UserID != 77 || state.UniqueID != 9001 || state.CharName != "terminal-state" {
			t.Fatalf("terminal hook observed incomplete UserContext: %+v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal hook did not run after workers and sender drained")
	}
	select {
	case <-session.Drained():
	case <-time.After(time.Second):
		t.Fatal("session did not report drained after terminal hook")
	}
	select {
	case <-sendFinished:
	case <-time.After(time.Second):
		t.Fatal("blocked sender call did not return after drain")
	}
	session.Close()
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("terminal hook calls = %d, want 1", got)
	}
}

func TestSessionCloseBeforeStartDrainsTerminalHookOnce(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	session.onClose = engine.closeSession
	session.onDrained = engine.sessionDrained
	engine.AddSession(session)

	var callbackCount atomic.Int32
	engine.SetSessionClosedHandler(func(*Session) { callbackCount.Add(1) })
	session.Close()
	session.Close()

	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("Close-before-Start did not publish immediate shutdown")
	}
	select {
	case <-session.Drained():
	case <-time.After(time.Second):
		t.Fatal("Close-before-Start did not drain")
	}
	if got := engine.SessionCount(); got != 0 {
		t.Fatalf("SessionCount() after Close-before-Start = %d, want 0", got)
	}
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("terminal hook calls = %d, want 1", got)
	}

	finished := make(chan struct{})
	go func() {
		session.StartHandling()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("StartHandling blocked after Close-before-Start")
	}
}
