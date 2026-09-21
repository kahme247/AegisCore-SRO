package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Harvi8/AegisCore/download-server/catalog"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type testFile struct{ *bytes.Reader }

func (testFile) Close() error { return nil }

type testCatalog struct {
	data []byte
	size uint64
	err  error
}

func (c testCatalog) Open(uint32) (catalog.PreparedFile, uint64, error) {
	if c.err != nil {
		return nil, 0, c.err
	}
	return testFile{bytes.NewReader(c.data)}, c.size, nil
}

type sentPacket struct {
	opcode  uint16
	payload []byte
}

type recordingSender struct {
	packets []sentPacket
	sendErr error
}

func (s *recordingSender) Send(packet network.Packet) error {
	s.packets = append(s.packets, sentPacket{opcode: packet.MessageID, payload: append([]byte(nil), packet.Payload()...)})
	return s.sendErr
}

func requestPacket(t *testing.T, id, offset uint32, trailing ...byte) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.DownloadFileRequest
	if err := packet.WriteUInt32(id); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteUInt32(offset); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteBytes(trailing); err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestFileRequestStreamsFromResumeOffsetAndCompletes(t *testing.T) {
	var audits []TransferAudit
	handler := FileRequestHandler{
		Catalog: testCatalog{data: []byte("abcdefgh"), size: 8}, ChunkSize: 3,
		Audit: func(audit TransferAudit) { audits = append(audits, audit) },
	}
	sender := &recordingSender{}
	if err := handler.Transfer(context.Background(), sender, requestPacket(t, 9, 2)); err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if len(sender.packets) != 3 {
		t.Fatalf("sent %d packets, want 3", len(sender.packets))
	}
	if sender.packets[0].opcode != opcode.DownloadFileChunk || string(sender.packets[0].payload) != "cde" {
		t.Fatalf("first chunk = 0x%04X %q", sender.packets[0].opcode, sender.packets[0].payload)
	}
	if sender.packets[1].opcode != opcode.DownloadFileChunk || string(sender.packets[1].payload) != "fgh" {
		t.Fatalf("second chunk = 0x%04X %q", sender.packets[1].opcode, sender.packets[1].payload)
	}
	if sender.packets[2].opcode != opcode.DownloadFileCompleted || !bytes.Equal(sender.packets[2].payload, []byte{1}) {
		t.Fatalf("completion = 0x%04X %v", sender.packets[2].opcode, sender.packets[2].payload)
	}
	if len(audits) != 1 || !audits[0].Success || audits[0].Reason != "success" || audits[0].FileID != 9 || audits[0].Offset != 2 || audits[0].BytesSent != 6 {
		t.Fatalf("audit = %+v", audits)
	}
}

func TestFileRequestReturnsProtocolFailureForUnknownIDAndBadOffset(t *testing.T) {
	tests := []FileRequestHandler{
		{Catalog: testCatalog{err: errors.New("unknown")}},
		{Catalog: testCatalog{data: []byte("abc"), size: 3}},
	}
	requests := []network.Packet{requestPacket(t, 99, 0), requestPacket(t, 1, 4)}
	for index := range tests {
		sender := &recordingSender{}
		if err := tests[index].Transfer(context.Background(), sender, requests[index]); err != nil {
			t.Fatalf("Transfer() error = %v", err)
		}
		if len(sender.packets) != 1 || sender.packets[0].opcode != opcode.DownloadFileCompleted || !bytes.Equal(sender.packets[0].payload, []byte{0}) {
			t.Fatalf("failure packets = %#v", sender.packets)
		}
	}
}

func TestFileRequestJoinsCatalogAndCompletionFailures(t *testing.T) {
	catalogErr := errors.New("catalog unavailable")
	completionErr := errors.New("completion unavailable")
	handler := FileRequestHandler{Catalog: testCatalog{err: catalogErr}}
	sender := &recordingSender{sendErr: completionErr}

	err := handler.Transfer(context.Background(), sender, requestPacket(t, 7, 0))
	if !errors.Is(err, catalogErr) || !errors.Is(err, completionErr) {
		t.Fatalf("Transfer() error = %v, want both catalog and completion failures", err)
	}
	if got, want := err.Error(), catalogErr.Error()+"\n"+completionErr.Error(); got != want {
		t.Fatalf("Transfer() error order = %q, want %q", got, want)
	}
	if len(sender.packets) != 1 || sender.packets[0].opcode != opcode.DownloadFileCompleted || !bytes.Equal(sender.packets[0].payload, []byte{0}) {
		t.Fatalf("failure packets = %#v", sender.packets)
	}
}

func TestFileRequestAuditsCatalogFailureAndConcurrencyRejection(t *testing.T) {
	limiter, err := NewTransferLimiter(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	release, err := limiter.Acquire("unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var audits []TransferAudit
	handler := FileRequestHandler{
		Catalog: testCatalog{data: []byte("abc"), size: 3}, Limiter: limiter,
		Audit: func(audit TransferAudit) { audits = append(audits, audit) },
	}
	sender := &recordingSender{}
	if err := handler.Transfer(context.Background(), sender, requestPacket(t, 7, 0)); err != nil {
		t.Fatalf("limited Transfer() error = %v", err)
	}
	if len(sender.packets) != 1 || !bytes.Equal(sender.packets[0].payload, []byte{0}) {
		t.Fatalf("limited completion = %#v", sender.packets)
	}
	if len(audits) != 1 || audits[0].Success || audits[0].Reason != "concurrency-limit" || audits[0].FileID != 7 {
		t.Fatalf("limited audit = %+v", audits)
	}

	release()
	audits = nil
	sender = &recordingSender{}
	handler.Catalog = testCatalog{err: errors.New("unknown")}
	if err := handler.Transfer(context.Background(), sender, requestPacket(t, 8, 0)); err != nil {
		t.Fatalf("catalog Transfer() error = %v", err)
	}
	if len(audits) != 1 || audits[0].Success || audits[0].Reason != "catalog" || audits[0].FileID != 8 {
		t.Fatalf("catalog audit = %+v", audits)
	}
}

func TestFileRequestRejectsMalformedPayloadAndOversizedChunk(t *testing.T) {
	handler := FileRequestHandler{Catalog: testCatalog{data: []byte("abc"), size: 3}}
	sender := &recordingSender{}
	if err := handler.Transfer(context.Background(), sender, requestPacket(t, 1, 0, 0xFF)); err == nil {
		t.Fatal("Transfer() accepted trailing request data")
	}
	handler.ChunkSize = network.DefaultMaxFramePayload + 1
	if err := handler.Transfer(context.Background(), sender, requestPacket(t, 1, 0)); err == nil {
		t.Fatal("Transfer() accepted an oversized chunk")
	}
}

func TestFileRequestDetectsShortReadAndCancellation(t *testing.T) {
	handler := FileRequestHandler{Catalog: testCatalog{data: []byte("abc"), size: 4}}
	sender := &recordingSender{}
	if err := handler.Transfer(context.Background(), sender, requestPacket(t, 1, 0)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Transfer() error = %v, want unexpected EOF", err)
	}
	if got := sender.packets[len(sender.packets)-1]; got.opcode != opcode.DownloadFileCompleted || !bytes.Equal(got.payload, []byte{0}) {
		t.Fatalf("short-read completion = %#v", got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	sender = &recordingSender{}
	handler = FileRequestHandler{Catalog: testCatalog{data: []byte("abc"), size: 3}}
	if err := handler.Transfer(cancelled, sender, requestPacket(t, 1, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Transfer() error = %v, want context canceled", err)
	}
}
