package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/framework/security/blowfish"
)

func TestFrameEncoderEncryptsAndDecryptsPaddedPayloadOverLegacyBuffer(t *testing.T) {
	const payloadSize = 4090
	cipher, err := blowfish.NewCipher([]byte("stage-one-test-key"))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	payload := patternedBytes(payloadSize)
	packet := NewClientPacket(0xA100)
	packet.Encrypted = true
	packet.Sequence = 0x4A
	packet.CRC = 0xB3
	if err := packet.WriteBytes(payload); err != nil {
		t.Fatalf("WriteBytes() error = %v", err)
	}

	wire, err := NewFrameEncoder(cipher).Encode(packet)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if got, want := len(wire), int(EncryptOffset)+blowfish.GetBufferLength(payloadSize+int(EncryptSize)); got != want {
		t.Fatalf("encrypted wire size = %d, want %d", got, want)
	}
	if rawSize := binary.LittleEndian.Uint16(wire[:2]); rawSize != uint16(payloadSize)|EncryptMask {
		t.Fatalf("encrypted size field = 0x%04X, want 0x%04X", rawSize, uint16(payloadSize)|EncryptMask)
	}

	incoming := NewPacket(wire)
	decoded, err := incoming.DecryptChecked(cipher)
	if err != nil {
		t.Fatalf("DecryptChecked() error = %v", err)
	}
	if decoded.MessageID != packet.MessageID {
		t.Fatalf("decoded opcode = 0x%04X, want 0x%04X", decoded.MessageID, packet.MessageID)
	}
	if decoded.Sequence != packet.Sequence || decoded.CRC != packet.CRC {
		t.Fatalf("decoded EDC = (%d, %d), want (%d, %d)", decoded.Sequence, decoded.CRC, packet.Sequence, packet.CRC)
	}
	if !bytes.Equal(decoded.Payload(), payload) {
		t.Fatalf("decoded payload differs from source")
	}
}

func TestFrameEncoderRequiresCipherForEncryptedPacket(t *testing.T) {
	packet := NewClientPacket(0xA100)
	packet.Encrypted = true

	_, err := NewFrameEncoder(nil).Encode(packet)
	if !errors.Is(err, ErrPacketCipherRequired) {
		t.Fatalf("Encode() error = %v, want ErrPacketCipherRequired", err)
	}
}
