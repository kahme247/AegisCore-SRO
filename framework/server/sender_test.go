package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSenderCompletesShortWritesInOrder(t *testing.T) {
	writer := &shortDeadlineWriter{chunkSize: 2}
	sender := NewSender(writer, marshalPacket)
	defer sender.Close()

	first := network.NewServerPacket(0x5000)
	_ = first.WriteBytes([]byte{0x01, 0x02})
	second := network.NewServerPacket(0x600D)
	_ = second.WriteBytes([]byte{0x03})
	if err := sender.Send(*first); err != nil {
		t.Fatalf("first Send() error = %v", err)
	}
	if err := sender.Send(*second); err != nil {
		t.Fatalf("second Send() error = %v", err)
	}

	wantFirst, _ := first.MarshalBinary()
	wantSecond, _ := second.MarshalBinary()
	if got, want := writer.Bytes(), append(wantFirst, wantSecond...); !bytes.Equal(got, want) {
		t.Fatalf("written bytes = %X, want %X", got, want)
	}
	if writer.deadlineCalls < 4 {
		t.Fatalf("SetWriteDeadline called %d times, want at least one set/clear per frame", writer.deadlineCalls)
	}
}

func TestSenderWritesMassiveBatchBeforeNextJob(t *testing.T) {
	writer := &shortDeadlineWriter{chunkSize: 1024}
	sender := NewSender(writer, marshalPacket)
	defer sender.Close()

	source := network.NewServerPacket(0xA100)
	_ = source.WriteBytes(bytes.Repeat([]byte{0xA5}, network.MassiveBodyPayloadSize+1))
	if err := sender.SendMassive(*source); err != nil {
		t.Fatalf("SendMassive() error = %v", err)
	}
	after := network.NewServerPacket(0x5000)
	_ = after.WriteByte(0x7F)
	if err := sender.Send(*after); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	decoder := network.NewFrameDecoder()
	frames, err := decoder.Push(writer.Bytes())
	if err != nil {
		t.Fatalf("FrameDecoder.Push() error = %v", err)
	}
	if got, want := len(frames), 4; got != want {
		t.Fatalf("frame count = %d, want %d", got, want)
	}
	for index := 0; index < 3; index++ {
		if got := binary.LittleEndian.Uint16(frames[index][network.IDOffset : network.IDOffset+2]); got != network.MassivePacketID {
			t.Fatalf("frame %d opcode = 0x%04X, want massive wrapper", index, got)
		}
	}
	if got := binary.LittleEndian.Uint16(frames[3][network.IDOffset : network.IDOffset+2]); got != after.MessageID {
		t.Fatalf("final opcode = 0x%04X, want 0x%04X", got, after.MessageID)
	}
}

func TestSenderWritesOrdinaryBatchInOrder(t *testing.T) {
	writer := &shortDeadlineWriter{chunkSize: 1024}
	sender := NewSender(writer, marshalPacket)
	defer sender.Close()

	packets := []network.Packet{
		*network.NewServerPacket(0x3017),
		*network.NewServerPacket(0x3019),
		*network.NewServerPacket(0x3018),
	}
	if err := sender.SendBatch(packets); err != nil {
		t.Fatalf("SendBatch() error = %v", err)
	}
	decoder := network.NewFrameDecoder()
	frames, err := decoder.Push(writer.Bytes())
	if err != nil {
		t.Fatalf("FrameDecoder.Push() error = %v", err)
	}
	if len(frames) != len(packets) {
		t.Fatalf("frame count = %d, want %d", len(frames), len(packets))
	}
	for index, packet := range packets {
		if got := binary.LittleEndian.Uint16(frames[index][network.IDOffset : network.IDOffset+2]); got != packet.MessageID {
			t.Fatalf("frame %d opcode = 0x%04X, want 0x%04X", index, got, packet.MessageID)
		}
	}
}

func TestSenderReturnsClosedError(t *testing.T) {
	sender := NewSender(&shortDeadlineWriter{chunkSize: 1}, marshalPacket)
	sender.Close()
	packet := network.NewServerPacket(0x5000)
	if err := sender.Send(*packet); !errors.Is(err, ErrSenderClosed) {
		t.Fatalf("Send() error = %v, want ErrSenderClosed", err)
	}
}

func TestSenderTrySendRejectsFullQueueAndReportsStats(t *testing.T) {
	writer := &blockingDeadlineWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	sender := NewSender(writer, marshalPacket)
	defer func() {
		sender.Close()
		sender.Wait()
	}()
	packet := network.NewServerPacket(0xB021)

	if err := sender.TrySend(*packet); err != nil {
		t.Fatalf("initial TrySend() error = %v", err)
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("sender worker did not start blocked write")
	}
	for index := 0; index < cap(sender.queue); index++ {
		if err := sender.TrySend(*packet); err != nil {
			t.Fatalf("TrySend() filling slot %d error = %v", index, err)
		}
	}
	if err := sender.TrySend(*packet); !errors.Is(err, ErrSenderQueueFull) {
		t.Fatalf("TrySend() full queue error = %v, want %v", err, ErrSenderQueueFull)
	}
	stats := sender.Stats()
	if stats.Queued != stats.QueueCapacity || stats.Enqueued != uint64(stats.QueueCapacity+1) || stats.Rejected != 1 {
		t.Fatalf("full queue stats = %+v", stats)
	}

	close(writer.release)
	if err := sender.Send(*packet); err != nil {
		t.Fatalf("barrier Send() error = %v", err)
	}
	stats = sender.Stats()
	if stats.Queued != 0 || stats.Completed != stats.Enqueued || stats.Failed != 0 {
		t.Fatalf("drained queue stats = %+v", stats)
	}
}

func marshalPacket(packet network.Packet) ([]byte, error) {
	return packet.MarshalBinary()
}

type shortDeadlineWriter struct {
	mu            sync.Mutex
	chunkSize     int
	buffer        bytes.Buffer
	deadlineCalls int
}

type blockingDeadlineWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingDeadlineWriter) Write(bytes []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(bytes), nil
}

func (*blockingDeadlineWriter) SetWriteDeadline(time.Time) error { return nil }

func (w *shortDeadlineWriter) Write(bytes []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := w.chunkSize
	if count > len(bytes) {
		count = len(bytes)
	}
	_, _ = w.buffer.Write(bytes[:count])
	return count, nil
}

func (w *shortDeadlineWriter) SetWriteDeadline(time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadlineCalls++
	return nil
}

func (w *shortDeadlineWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...)
}
