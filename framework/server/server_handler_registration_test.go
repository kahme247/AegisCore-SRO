package server

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestServerRegisterHandlerIsEngineScopedAndOrdered(t *testing.T) {
	first := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	second := NewEngine("127.0.0.1", 0, network.EncodingOptions{})

	seen := make([]byte, 0, 2)
	first.RegisterHandler(0xA100, func(_ context.Context, _ *Session, packet network.Packet) error {
		value, err := packet.ReadByte()
		if err != nil {
			return err
		}
		seen = append(seen, value)
		return nil
	})

	for _, value := range []byte{0x01, 0x02} {
		packet := network.EmptyClientPacket()
		packet.MessageID = 0xA100
		if err := packet.WriteByte(value); err != nil {
			t.Fatalf("WriteByte() error = %v", err)
		}
		handled, err := first.handlers.Dispatch(context.Background(), nil, packet)
		if err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
		if !handled {
			t.Fatal("registered handler was not dispatched")
		}
	}
	if got, want := string(seen), string([]byte{0x01, 0x02}); got != want {
		t.Fatalf("callback order = %v, want %v", seen, []byte{0x01, 0x02})
	}

	packet := network.EmptyClientPacket()
	packet.MessageID = 0xA100
	handled, err := second.handlers.Dispatch(context.Background(), nil, packet)
	if err != nil {
		t.Fatalf("second engine Dispatch() error = %v", err)
	}
	if handled {
		t.Fatal("handler leaked into another engine registry")
	}
}

func TestHandlerRegistryRecordsDispatchMetrics(t *testing.T) {
	registry := NewHandlerRegistry()
	registry.Register(0x7777, func(context.Context, *Session, network.Packet) error { return nil })
	packet := network.EmptyPacket()
	packet.MessageID = 0x7777
	if handled, err := registry.Dispatch(context.Background(), nil, packet); err != nil || !handled {
		t.Fatalf("Dispatch() = %t, %v", handled, err)
	}
	metrics := registry.Metrics()
	if metrics.Count != 1 || metrics.TotalDuration < 0 || metrics.MaxDuration < 0 {
		t.Fatalf("Metrics() = %+v", metrics)
	}
}
