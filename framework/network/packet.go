package network

import (
	"encoding/binary"
	"fmt"
	"github.com/Harvi8/AegisCore/framework/security/blowfish"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"math"
	"strings"
)

const (
	BufferSize uint16 = 4096
	HeaderSize uint16 = 6
	DataSize   uint16 = BufferSize - HeaderSize

	initialPacketBufferSize = 64

	HeaderOffset   uint16 = 0
	SizeOffset            = HeaderOffset + 0
	IDOffset              = HeaderOffset + 2
	SequenceOffset        = HeaderOffset + 4
	CRCOffset             = HeaderOffset + 5
	DataOffset            = HeaderOffset + HeaderSize

	EncryptOffset        = HeaderOffset + 2
	EncryptSize          = HeaderSize - EncryptOffset
	EncryptMask   uint16 = 0x8000
)

var (
	// ErrPacketPayloadTooLarge reports an attempted normal-frame serialization
	// whose logical payload cannot fit in the protocol's 15-bit size field.
	ErrPacketPayloadTooLarge = errors.New("packet payload exceeds 15-bit frame limit")

	// ErrPacketStringTooLong reports a string that cannot be represented by the
	// protocol's uint16 length prefix.
	ErrPacketStringTooLong = errors.New("packet string exceeds uint16 length")

	// ErrPacketWrite records an invalid writer state rather than allowing a
	// slice panic to take down a server goroutine.
	ErrPacketWrite = errors.New("packet write failed")

	// ErrPacketCipherRequired reports an encrypted frame operation without an
	// established session cipher.
	ErrPacketCipherRequired = errors.New("encrypted packet requires a cipher")

	// ErrPacketEncryptedFrameTruncated reports a frame whose declared logical
	// size requires more padded ciphertext than is available.
	ErrPacketEncryptedFrameTruncated = errors.New("encrypted packet frame is truncated")
)

type Packet struct {
	MessageID             uint16
	Encrypted             bool
	Buffer                []byte
	Data                  []byte
	Size                  int
	DataSize              int
	Sequence              byte
	CRC                   byte
	ServerPacket          bool
	readPosition          int
	writePosition         int
	writeErr              error
	IsSequenceInitialized bool
}

func NewPacket(buffer []byte) Packet {
	messageID := uint16(0)
	encrypted := false
	crc := byte(0)
	sequence := byte(0)
	data := buffer

	if len(buffer) >= int(IDOffset)+2 {
		messageID = binary.LittleEndian.Uint16(buffer[IDOffset : IDOffset+2])
	}
	if len(buffer) > int(SequenceOffset) {
		sequence = buffer[SequenceOffset]
	}
	if len(buffer) > int(CRCOffset) {
		crc = buffer[CRCOffset]
	}
	if len(buffer) >= int(DataOffset) {
		data = buffer[DataOffset:]
	}
	if len(buffer) >= int(EncryptOffset) && (binary.LittleEndian.Uint16(buffer[:EncryptOffset])&EncryptMask) != 0 {
		encrypted = true
	}
	rawSize := uint16(0)
	if len(buffer) >= 2 {
		rawSize = binary.LittleEndian.Uint16(buffer[:2])
	}
	size := rawSize &^ EncryptMask

	return Packet{
		MessageID:     messageID,
		Encrypted:     encrypted,
		Buffer:        buffer,
		Data:          data,
		Size:          int(size + DataOffset),
		DataSize:      int(size),
		CRC:           crc,
		Sequence:      sequence,
		ServerPacket:  false,
		readPosition:  0,
		writePosition: 0,
	}
}

func EmptyPacket() Packet {
	return newEmptyPacket(true)
}

func EmptyClientPacket() Packet {
	return newEmptyPacket(false)
}

// NewServerPacket returns a writable server packet backed by one owned,
// dynamically growing allocation. EmptyPacket remains for source compatibility
// with existing handlers.
func NewServerPacket(messageID uint16) *Packet {
	packet := EmptyPacket()
	packet.MessageID = messageID
	return &packet
}

// NewClientPacket returns a writable client packet backed by one owned,
// dynamically growing allocation. EmptyClientPacket remains for source
// compatibility with existing handlers.
func NewClientPacket(messageID uint16) *Packet {
	packet := EmptyClientPacket()
	packet.MessageID = messageID
	return &packet
}

func newEmptyPacket(serverPacket bool) Packet {
	buffer := make([]byte, initialPacketBufferSize)
	return Packet{
		MessageID:     0,
		Encrypted:     false,
		Buffer:        buffer,
		Data:          buffer[DataOffset:],
		Size:          0,
		DataSize:      0,
		Sequence:      0,
		CRC:           0,
		ServerPacket:  serverPacket,
		readPosition:  0,
		writePosition: 0,
	}
}

// Payload returns the logical payload bytes currently available on the packet.
// It never exposes the encryption padding that may be present in Buffer.
func (p *Packet) Payload() []byte {
	if p.DataSize <= 0 || len(p.Data) == 0 {
		return nil
	}
	length := p.DataSize
	if length > len(p.Data) {
		length = len(p.Data)
	}
	return p.Data[:length]
}

// PayloadLen returns the logical payload size, bounded by the bytes currently
// available in Data so malformed manual Packet values cannot over-report.
func (p *Packet) PayloadLen() int {
	return len(p.Payload())
}

// Remaining returns the number of unread logical payload bytes. It is bounded
// by DataSize so writable packets with spare backing capacity cannot make a
// parser accept trailing zeroes that were never sent on the wire.
func (p *Packet) Remaining() int {
	limit := p.DataSize
	if limit < 0 {
		return 0
	}
	if limit > len(p.Data) {
		limit = len(p.Data)
	}
	if p.readPosition >= limit {
		return 0
	}
	return limit - p.readPosition
}

// WriteError returns the first write or serialization preparation error. The
// writer latches that error so callers that intentionally ignore individual
// Write* results still fail closed at MarshalBinary.
func (p *Packet) WriteError() error {
	return p.writeErr
}

func (p *Packet) ReadByte() (byte, error) {
	if p.Remaining() >= 1 {
		value := p.Data[p.readPosition]
		p.readPosition++
		return value, nil
	}
	return 0, errors.New("Not enough data available to read")
}

func (p *Packet) ReadUInt16() (uint16, error) {
	if p.Remaining() >= 2 {
		value := p.Data[p.readPosition : p.readPosition+2]
		p.readPosition += 2
		return binary.LittleEndian.Uint16(value), nil
	}
	return 0, errors.New("Not enough data available to read")
}

func (p *Packet) ReadInt16() (int16, error) {
	uVal, err := p.ReadUInt16()
	if err != nil {
		return 0, err
	}
	return int16(uVal), nil
}

func (p *Packet) ReadUInt32() (uint32, error) {
	if p.Remaining() >= 4 {
		value := p.Data[p.readPosition : p.readPosition+4]
		p.readPosition += 4
		return binary.LittleEndian.Uint32(value), nil
	}
	return 0, errors.New("Not enough data available to read")
}

func (p *Packet) ReadInt32() (int32, error) {
	uVal, err := p.ReadUInt32()
	if err != nil {
		return 0, err
	}
	return int32(uVal), nil
}

func (p *Packet) ReadUInt64() (uint64, error) {
	if p.Remaining() >= 8 {
		value := p.Data[p.readPosition : p.readPosition+8]
		p.readPosition += 8
		return binary.LittleEndian.Uint64(value), nil
	}
	return 0, errors.New("Not enough data available to read")
}

func (p *Packet) ReadInt64() (int64, error) {
	uVal, err := p.ReadUInt64()
	if err != nil {
		return 0, err
	}
	return int64(uVal), nil
}

func (p *Packet) ReadString() (string, error) {
	stringLength, err := p.ReadUInt16()

	if err != nil {
		return "", err
	}

	if p.Remaining() >= int(stringLength) {
		characters := p.Data[p.readPosition : p.readPosition+int(stringLength)]
		p.readPosition += int(stringLength)
		return string(characters), nil
	}
	return "0", errors.New("Not enough data available to read")
}

func (p *Packet) ReadFloat32() (float32, error) {
	if p.Remaining() >= 4 {
		value := p.Data[p.readPosition : p.readPosition+4]
		p.readPosition += 4
		return utils.Float32FromByteArray(value), nil
	}
	return 0, errors.New("Not enough data available to read")
}

func (p *Packet) ReadFloat64() (float64, error) {
	if p.Remaining() >= 8 {
		value := p.Data[p.readPosition : p.readPosition+8]
		p.readPosition += 8
		return utils.Float64FromByteArray(value), nil
	}
	return 0, errors.New("Not enough data available to read")
}

func (p *Packet) ReadBool() (bool, error) {
	b, err := p.ReadByte()
	if err != nil {
		return false, err
	}

	return b != 0, nil
}

func (p *Packet) ReadBytes(n int) ([]byte, error) {
	if n < 0 {
		return nil, errors.New("cannot read a negative number of bytes")
	}
	if p.Remaining() >= n {
		value := p.Data[p.readPosition : p.readPosition+n]
		p.readPosition += n
		return value, nil
	}
	return nil, errors.New("Not enough data available to read")
}

func (p *Packet) WriteByte(value byte) error {
	return p.writeBytes([]byte{value})
}

func (p *Packet) WriteUInt16(value uint16) error {
	var bytes [2]byte
	binary.LittleEndian.PutUint16(bytes[:], value)
	return p.writeBytes(bytes[:])
}

func (p *Packet) WriteUInt32(value uint32) error {
	var bytes [4]byte
	binary.LittleEndian.PutUint32(bytes[:], value)
	return p.writeBytes(bytes[:])
}

func (p *Packet) WriteUInt64(value uint64) error {
	var bytes [8]byte
	binary.LittleEndian.PutUint64(bytes[:], value)
	return p.writeBytes(bytes[:])
}

func (p *Packet) WriteString(value string) error {
	if len(value) > math.MaxUint16 {
		return p.setWriteError(fmt.Errorf("%w: got %d bytes", ErrPacketStringTooLong, len(value)))
	}

	// Reserve and copy the prefix plus payload in one operation so a rejected
	// or failed string never leaves a partial length prefix behind.
	bytes := make([]byte, 2+len(value))
	binary.LittleEndian.PutUint16(bytes[:2], uint16(len(value)))
	copy(bytes[2:], value)
	return p.writeBytes(bytes)
}

func (p *Packet) WriteFloat32(value float32) error {
	var bytes [4]byte
	binary.LittleEndian.PutUint32(bytes[:], math.Float32bits(value))
	return p.writeBytes(bytes[:])
}

func (p *Packet) WriteFloat64(value float64) error {
	var bytes [8]byte
	binary.LittleEndian.PutUint64(bytes[:], math.Float64bits(value))
	return p.writeBytes(bytes[:])
}

func (p *Packet) WriteBool(value bool) error {
	if value {
		return p.WriteByte(1)
	}
	return p.WriteByte(0)
}

func (p *Packet) WriteBytes(bytes []byte) error {
	return p.writeBytes(bytes)
}

func (p *Packet) WriteMessageID() error {
	if err := p.ensurePayloadCapacity(p.DataSize); err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(p.Buffer[IDOffset:IDOffset+2], p.MessageID)
	return nil
}

func (p *Packet) WriteCRC() error {
	if err := p.ensurePayloadCapacity(p.DataSize); err != nil {
		return err
	}
	if p.ServerPacket {
		p.Buffer[CRCOffset] = 0x00
	} else {
		p.Buffer[CRCOffset] = p.CRC
	}
	return nil
}

func (p *Packet) WriteSequence() error {
	if err := p.ensurePayloadCapacity(p.DataSize); err != nil {
		return err
	}
	if p.ServerPacket {
		p.Buffer[SequenceOffset] = 0x00
	} else {
		// Packet serialization must preserve the explicit sequence byte. The
		// session layer is responsible for assigning and validating it; panicking
		// here turns an invalid caller state into a process-wide failure.
		p.Buffer[SequenceOffset] = p.Sequence
	}
	return nil
}

func (p *Packet) WriteSize() error {
	if p.DataSize < 0 || p.DataSize > DefaultMaxFramePayload {
		return p.setWriteError(fmt.Errorf("%w: got %d bytes", ErrPacketPayloadTooLarge, p.DataSize))
	}
	if err := p.ensurePayloadCapacity(p.DataSize); err != nil {
		return err
	}

	size := uint16(p.DataSize)
	if p.Encrypted {
		size |= EncryptMask
	}
	binary.LittleEndian.PutUint16(p.Buffer[:2], size)
	return nil
}

// MarshalBinary validates a logical packet and returns an owned, ready-to-send
// plaintext frame. It is deliberately the error-returning serialization API;
// a caller must encrypt the returned logical contents through its session
// encoder when Encrypted is set.
func (p *Packet) MarshalBinary() ([]byte, error) {
	if p.writeErr != nil {
		return nil, p.writeErr
	}
	if p.DataSize < 0 || p.DataSize > DefaultMaxFramePayload {
		return nil, p.setWriteError(fmt.Errorf("%w: got %d bytes", ErrPacketPayloadTooLarge, p.DataSize))
	}
	if err := p.ensurePayloadCapacity(p.DataSize); err != nil {
		return nil, err
	}
	if err := p.WriteSize(); err != nil {
		return nil, err
	}
	if err := p.WriteMessageID(); err != nil {
		return nil, err
	}
	if err := p.WriteSequence(); err != nil {
		return nil, err
	}
	if err := p.WriteCRC(); err != nil {
		return nil, err
	}

	p.Size = int(HeaderSize) + p.DataSize
	frame := make([]byte, p.Size)
	copy(frame, p.Buffer[:p.Size])
	return frame, nil
}

// ToBytes is retained for existing handlers while they migrate to session
// Sender and MarshalBinary. New code must use MarshalBinary so it cannot lose
// serialization errors. Returning nil here is safer than the old panic path
// and is accompanied by an error log for legacy callers.
func (p *Packet) ToBytes() []byte {
	frame, err := p.MarshalBinary()
	if err != nil {
		log.Errorf("failed to serialize packet 0x%04X: %v", p.MessageID, err)
		return nil
	}
	return frame
}

func (p *Packet) writeBytes(bytes []byte) error {
	if p.writeErr != nil {
		return p.writeErr
	}
	if len(bytes) == 0 {
		return nil
	}
	if err := p.ensurePayloadCapacity(p.writePosition + len(bytes)); err != nil {
		return err
	}
	copy(p.Data[p.writePosition:p.writePosition+len(bytes)], bytes)
	p.writePosition += len(bytes)
	p.DataSize += len(bytes)
	return nil
}

func (p *Packet) ensurePayloadCapacity(requiredPayload int) error {
	if p.writeErr != nil {
		return p.writeErr
	}
	if requiredPayload < 0 || p.DataSize < 0 {
		return p.setWriteError(fmt.Errorf("%w: negative packet size", ErrPacketWrite))
	}
	if p.DataSize > requiredPayload {
		requiredPayload = p.DataSize
	}
	if requiredPayload > int(^uint(0)>>1)-int(HeaderSize) {
		return p.setWriteError(fmt.Errorf("%w: packet size overflow", ErrPacketWrite))
	}

	required := int(HeaderSize) + requiredPayload
	if cap(p.Buffer) < required {
		capacity := cap(p.Buffer)
		if capacity < initialPacketBufferSize {
			capacity = initialPacketBufferSize
		}
		for capacity < required {
			if capacity > int(^uint(0)>>1)/2 {
				capacity = required
				break
			}
			capacity *= 2
		}

		buffer := make([]byte, capacity)
		copy(buffer, p.Buffer)
		// p.Data may have been detached by the legacy growth implementation;
		// its logical bytes are the authoritative payload during migration.
		copy(buffer[DataOffset:], p.Data)
		p.Buffer = buffer
	} else {
		p.Buffer = p.Buffer[:cap(p.Buffer)]
		if len(p.Data) > 0 {
			copy(p.Buffer[DataOffset:], p.Data)
		}
	}

	p.Data = p.Buffer[DataOffset:]
	return nil
}

func (p *Packet) setWriteError(err error) error {
	if p.writeErr == nil {
		p.writeErr = err
	}
	return p.writeErr
}

// MarshalWire returns a complete caller-owned wire frame. For encrypted
// packets it preserves the logical masked size in the first two bytes and
// applies Blowfish only to the padded remainder of the header and payload.
func (p *Packet) MarshalWire(cipher *blowfish.Cipher) ([]byte, error) {
	logical, err := p.MarshalBinary()
	if err != nil {
		return nil, err
	}
	if !p.Encrypted {
		return logical, nil
	}
	if cipher == nil {
		return nil, ErrPacketCipherRequired
	}

	paddedSize := blowfish.GetBufferLength(p.DataSize + int(EncryptSize))
	wire := make([]byte, int(EncryptOffset)+paddedSize)
	copy(wire[:EncryptOffset], logical[:EncryptOffset])
	copy(wire[EncryptOffset:], logical[EncryptOffset:])
	for offset := int(EncryptOffset); offset < len(wire); offset += 8 {
		cipher.EncryptRev(wire[offset:offset+8], wire[offset:offset+8])
	}
	return wire, nil
}

// DecryptChecked returns a parsed logical packet or a checked error. It never
// slices beyond the received wire frame, even when the length field is corrupt.
func (p *Packet) DecryptChecked(cipher *blowfish.Cipher) (Packet, error) {
	if !p.Encrypted {
		return *p, nil
	}
	if cipher == nil {
		return Packet{}, ErrPacketCipherRequired
	}
	if len(p.Buffer) < int(EncryptOffset) {
		return Packet{}, fmt.Errorf("%w: got %d bytes", ErrPacketEncryptedFrameTruncated, len(p.Buffer))
	}
	rawSize := binary.LittleEndian.Uint16(p.Buffer[:2])
	logicalSize := int(rawSize &^ EncryptMask)
	paddedSize := blowfish.GetBufferLength(logicalSize + int(EncryptSize))
	wireSize := int(EncryptOffset) + paddedSize
	if len(p.Buffer) < wireSize {
		return Packet{}, fmt.Errorf("%w: got %d bytes, need %d", ErrPacketEncryptedFrameTruncated, len(p.Buffer), wireSize)
	}

	decoded := make([]byte, wireSize)
	copy(decoded, p.Buffer[:wireSize])
	for offset := int(EncryptOffset); offset < len(decoded); offset += 8 {
		cipher.DecryptRev(decoded[offset:offset+8], decoded[offset:offset+8])
	}
	return NewPacket(decoded[:int(HeaderSize)+logicalSize]), nil
}

// Decrypt retains the legacy value-returning API. New session code should use
// DecryptChecked and close the connection on an error instead of continuing
// with a half-decoded packet.
func (p *Packet) Decrypt(cipher *blowfish.Cipher) Packet {
	packet, err := p.DecryptChecked(cipher)
	if err != nil {
		p.setWriteError(err)
		return *p
	}
	return packet
}

// Encrypt retains the legacy mutable API used by the framework client. It
// delegates to MarshalWire so padded ciphertext growth cannot detach Buffer
// and Data or cause an out-of-bounds slice. New sender code should use
// MarshalWire directly and retain the logical Packet unchanged.
func (p *Packet) Encrypt(cipher *blowfish.Cipher) Packet {
	if !p.Encrypted {
		return *p
	}
	wire, err := p.MarshalWire(cipher)
	if err != nil {
		p.setWriteError(err)
		return *p
	}
	p.Buffer = wire
	p.Data = wire[DataOffset:]
	p.DataSize = len(wire) - int(HeaderSize)
	p.Size = len(wire)
	return *p
}

func (p *Packet) IsMassive() bool {
	return p.DataSize > MassiveBodyPayloadSize || p.Size > int(HeaderSize)+MassiveBodyPayloadSize
}

// SplitIntoMassivePackets is retained for source compatibility. New session
// code should use MassiveCodec.EncodeMassive and enqueue the resulting batch as
// one ordered send operation so body frames cannot interleave with another
// packet.
func (p *Packet) SplitIntoMassivePackets() []*Packet {
	if !p.IsMassive() {
		return []*Packet{p}
	}

	frames, err := NewDefaultMassiveCodec().EncodeMassive(*p)
	if err != nil {
		log.Errorf("failed to split massive packet 0x%04X: %v", p.MessageID, err)
		return nil
	}
	packets := make([]*Packet, 0, len(frames))
	for i := range frames {
		packet := frames[i]
		packets = append(packets, &packet)
	}
	return packets
}

func (p *Packet) String() string {
	sb := strings.Builder{}
	sb2 := strings.Builder{}
	sb.WriteString("\nPacket\n")
	sb.WriteString(fmt.Sprintf("[%X]\n", p.MessageID))
	sb.WriteString(fmt.Sprintf("Encrypted [%v]\n", p.Encrypted))
	sb.WriteString(fmt.Sprintf("Massive [%v]\n", p.IsMassive()))
	sb.WriteString(fmt.Sprintf("Sequence [%v]\n", p.Sequence))
	sb.WriteString(fmt.Sprintf("CRC [%v]\n", p.CRC))
	sb.WriteString(fmt.Sprintf("DataSize [%v]\n", p.DataSize))
	sb.WriteString(fmt.Sprintf("Size [%v]\n", p.Size))
	for i := 0; i < p.DataSize; i++ {
		sb.WriteString(fmt.Sprintf("%02X ", p.Data[i]))
		sb2.WriteString(dumpByte(p.Data[i]))
		if (i+1)%8 == 0 {
			sb.WriteString(fmt.Sprintf("\t\t[%8s]\n", sb2.String()))
			sb2.Reset()
		} else if (i + 1) == p.DataSize {
			m := 8 - ((i + 1) % 8)
			for j := 0; j < m; j++ {
				sb.WriteString("   ")
			}
			sb.WriteString(fmt.Sprintf("\t\t[%-8s]\n", sb2.String()))
			sb2.Reset()
		}
	}
	return sb.String()
}

func dumpByte(value byte) string {
	if value < 32 || value > 126 {
		return "."
	}
	return string(value)
}

func (p *Packet) extendBuffer() {
	newBuffer := make([]byte, cap(p.Data)*2)
	copy(newBuffer, p.Data)
	p.Data = newBuffer
}
