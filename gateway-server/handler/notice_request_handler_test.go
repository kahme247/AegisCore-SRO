package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/gateway-server/config"
	"github.com/ferdoran/go-sro/gateway-server/db"
)

func TestParseNoticeRequestRequiresExactlyOneContentID(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.NoticeRequest
	if err := packet.WriteByte(122); err != nil {
		t.Fatal(err)
	}
	contentID, err := parseNoticeRequest(packet)
	if err != nil {
		t.Fatalf("parseNoticeRequest() error = %v", err)
	}
	if contentID != 122 {
		t.Fatalf("content ID = %d, want 122", contentID)
	}

	trailing := network.EmptyClientPacket()
	if err := trailing.WriteByte(122); err != nil {
		t.Fatal(err)
	}
	if err := trailing.WriteByte(1); err != nil {
		t.Fatal(err)
	}
	if _, err := parseNoticeRequest(trailing); err == nil {
		t.Fatal("parseNoticeRequest() error = nil, want trailing data rejection")
	}
}

func TestBuildNoticeResponseCapsAt255AndWritesNanoseconds(t *testing.T) {
	timestamp := time.Date(2026, time.July, 13, 19, 42, 17, 987654321, time.UTC)
	notices := make([]db.Notice, 256)
	for index := range notices {
		notices[index] = db.Notice{
			Id:      index + 1,
			Subject: fmt.Sprintf("Notice %03d", index),
			Article: "This message makes the assembled list large enough to exercise shared massive framing.",
			Ctime:   timestamp,
		}
	}

	response, err := BuildNoticeResponse(notices)
	if err != nil {
		t.Fatalf("BuildNoticeResponse() error = %v", err)
	}
	if response.MessageID != opcode.NoticeResponse {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", response.MessageID, opcode.NoticeResponse)
	}
	count, err := response.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if count != 255 {
		t.Fatalf("notice count = %d, want 255", count)
	}
	subject, err := response.ReadString()
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Notice 000" {
		t.Fatalf("first subject = %q, want Notice 000", subject)
	}
	if _, err := response.ReadString(); err != nil {
		t.Fatal(err)
	}
	for field := 0; field < 6; field++ {
		if _, err := response.ReadUInt16(); err != nil {
			t.Fatal(err)
		}
	}
	nanoseconds, err := response.ReadUInt32()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := nanoseconds, uint32(timestamp.Nanosecond()); got != want {
		t.Fatalf("nanoseconds = %d, want %d", got, want)
	}

	frames, err := network.NewDefaultMassiveCodec().EncodeMassive(response)
	if err != nil {
		t.Fatalf("EncodeMassive() error = %v", err)
	}
	if len(frames) <= 2 {
		t.Fatalf("massive frame count = %d, want header plus multiple bodies", len(frames))
	}
	assembler := network.NewMassiveAssembler(network.NewDefaultMassiveCodec())
	var reassembled network.Packet
	for _, frame := range frames {
		packet, done, err := assembler.ProcessFrame(frame)
		if err != nil {
			t.Fatalf("ProcessFrame() error = %v", err)
		}
		if done {
			reassembled = packet
		}
	}
	if reassembled.MessageID != opcode.NoticeResponse {
		t.Fatalf("reassembled opcode = 0x%04X, want 0x%04X", reassembled.MessageID, opcode.NoticeResponse)
	}
	if got, want := reassembled.Payload(), response.Payload(); !bytes.Equal(got, want) {
		t.Fatal("reassembled notice payload differs from source payload")
	}
}

func TestNoticeRequestHandlerUsesInjectedCatalogAndPropagatesFailures(t *testing.T) {
	want := errors.New("catalog unavailable")
	contextKey := struct{}{}
	requestContext := context.WithValue(context.Background(), contextKey, "request")
	var receivedContext context.Context
	handler := NewNoticeRequestHandler(
		func() (config.ProtocolProfile, error) {
			return config.ProtocolProfile{Name: config.VSRO188Profile, ContentID: 122, ModuleName: "SRO_Client", Version: 1}, nil
		},
		db.NoticeCatalogFunc(func(ctx context.Context) ([]db.Notice, error) {
			receivedContext = ctx
			return nil, want
		}),
	)
	packet := network.EmptyClientPacket()
	if err := packet.WriteByte(122); err != nil {
		t.Fatal(err)
	}
	err := handler.Handle(requestContext, nil, packet)
	if !errors.Is(err, want) {
		t.Fatalf("Handle error = %v, want wrapped %v", err, want)
	}
	if receivedContext != requestContext {
		t.Fatal("handler did not pass the session context to the injected catalog")
	}
}
