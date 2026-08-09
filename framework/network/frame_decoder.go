package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Harvi8/AegisCore/framework/security/blowfish"
)

const (
	// DefaultMaxFramePayload is the largest logical payload accepted by a
	// default decoder: the 15 data bits available in the packet length field.
	// Applications that need a smaller admission limit can use
	// NewFrameDecoderWithMaxPayload.
	DefaultMaxFramePayload = 0x7FFF
)

var (
	// ErrFrameTooLarge reports a packet whose declared logical payload exceeds
	// the decoder's configured maximum.
	ErrFrameTooLarge = errors.New("network frame payload exceeds configured maximum")

	// ErrFrameDecoderClosed reports an attempt to feed a decoder after a clean
	// Finish or Close.
	ErrFrameDecoderClosed = errors.New("network frame decoder is closed")

	// ErrTruncatedFrame reports EOF while a packet header or body is pending.
	// It is returned together with io.ErrUnexpectedEOF from Finish and Close.
	ErrTruncatedFrame = errors.New("network frame truncated at end of stream")

	// ErrInvalidFramePayloadLimit reports a payload limit outside the 15-bit
	// packet length field's range.
	ErrInvalidFramePayloadLimit = errors.New("network frame payload limit is invalid")
)

// FrameDecoder incrementally turns a TCP byte stream into complete packet wire
// frames. It retains an incomplete header or body between Push calls. Each
// emitted frame has independent backing storage and is safe to retain or
// mutate after Push returns.
//
// Packet wire framing always has a logical HeaderSize-byte header. For an
// encrypted packet, only the two-byte length field is unencrypted; the rest of
// the wire frame is Blowfish-padded. The length field's high bit is the
// encryption marker, not part of the declared payload size.
type FrameDecoder struct {
	mu sync.Mutex

	maxPayload  int
	pending     []byte
	closed      bool
	terminalErr error
}

// NewFrameDecoder creates a decoder with DefaultMaxFramePayload (32,767
// logical payload bytes). The zero value of FrameDecoder uses the same limit.
func NewFrameDecoder() *FrameDecoder {
	return &FrameDecoder{maxPayload: DefaultMaxFramePayload}
}

// NewFrameDecoderWithMaxPayload creates a decoder with a smaller or equal
// logical payload limit. A positive limit is required because zero-length
// packets remain valid even when the configured maximum is one byte.
func NewFrameDecoderWithMaxPayload(maxPayload int) (*FrameDecoder, error) {
	if maxPayload <= 0 || maxPayload > DefaultMaxFramePayload {
		return nil, fmt.Errorf("%w: got %d, want 1..%d", ErrInvalidFramePayloadLimit, maxPayload, DefaultMaxFramePayload)
	}

	return &FrameDecoder{maxPayload: maxPayload}, nil
}

// Push appends TCP bytes and returns every complete wire frame now available,
// in stream order. Partial frames remain pending until a later Push. An
// oversized declaration is terminal because continuing after it would lose
// frame alignment.
func (d *FrameDecoder) Push(data []byte) ([][]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		if d.terminalErr != nil {
			return nil, d.terminalErr
		}
		return nil, ErrFrameDecoderClosed
	}

	if len(data) == 0 {
		return nil, nil
	}

	d.pending = append(d.pending, data...)
	return d.decodeLocked()
}

// Feed is an alias for Push for callers that describe stream input as feeds.
func (d *FrameDecoder) Feed(data []byte) ([][]byte, error) {
	return d.Push(data)
}

// Finish marks the input stream as complete. It returns nil if no bytes are
// pending, or an error matching both ErrTruncatedFrame and io.ErrUnexpectedEOF
// when EOF arrives mid-frame. Finish is idempotent.
func (d *FrameDecoder) Finish() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return d.terminalErr
	}

	d.closed = true
	if len(d.pending) == 0 {
		return nil
	}

	d.terminalErr = fmt.Errorf("%w: %w (%d pending byte(s))", ErrTruncatedFrame, io.ErrUnexpectedEOF, len(d.pending))
	d.pending = nil
	return d.terminalErr
}

// Close is an io.Closer-compatible alias for Finish.
func (d *FrameDecoder) Close() error {
	return d.Finish()
}

func (d *FrameDecoder) decodeLocked() ([][]byte, error) {
	frames := make([][]byte, 0)
	offset := 0

	for len(d.pending)-offset >= int(HeaderSize) {
		frameStart := d.pending[offset:]
		rawSize := binary.LittleEndian.Uint16(frameStart[:2])
		payloadSize := int(rawSize &^ EncryptMask)
		maxPayload := d.payloadLimit()
		if payloadSize > maxPayload {
			return d.failLocked(frames, fmt.Errorf("%w: declared %d bytes, limit %d", ErrFrameTooLarge, payloadSize, maxPayload))
		}

		frameSize := int(HeaderSize) + payloadSize
		if rawSize&EncryptMask != 0 {
			// The first two length bytes stay in clear text. The remaining four
			// logical header bytes plus the payload are Blowfish block padded.
			frameSize = blowfish.GetBufferLength(payloadSize+int(EncryptSize)) + int(EncryptOffset)
		}

		if len(frameStart) < frameSize {
			break
		}

		// A session queues frames to another goroutine, where decrypt/CRC work
		// mutates their bytes. Never expose pending's reusable backing array.
		frame := make([]byte, frameSize)
		copy(frame, frameStart[:frameSize])
		frames = append(frames, frame)
		offset += frameSize
	}

	if offset == 0 {
		return frames, nil
	}

	if offset == len(d.pending) {
		d.pending = nil
		return frames, nil
	}

	// Retain only the incomplete tail, releasing capacity used by a large
	// coalesced read and bounding persistent decoder storage to one frame.
	pending := make([]byte, len(d.pending)-offset)
	copy(pending, d.pending[offset:])
	d.pending = pending
	return frames, nil
}

func (d *FrameDecoder) failLocked(frames [][]byte, err error) ([][]byte, error) {
	d.pending = nil
	d.closed = true
	d.terminalErr = err
	return frames, err
}

func (d *FrameDecoder) payloadLimit() int {
	if d.maxPayload == 0 {
		return DefaultMaxFramePayload
	}
	return d.maxPayload
}
