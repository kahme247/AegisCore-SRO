package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestMassiveCodecRoundTripsRequiredBoundaries(t *testing.T) {
	codec := NewDefaultMassiveCodec()
	sizes := []int{0, 4089, 4090, 4091, 8178, 10000}

	for _, size := range sizes {
		t.Run("payload-size-"+itoa(size), func(t *testing.T) {
			payload := patternedBytes(size)
			source := NewServerPacket(0xA100)
			if err := source.WriteBytes(payload); err != nil {
				t.Fatalf("WriteBytes() error = %v", err)
			}

			frames, err := codec.EncodeMassive(*source)
			if err != nil {
				t.Fatalf("EncodeMassive() error = %v", err)
			}
			bodyCount := (size + MassiveBodyPayloadSize - 1) / MassiveBodyPayloadSize
			if got, want := len(frames), bodyCount+1; got != want {
				t.Fatalf("frame count = %d, want %d", got, want)
			}

			header := frames[0]
			if header.MessageID != MassivePacketID {
				t.Fatalf("header opcode = 0x%04X, want 0x%04X", header.MessageID, MassivePacketID)
			}
			if got, want := header.PayloadLen(), 5; got != want {
				t.Fatalf("header payload size = %d, want %d", got, want)
			}
			if header.Payload()[0] != 1 {
				t.Fatalf("header marker = %d, want 1", header.Payload()[0])
			}
			if got := binary.LittleEndian.Uint16(header.Payload()[1:3]); got != uint16(bodyCount) {
				t.Fatalf("header body count = %d, want %d", got, bodyCount)
			}
			if got := binary.LittleEndian.Uint16(header.Payload()[3:5]); got != source.MessageID {
				t.Fatalf("header target opcode = 0x%04X, want 0x%04X", got, source.MessageID)
			}

			for index, body := range frames[1:] {
				if body.MessageID != MassivePacketID {
					t.Fatalf("body %d opcode = 0x%04X, want 0x%04X", index, body.MessageID, MassivePacketID)
				}
				if body.Payload()[0] != 0 {
					t.Fatalf("body %d marker = %d, want 0", index, body.Payload()[0])
				}
				wantChunk := MassiveBodyPayloadSize
				if remaining := size - index*MassiveBodyPayloadSize; remaining < wantChunk {
					wantChunk = remaining
				}
				if got := body.PayloadLen() - 1; got != wantChunk {
					t.Fatalf("body %d chunk length = %d, want %d", index, got, wantChunk)
				}
			}

			assembler := NewMassiveAssembler(codec)
			now := time.Unix(1_700_000_000, 0)
			var complete Packet
			for index, frame := range frames {
				packet, done, err := assembler.AcceptAt(frame, now.Add(time.Duration(index)*time.Millisecond))
				if err != nil {
					t.Fatalf("AcceptAt(frame %d) error = %v", index, err)
				}
				if done {
					complete = packet
				}
			}
			if complete.MessageID != source.MessageID {
				t.Fatalf("complete opcode = 0x%04X, want 0x%04X", complete.MessageID, source.MessageID)
			}
			if !bytes.Equal(complete.Payload(), payload) {
				t.Fatalf("complete payload differs from source")
			}
		})
	}
}

func TestMassiveAssemblerRejectsMalformedSequencesAndExpires(t *testing.T) {
	codec := NewDefaultMassiveCodec()
	assembler := NewMassiveAssembler(codec)
	now := time.Unix(1_700_000_000, 0)

	body := massiveTestBody([]byte{0x01})
	_, _, err := assembler.AcceptAt(body, now)
	if !errors.Is(err, ErrMassiveBodyBeforeHeader) {
		t.Fatalf("body before header error = %v, want ErrMassiveBodyBeforeHeader", err)
	}

	header := massiveTestHeader(2, 0xA100)
	_, done, err := assembler.AcceptAt(header, now)
	if err != nil || done {
		t.Fatalf("first header = done %v, error %v", done, err)
	}
	_, _, err = assembler.AcceptAt(header, now)
	if !errors.Is(err, ErrMassiveDuplicateHeader) {
		t.Fatalf("duplicate header error = %v, want ErrMassiveDuplicateHeader", err)
	}

	_, _, err = assembler.AcceptAt(header, now)
	if err != nil {
		t.Fatalf("second assembly header error = %v", err)
	}
	shortNonFinal := massiveTestBody([]byte{0x01})
	_, _, err = assembler.AcceptAt(shortNonFinal, now)
	if !errors.Is(err, ErrMassiveMalformedBody) {
		t.Fatalf("short non-final body error = %v, want ErrMassiveMalformedBody", err)
	}

	_, _, err = assembler.AcceptAt(massiveTestHeader(1, 0xA100), now)
	if err != nil {
		t.Fatalf("missing-body header error = %v", err)
	}
	if err := assembler.Finish(); !errors.Is(err, ErrMassiveMissingBodies) {
		t.Fatalf("Finish() error = %v, want ErrMassiveMissingBodies", err)
	}

	_, _, err = assembler.AcceptAt(massiveTestHeader(1, 0xA100), now)
	if err != nil {
		t.Fatalf("expiry header error = %v", err)
	}
	_, _, err = assembler.AcceptAt(massiveTestBody([]byte{0x01}), now.Add(DefaultMassiveLimits().MaxAssemblyAge))
	if !errors.Is(err, ErrMassiveAssemblyExpired) {
		t.Fatalf("expired assembly error = %v, want ErrMassiveAssemblyExpired", err)
	}
}

func TestMassiveAssemblerRejectsInterleavedNormalFramesAndExpiresOnEveryFrame(t *testing.T) {
	codec := NewDefaultMassiveCodec()
	assembler := NewMassiveAssembler(codec)
	now := time.Unix(1_700_000_000, 0)
	header := massiveTestHeader(1, 0xA100)
	keepAlive := NewServerPacket(0x2002)

	if _, done, err := assembler.ProcessFrameAt(header, now); err != nil || done {
		t.Fatalf("ProcessFrameAt(header) = done %v, error %v", done, err)
	}
	if _, _, err := assembler.ProcessFrameAt(*keepAlive, now.Add(time.Millisecond)); !errors.Is(err, ErrMassiveInterleavedFrame) {
		t.Fatalf("interleaved keepalive error = %v, want ErrMassiveInterleavedFrame", err)
	}
	passed, done, err := assembler.ProcessFrameAt(*keepAlive, now.Add(2*time.Millisecond))
	if err != nil || !done || passed.MessageID != keepAlive.MessageID {
		t.Fatalf("normal frame after interleaving reset = (%#x, done %v, error %v)", passed.MessageID, done, err)
	}

	if _, done, err := assembler.ProcessFrameAt(header, now); err != nil || done {
		t.Fatalf("ProcessFrameAt(expiry header) = done %v, error %v", done, err)
	}
	if _, _, err := assembler.ProcessFrameAt(*keepAlive, now.Add(DefaultMassiveLimits().MaxAssemblyAge)); !errors.Is(err, ErrMassiveAssemblyExpired) {
		t.Fatalf("normal frame after expiry error = %v, want ErrMassiveAssemblyExpired", err)
	}
	passed, done, err = assembler.ProcessFrameAt(*keepAlive, now.Add(DefaultMassiveLimits().MaxAssemblyAge+time.Millisecond))
	if err != nil || !done || passed.MessageID != keepAlive.MessageID {
		t.Fatalf("normal frame after expiry reset = (%#x, done %v, error %v)", passed.MessageID, done, err)
	}
}

func TestMassiveCodecSupportsLogicalPayloadOverNormalFrameLimit(t *testing.T) {
	codec := NewDefaultMassiveCodec()
	payload := patternedBytes(DefaultMaxFramePayload + 1)
	source := NewServerPacket(0xA100)
	if err := source.WriteBytes(payload); err != nil {
		t.Fatalf("WriteBytes() error = %v", err)
	}
	frames, err := codec.EncodeMassive(*source)
	if err != nil {
		t.Fatalf("EncodeMassive() error = %v", err)
	}
	if len(frames) <= 1 {
		t.Fatalf("EncodeMassive() returned %d frames, want header and bodies", len(frames))
	}

	assembler := NewMassiveAssembler(codec)
	var complete Packet
	for _, frame := range frames {
		packet, done, err := assembler.Accept(frame)
		if err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
		if done {
			complete = packet
		}
	}
	if complete.DataSize != len(payload) {
		t.Fatalf("reassembled DataSize = %d, want %d", complete.DataSize, len(payload))
	}
	if !bytes.Equal(complete.Payload(), payload) {
		t.Fatal("reassembled payload differs from source")
	}
}

func massiveTestHeader(bodyCount uint16, target uint16) Packet {
	packet := NewServerPacket(MassivePacketID)
	_ = packet.WriteBool(true)
	_ = packet.WriteUInt16(bodyCount)
	_ = packet.WriteUInt16(target)
	return *packet
}

func massiveTestBody(chunk []byte) Packet {
	packet := NewServerPacket(MassivePacketID)
	_ = packet.WriteBool(false)
	_ = packet.WriteBytes(chunk)
	return *packet
}

func patternedBytes(size int) []byte {
	bytes := make([]byte, size)
	for i := range bytes {
		bytes[i] = byte((i*31 + 17) % 251)
	}
	return bytes
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	bytes := make([]byte, 0, 10)
	for value > 0 {
		bytes = append(bytes, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(bytes)-1; left < right; left, right = left+1, right-1 {
		bytes[left], bytes[right] = bytes[right], bytes[left]
	}
	return string(bytes)
}
