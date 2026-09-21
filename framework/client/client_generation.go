package client

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	log "github.com/sirupsen/logrus"
)

type clientGeneration struct {
	id               uint64
	conn             net.Conn
	ctx              context.Context
	cancel           context.CancelFunc
	ready            chan struct{}
	readyOnce        sync.Once
	done             chan struct{}
	doneOnce         sync.Once
	massiveAssembler *network.MassiveAssembler
	writerMu         sync.Mutex
	errMu            sync.Mutex
	err              error
}

func newClientGeneration(id uint64, conn net.Conn) *clientGeneration {
	ctx, cancel := context.WithCancel(context.Background())
	return &clientGeneration{
		id:               id,
		conn:             conn,
		ctx:              ctx,
		cancel:           cancel,
		ready:            make(chan struct{}),
		done:             make(chan struct{}),
		massiveAssembler: network.NewMassiveAssembler(network.NewDefaultMassiveCodec()),
	}
}

func (g *clientGeneration) stop(err error) {
	g.errMu.Lock()
	if g.err == nil && err != nil {
		g.err = err
	}
	g.errMu.Unlock()
	g.cancel()
	if g.conn != nil {
		_ = g.conn.Close()
	}
}

func (g *clientGeneration) finish() {
	g.doneOnce.Do(func() {
		close(g.done)
	})
}

func (g *clientGeneration) waitForWriter() {
	g.writerMu.Lock()
	g.writerMu.Unlock()
}

func (g *clientGeneration) isReady() bool {
	select {
	case <-g.ready:
		return true
	default:
		return false
	}
}

func (c *Client) waitForGeneration(ctx context.Context, generation *clientGeneration) {
	timeout := c.handshakeTimeout()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-generation.ready:
		c.markGenerationReady(generation)
		select {
		case <-generation.done:
		case <-ctx.Done():
			generation.stop(ErrClientClosed)
			<-generation.done
		}
	case <-generation.done:
	case <-timer.C:
		generation.stop(ErrClientHandshakeTimeout)
		<-generation.done
	case <-ctx.Done():
		generation.stop(ErrClientClosed)
		<-generation.done
	}
}

func (c *Client) readerLoop(generation *clientGeneration) {
	defer generation.finish()
	reader := network.NewPacketReader(generation.conn)
	for {
		if err := generation.refreshReadDeadline(); err != nil {
			if generation.ctx.Err() == nil {
				generation.stop(err)
			}
			return
		}
		packets, err := reader.ReadPackets()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if massiveErr := generation.expireMassiveAssembly(time.Now()); massiveErr != nil {
					err = massiveErr
				}
			}
			if generation.ctx.Err() == nil {
				log.Debugf("connection generation %d closed: %v", generation.id, err)
				generation.stop(err)
			}
			return
		}
		for _, packet := range packets {
			if generation.ctx.Err() != nil || !c.isActiveGeneration(generation) {
				return
			}
			if !c.handleIncomingPacketForGeneration(generation, packet) {
				return
			}
		}
	}
}

// refreshReadDeadline leaves normal client reads unbounded, preserving the
// legacy no-idle-timeout behavior. Once a massive header/body sequence is
// active, it caps the next blocking read at that sequence's expiry so a silent
// peer cannot retain the aggregate indefinitely.
func (g *clientGeneration) refreshReadDeadline() error {
	if g.conn == nil {
		return nil
	}
	deadline := time.Time{}
	if g.massiveAssembler != nil {
		if massiveDeadline, active := g.massiveAssembler.Deadline(); active {
			deadline = massiveDeadline
		}
	}
	return g.conn.SetReadDeadline(deadline)
}

func (g *clientGeneration) expireMassiveAssembly(now time.Time) error {
	if g.massiveAssembler == nil {
		return nil
	}
	return g.massiveAssembler.ExpireAt(now)
}

func (c *Client) handleIncomingPacketForGeneration(generation *clientGeneration, packet network.Packet) bool {
	c.securityMu.RLock()
	cipher := c.Cipher
	c.securityMu.RUnlock()
	packetDec, err := packet.DecryptChecked(cipher)
	if err != nil {
		log.Errorf("failed to decrypt incoming packet: %v", err)
		generation.stop(err)
		return false
	}
	// Process every physical frame so a normal packet cannot interleave with a
	// retained massive header/body sequence, and timeouts are checked even when
	// no further 0x600D wrapper arrives.
	complete, done, massiveErr := generation.massiveAssembler.ProcessFrame(packetDec)
	if massiveErr != nil {
		log.Errorf("rejecting malformed massive packet: %v", massiveErr)
		generation.stop(massiveErr)
		return false
	}
	if !done {
		return generation.ctx.Err() == nil
	}
	packetDec = complete
	if err := c.validateInboundHandshakePacket(generation, packetDec.MessageID); err != nil {
		log.Errorf("rejecting packet 0x%04X: %v", packetDec.MessageID, err)
		generation.stop(err)
		return false
	}
	c.dispatchIncomingPacketForGeneration(generation, packetDec)
	return generation.ctx.Err() == nil
}

// validateInboundHandshakePacket keeps the client fail-closed until the
// server has completed the expected setup sequence. KeyExchangeHandler owns
// the finer 0x5000 phase checks; this gate prevents unrelated application
// packets, premature module responses, and duplicate setup packets from
// reaching normal dispatch.
func (c *Client) validateInboundHandshakePacket(generation *clientGeneration, messageID uint16) error {
	c.securityMu.RLock()
	handshakeCompleted := c.Ctx.HandshakeCompleted
	c.securityMu.RUnlock()
	if !handshakeCompleted {
		if messageID != 0x5000 {
			return fmt.Errorf("%w: received 0x%04X before key exchange completed", ErrClientHandshakeProtocol, messageID)
		}
		return nil
	}

	if !generation.isReady() {
		if messageID != 0x2001 {
			return fmt.Errorf("%w: received 0x%04X before module identification", ErrClientHandshakeProtocol, messageID)
		}
		return nil
	}

	if messageID == 0x5000 || messageID == 0x2001 {
		return fmt.Errorf("%w: received duplicate setup packet 0x%04X", ErrClientHandshakeProtocol, messageID)
	}
	return nil
}

func (c *Client) dispatchIncomingPacketForGeneration(generation *clientGeneration, packet network.Packet) {
	handler := c.PacketHandlers[packet.MessageID]
	if handler != nil {
		handler.Handle(packet)
		return
	}
	select {
	case c.IncomingPacketChannel <- packet:
	case <-generation.ctx.Done():
	}
}

func (c *Client) dispatchIncomingPacket(packet network.Packet) {
	generation, _ := c.activeGeneration()
	if generation == nil {
		return
	}
	c.dispatchIncomingPacketForGeneration(generation, packet)
}
