package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/ferdoran/go-sro/framework/security/blowfish"
)

func TestFrameDecoderRetainsFragmentedHeaderAndPayload(t *testing.T) {
	frame := testPlainFrame(0x5000, []byte{0x10, 0x20, 0x30, 0x40})
	decoder := NewFrameDecoder()

	frames, err := decoder.Feed(frame[:1])
	assertNoFrames(t, frames, err)
	frames, err = decoder.Push(frame[1 : int(HeaderSize)+2])
	assertNoFrames(t, frames, err)

	frames, err = decoder.Push(frame[int(HeaderSize)+2:])
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	assertFramesEqual(t, frames, [][]byte{frame})

	if err := decoder.Finish(); err != nil {
		t.Fatalf("Finish() error = %v, want nil", err)
	}
}

func TestFrameDecoderEmitsCoalescedFramesInOrder(t *testing.T) {
	first := testPlainFrame(0x5000, []byte{0x01, 0x02})
	second := testPlainFrame(0x600D, []byte{0x03, 0x04, 0x05})
	decoder := NewFrameDecoder()

	frames, err := decoder.Push(append(first, second...))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	assertFramesEqual(t, frames, [][]byte{first, second})
}

func TestFrameDecoderEncryptedFragmentedAndCoalescedPayloadOver255(t *testing.T) {
	const encryptedPayloadSize = 300

	encrypted := testEncryptedFrame(encryptedPayloadSize)
	following := testPlainFrame(0x9000, []byte{0xAA, 0xBB})
	decoder := NewFrameDecoder()

	if got, want := len(encrypted), blowfish.GetBufferLength(encryptedPayloadSize+int(EncryptSize))+int(EncryptOffset); got != want {
		t.Fatalf("encrypted wire length = %d, want %d", got, want)
	}
	frames, err := decoder.Feed(encrypted[:50])
	assertNoFrames(t, frames, err)

	frames, err = decoder.Feed(append(encrypted[50:], following...))
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	assertFramesEqual(t, frames, [][]byte{encrypted, following})
}

func TestFrameDecoderRejectsPayloadAboveConfiguredMax(t *testing.T) {
	decoder, err := NewFrameDecoderWithMaxPayload(8)
	if err != nil {
		t.Fatalf("NewFrameDecoderWithMaxPayload() error = %v", err)
	}

	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint16(header[:2], uint16(9)|EncryptMask)
	frames, err := decoder.Push(header)
	if len(frames) != 0 {
		t.Fatalf("Push() returned %d frames, want 0", len(frames))
	}
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("Push() error = %v, want ErrFrameTooLarge", err)
	}

	_, err = decoder.Push(nil)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("Push() after terminal error = %v, want ErrFrameTooLarge", err)
	}
}

func TestFrameDecoderFinishReportsTruncatedFrame(t *testing.T) {
	frame := testPlainFrame(0x5000, []byte{0x01, 0x02, 0x03})
	decoder := NewFrameDecoder()
	frames, err := decoder.Push(frame[:int(HeaderSize)+1])
	assertNoFrames(t, frames, err)

	err = decoder.Close()
	if !errors.Is(err, ErrTruncatedFrame) {
		t.Fatalf("Close() error = %v, want ErrTruncatedFrame", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Close() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if err := decoder.Finish(); !errors.Is(err, ErrTruncatedFrame) {
		t.Fatalf("Finish() after Close() error = %v, want ErrTruncatedFrame", err)
	}
}

func TestFrameDecoderEmittedFramesOwnTheirBackingStorage(t *testing.T) {
	first := testPlainFrame(0x5000, []byte{0x01, 0x02})
	second := testPlainFrame(0x600D, []byte{0x03, 0x04})
	decoder := NewFrameDecoder()

	source := append(append([]byte{}, first...), second...)
	frames, err := decoder.Push(source)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	assertFramesEqual(t, frames, [][]byte{first, second})

	frames[0][DataOffset] = 0xFF
	if source[DataOffset] != first[DataOffset] {
		t.Fatalf("first emitted frame aliases source buffer")
	}
	if frames[1][DataOffset] != second[DataOffset] {
		t.Fatalf("emitted frames alias each other")
	}
}

func testPlainFrame(messageID uint16, payload []byte) []byte {
	frame := make([]byte, int(HeaderSize)+len(payload))
	binary.LittleEndian.PutUint16(frame[:2], uint16(len(payload)))
	binary.LittleEndian.PutUint16(frame[IDOffset:IDOffset+2], messageID)
	frame[SequenceOffset] = 0x21
	frame[CRCOffset] = 0x42
	copy(frame[DataOffset:], payload)
	return frame
}

func testEncryptedFrame(payloadSize int) []byte {
	frameSize := blowfish.GetBufferLength(payloadSize+int(EncryptSize)) + int(EncryptOffset)
	frame := make([]byte, frameSize)
	binary.LittleEndian.PutUint16(frame[:2], uint16(payloadSize)|EncryptMask)
	for i := int(EncryptOffset); i < len(frame); i++ {
		frame[i] = byte((i * 37) % 251)
	}
	return frame
}

func assertNoFrames(t *testing.T, frames [][]byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("decoder error = %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("decoder returned %d frames, want 0", len(frames))
	}
}

func assertFramesEqual(t *testing.T, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("frame count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("frame %d = %X, want %X", i, got[i], want[i])
		}
	}
}
