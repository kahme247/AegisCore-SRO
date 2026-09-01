package server

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
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
	dispatchCount    atomic.Uint64
	dispatchNanos    atomic.Uint64
	maxDispatchNanos atomic.Uint64
}

type DispatchMetrics struct {
	Count         uint64        `json:"count"`
	TotalDuration time.Duration `json:"totalDuration"`
	MaxDuration   time.Duration `json:"maxDuration"`
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{handlers: make(map[uint16]SessionHandler)}
}

func (r *HandlerRegistry) Register(opcode uint16, handler SessionHandler) {
	if handler == nil {
		return
	}
	r.mu.Lock()
	r.handlers[opcode] = handler
	r.mu.Unlock()
}

func (r *HandlerRegistry) Metrics() DispatchMetrics {
	if r == nil {
		return DispatchMetrics{}
	}
	return DispatchMetrics{Count: r.dispatchCount.Load(), TotalDuration: time.Duration(r.dispatchNanos.Load()), MaxDuration: time.Duration(r.maxDispatchNanos.Load())}
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
	r.mu.RUnlock()
	if handler == nil {
		return false, nil
	}
	startedAt := time.Now()
	defer func() {
		duration := uint64(time.Since(startedAt))
		r.dispatchCount.Add(1)
		r.dispatchNanos.Add(duration)
		for {
			current := r.maxDispatchNanos.Load()
			if duration <= current || r.maxDispatchNanos.CompareAndSwap(current, duration) {
				break
			}
		}
	}()

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("packet handler panic for opcode 0x%04X: %v", packet.MessageID, recovered)
		}
	}()
	return true, handler(ctx, session, packet)
}
