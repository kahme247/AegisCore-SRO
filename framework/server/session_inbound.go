package server

import (
	"sync"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
)

// inboundMassive owns the mutable 0x600D assembly state for one Session.
// Keeping the lock and assembler together prevents socket-deadline, dispatcher,
// and shutdown paths from acquiring or replacing the assembler inconsistently.
type inboundMassive struct {
	mu        sync.Mutex
	assembler *network.MassiveAssembler
}

func newInboundMassive() *inboundMassive {
	return &inboundMassive{assembler: network.NewMassiveAssembler(network.NewDefaultMassiveCodec())}
}

func (m *inboundMassive) earlierDeadline(fallback time.Time) time.Time {
	if m == nil {
		return fallback
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.assembler == nil {
		return fallback
	}
	if deadline, active := m.assembler.Deadline(); active && deadline.Before(fallback) {
		return deadline
	}
	return fallback
}

func (m *inboundMassive) expireAt(now time.Time) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.assembler == nil {
		return nil
	}
	return m.assembler.ExpireAt(now)
}

func (m *inboundMassive) process(frame network.Packet) (network.Packet, bool, error) {
	if m == nil {
		return frame, true, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.assembler == nil {
		return frame, true, nil
	}
	return m.assembler.ProcessFrame(frame)
}

func (m *inboundMassive) finish() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.assembler == nil {
		return nil
	}
	return m.assembler.Finish()
}

// replaceAssembler is intentionally package-private and exists for deterministic
// timeout tests that need a shorter assembly age than the production profile.
func (m *inboundMassive) replaceAssembler(assembler *network.MassiveAssembler) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.assembler = assembler
	m.mu.Unlock()
}
