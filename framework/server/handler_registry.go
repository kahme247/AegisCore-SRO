package server

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

// SessionHandler is invoked by a session's single inbound worker, preserving
// command order for that session. It returns an error for malformed or invalid
// client input; Session then closes the connection rather than continuing in a
// half-valid state.
type SessionHandler func(context.Context, *Session, network.Packet) error

// HandlerRegistry is the replacement boundary for global opcode queues. It is
// safe for startup registration and concurrent session dispatch.
type HandlerRegistry struct {
	mu               sync.RWMutex
	handlers         map[uint16]SessionHandler
	opcodeMetrics    map[uint16]*dispatchCounters
	dispatchCount    atomic.Uint64
	dispatchNanos    atomic.Uint64
	maxDispatchNanos atomic.Uint64
	dispatchErrors   atomic.Uint64
	dispatchPanics   atomic.Uint64
}

type DispatchMetrics struct {
	Count         uint64                           `json:"count"`
	TotalDuration time.Duration                    `json:"totalDuration"`
	MaxDuration   time.Duration                    `json:"maxDuration"`
	Errors        uint64                           `json:"errors"`
	Panics        uint64                           `json:"panics"`
	Opcodes       map[uint16]OpcodeDispatchMetrics `json:"opcodes,omitempty"`
}

type OpcodeDispatchMetrics struct {
	Count         uint64        `json:"count"`
	TotalDuration time.Duration `json:"totalDuration"`
	MaxDuration   time.Duration `json:"maxDuration"`
	Errors        uint64        `json:"errors"`
	Panics        uint64        `json:"panics"`
}

type dispatchCounters struct {
	count    atomic.Uint64
	nanos    atomic.Uint64
	maxNanos atomic.Uint64
	errors   atomic.Uint64
	panics   atomic.Uint64
}

var (
	ErrNilSessionHandler        = errors.New("session handler is nil")
	ErrHandlerAlreadyRegistered = errors.New("session handler is already registered")
)

const maxHandlerPanicStackBytes = 8 * 1024

// HandlerPanicError preserves bounded call-stack evidence for a recovered
// application handler panic. Session handling still fails closed, while the
// flight recorder and logs retain the function that violated the boundary.
type HandlerPanicError struct {
	Opcode uint16
	Value  any
	Stack  []byte
}

func (e *HandlerPanicError) Error() string {
	if len(e.Stack) == 0 {
		return fmt.Sprintf("packet handler panic for opcode 0x%04X: %v", e.Opcode, e.Value)
	}
	return fmt.Sprintf("packet handler panic for opcode 0x%04X: %v\n%s", e.Opcode, e.Value, e.Stack)
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers:      make(map[uint16]SessionHandler),
		opcodeMetrics: make(map[uint16]*dispatchCounters),
	}
}

func (r *HandlerRegistry) Register(opcode uint16, handler SessionHandler) error {
	if handler == nil {
		return fmt.Errorf("%w for opcode 0x%04X", ErrNilSessionHandler, opcode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[opcode]; exists {
		return fmt.Errorf("%w: opcode 0x%04X", ErrHandlerAlreadyRegistered, opcode)
	}
	r.handlers[opcode] = handler
	r.opcodeMetrics[opcode] = &dispatchCounters{}
	return nil
}

func (r *HandlerRegistry) Metrics() DispatchMetrics {
	if r == nil {
		return DispatchMetrics{}
	}
	r.mu.RLock()
	opcodes := make(map[uint16]OpcodeDispatchMetrics, len(r.opcodeMetrics))
	for opcode, counters := range r.opcodeMetrics {
		opcodes[opcode] = counters.snapshot()
	}
	r.mu.RUnlock()
	return DispatchMetrics{
		Count:         r.dispatchCount.Load(),
		TotalDuration: time.Duration(r.dispatchNanos.Load()),
		MaxDuration:   time.Duration(r.maxDispatchNanos.Load()),
		Errors:        r.dispatchErrors.Load(),
		Panics:        r.dispatchPanics.Load(),
		Opcodes:       opcodes,
	}
}

func (c *dispatchCounters) snapshot() OpcodeDispatchMetrics {
	if c == nil {
		return OpcodeDispatchMetrics{}
	}
	return OpcodeDispatchMetrics{
		Count:         c.count.Load(),
		TotalDuration: time.Duration(c.nanos.Load()),
		MaxDuration:   time.Duration(c.maxNanos.Load()),
		Errors:        c.errors.Load(),
		Panics:        c.panics.Load(),
	}
}

func (r *HandlerRegistry) HandlerName(opcode uint16) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	handler := r.handlers[opcode]
	r.mu.RUnlock()
	if handler == nil {
		return ""
	}
	function := runtime.FuncForPC(reflect.ValueOf(handler).Pointer())
	if function == nil {
		return ""
	}
	return function.Name()
}

func (r *HandlerRegistry) Dispatch(ctx context.Context, session *Session, packet network.Packet) (handled bool, err error) {
	r.mu.RLock()
	handler := r.handlers[packet.MessageID]
	counters := r.opcodeMetrics[packet.MessageID]
	r.mu.RUnlock()
	if handler == nil {
		return false, nil
	}
	startedAt := time.Now()
	panicked := false
	defer func() {
		duration := uint64(time.Since(startedAt))
		r.dispatchCount.Add(1)
		r.dispatchNanos.Add(duration)
		updateMaxDuration(&r.maxDispatchNanos, duration)
		if err != nil {
			r.dispatchErrors.Add(1)
		}
		if panicked {
			r.dispatchPanics.Add(1)
		}
		if counters != nil {
			counters.count.Add(1)
			counters.nanos.Add(duration)
			updateMaxDuration(&counters.maxNanos, duration)
			if err != nil {
				counters.errors.Add(1)
			}
			if panicked {
				counters.panics.Add(1)
			}
		}
	}()

	defer func() {
		if recovered := recover(); recovered != nil {
			panicked = true
			stack := debug.Stack()
			if len(stack) > maxHandlerPanicStackBytes {
				stack = stack[:maxHandlerPanicStackBytes]
			}
			err = &HandlerPanicError{Opcode: packet.MessageID, Value: recovered, Stack: append([]byte(nil), stack...)}
		}
	}()
	handled = true
	err = handler(ctx, session, packet)
	return handled, err
}

func updateMaxDuration(target *atomic.Uint64, duration uint64) {
	for {
		current := target.Load()
		if duration <= current || target.CompareAndSwap(current, duration) {
			return
		}
	}
}
