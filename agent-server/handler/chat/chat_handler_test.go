package chat

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestBuildChatResultPreservesSuccessAndFailureWire(t *testing.T) {
	request := MessageRequest{ChatType: Party, ChatIndex: 7}
	for _, result := range []byte{1, 2} {
		packet, err := buildChatResult(request, result)
		if err != nil {
			t.Fatalf("buildChatResult(%d) error = %v", result, err)
		}
		if packet.MessageID != opcode.ChatResponse || !bytes.Equal(packet.Payload(), []byte{result, Party, 7}) {
			t.Fatalf("buildChatResult(%d) = (0x%04X, % X)", result, packet.MessageID, packet.Payload())
		}
	}
}

func TestBuildNamedChatUpdatePreservesWire(t *testing.T) {
	request := MessageRequest{ChatType: Party, Message: "Hello"}
	packet, err := buildNamedChatUpdate(request, "Khaled")
	if err != nil {
		t.Fatalf("buildNamedChatUpdate() error = %v", err)
	}
	want := []byte{Party, 6, 0, 'K', 'h', 'a', 'l', 'e', 'd', 5, 0, 'H', 'e', 'l', 'l', 'o'}
	if packet.MessageID != opcode.ChatUpdate || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("buildNamedChatUpdate() = (0x%04X, % X), want (0x%04X, % X)", packet.MessageID, packet.Payload(), opcode.ChatUpdate, want)
	}
}

func TestChatHandlerRejectsInvalidTextBeforeRouting(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		trailing bool
		wantText bool
	}{
		{name: "blank", message: "   ", wantText: true},
		{name: "NUL", message: "bad\x00message", wantText: true},
		{name: "trailing data", message: "valid", trailing: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := network.EmptyClientPacket()
			if err := packet.WriteByte(All); err != nil {
				t.Fatal(err)
			}
			if err := packet.WriteByte(1); err != nil {
				t.Fatal(err)
			}
			if err := packet.WriteString(test.message); err != nil {
				t.Fatal(err)
			}
			if test.trailing {
				if err := packet.WriteByte(0); err != nil {
					t.Fatal(err)
				}
			}

			err := (&ChatHandler{}).handlePacket(context.Background(), nil, packet)
			if err == nil {
				t.Fatal("invalid chat request was accepted")
			}
			if test.wantText && !errors.Is(err, vsro188.ErrInvalidText) {
				t.Fatalf("error = %v, want ErrInvalidText", err)
			}
		})
	}
}
