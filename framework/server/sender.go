package server

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

const DefaultWriteTimeout = 5 * time.Second

var (
	ErrSenderClosed  = errors.New("session sender is closed")
	ErrNilSenderConn = errors.New("session sender has no connection")
)

// deadlineWriter is intentionally smaller than net.Conn so Sender can be
// tested with deterministic short writers while still enforcing deadlines for
// real sockets.
type deadlineWriter interface {
	io.Writer
	SetWriteDeadline(time.Time) error
}

type packetEncoder func(network.Packet) ([]byte, error)

// Sender serializes all outbound writes for one session. A job owns either one
// frame or an entire massive frame batch, so concurrent handlers cannot
// interleave the header and body packets of a logical message.
type Sender struct {
	conn         deadlineWriter
	encode       packetEncoder
	massiveCodec network.MassiveCodec
	writeTimeout time.Duration

	queue     chan outboundJob
	done      chan struct{}
	closeOnce sync.Once
	workerWG  sync.WaitGroup
}

type outboundJob struct {
	frames []outboundFrame
	result chan error
}

type outboundFrame struct {
	packet *network.Packet
	raw    []byte
}

func NewSender(conn deadlineWriter, encode packetEncoder) *Sender {
	return NewSenderWithOptions(conn, encode, network.NewDefaultMassiveCodec(), DefaultWriteTimeout)
}

func NewSenderWithOptions(conn deadlineWriter, encode packetEncoder, massiveCodec network.MassiveCodec, writeTimeout time.Duration) *Sender {
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	sender := &Sender{
		conn:         conn,
		encode:       encode,
		massiveCodec: massiveCodec,
		writeTimeout: writeTimeout,
		queue:        make(chan outboundJob, 128),
		done:         make(chan struct{}),
	}
	sender.workerWG.Add(1)
	go func() {
		defer sender.workerWG.Done()
		sender.run()
	}()
	return sender
}

// Send queues one logical packet and waits until it has been fully written or
// failed. The packet is cloned before queueing, while final encoding happens
// inside the single writer goroutine so per-session EDC and encryption state
// can be applied in exactly wire order.
func (s *Sender) Send(packet network.Packet) error {
	clone, err := cloneOutboundPacket(packet)
	if err != nil {
		return err
	}
	return s.enqueue([]outboundFrame{{packet: &clone}})
}

// SendBatch queues ordinary packets as one indivisible writer job. It is used
// for protocol sequences whose framing state must not be interleaved, such as
// group-spawn begin/data/end.
func (s *Sender) SendBatch(packets []network.Packet) error {
	if len(packets) == 0 {
		return nil
	}
	frames := make([]outboundFrame, 0, len(packets))
	for _, packet := range packets {
		clone, err := cloneOutboundPacket(packet)
		if err != nil {
			return err
		}
		frames = append(frames, outboundFrame{packet: &clone})
	}
	return s.enqueue(frames)
}

// SendMassive encodes all 0x600D header/body frames before queueing them as one
// job. It therefore preserves physical frame order even with concurrent sends.
func (s *Sender) SendMassive(packet network.Packet) error {
	frames, err := s.massiveCodec.EncodeMassive(packet)
	if err != nil {
		return err
	}
	outboundFrames := make([]outboundFrame, 0, len(frames))
	for _, frame := range frames {
		clone, err := cloneOutboundPacket(frame)
		if err != nil {
			return err
		}
		outboundFrames = append(outboundFrames, outboundFrame{packet: &clone})
	}
	return s.enqueue(outboundFrames)
}

// SendBytes is the transitional escape hatch for documented raw protocol
// responses. It still gains the sender's serialization, deadline, and full
// write behavior. New handlers should prefer Send or SendMassive.
func (s *Sender) SendBytes(bytes []byte) error {
	if len(bytes) == 0 {
		return nil
	}
	frame := append([]byte(nil), bytes...)
	return s.enqueue([]outboundFrame{{raw: frame}})
}

func (s *Sender) encodePacket(packet network.Packet) ([]byte, error) {
	if s.encode == nil {
		return nil, errors.New("session sender has no packet encoder")
	}
	frame, err := s.encode(packet)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

func (s *Sender) enqueue(frames []outboundFrame) error {
	if s.conn == nil {
		return ErrNilSenderConn
	}
	job := outboundJob{frames: frames, result: make(chan error, 1)}
	select {
	case <-s.done:
		return ErrSenderClosed
	case s.queue <- job:
	}

	select {
	case <-s.done:
		return ErrSenderClosed
	case err := <-job.result:
		return err
	}
}

func (s *Sender) run() {
	for {
		select {
		case <-s.done:
			return
		case job := <-s.queue:
			err := s.writeJob(job.frames)
			select {
			case job.result <- err:
			case <-s.done:
			}
		}
	}
}

func (s *Sender) writeJob(frames []outboundFrame) error {
	for _, frame := range frames {
		packet := frame.raw
		if frame.packet != nil {
			var err error
			packet, err = s.encodePacket(*frame.packet)
			if err != nil {
				return err
			}
		}
		if len(packet) == 0 {
			continue
		}
		if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
			return err
		}
		err := writeAll(s.conn, packet)
		clearErr := s.conn.SetWriteDeadline(time.Time{})
		if err != nil {
			return err
		}
		if clearErr != nil {
			return clearErr
		}
	}
	return nil
}

func cloneOutboundPacket(source network.Packet) (network.Packet, error) {
	if err := source.WriteError(); err != nil {
		return network.Packet{}, err
	}
	var clone network.Packet
	if source.ServerPacket {
		clone = network.EmptyPacket()
	} else {
		clone = network.EmptyClientPacket()
	}
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

func writeAll(writer io.Writer, bytes []byte) error {
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

func (s *Sender) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}

// Wait blocks until the sender goroutine has stopped. Session uses it after
// closing its socket; tests and controlled shutdown paths can use it to prove
// that no writer goroutine remains retained.
func (s *Sender) Wait() {
	s.workerWG.Wait()
}
