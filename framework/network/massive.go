package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	// MassivePacketID is the vSRO physical wrapper opcode for a fragmented
	// logical message.
	MassivePacketID uint16 = 0x600D

	// MassiveBodyPayloadSize leaves one byte in every 4090-byte body payload
	// for the body marker.
	MassiveBodyPayloadSize = 4089

	// DefaultMaxMassiveAggregateBytes keeps a normal high-volume response
	// (such as notices) bounded while allowing it to exceed one 15-bit frame.
	DefaultMaxMassiveAggregateBytes = 4 * 1024 * 1024

	// MaxMassiveAggregateBytes is a hard defensive ceiling for this profile.
	// It prevents a malicious header/body stream from reserving unbounded heap.
	MaxMassiveAggregateBytes = 16 * 1024 * 1024
)

var (
	ErrInvalidMassiveLimits    = errors.New("invalid massive codec limits")
	ErrMassivePayloadTooLarge  = errors.New("massive payload exceeds configured aggregate limit")
	ErrMassiveTooManyFragments = errors.New("massive fragment count exceeds configured limit")
	ErrMassiveUnexpectedFrame  = errors.New("unexpected massive frame")
	ErrMassiveBodyBeforeHeader = errors.New("massive body arrived before header")
	ErrMassiveDuplicateHeader  = errors.New("massive header arrived while assembly is active")
	ErrMassiveMalformedHeader  = errors.New("malformed massive header")
	ErrMassiveMalformedBody    = errors.New("malformed massive body")
	ErrMassiveAssemblyExpired  = errors.New("massive assembly expired")
	ErrMassiveMissingBodies    = errors.New("massive assembly ended before all bodies arrived")
	ErrMassiveInterleavedFrame = errors.New("non-massive frame interleaved with active massive assembly")
)

// MassiveLimits bound memory and time consumed by one logical message.
// Reassembled messages are represented by a logical Packet and may exceed one
// normal 15-bit frame, but are always bounded by MaxMassiveAggregateBytes.
type MassiveLimits struct {
	MaxAggregateBytes int
	MaxFragments      int
	MaxAssemblyAge    time.Duration
}

// DefaultMassiveLimits provides safe per-session bounds while still covering
// every logical payload that can be represented by the current Packet type.
func DefaultMassiveLimits() MassiveLimits {
	return MassiveLimits{
		MaxAggregateBytes: DefaultMaxMassiveAggregateBytes,
		MaxFragments:      (DefaultMaxMassiveAggregateBytes + MassiveBodyPayloadSize - 1) / MassiveBodyPayloadSize,
		MaxAssemblyAge:    30 * time.Second,
	}
}

// MassiveCodec is immutable and safe to share across sessions. Assemblers are
// stateful and must be owned by exactly one session.
type MassiveCodec struct {
	limits MassiveLimits
}

func NewMassiveCodec(limits MassiveLimits) (MassiveCodec, error) {
	if limits.MaxAggregateBytes <= 0 || limits.MaxAggregateBytes > MaxMassiveAggregateBytes {
		return MassiveCodec{}, fmt.Errorf("%w: aggregate bytes must be 1..%d", ErrInvalidMassiveLimits, MaxMassiveAggregateBytes)
	}
	if limits.MaxFragments <= 0 || limits.MaxFragments > int(^uint16(0)) {
		return MassiveCodec{}, fmt.Errorf("%w: fragments must be 1..%d", ErrInvalidMassiveLimits, ^uint16(0))
	}
	if limits.MaxAssemblyAge <= 0 {
		return MassiveCodec{}, fmt.Errorf("%w: assembly age must be positive", ErrInvalidMassiveLimits)
	}
	return MassiveCodec{limits: limits}, nil
}

func NewDefaultMassiveCodec() MassiveCodec {
	codec, err := NewMassiveCodec(DefaultMassiveLimits())
	if err != nil {
		panic(err)
	}
	return codec
}

// EncodeMassive always produces a 0x600D header followed by exact
// ceil(payload / 4089) body frames. Each output packet owns its payload; it is
// safe for a sender to apply per-session sequence, CRC, and encryption later.
func (c MassiveCodec) EncodeMassive(packet Packet) ([]Packet, error) {
	if packet.WriteError() != nil {
		return nil, packet.WriteError()
	}
	payload := packet.Payload()
	if len(payload) > c.limits.MaxAggregateBytes {
		return nil, fmt.Errorf("%w: got %d bytes, limit %d", ErrMassivePayloadTooLarge, len(payload), c.limits.MaxAggregateBytes)
	}
	bodyCount := (len(payload) + MassiveBodyPayloadSize - 1) / MassiveBodyPayloadSize
	if bodyCount > c.limits.MaxFragments {
		return nil, fmt.Errorf("%w: got %d, limit %d", ErrMassiveTooManyFragments, bodyCount, c.limits.MaxFragments)
	}

	header := newMassivePacket(packet)
	if err := header.WriteBool(true); err != nil {
		return nil, err
	}
	if err := header.WriteUInt16(uint16(bodyCount)); err != nil {
		return nil, err
	}
	if err := header.WriteUInt16(packet.MessageID); err != nil {
		return nil, err
	}

	frames := make([]Packet, 0, bodyCount+1)
	frames = append(frames, header)
	for offset := 0; offset < len(payload); offset += MassiveBodyPayloadSize {
		end := offset + MassiveBodyPayloadSize
		if end > len(payload) {
			end = len(payload)
		}
		body := newMassivePacket(packet)
		if err := body.WriteBool(false); err != nil {
			return nil, err
		}
		if err := body.WriteBytes(payload[offset:end]); err != nil {
			return nil, err
		}
		frames = append(frames, body)
	}
	return frames, nil
}

// FragmentIfNeeded keeps a normal frame intact through the maximum body chunk
// size and uses massive framing only when the packet cannot fit in one body.
func (c MassiveCodec) FragmentIfNeeded(packet Packet) ([]Packet, error) {
	if packet.WriteError() != nil {
		return nil, packet.WriteError()
	}
	if packet.PayloadLen() <= MassiveBodyPayloadSize {
		return []Packet{packet}, nil
	}
	return c.EncodeMassive(packet)
}

func newMassivePacket(source Packet) Packet {
	packet := newEmptyPacket(source.ServerPacket)
	packet.MessageID = MassivePacketID
	packet.Encrypted = source.Encrypted
	return packet
}

// MassiveAssembler reassembles physical massive frames after the session has
// decrypted and EDC-validated each individual wrapper frame.
type MassiveAssembler struct {
	codec MassiveCodec

	active         bool
	startedAt      time.Time
	expectedBodies int
	receivedBodies int
	targetOpcode   uint16
	payload        []byte
	serverPacket   bool
}

func NewMassiveAssembler(codec MassiveCodec) *MassiveAssembler {
	return &MassiveAssembler{codec: codec}
}

// ProcessFrame consumes one physical inbound frame. Normal frames pass through
// unchanged when no massive assembly is active. Every frame advances the
// assembly timeout check: a normal frame while an assembly is active is a
// protocol violation, resets the retained payload, and is rejected instead of
// allowing an attacker to retain a partial aggregate indefinitely.
func (a *MassiveAssembler) ProcessFrame(frame Packet) (Packet, bool, error) {
	return a.ProcessFrameAt(frame, time.Now())
}

// ProcessFrameAt is ProcessFrame with an injected clock for deterministic
// tests. A returned done=false means the massive assembly remains incomplete;
// done=true returns either a completed logical packet or an untouched normal
// frame.
func (a *MassiveAssembler) ProcessFrameAt(frame Packet, now time.Time) (Packet, bool, error) {
	if err := a.ExpireAt(now); err != nil {
		return Packet{}, false, err
	}
	if frame.MessageID != MassivePacketID {
		if a.active {
			a.Reset()
			return Packet{}, false, fmt.Errorf("%w: opcode 0x%04X", ErrMassiveInterleavedFrame, frame.MessageID)
		}
		return frame, true, nil
	}
	return a.acceptMassiveFrame(frame, now)
}

// Deadline reports when an active partial assembly must be expired. Callers
// that block on socket reads should cap their wakeup deadline with this value
// so a silent peer cannot retain an aggregate until it sends another frame.
func (a *MassiveAssembler) Deadline() (time.Time, bool) {
	if !a.active {
		return time.Time{}, false
	}
	return a.startedAt.Add(a.codec.limits.MaxAssemblyAge), true
}

func (a *MassiveAssembler) Accept(frame Packet) (Packet, bool, error) {
	return a.AcceptAt(frame, time.Now())
}

// AcceptAt consumes one physical wrapper frame. On completion it returns a
// new logical Packet and done=true. Callers must route that packet normally,
// without applying sequence/CRC validation a second time.
func (a *MassiveAssembler) AcceptAt(frame Packet, now time.Time) (Packet, bool, error) {
	if err := a.ExpireAt(now); err != nil {
		return Packet{}, false, err
	}
	if frame.MessageID != MassivePacketID {
		if a.active {
			a.Reset()
			return Packet{}, false, fmt.Errorf("%w: opcode 0x%04X", ErrMassiveInterleavedFrame, frame.MessageID)
		}
		return Packet{}, false, fmt.Errorf("%w: opcode 0x%04X", ErrMassiveUnexpectedFrame, frame.MessageID)
	}
	return a.acceptMassiveFrame(frame, now)
}

func (a *MassiveAssembler) acceptMassiveFrame(frame Packet, now time.Time) (Packet, bool, error) {
	payload := frame.Payload()
	if len(payload) == 0 {
		a.Reset()
		return Packet{}, false, fmt.Errorf("%w: empty payload", ErrMassiveMalformedBody)
	}

	switch payload[0] {
	case 1:
		return a.acceptHeader(frame, payload, now)
	case 0:
		return a.acceptBody(payload)
	default:
		a.Reset()
		return Packet{}, false, fmt.Errorf("%w: marker %d", ErrMassiveMalformedBody, payload[0])
	}
}

func (a *MassiveAssembler) acceptHeader(frame Packet, payload []byte, now time.Time) (Packet, bool, error) {
	if a.active {
		a.Reset()
		return Packet{}, false, ErrMassiveDuplicateHeader
	}
	if len(payload) != 5 {
		return Packet{}, false, fmt.Errorf("%w: got %d payload bytes, want 5", ErrMassiveMalformedHeader, len(payload))
	}

	bodyCount := int(binary.LittleEndian.Uint16(payload[1:3]))
	targetOpcode := binary.LittleEndian.Uint16(payload[3:5])
	if targetOpcode == MassivePacketID {
		return Packet{}, false, fmt.Errorf("%w: nested massive target", ErrMassiveMalformedHeader)
	}
	if bodyCount > a.codec.limits.MaxFragments {
		return Packet{}, false, fmt.Errorf("%w: got %d, limit %d", ErrMassiveTooManyFragments, bodyCount, a.codec.limits.MaxFragments)
	}
	if bodyCount == 0 {
		return makeLogicalMassivePacket(targetOpcode, nil, frame.ServerPacket), true, nil
	}

	a.active = true
	a.startedAt = now
	a.expectedBodies = bodyCount
	a.receivedBodies = 0
	a.targetOpcode = targetOpcode
	a.payload = make([]byte, 0, minInt(bodyCount*MassiveBodyPayloadSize, a.codec.limits.MaxAggregateBytes))
	a.serverPacket = frame.ServerPacket
	return Packet{}, false, nil
}

func (a *MassiveAssembler) acceptBody(payload []byte) (Packet, bool, error) {
	if !a.active {
		return Packet{}, false, ErrMassiveBodyBeforeHeader
	}
	chunk := payload[1:]
	if len(chunk) == 0 || len(chunk) > MassiveBodyPayloadSize {
		a.Reset()
		return Packet{}, false, fmt.Errorf("%w: got %d body bytes", ErrMassiveMalformedBody, len(chunk))
	}
	if a.receivedBodies < a.expectedBodies-1 && len(chunk) != MassiveBodyPayloadSize {
		a.Reset()
		return Packet{}, false, fmt.Errorf("%w: non-final body has %d bytes, want %d", ErrMassiveMalformedBody, len(chunk), MassiveBodyPayloadSize)
	}
	if len(a.payload)+len(chunk) > a.codec.limits.MaxAggregateBytes {
		a.Reset()
		return Packet{}, false, fmt.Errorf("%w: got more than %d bytes", ErrMassivePayloadTooLarge, a.codec.limits.MaxAggregateBytes)
	}

	a.payload = append(a.payload, chunk...)
	a.receivedBodies++
	if a.receivedBodies < a.expectedBodies {
		return Packet{}, false, nil
	}
	if a.receivedBodies > a.expectedBodies {
		a.Reset()
		return Packet{}, false, fmt.Errorf("%w: received %d bodies, expected %d", ErrMassiveMalformedBody, a.receivedBodies, a.expectedBodies)
	}

	packet := makeLogicalMassivePacket(a.targetOpcode, a.payload, a.serverPacket)
	a.Reset()
	return packet, true, nil
}

func makeLogicalMassivePacket(opcode uint16, payload []byte, serverPacket bool) Packet {
	packet := newEmptyPacket(serverPacket)
	packet.MessageID = opcode
	packet.Encrypted = false
	if len(payload) > 0 {
		_ = packet.WriteBytes(payload)
	}
	return packet
}

// ExpireAt rejects a stale partial assembly. It does nothing when idle.
func (a *MassiveAssembler) ExpireAt(now time.Time) error {
	if !a.active || now.Sub(a.startedAt) < a.codec.limits.MaxAssemblyAge {
		return nil
	}
	a.Reset()
	return ErrMassiveAssemblyExpired
}

// Finish reports a missing-body sequence at end-of-stream.
func (a *MassiveAssembler) Finish() error {
	if !a.active {
		return nil
	}
	a.Reset()
	return ErrMassiveMissingBodies
}

func (a *MassiveAssembler) Reset() {
	a.active = false
	a.startedAt = time.Time{}
	a.expectedBodies = 0
	a.receivedBodies = 0
	a.targetOpcode = 0
	a.payload = nil
	a.serverPacket = false
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
