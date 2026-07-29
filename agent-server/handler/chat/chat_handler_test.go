package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/protocol/vsro188"
)

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
