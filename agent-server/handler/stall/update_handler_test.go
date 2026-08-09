package stall

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestStallUpdateRejectsTruncatedItemPayloadWithoutPanic(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "test")
	defer session.Close()

	packet := network.EmptyClientPacket()
	packet.MessageID = 0x70BA
	if err := packet.WriteByte(StallUpdateItem); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}

	handler := &StallUpdateHandler{}
	if err := handler.handlePacket(context.Background(), session, packet); err == nil {
		t.Fatal("truncated stall item update was accepted")
	}
}

func TestStallStringInputsRejectInvalidText(t *testing.T) {
	tests := []struct {
		name   string
		handle func(server.PacketChannelData) error
	}{
		{name: "name", handle: updateStallName},
		{name: "message", handle: updateMessage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := network.EmptyClientPacket()
			if err := packet.WriteString(" \x00 "); err != nil {
				t.Fatal(err)
			}
			err := test.handle(server.PacketChannelData{Session: &server.Session{}, Packet: packet})
			if !errors.Is(err, vsro188.ErrInvalidText) {
				t.Fatalf("handler error = %v, want ErrInvalidText", err)
			}
		})
	}
}

func TestStallCreateRejectsInvalidNameBeforeSending(t *testing.T) {
	packet := network.EmptyClientPacket()
	if err := packet.WriteString(" \x00 "); err != nil {
		t.Fatal(err)
	}
	if err := handleStallCreate(context.Background(), nil, packet); !errors.Is(err, vsro188.ErrInvalidText) {
		t.Fatalf("handleStallCreate() error = %v, want ErrInvalidText", err)
	}
}
