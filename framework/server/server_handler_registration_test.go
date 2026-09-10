package server

import (
	"bytes"
	"context"
	"errors"
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

func TestHandlerRegistryRejectsNilAndDuplicateHandlers(t *testing.T) {
	registry := NewHandlerRegistry()
	if err := registry.Register(0x7777, nil); !errors.Is(err, ErrNilSessionHandler) {
		t.Fatalf("Register(nil) error = %v, want ErrNilSessionHandler", err)
	}
	if err := registry.Register(0x7777, func(context.Context, *Session, network.Packet) error { return nil }); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(0x7777, func(context.Context, *Session, network.Packet) error { return nil }); !errors.Is(err, ErrHandlerAlreadyRegistered) {
		t.Fatalf("Register(duplicate) error = %v, want ErrHandlerAlreadyRegistered", err)
	}
}

func TestServerRegisterHandlerPanicsOnDuplicateOpcode(t *testing.T) {
	engine := NewEngine("127.0.0.1", 0, network.EncodingOptions{})
	engine.RegisterHandler(0x7777, func(context.Context, *Session, network.Packet) error { return nil })
	defer func() {
		if recover() == nil {
			t.Fatal("RegisterHandler(duplicate) did not panic")
		}
	}()
	engine.RegisterHandler(0x7777, func(context.Context, *Session, network.Packet) error { return nil })
}

func TestHandlerRegistryRecordsPerOpcodeErrorsAndPanics(t *testing.T) {
	registry := NewHandlerRegistry()
	const (
		errorOpcode = 0x7701
		panicOpcode = 0x7702
	)
	registeredError := errors.New("handler failed")
	if err := registry.Register(errorOpcode, func(context.Context, *Session, network.Packet) error {
		return registeredError
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(panicOpcode, func(context.Context, *Session, network.Packet) error {
		panic("handler panicked")
	}); err != nil {
		t.Fatal(err)
	}

	for opcode, wantError := range map[uint16]error{
		errorOpcode: registeredError,
		panicOpcode: nil,
	} {
		packet := network.EmptyPacket()
		packet.MessageID = opcode
		handled, err := registry.Dispatch(context.Background(), nil, packet)
		if !handled || err == nil {
			t.Fatalf("Dispatch(0x%04X) = %t, %v; want handled error", opcode, handled, err)
		}
		if wantError != nil && !errors.Is(err, wantError) {
			t.Fatalf("Dispatch(0x%04X) error = %v, want %v", opcode, err, wantError)
		}
		if opcode == panicOpcode {
			var panicError *HandlerPanicError
			if !errors.As(err, &panicError) {
				t.Fatalf("Dispatch(panic) error type = %T, want *HandlerPanicError", err)
			}
			if panicError.Opcode != panicOpcode || len(panicError.Stack) == 0 || len(panicError.Stack) > maxHandlerPanicStackBytes {
				t.Fatalf("panic evidence = opcode 0x%04X stack bytes %d", panicError.Opcode, len(panicError.Stack))
			}
			if !bytes.Contains(panicError.Stack, []byte("TestHandlerRegistryRecordsPerOpcodeErrorsAndPanics")) {
				t.Fatalf("panic stack does not name the panicking test:\n%s", panicError.Stack)
			}
		}
	}

	metrics := registry.Metrics()
	if metrics.Count != 2 || metrics.Errors != 2 || metrics.Panics != 1 {
		t.Fatalf("global metrics = %+v, want count=2 errors=2 panics=1", metrics)
	}
	if got := metrics.Opcodes[errorOpcode]; got.Count != 1 || got.Errors != 1 || got.Panics != 0 {
		t.Fatalf("error opcode metrics = %+v", got)
	}
	if got := metrics.Opcodes[panicOpcode]; got.Count != 1 || got.Errors != 1 || got.Panics != 1 {
		t.Fatalf("panic opcode metrics = %+v", got)
	}
}
