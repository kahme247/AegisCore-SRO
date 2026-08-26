// Package runtime owns process-level Agent runtime state. Character session
// lifecycle remains in framework/server and is intentionally separate.
package runtime

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Starting State = "starting"
	Loading  State = "loading"
	Ready    State = "ready"
	Degraded State = "degraded"
	Draining State = "draining"
	Stopped  State = "stopped"
)

var ErrInvalidTransition = errors.New("invalid Agent lifecycle transition")

type Snapshot struct {
	State     State
	Reason    string
	ChangedAt time.Time
}

// Lifecycle serializes process-state transitions and provides consistent
// snapshots to admission, readiness, diagnostics, and shutdown code.
type Lifecycle struct {
	mu        sync.RWMutex
	state     State
	reason    string
	changedAt time.Time
	now       func() time.Time
}

func NewLifecycle() *Lifecycle {
	return newLifecycle(time.Now)
}

func newLifecycle(now func() time.Time) *Lifecycle {
	if now == nil {
		now = time.Now
	}
	return &Lifecycle{state: Starting, changedAt: now().UTC(), now: now}
}

func (l *Lifecycle) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{State: Stopped, Reason: "lifecycle is not configured"}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Snapshot{State: l.state, Reason: l.reason, ChangedAt: l.changedAt}
}

func (l *Lifecycle) IsReady() bool {
	return l != nil && l.Snapshot().State == Ready
}

func (l *Lifecycle) Transition(next State, reason string) error {
	if l == nil {
		return errors.New("Agent lifecycle is nil")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state == next {
		return nil
	}
	if !transitionAllowed(l.state, next) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, l.state, next)
	}
	if next == Degraded && strings.TrimSpace(reason) == "" {
		return errors.New("degraded Agent lifecycle requires a reason")
	}
	l.state = next
	l.reason = strings.TrimSpace(reason)
	l.changedAt = l.now().UTC()
	return nil
}

func transitionAllowed(current, next State) bool {
	switch current {
	case Starting:
		return next == Loading || next == Degraded || next == Draining
	case Loading:
		return next == Ready || next == Degraded || next == Draining
	case Ready:
		return next == Degraded || next == Draining
	case Degraded:
		return next == Loading || next == Draining
	case Draining:
		return next == Stopped
	case Stopped:
		return false
	default:
		return false
	}
}
