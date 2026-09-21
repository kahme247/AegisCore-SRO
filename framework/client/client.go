package client

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/security/blowfish"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultClientDialTimeout      = 10 * time.Second
	DefaultClientHandshakeTimeout = 15 * time.Second
	DefaultClientWriteTimeout     = 5 * time.Second
	privateKeyReadAttempts        = 32
)

var (
	ErrClientClosed            = errors.New("client is closed")
	ErrClientHandshakeTimeout  = errors.New("client handshake timed out")
	ErrClientGenerationStale   = errors.New("client connection generation is no longer active")
	ErrClientPrivateKey        = errors.New("unable to generate a private key")
	ErrClientHandshakeProtocol = errors.New("unexpected packet during client handshake")
)

type clientDialContext func(context.Context, string, string) (net.Conn, error)
type clientRandomBytes func([]byte) (int, error)

// Client is a reconnecting vSRO client. One lifecycle owns a single outbound
// writer and at most one active TCP generation. A generation owns its socket
// and reader; it cannot affect a newer connection after it has been cancelled.
type Client struct {
	host                      string
	port                      int
	Conn                      net.Conn
	Ctx                       SecurityContext
	EncodingOptions           network.EncodingOptions
	Cipher                    *blowfish.Cipher
	Sequence                  network.MessageSequence
	Crc                       network.MessageCRC
	ModuleID                  string
	PacketHandlers            map[uint16]PacketHandler
	IncomingPacketChannel     chan network.Packet
	OutgoingPacketChannel     chan network.Packet
	ConnectionClosed          chan bool
	ConnectionEstablished     chan bool
	Reconnected               chan bool
	mutex                     *sync.Mutex
	securityMu                sync.RWMutex
	massiveAssembler          *network.MassiveAssembler
	AutoReconnect             bool
	reconnectCount            int
	MaxReconnectAttempts      int
	HandlingOutgoingPackets   bool
	ReconnectTimeoutInSeconds time.Duration
	DialTimeout               time.Duration
	HandshakeTimeout          time.Duration

	dialContext   clientDialContext
	randomBytes   clientRandomBytes
	privateKeyErr error

	lifecycleCtx     context.Context
	lifecycleCancel  context.CancelFunc
	lifecycleDone    chan struct{}
	lifecycleRunning bool
	lifecycleStarted bool
	closeNotified    bool
	firstReady       chan struct{}
	firstReadyClosed bool
	senderDone       chan struct{}
	senderJobs       chan clientOutboundJob

	generation        *clientGeneration
	generationChanged chan struct{}
	generationID      uint64
	everConnected     bool
}

type clientOutboundJob struct {
	packet     network.Packet
	generation *clientGeneration
	result     chan error
}

func NewClient(host string, port int, moduleId string) *Client {
	c := Client{
		host:                      host,
		port:                      port,
		Ctx:                       SecurityContext{},
		EncodingOptions:           network.EncodingOptions{},
		Sequence:                  network.NewMessageSequence(0),
		Crc:                       network.NewMessageCRC(0),
		ModuleID:                  moduleId,
		PacketHandlers:            make(map[uint16]PacketHandler),
		IncomingPacketChannel:     make(chan network.Packet, 8),
		OutgoingPacketChannel:     make(chan network.Packet, 8),
		mutex:                     &sync.Mutex{},
		MaxReconnectAttempts:      100,
		ConnectionEstablished:     make(chan bool, 1),
		Reconnected:               make(chan bool, 1),
		ConnectionClosed:          make(chan bool, 1),
		ReconnectTimeoutInSeconds: 10,
		DialTimeout:               DefaultClientDialTimeout,
		HandshakeTimeout:          DefaultClientHandshakeTimeout,
		dialContext:               defaultClientDialContext,
		randomBytes:               cryptorand.Read,
		generationChanged:         make(chan struct{}),
	}
	c.Ctx.Private, c.privateKeyErr = c.newPrivateKey(0)
	c.PacketHandlers[0x5000] = &KeyExchangeHandler{client: &c}
	c.PacketHandlers[0x2001] = &ModuleIdentificationHandler{client: &c}
	return &c
}

func defaultClientDialContext(ctx context.Context, networkName, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, networkName, address)
}

// Connect preserves the legacy synchronous API: it returns once the first
// generation has completed module identification, or once a terminal failure
// or Close ends the lifecycle. Reconnect attempts are managed by one loop;
// Connect never calls Reconnect recursively.
func (c *Client) Connect() {
	ready, done := c.ensureLifecycle()
	select {
	case <-ready:
	case <-done:
	}
}

// Reconnect requests replacement of the active generation. Automatic retry
// policy remains controlled by AutoReconnect and MaxReconnectAttempts.
func (c *Client) Reconnect() {
	c.mutex.Lock()
	running := c.lifecycleRunning
	autoReconnect := c.AutoReconnect
	generation := c.generation
	c.mutex.Unlock()

	if !running {
		if autoReconnect {
			c.Connect()
		} else {
			c.notifyConnectionClosed()
		}
		return
	}
	if !autoReconnect {
		c.Close()
		return
	}
	if generation != nil {
		generation.stop(errors.New("client reconnect requested"))
	}
}

// Close cancels pending dials, closes the active socket, and stops the owned
// sender. It is idempotent and does not wait for user-provided packet handlers.
func (c *Client) Close() {
	c.mutex.Lock()
	cancel := c.lifecycleCancel
	generation := c.generation
	c.mutex.Unlock()

	if generation != nil {
		generation.stop(ErrClientClosed)
	}
	if cancel != nil {
		cancel()
	}
}

// Done closes after the active client lifecycle and its sender have stopped.
// A nil result means Connect has not yet started a lifecycle.
func (c *Client) Done() <-chan struct{} {
	c.mutex.Lock()
	done := c.lifecycleDone
	c.mutex.Unlock()
	return done
}

// Wait is useful to deterministic shutdown tests and callers that need to
// wait until the client-owned network goroutines have exited.
func (c *Client) Wait() {
	if done := c.Done(); done != nil {
		<-done
	}
}

func (c *Client) IsConnected() bool {
	c.mutex.Lock()
	generation := c.generation
	connected := c.lifecycleRunning && generation != nil && generation.isReady()
	c.mutex.Unlock()
	return connected
}

func (c *Client) ensureLifecycle() (<-chan struct{}, <-chan struct{}) {
	c.mutex.Lock()
	if !c.lifecycleRunning {
		c.startLifecycleLocked()
	}
	ready := c.firstReady
	done := c.lifecycleDone
	c.mutex.Unlock()
	return ready, done
}

func (c *Client) startLifecycleLocked() {
	needsFreshPrivateKey := c.lifecycleStarted
	c.lifecycleStarted = true
	ctx, cancel := context.WithCancel(context.Background())
	c.lifecycleCtx = ctx
	c.lifecycleCancel = cancel
	c.lifecycleDone = make(chan struct{})
	c.lifecycleRunning = true
	c.closeNotified = false
	c.firstReady = make(chan struct{})
	c.firstReadyClosed = false
	c.senderDone = make(chan struct{})
	c.senderJobs = make(chan clientOutboundJob, 32)
	c.everConnected = false
	c.HandlingOutgoingPackets = false
	c.reconnectCount = 0

	go c.outboundLoop(ctx, c.senderJobs, c.senderDone)
	go c.runLifecycle(ctx, c.lifecycleDone, c.senderDone, needsFreshPrivateKey)
}

func (c *Client) runLifecycle(ctx context.Context, done chan struct{}, senderDone chan struct{}, needsFreshPrivateKey bool) {
	defer func() {
		c.finishLifecycle(ctx, done, senderDone)
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		if needsFreshPrivateKey {
			if err := c.resetSecurityForReconnect(); err != nil {
				log.Errorf("cannot reset client security state: %v", err)
				c.notifyConnectionClosed()
				return
			}
		}
		if err := c.initialPrivateKeyError(); err != nil {
			log.Errorf("cannot start client without a private key: %v", err)
			c.notifyConnectionClosed()
			return
		}

		conn, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Debugf("failed to connect: %v", err)
			if !c.beginReconnectAttempt() {
				c.notifyConnectionClosed()
				return
			}
			needsFreshPrivateKey = true
			if !c.waitReconnect(ctx) {
				return
			}
			continue
		}

		generation := newClientGeneration(c.nextGenerationID(), conn)
		c.installGeneration(generation)
		log.Debugf("Connected to %v", conn.RemoteAddr())
		go c.readerLoop(generation)

		c.waitForGeneration(ctx, generation)
		generation.waitForWriter()
		c.clearGeneration(generation)
		c.notifyConnectionClosed()

		if ctx.Err() != nil {
			return
		}
		if !c.beginReconnectAttempt() {
			return
		}
		needsFreshPrivateKey = true
		if !c.waitReconnect(ctx) {
			return
		}
	}
}

func (c *Client) finishLifecycle(ctx context.Context, done chan struct{}, senderDone chan struct{}) {
	c.mutex.Lock()
	if c.lifecycleCtx != ctx {
		c.mutex.Unlock()
		return
	}
	cancel := c.lifecycleCancel
	generation := c.generation
	c.mutex.Unlock()

	if generation != nil {
		generation.stop(ErrClientClosed)
	}
	if cancel != nil {
		cancel()
	}
	<-senderDone

	shouldNotify := false
	c.mutex.Lock()
	if c.lifecycleCtx == ctx {
		c.lifecycleRunning = false
		c.lifecycleCtx = nil
		c.lifecycleCancel = nil
		c.generation = nil
		c.Conn = nil
		c.massiveAssembler = nil
		c.HandlingOutgoingPackets = false
		c.replaceGenerationChangedLocked()
		if !c.closeNotified {
			c.closeNotified = true
			shouldNotify = true
		}
		close(done)
	}
	c.mutex.Unlock()
	if shouldNotify {
		c.publishConnectionClosed()
	}
}

func (c *Client) markConnectionEstablished() {
	generation, _ := c.activeGeneration()
	if generation == nil || generation.ctx.Err() != nil {
		return
	}
	generation.readyOnce.Do(func() {
		close(generation.ready)
		select {
		case c.ConnectionEstablished <- true:
		default:
		}
	})
}

func (c *Client) markGenerationReady(generation *clientGeneration) {
	c.mutex.Lock()
	if c.generation != generation || generation.ctx.Err() != nil {
		c.mutex.Unlock()
		return
	}
	wasReconnected := c.everConnected
	c.everConnected = true
	c.reconnectCount = 0
	c.HandlingOutgoingPackets = true
	if !c.firstReadyClosed {
		close(c.firstReady)
		c.firstReadyClosed = true
	}
	c.mutex.Unlock()

	if wasReconnected {
		c.notifyReconnected()
	}
	log.Debugf("Connection established")
}

func (c *Client) outboundLoop(ctx context.Context, jobs <-chan clientOutboundJob, done chan<- struct{}) {
	defer close(done)
	for {
		generation, changed := c.activeGeneration()
		if generation == nil {
			select {
			case job := <-jobs:
				c.finishOutboundJob(job, ErrClientGenerationStale)
			case <-changed:
			case <-ctx.Done():
				return
			}
			continue
		}

		if generation.ctx.Err() != nil {
			select {
			case job := <-jobs:
				c.finishOutboundJob(job, ErrClientGenerationStale)
			case <-changed:
			case <-ctx.Done():
				return
			}
			continue
		}

		if !generation.isReady() {
			select {
			case job := <-jobs:
				c.writeControlJob(job)
			case <-generation.ready:
			case <-changed:
			case <-ctx.Done():
				return
			}
			continue
		}

		select {
		case job := <-jobs:
			c.writeControlJob(job)
		case packet := <-c.OutgoingPacketChannel:
			clone, err := cloneClientOutboundPacket(packet)
			if err != nil {
				log.Errorf("dropping invalid outbound packet: %v", err)
				continue
			}
			if err := c.writePacketForGeneration(generation, clone); err != nil {
				if !errors.Is(err, ErrClientGenerationStale) && !errors.Is(err, ErrClientClosed) {
					log.Errorf("failed to send packet 0x%04X: %v", clone.MessageID, err)
				}
			}
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) writeControlJob(job clientOutboundJob) {
	if job.generation == nil || !c.isActiveGeneration(job.generation) {
		c.finishOutboundJob(job, ErrClientGenerationStale)
		return
	}
	c.finishOutboundJob(job, c.writePacketForGeneration(job.generation, job.packet))
}

func (c *Client) finishOutboundJob(job clientOutboundJob, err error) {
	if job.result == nil {
		return
	}
	select {
	case job.result <- err:
	default:
	}
}

func (c *Client) sendPacket(packet network.Packet) error {
	clone, err := cloneClientOutboundPacket(packet)
	if err != nil {
		return err
	}
	c.mutex.Lock()
	generation := c.generation
	jobs := c.senderJobs
	ctx := c.lifecycleCtx
	running := c.lifecycleRunning
	c.mutex.Unlock()
	if !running || generation == nil || jobs == nil || ctx == nil {
		return ErrClientGenerationStale
	}

	result := make(chan error, 1)
	job := clientOutboundJob{packet: clone, generation: generation, result: result}
	select {
	case jobs <- job:
	case <-generation.ctx.Done():
		return ErrClientGenerationStale
	case <-ctx.Done():
		return ErrClientClosed
	}
	select {
	case err := <-result:
		return err
	case <-generation.ctx.Done():
		return ErrClientGenerationStale
	case <-ctx.Done():
		return ErrClientClosed
	}
}

func (c *Client) writePacketForGeneration(generation *clientGeneration, packet network.Packet) error {
	generation.writerMu.Lock()
	defer generation.writerMu.Unlock()
	if generation.ctx.Err() != nil {
		return ErrClientClosed
	}
	if !c.isActiveGeneration(generation) {
		return ErrClientGenerationStale
	}

	frame, err := c.marshalClientPacket(&packet)
	if err != nil {
		return err
	}
	if err := writeClientFrame(generation.conn, frame); err != nil {
		if generation.ctx.Err() == nil {
			generation.stop(err)
		}
		return err
	}
	return nil
}

func (c *Client) marshalClientPacket(packet *network.Packet) ([]byte, error) {
	c.securityMu.RLock()
	defer c.securityMu.RUnlock()

	packet.ServerPacket = false
	if c.Ctx.HandshakeCompleted && c.EncodingOptions.Encryption {
		packet.Encrypted = true
	}
	if err := packet.WriteMessageID(); err != nil {
		return nil, err
	}
	if err := packet.WriteSize(); err != nil {
		return nil, err
	}

	packet.Sequence = c.Sequence.Next()
	packet.IsSequenceInitialized = true
	if err := packet.WriteSequence(); err != nil {
		return nil, err
	}
	packet.CRC = c.Crc.Compute(packet.Buffer[:packet.DataSize+int(network.HeaderSize)])
	if err := packet.WriteCRC(); err != nil {
		return nil, err
	}
	return packet.MarshalWire(c.Cipher)
}

func cloneClientOutboundPacket(source network.Packet) (network.Packet, error) {
	if err := source.WriteError(); err != nil {
		return network.Packet{}, err
	}
	clone := network.EmptyClientPacket()
	clone.MessageID = source.MessageID
	clone.Encrypted = source.Encrypted
	clone.Sequence = source.Sequence
	clone.CRC = source.CRC
	clone.IsSequenceInitialized = source.IsSequenceInitialized
	if err := clone.WriteBytes(source.Payload()); err != nil {
		return network.Packet{}, err
	}
	return clone, nil
}

func writeClientFrame(conn net.Conn, frame []byte) error {
	if conn == nil {
		return ErrClientGenerationStale
	}
	if err := conn.SetWriteDeadline(time.Now().Add(DefaultClientWriteTimeout)); err != nil {
		return err
	}
	err := writeAllClient(conn, frame)
	clearErr := conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return err
	}
	return clearErr
}

func writeAllClient(writer io.Writer, bytes []byte) error {
	for len(bytes) > 0 {
		count, err := writer.Write(bytes)
		if count < 0 || count > len(bytes) {
			return io.ErrShortWrite
		}
		bytes = bytes[count:]
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// resetSecurityForReconnect removes all per-connection handshake state and
// obtains a fresh private exponent. Randomness failure is terminal: predictable
// fallback material is never used for a vSRO key exchange.
func (c *Client) resetSecurityForReconnect() error {
	c.securityMu.RLock()
	previousPrivate := c.Ctx.Private
	c.securityMu.RUnlock()
	private, err := c.newPrivateKey(previousPrivate)
	if err != nil {
		return err
	}

	c.securityMu.Lock()
	c.Ctx = SecurityContext{}
	c.Ctx.Private = private
	c.Cipher = nil
	c.EncodingOptions = network.EncodingOptions{}
	c.Sequence = network.NewMessageSequence(0)
	c.Crc = network.NewMessageCRC(0)
	c.securityMu.Unlock()

	c.mutex.Lock()
	c.privateKeyErr = nil
	c.mutex.Unlock()
	return nil
}

func (c *Client) newPrivateKey(exclude uint32) (uint32, error) {
	for attempt := 0; attempt < privateKeyReadAttempts; attempt++ {
		var bytes [4]byte
		n, err := c.randomBytes(bytes[:])
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrClientPrivateKey, err)
		}
		if n != len(bytes) {
			return 0, fmt.Errorf("%w: short CSPRNG read: got %d bytes", ErrClientPrivateKey, n)
		}
		private := binary.LittleEndian.Uint32(bytes[:])
		if private != 0 && private != exclude {
			return private, nil
		}
	}
	return 0, fmt.Errorf("%w: CSPRNG repeatedly returned an unusable exponent", ErrClientPrivateKey)
}

func (c *Client) initialPrivateKeyError() error {
	c.mutex.Lock()
	err := c.privateKeyErr
	c.mutex.Unlock()
	return err
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	c.mutex.Lock()
	dialContext := c.dialContext
	host := c.host
	port := c.port
	timeout := c.DialTimeout
	c.mutex.Unlock()
	if timeout <= 0 {
		timeout = DefaultClientDialTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return dialContext(dialCtx, "tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
}

func (c *Client) handshakeTimeout() time.Duration {
	c.mutex.Lock()
	timeout := c.HandshakeTimeout
	c.mutex.Unlock()
	if timeout <= 0 {
		return DefaultClientHandshakeTimeout
	}
	return timeout
}

func (c *Client) beginReconnectAttempt() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if !c.AutoReconnect || c.reconnectCount >= c.MaxReconnectAttempts {
		return false
	}
	c.reconnectCount++
	log.Infof("Reconnecting... Attempt [%v/%v]", c.reconnectCount, c.MaxReconnectAttempts)
	return true
}

func (c *Client) waitReconnect(ctx context.Context) bool {
	c.mutex.Lock()
	delaySeconds := c.ReconnectTimeoutInSeconds
	c.mutex.Unlock()
	if delaySeconds <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(time.Second * delaySeconds)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (c *Client) nextGenerationID() uint64 {
	c.mutex.Lock()
	c.generationID++
	id := c.generationID
	c.mutex.Unlock()
	return id
}

func (c *Client) installGeneration(generation *clientGeneration) {
	c.mutex.Lock()
	c.generation = generation
	c.Conn = generation.conn
	c.massiveAssembler = generation.massiveAssembler
	c.HandlingOutgoingPackets = false
	c.closeNotified = false
	c.replaceGenerationChangedLocked()
	c.mutex.Unlock()
}

func (c *Client) clearGeneration(generation *clientGeneration) {
	c.mutex.Lock()
	if c.generation == generation {
		c.generation = nil
		c.Conn = nil
		c.massiveAssembler = nil
		c.HandlingOutgoingPackets = false
		c.replaceGenerationChangedLocked()
	}
	c.mutex.Unlock()
}

func (c *Client) activeGeneration() (*clientGeneration, <-chan struct{}) {
	c.mutex.Lock()
	generation := c.generation
	changed := c.generationChanged
	c.mutex.Unlock()
	return generation, changed
}

func (c *Client) isActiveGeneration(generation *clientGeneration) bool {
	c.mutex.Lock()
	active := c.lifecycleRunning && c.generation == generation
	c.mutex.Unlock()
	return active
}

func (c *Client) replaceGenerationChangedLocked() {
	close(c.generationChanged)
	c.generationChanged = make(chan struct{})
}

func (c *Client) failActiveGeneration(err error) {
	if err == nil {
		err = errors.New("client protocol failure")
	}
	generation, _ := c.activeGeneration()
	if generation != nil {
		generation.stop(err)
	}
}

// closeConnection is retained for source compatibility with the old internal
// lifecycle helper. It now follows the same cancellable Close path.
func (c *Client) closeConnection() {
	c.Close()
}

func (c *Client) notifyConnectionClosed() {
	c.mutex.Lock()
	if c.closeNotified {
		c.mutex.Unlock()
		return
	}
	c.closeNotified = true
	c.mutex.Unlock()
	c.publishConnectionClosed()
}

func (c *Client) publishConnectionClosed() {
	select {
	case c.ConnectionClosed <- true:
	default:
	}
}

func (c *Client) notifyReconnected() {
	select {
	case c.Reconnected <- true:
	default:
	}
}
