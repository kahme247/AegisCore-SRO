package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/Harvi8/AegisCore/framework/network"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
)

type Server struct {
	host              string
	port              int
	Sessions          map[string]*Session
	options           network.EncodingOptions
	ModuleID          string
	PacketChannel     chan PacketChannelData
	SessionClosed     chan *Session
	sessionsMu        sync.RWMutex
	handlers          *HandlerRegistry
	admissionMu       sync.RWMutex
	admissionPolicy   PacketAdmissionPolicy
	trafficFilterMu   sync.RWMutex
	trafficFilter     TrafficFilterOptions
	closeHandlerMu    sync.RWMutex
	sessionClosedHook func(*Session)

	moduleIdentifiedMu   sync.RWMutex
	moduleIdentifiedHook func(context.Context, *Session) error

	listenerMu sync.Mutex
	listener   net.Listener
	serving    bool
	// sessionGate serializes the handoff from Accept to the session registry
	// with StopAccepting/DrainSessions. Without it, a connection returned by
	// Accept just before listener close could be added after shutdown had
	// already snapshotted the sessions to drain.
	sessionGate sync.Mutex
	stopping    bool
}

func NewEngine(host string, port int, options network.EncodingOptions) Server {
	packetChannel := make(chan PacketChannelData)

	handlers := NewHandlerRegistry()
	registerCoreHandlers(handlers)

	return Server{
		host:          host,
		port:          port,
		Sessions:      make(map[string]*Session),
		options:       options,
		ModuleID:      "SampleServer",
		PacketChannel: packetChannel,
		SessionClosed: make(chan *Session, 8),
		handlers:      handlers,
		trafficFilter: DefaultTrafficFilterOptions(),
	}

}

// SetTrafficFilter configures the per-connection physical-frame budget used
// by sessions accepted after this call. Invalid zero values select safe
// defaults rather than silently disabling the listener-edge protection.
func (e *Server) SetTrafficFilter(options TrafficFilterOptions) {
	if e == nil {
		return
	}
	e.trafficFilterMu.Lock()
	e.trafficFilter = normalizeTrafficFilterOptions(options)
	e.trafficFilterMu.Unlock()
}

func (e *Server) sessionTrafficFilterOptions() TrafficFilterOptions {
	e.trafficFilterMu.RLock()
	defer e.trafficFilterMu.RUnlock()
	return e.trafficFilter
}

// RegisterHandler attaches an application packet handler to this server only.
// Sessions created by the server dispatch through this registry from their
// single inbound worker, so callbacks retain packet order for each session.
func (e *Server) RegisterHandler(opcode uint16, handler SessionHandler) {
	if e == nil || e.handlers == nil {
		return
	}
	e.handlers.Register(opcode, handler)
}

// SetAdmissionPolicy installs the listener-specific gate used by newly
// accepted sessions. It should be configured during server construction;
// existing sessions retain the policy snapshot they were accepted with.
func (e *Server) SetAdmissionPolicy(policy PacketAdmissionPolicy) {
	if e == nil {
		return
	}
	e.admissionMu.Lock()
	e.admissionPolicy = policy
	e.admissionMu.Unlock()
}

func (e *Server) sessionAdmissionPolicy() PacketAdmissionPolicy {
	e.admissionMu.RLock()
	defer e.admissionMu.RUnlock()
	return e.admissionPolicy
}

// SetSessionClosedHandler installs a non-blocking terminal lifecycle hook that
// runs exactly once for each accepted session after it has been removed from
// the synchronized registry and after its inbound workers and sender have
// drained. It is the reliable cleanup path for domain state; SessionClosed
// remains a best-effort compatibility notification and can drop under burst
// disconnects.
//
// Configure the hook during startup. Implementations must return promptly and
// must not call Session.Close recursively.
func (e *Server) SetSessionClosedHandler(handler func(*Session)) {
	if e == nil {
		return
	}
	e.closeHandlerMu.Lock()
	e.sessionClosedHook = handler
	e.closeHandlerMu.Unlock()
}

// SetModuleIdentifiedHandler installs a listener-specific hook that runs after
// the normal 0x2001 module-identification response has been written. Services
// such as DownloadServer use it for protocol messages that are valid only once
// the Silkroad handshake is complete.
func (e *Server) SetModuleIdentifiedHandler(handler func(context.Context, *Session) error) {
	if e == nil {
		return
	}
	e.moduleIdentifiedMu.Lock()
	e.moduleIdentifiedHook = handler
	e.moduleIdentifiedMu.Unlock()
}

func (e *Server) sessionModuleIdentifiedHandler() func(context.Context, *Session) error {
	e.moduleIdentifiedMu.RLock()
	defer e.moduleIdentifiedMu.RUnlock()
	return e.moduleIdentifiedHook
}

func (e *Server) closeSession(session *Session) {
	e.RemoveSession(session)
}

func (e *Server) sessionDrained(session *Session) {
	e.closeHandlerMu.RLock()
	hook := e.sessionClosedHook
	e.closeHandlerMu.RUnlock()
	if hook != nil {
		hook(session)
	}
}

// Listen binds the public listener without accepting traffic yet. It lets a
// multi-listener service reserve its client port before exposing any dependent
// control plane, avoiding a state where private admission accepts tickets for
// an Agent whose public port could not bind.
func (e *Server) Listen() (net.Listener, error) {
	if e == nil {
		return nil, errors.New("server is nil")
	}
	e.sessionGate.Lock()
	stopping := e.stopping
	e.sessionGate.Unlock()
	if stopping {
		return nil, errors.New("server is shutting down")
	}
	e.listenerMu.Lock()
	defer e.listenerMu.Unlock()
	if e.listener != nil {
		return nil, errors.New("server listener is already bound")
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", e.host, e.port))
	if err != nil {
		return nil, err
	}
	e.listener = listener
	return listener, nil
}

// CloseListener releases a listener reserved by Listen. It is mainly used by
// startup rollback; sessions already accepted remain governed by their own
// close lifecycle.
func (e *Server) CloseListener() error {
	if e == nil {
		return nil
	}
	e.listenerMu.Lock()
	listener := e.listener
	e.listener = nil
	e.listenerMu.Unlock()
	if listener == nil {
		return nil
	}
	return listener.Close()
}

// StopAccepting permanently closes the public listener for this Server
// instance and prevents an in-flight Accept from being admitted after the
// shutdown snapshot begins. CloseListener remains available for reversible
// startup rollback; production shutdown paths should use this method.
func (e *Server) StopAccepting() error {
	if e == nil {
		return nil
	}
	e.sessionGate.Lock()
	e.stopping = true
	e.sessionGate.Unlock()
	return e.CloseListener()
}

// DrainSessions closes the current session snapshot and waits for every
// session-owned reader, dispatcher, sender, and terminal cleanup hook to
// finish. It deliberately does not close the listener: callers with a
// separate admission/control plane can first stop new admissions, then drain
// existing public sessions in a defined order.
func (e *Server) DrainSessions(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Snapshot under the same gate that wraps the accept-to-registry handoff.
	// Callers that use StopAccepting first therefore have a closed set of
	// sessions to drain rather than a race with a late accepted connection.
	e.sessionGate.Lock()
	sessions := e.SnapshotSessions()
	e.sessionGate.Unlock()
	for _, session := range sessions {
		if session != nil {
			session.Close()
		}
	}

	for _, session := range sessions {
		if session == nil {
			continue
		}
		select {
		case <-session.Drained():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Shutdown stops accepts first, then closes and drains the sessions that were
// already admitted. It is safe to call more than once; Session.Close and
// CloseListener are both idempotent at the lifecycle boundary.
func (e *Server) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}

	var shutdownErrors []error
	if err := e.StopAccepting(); err != nil && !errors.Is(err, net.ErrClosed) {
		shutdownErrors = append(shutdownErrors, err)
	}
	if err := e.DrainSessions(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, err)
	}
	return errors.Join(shutdownErrors...)
}

// Start preserves the conventional bind-and-serve API.
func (e *Server) Start() error {
	listener, err := e.Listen()
	if err != nil {
		return err
	}
	return e.Serve(listener)
}

// Serve accepts public connections from a listener previously obtained from
// Listen. A caller may run it in a goroutine once all dependent startup
// checks have succeeded.
func (e *Server) Serve(listener net.Listener) error {
	if e == nil || listener == nil {
		return errors.New("server listener is not configured")
	}
	e.listenerMu.Lock()
	if e.listener != listener {
		e.listenerMu.Unlock()
		return errors.New("server listener does not belong to this server")
	}
	if e.serving {
		e.listenerMu.Unlock()
		return errors.New("server is already serving")
	}
	e.serving = true
	e.listenerMu.Unlock()
	defer func() {
		e.listenerMu.Lock()
		e.serving = false
		if e.listener == listener {
			e.listener = nil
		}
		e.listenerMu.Unlock()
	}()

	log.Infof("Started listening on %s:%d\n", e.host, e.port)

	for {
		log.Infof("waiting for connection to accept...")

		conn, err := listener.Accept()

		if err != nil {
			if err == io.EOF || errors.Is(err, net.ErrClosed) {
				// Intentional shutdown is owned by the application, not by an
				// accept-loop signal goroutine. Returning nil prevents expected
				// listener closure from being logged as a service failure.
				return nil
			}
			log.Errorf("accept failed: %v", err)
			continue
		}
		log.Infof("New connection from %v\n", conn.RemoteAddr())

		// Hold the gate from the shutdown-state check through registry
		// insertion. This is what makes StopAccepting followed by
		// DrainSessions a true accept barrier rather than best effort.
		e.sessionGate.Lock()
		if e.stopping {
			e.sessionGate.Unlock()
			_ = conn.Close()
			continue
		}

		session := NewSession(
			conn,
			e.options,
			e.PacketChannel,
			e.SessionClosed,
			e.ModuleID,
		)
		session.onClose = e.closeSession
		session.onDrained = e.sessionDrained
		session.onModuleIdentified = e.sessionModuleIdentifiedHandler()
		session.handlers = e.handlers
		session.admissionPolicy = e.sessionAdmissionPolicy()
		session.trafficFilter = newSessionTrafficFilter(e.sessionTrafficFilterOptions())
		e.AddSession(session)
		e.sessionGate.Unlock()
		go session.StartHandling()
	}
}

// AddSession and RemoveSession are the synchronized registry boundary used by
// engine and application lifecycle code.
func (e *Server) AddSession(session *Session) {
	if session == nil {
		return
	}
	e.sessionsMu.Lock()
	e.Sessions[session.ID] = session
	e.sessionsMu.Unlock()
}

func (e *Server) RemoveSession(session *Session) {
	if session == nil {
		return
	}
	e.sessionsMu.Lock()
	delete(e.Sessions, session.ID)
	e.sessionsMu.Unlock()
}

func (e *Server) SnapshotSessions() []*Session {
	e.sessionsMu.RLock()
	defer e.sessionsMu.RUnlock()
	sessions := make([]*Session, 0, len(e.Sessions))
	for _, session := range e.Sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

func (e *Server) SessionCount() int {
	e.sessionsMu.RLock()
	defer e.sessionsMu.RUnlock()
	return len(e.Sessions)
}
