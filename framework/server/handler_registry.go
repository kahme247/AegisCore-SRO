package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/framework/network"
)

// SessionHandler is invoked by a session's single inbound worker, preserving
// command order for that session. It returns an error for malformed or invalid
// client input; Session then closes the connection rather than continuing in a
// half-valid state.
type SessionHandler func(context.Context, *Session, network.Packet) error

// HandlerRegistry is the replacement boundary for global opcode queues. It is
// safe for startup registration and concurrent session dispatch.
type HandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[uint16]SessionHandler
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

func (r *HandlerRegistry) Dispatch(ctx context.Context, session *Session, packet network.Packet) (handled bool, err error) {
	r.mu.RLock()
	handler := r.handlers[packet.MessageID]
	r.mu.RUnlock()
	if handler == nil {
		return false, nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("packet handler panic for opcode 0x%04X: %v", packet.MessageID, recovered)
		}
	}()
	return true, handler(ctx, session, packet)
}
