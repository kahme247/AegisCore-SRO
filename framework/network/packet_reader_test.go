package network

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestPacketReaderRetainsFragmentsAcrossCalls(t *testing.T) {
	first := testPlainFrame(0x5000, []byte{0x01, 0x02})
	second := testPlainFrame(0x600D, []byte{0x03, 0x04, 0x05})
	reader := NewPacketReader(&scriptedReader{steps: []readStep{
		{data: append(append([]byte{}, first...), second[:3]...)},
		{data: second[3:]},
	}})

	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("first ReadPackets() error = %v", err)
	}
	assertPacketFrames(t, packets, [][]byte{first})

	packets, err = reader.ReadPackets()
	if err != nil {
		t.Fatalf("second ReadPackets() error = %v", err)
	}
	assertPacketFrames(t, packets, [][]byte{second})
}

func TestPacketReaderEmitsCoalescedFrames(t *testing.T) {
	first := testPlainFrame(0x5000, []byte{0x10})
	second := testPlainFrame(0x600D, []byte{0x20, 0x30})
	reader := NewPacketReader(&scriptedReader{steps: []readStep{{data: append(first, second...)}}})

	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("ReadPackets() error = %v", err)
	}
	assertPacketFrames(t, packets, [][]byte{first, second})
}

func TestPacketReaderDefersEOFUntilCompletedPacketsAreReturned(t *testing.T) {
	frame := testPlainFrame(0x5000, []byte{0x10, 0x20})
	reader := NewPacketReader(&scriptedReader{steps: []readStep{{data: frame, err: io.EOF}}})

	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("ReadPackets() returned final packet with error = %v", err)
	}
	assertPacketFrames(t, packets, [][]byte{frame})

	packets, err = reader.ReadPackets()
	if len(packets) != 0 {
		t.Fatalf("terminal ReadPackets() returned %d packets, want 0", len(packets))
	}
	if err != io.EOF {
		t.Fatalf("terminal ReadPackets() error = %v, want io.EOF", err)
	}
}

func TestPacketReaderReportsUnexpectedEOFForTruncatedFrame(t *testing.T) {
	frame := testPlainFrame(0x5000, []byte{0x10, 0x20, 0x30})
	reader := NewPacketReader(&scriptedReader{steps: []readStep{{data: frame[:int(HeaderSize)+1], err: io.EOF}}})

	packets, err := reader.ReadPackets()
	if len(packets) != 0 {
		t.Fatalf("ReadPackets() returned %d packets, want 0", len(packets))
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadPackets() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if !errors.Is(err, ErrTruncatedFrame) {
		t.Fatalf("ReadPackets() error = %v, want ErrTruncatedFrame", err)
	}
}

func TestPacketReaderParsesEncryptedPayloadOver255Bytes(t *testing.T) {
	const payloadSize = 300
	frame := testEncryptedFrame(payloadSize)
	reader := NewPacketReader(&scriptedReader{steps: []readStep{{data: frame}}})

	packets, err := reader.ReadPackets()
	if err != nil {
		t.Fatalf("ReadPackets() error = %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("ReadPackets() returned %d packets, want 1", len(packets))
	}
	if !packets[0].Encrypted {
		t.Fatalf("packet Encrypted = false, want true")
	}
	if packets[0].DataSize != payloadSize {
		t.Fatalf("packet DataSize = %d, want %d", packets[0].DataSize, payloadSize)
	}
	if !bytes.Equal(packets[0].Buffer, frame) {
		t.Fatalf("packet buffer does not preserve the complete encrypted frame")
	}
}

func TestPacketReaderRejectsNoProgress(t *testing.T) {
	reader := NewPacketReader(&scriptedReader{steps: []readStep{{}}})

	packets, err := reader.ReadPackets()
	if len(packets) != 0 {
		t.Fatalf("ReadPackets() returned %d packets, want 0", len(packets))
	}
	if err != io.ErrNoProgress {
		t.Fatalf("ReadPackets() error = %v, want io.ErrNoProgress", err)
	}
}

type readStep struct {
	data []byte
	err  error
}

type scriptedReader struct {
	steps []readStep
}

func (r *scriptedReader) Read(buffer []byte) (int, error) {
	if len(r.steps) == 0 {
		return 0, io.EOF
	}

	step := r.steps[0]
	r.steps = r.steps[1:]
	if len(step.data) > len(buffer) {
		panic("scripted reader step exceeds PacketReader buffer")
	}
	return copy(buffer, step.data), step.err
}

func assertPacketFrames(t *testing.T, packets []Packet, frames [][]byte) {
	t.Helper()
	if len(packets) != len(frames) {
		t.Fatalf("packet count = %d, want %d", len(packets), len(frames))
	}
	for i := range frames {
		if !bytes.Equal(packets[i].Buffer, frames[i]) {
			t.Errorf("packet %d buffer = %X, want %X", i, packets[i].Buffer, frames[i])
		}
	}
}
