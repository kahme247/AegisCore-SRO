package server

import (
	"context"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/security/blowfish"
	utils2 "github.com/Harvi8/AegisCore/framework/utils"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultSessionIdleTimeout = 30 * time.Second

// HandshakePhase is the single authoritative progression for the vSRO
// challenge exchange. The old boolean fields remain in SecurityContext for
// compatibility with handlers, but packet admission must consult this ordered
// state instead of accepting any combination of booleans.
type HandshakePhase uint8

const (
	HandshakeNew HandshakePhase = iota
	HandshakeChallengeSent
	HandshakeAwaitingCompletion
	HandshakeEstablished
)

type Session struct {
	ID          string
	Conn        net.Conn
	Context     SecurityContext
	UserContext UserContext
	network.EncodingOptions
	Sequence            network.MessageSequence
	Crc                 network.MessageCRC
	Cipher              *blowfish.Cipher
	ServerPacketChannel chan PacketChannelData
	ConnectionClosed    chan *Session
	mutex               *sync.Mutex
	securityMu          sync.RWMutex
	ServerModuleID      string
	packetChannel       chan network.Packet
	massive             *inboundMassive
	Sender              *Sender
	lifetime            *sessionLifetime
	onClose             func(*Session)
	onDrained           func(*Session)
	onModuleIdentified  func(context.Context, *Session) error
	handlers            *HandlerRegistry
	admissionPolicy     PacketAdmissionPolicy
	trafficFilter       *sessionTrafficFilter
	handshakePhase      atomic.Uint32
	moduleIdentified    atomic.Bool
	authenticated       atomic.Bool
	lifecyclePhase      atomic.Uint32
	idleTimeoutNanos    atomic.Int64
	lastSeenUnixNano    atomic.Int64
}

// HandshakePhase returns the ordered protocol state for this session.
func (s *Session) HandshakePhase() HandshakePhase {
	return HandshakePhase(s.handshakePhase.Load())
}

func (s *Session) setHandshakePhaseLocked(phase HandshakePhase) {
	s.handshakePhase.Store(uint32(phase))
}

// ModuleIdentified is set only after a complete, ordered module
// identification packet has been accepted.
func (s *Session) ModuleIdentified() bool {
	return s.moduleIdentified.Load()
}

// MarkAuthenticated records successful application admission. Authentication
// handlers call it only after atomically redeeming their one-time ticket.
func (s *Session) MarkAuthenticated() {
	if s == nil || !s.markAuthenticated() {
		return
	}
	s.authenticated.Store(true)
}

func (s *Session) IsAuthenticated() bool {
	return s.authenticated.Load()
}

func NewSession(
	conn net.Conn,
	options network.EncodingOptions,
	packetChannel chan PacketChannelData,
	closedConnectionChannel chan *Session,
	serverModuleId string,
) *Session {
	sessionId, err := uuid.NewRandom()
	if err != nil {
		log.Error("Failed to create session id")
	}

	session := &Session{
		ID:                  sessionId.String(),
		Conn:                conn,
		EncodingOptions:     options,
		Context:             NewContext(),
		ServerPacketChannel: packetChannel,
		mutex:               &sync.Mutex{},
		ConnectionClosed:    closedConnectionChannel,
		ServerModuleID:      serverModuleId,
		packetChannel:       make(chan network.Packet, 1024),
		massive:             newInboundMassive(),
		lifetime:            newSessionLifetime(),
	}
	session.Sender = NewSender(conn, func(packet network.Packet) ([]byte, error) {
		return session.encodeOutboundPacket(packet)
	})
	session.idleTimeoutNanos.Store(int64(DefaultSessionIdleTimeout))
	session.markSeen()

	return session
}

func (s *Session) StartHandling() {
	if !s.startInboundWorkers() {
		return
	}
	defer func() {
		// This goroutine is one of the two workers reserved under lifecycleMu.
		// Mark it done before Close starts terminal draining so the final wait
		// cannot wait on itself.
		s.lifetime.workerDone()
		s.Close()
		s.lifetime.waitWorkers()
		if s.Sender != nil {
			s.Sender.Wait()
		}
	}()
	if sessionTraceEnabled() {
		log.Infof("SESSION_TRACE connected module=%s remote=%s", s.ServerModuleID, s.Conn.RemoteAddr())
	}
	if s.HandshakePhase() == HandshakeNew {
		s.initialize()
	}
	go func() {
		defer s.lifetime.workerDone()
		for {
			select {
			case p := <-s.packetChannel:
				s.handleIncomingPacket(p)
			case <-s.lifetime.ctx.Done():
				return
			}
		}
	}()
	// The idle budget begins when the reader is ready, not when the Session was
	// constructed. Subsequent complete frames refresh this timestamp before
	// they are handed to the dispatcher.
	s.markSeen()
	reader := network.NewPacketReader(s.Conn)
	for {
		if err := s.refreshReadDeadline(); err != nil {
			log.Errorf("failed to set session read deadline: %v", err)
			return
		}
		packets, err := reader.ReadPackets()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if massiveErr := s.expireMassiveAssembly(time.Now()); massiveErr != nil {
					err = massiveErr
				}
			}
			if err != io.EOF {
				log.Errorln(err)
			}
			if sessionTraceEnabled() {
				log.Infof("SESSION_TRACE closed module=%s remote=%s reason=%v", s.ServerModuleID, s.Conn.RemoteAddr(), err)
			}

			log.Debugf("Connection closed: %s\n", s.Conn.RemoteAddr().String())
			return
		}

		for _, packet := range packets {
			s.markSeen()
			if sessionTraceEnabled() {
				log.Infof("SESSION_TRACE received-raw module=%s remote=%s opcode=0x%04X encrypted=%t size=%d", s.ServerModuleID, s.Conn.RemoteAddr(), packet.MessageID, packet.Encrypted, packet.DataSize)
			}
			//log.Tracef(packet.String())
			select {
			case s.packetChannel <- packet:
			case <-s.lifetime.ctx.Done():
				return
			}
		}
	}
}

// startInboundWorkers establishes the ordering needed by Close: once Close
// begins, no later StartHandling call may add a worker after terminal draining
// has started. Reserve both the reader and packet-dispatch workers while
// lifecycleMu is held so Close can safely wait even when it races the first
// StartHandling call.
func (s *Session) startInboundWorkers() bool {
	return s.lifetime != nil && s.lifetime.startInbound(2)
}

// SetIdleTimeout configures the maximum interval without a complete inbound
// frame. Values at or below zero restore the safe 30-second default.
func (s *Session) SetIdleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		timeout = DefaultSessionIdleTimeout
	}
	s.idleTimeoutNanos.Store(int64(timeout))
}

func (s *Session) IdleTimeout() time.Duration {
	timeout := time.Duration(s.idleTimeoutNanos.Load())
	if timeout <= 0 {
		return DefaultSessionIdleTimeout
	}
	return timeout
}

func (s *Session) LastSeen() time.Time {
	nanos := s.lastSeenUnixNano.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

func (s *Session) markSeen() {
	s.lastSeenUnixNano.Store(time.Now().UnixNano())
}

// refreshReadDeadline preserves the normal idle deadline while ensuring an
// active massive assembly wakes the reader no later than its own expiry. The
// packet dispatcher also calls it after a massive wrapper, which updates a
// reader that was already blocked before the header was dispatched.
func (s *Session) refreshReadDeadline() error {
	if s.Conn == nil {
		return nil
	}
	deadline := time.Now().Add(s.IdleTimeout())
	if lastSeen := s.LastSeen(); !lastSeen.IsZero() {
		deadline = lastSeen.Add(s.IdleTimeout())
	}
	deadline = s.massive.earlierDeadline(deadline)
	return s.Conn.SetReadDeadline(deadline)
}

func (s *Session) expireMassiveAssembly(now time.Time) error {
	return s.massive.expireAt(now)
}

func (s *Session) handleIncomingPacket(p network.Packet) {
	// 1. Decode if Encrypted
	//log.Debugf("Received Packet: %s\n", p.String())
	pDec, err := p.DecryptChecked(s.inboundCipher())
	if err != nil {
		log.Errorf("Failed to decrypt packet: %v", err)
		s.Close()
		return
	}
	if sessionTraceEnabled() {
		log.Infof("SESSION_TRACE received-decoded module=%s remote=%s opcode=0x%04X size=%d", s.ServerModuleID, s.Conn.RemoteAddr(), pDec.MessageID, pDec.DataSize)
	}
	// 2. Check Sequence & CRC
	if s.EncodingOptions.EDC {
		err = s.validateEDC(pDec)
		if err != nil {
			log.Errorf("Error: %v\n", err)
			s.Close()
			return
		}
	}
	// Count every validated physical frame before massive reassembly or core
	// dispatch. This prevents handshake and 0x600D wrapper floods from bypassing
	// the application admission policy.
	if s.trafficFilter != nil {
		if err := s.trafficFilter.Admit(); err != nil {
			remote := "unknown"
			if s.Conn != nil && s.Conn.RemoteAddr() != nil {
				remote = s.Conn.RemoteAddr().String()
			}
			log.Errorf("Rejecting traffic from %s module=%s opcode=0x%04X: %v", remote, s.ServerModuleID, pDec.MessageID, err)
			s.Close()
			return
		}
	}
	physicalMassive := pDec.MessageID == network.MassivePacketID
	// 3. Reassemble massive packets only after each physical wrapper passed
	// decryption and EDC. ProcessFrame also observes ordinary frames so a
	// partial massive assembly expires and rejects interleaving instead of
	// retaining its aggregate until another 0x600D arrives.
	complete, done, massiveErr := s.massive.process(pDec)
	if massiveErr != nil {
		log.Errorf("Rejecting malformed massive packet: %v", massiveErr)
		s.Close()
		return
	}
	if physicalMassive {
		if err := s.refreshReadDeadline(); err != nil && s.lifetime.ctx.Err() == nil {
			log.Errorf("failed to cap session read deadline for massive assembly: %v", err)
			s.Close()
			return
		}
	}
	if !done {
		return
	}
	pDec = complete
	if !isCoreOpcode(pDec.MessageID) {
		// Application packets are never valid until the ordered handshake and
		// one-time module identification have both completed. This is enforced
		// here rather than delegated only to listener policies, so a newly added
		// listener cannot accidentally reopen a pre-authentication opcode path.
		if s.HandshakePhase() != HandshakeEstablished || !s.ModuleIdentified() {
			log.Errorf("Rejecting application packet 0x%04X before session admission", pDec.MessageID)
			s.Close()
			return
		}
		if s.admissionPolicy != nil {
			if err := s.admissionPolicy(s, pDec.MessageID); err != nil {
				log.Errorf("Rejecting packet 0x%04X: %v", pDec.MessageID, err)
				s.Close()
				return
			}
		}
	}
	if s.handlers != nil {
		handled, handlerErr := s.handlers.Dispatch(s.lifetime.ctx, s, pDec)
		if handlerErr != nil {
			log.Errorf("Rejecting packet 0x%04X: %v", pDec.MessageID, handlerErr)
			s.Close()
			return
		}
		if handled {
			return
		}
	}
	log.Tracef("Unknown packet received opcode=0x%04X size=%d", pDec.MessageID, pDec.PayloadLen())
	select {
	case s.ServerPacketChannel <- PacketChannelData{Session: s, Packet: pDec}:
	case <-s.lifetime.ctx.Done():
	}
}

// LifetimeContext is cancelled exactly once when the connection closes.
// Long-lived handlers can use it to stop background work associated with this
// session. It is deliberately named separately from the existing protocol
// security Context field.
func (s *Session) LifetimeContext() context.Context {
	return s.lifetime.ctx
}

// Done closes when the session begins shutdown.
func (s *Session) Done() <-chan struct{} {
	return s.lifetime.done
}

// Drained closes after every session-owned inbound worker and the ordered
// sender have exited, and after the terminal domain callback has run. Done
// remains the immediate shutdown signal for cancellation-sensitive callers.
func (s *Session) Drained() <-chan struct{} {
	return s.lifetime.drained
}

// Close is idempotent and never blocks on application notification. It first
// cancels local workers, closes the socket, and removes the session from its
// server registry through onClose. Domain cleanup is intentionally deferred
// to finishDrain so it cannot race a packet handler still writing UserContext.
func (s *Session) Close() {
	if s == nil || s.lifetime == nil {
		return
	}
	s.lifetime.close(s.markClosing, func() {
		if err := s.massive.finish(); err != nil {
			log.Debugf("discarding incomplete massive packet for session %s: %v", s.ID, err)
		}
		if s.Conn != nil {
			_ = s.Conn.Close()
		}
		if s.Sender != nil {
			s.Sender.Close()
		}
		if s.onClose != nil {
			s.onClose(s)
		}
	}, s.finishDrain)
}

func (s *Session) finishDrain() {
	s.lifetime.waitWorkers()
	if s.Sender != nil {
		s.Sender.Wait()
	}
	if s.onDrained != nil {
		s.onDrained(s)
	}
	if s.ConnectionClosed != nil {
		select {
		case s.ConnectionClosed <- s:
		default:
			log.Debugf("session close notification queue is full; session %s was already cleaned up", s.ID)
		}
	}
	s.lifetime.finishDrain()
}

func (s *Session) validateEDC(p network.Packet) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	nextSeqVal := s.Sequence.Next()
	if nextSeqVal != p.Sequence {
		return errors.Errorf("Invalid Sequence. Got = %v, want %v", p.Sequence, nextSeqVal)
	}
	oldCrc := p.Buffer[network.CRCOffset]
	p.Buffer[network.CRCOffset] = 0
	nextCrcVal := s.Crc.Compute(p.Buffer[:p.DataSize+int(network.HeaderSize)])
	if nextCrcVal != oldCrc {
		err := errors.Errorf("Invalid CRC. Got = %v, want %v", p.CRC, nextCrcVal)
		return err
		//return errors.Errorf("Invalid CRC. Got = %v, want %v", p.CRC, nextCrcVal)
	}
	return nil
}

func (s *Session) initialize() {
	log.Debugln("Writing challenge")
	s.markHandshaking()
	s.securityMu.Lock()
	if s.HandshakePhase() != HandshakeNew {
		s.securityMu.Unlock()
		return
	}
	packet := network.EmptyPacket()
	packet.MessageID = 0x5000
	if err := packet.WriteByte(s.EncodingOptions.GetEncodingOptionsByte()); err != nil {
		s.securityMu.Unlock()
		log.Errorf("failed to prepare handshake options: %v", err)
		s.Close()
		return
	}

	if err := s.writeEncryptionSetup(&packet); err != nil {
		s.securityMu.Unlock()
		log.Errorf("failed to prepare encryption setup: %v", err)
		s.Close()
		return
	}
	if err := s.writeEDC(&packet); err != nil {
		s.securityMu.Unlock()
		log.Errorf("failed to prepare EDC setup: %v", err)
		s.Close()
		return
	}
	if err := s.setupKeyExchange(&packet); err != nil {
		s.securityMu.Unlock()
		log.Errorf("failed to prepare key exchange setup: %v", err)
		s.Close()
		return
	}
	s.Context.StartedHandshake = true
	if s.EncodingOptions.KeyExchange {
		s.setHandshakePhaseLocked(HandshakeChallengeSent)
	} else {
		// Profiles without key exchange do not have a 0x5000/0x9000 response
		// round trip. They are considered protocol-ready after the setup frame.
		s.Context.CompletedLocalSetup = true
		s.Context.CompletedRemoteSetup = true
		s.setHandshakePhaseLocked(HandshakeEstablished)
	}
	s.securityMu.Unlock()

	if err := s.Send(packet); err != nil {
		log.Errorf("Failed to send encryption setup: %v", err)
	}
	if sessionTraceEnabled() {
		log.Infof("SESSION_TRACE sent-handshake module=%s remote=%s", s.ServerModuleID, s.Conn.RemoteAddr())
	}
	log.Debugln("Finished init")
}

// Send serializes an outbound packet through this session's single writer.
// The sender copies and owns the resulting frame, so callers may safely reuse
// packet builders for broadcasts after Send returns.
func (s *Session) Send(packet network.Packet) error {
	if s.Sender == nil {
		return ErrSenderClosed
	}
	packet.ServerPacket = true
	err := s.Sender.Send(packet)
	if sessionTraceEnabled() {
		log.Infof("SESSION_TRACE sent module=%s remote=%s opcode=0x%04X size=%d", s.ServerModuleID, s.Conn.RemoteAddr(), packet.MessageID, packet.DataSize)
	}
	if err != nil {
		log.Errorf("failed to send packet 0x%04X for session %s: %v", packet.MessageID, s.ID, err)
	}
	return err
}

// SendBatch writes an ordered protocol sequence as one sender job.
func (s *Session) SendBatch(packets []network.Packet) error {
	if s.Sender == nil {
		return ErrSenderClosed
	}
	for index := range packets {
		packets[index].ServerPacket = true
	}
	return s.Sender.SendBatch(packets)
}

// SendMassive writes a complete 0x600D header/body batch without allowing
// another handler's packet to interleave between its frames.
func (s *Session) SendMassive(packet network.Packet) error {
	if s.Sender == nil {
		return ErrSenderClosed
	}
	packet.ServerPacket = true
	err := s.Sender.SendMassive(packet)
	if sessionTraceEnabled() {
		log.Infof("SESSION_TRACE sent-massive module=%s remote=%s opcode=0x%04X size=%d", s.ServerModuleID, s.Conn.RemoteAddr(), packet.MessageID, packet.DataSize)
	}
	if err != nil {
		log.Errorf("failed to send massive packet 0x%04X for session %s: %v", packet.MessageID, s.ID, err)
	}
	return err
}

// SendBytes is a temporary route for documented pre-serialized responses.
func (s *Session) SendBytes(bytes []byte) error {
	if s.Sender == nil {
		return ErrSenderClosed
	}
	err := s.Sender.SendBytes(bytes)
	if err != nil {
		log.Errorf("failed to send raw bytes for session %s: %v", s.ID, err)
	}
	return err
}

func (s *Session) encodeOutboundPacket(packet network.Packet) ([]byte, error) {
	packet.ServerPacket = true
	s.securityMu.RLock()
	finishedHandshake := s.Context.FinishedHandshake
	cipher := s.Cipher
	s.securityMu.RUnlock()
	// The server-side EDC fields are zero in the current vSRO implementation;
	// only the inbound direction consumes the negotiated sequence/CRC state.
	// Do not invent outbound EDC bytes without captured-client evidence.
	if finishedHandshake && s.EncodingOptions.Encryption && packet.MessageID != network.MassivePacketID {
		packet.Encrypted = true
	}
	return packet.MarshalWire(cipher)
}

func (s *Session) inboundCipher() *blowfish.Cipher {
	s.securityMu.RLock()
	defer s.securityMu.RUnlock()
	return s.Cipher
}

func sessionTraceEnabled() bool {
	return os.Getenv("SRO_SESSION_TRACE") == "1"
}

func (s *Session) writeEncryptionSetup(p *network.Packet) error {
	if s.EncodingOptions.Encryption {
		cipher, err := blowfish.NewCipher(utils2.Uint64ToByteArray(s.Context.InitialBlowfishKey))
		if err != nil {
			return err
		}
		s.Cipher = cipher
		if err := p.WriteUInt64(s.Context.InitialBlowfishKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) writeEDC(p *network.Packet) error {
	if s.EncodingOptions.EDC {
		sequenceSeed := secureRandomBytes(1)[0]
		s.Sequence = network.NewMessageSequence(uint32(sequenceSeed))
		if err := p.WriteUInt32(uint32(sequenceSeed)); err != nil {
			return err
		}
		crc := secureRandomBytes(1)[0]
		s.Crc = network.NewMessageCRC(uint32(crc))
		if err := p.WriteUInt32(uint32(crc)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) setupKeyExchange(p *network.Packet) error {
	if s.KeyExchange {
		if err := p.WriteUInt64(s.Context.HandshakeBlowfishKey); err != nil {
			return err
		}
		if err := p.WriteUInt32(s.Context.Generator); err != nil {
			return err
		}
		if err := p.WriteUInt32(s.Context.Prime); err != nil {
			return err
		}
		if err := p.WriteUInt32(s.Context.LocalPublic); err != nil {
			return err
		}
	}
	return nil
}
