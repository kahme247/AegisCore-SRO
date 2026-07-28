package server

import (
	"context"
	"sync"
)

// sessionLifetime owns cancellation, worker accounting, and terminal signals
// for one Session. Protocol security remains on Session; transport lifetime is
// kept here so future handler changes cannot accidentally create a second
// cancellation or drain authority.
type sessionLifetime struct {
	ctx    context.Context
	cancel context.CancelFunc

	done    chan struct{}
	drained chan struct{}
	once    sync.Once

	mu      sync.Mutex
	started bool
	closed  bool
	workers sync.WaitGroup
}

func newSessionLifetime() *sessionLifetime {
	ctx, cancel := context.WithCancel(context.Background())
	return &sessionLifetime{
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		drained: make(chan struct{}),
	}
}

func (l *sessionLifetime) startInbound(workerCount int) bool {
	if l == nil || workerCount <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.started {
		return false
	}
	l.started = true
	l.workers.Add(workerCount)
	return true
}

func (l *sessionLifetime) workerDone() {
	if l != nil {
		l.workers.Done()
	}
}

func (l *sessionLifetime) waitWorkers() {
	if l != nil {
		l.workers.Wait()
	}
}

// close preserves the historical Session.Close ordering: lifecycle phase,
// closed-state publication and cancellation precede transport teardown; Done
// closes before the asynchronous terminal drain starts.
func (l *sessionLifetime) close(markClosing, shutdown, drain func()) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if markClosing != nil {
			markClosing()
		}
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		if l.cancel != nil {
			l.cancel()
		}
		if shutdown != nil {
			shutdown()
		}
		close(l.done)
		if drain != nil {
			go drain()
		}
	})
}

func (l *sessionLifetime) finishDrain() {
	if l != nil {
		close(l.drained)
	}
}

// addWorkerForTest models a handler already in flight without exposing worker
// accounting on Session's production API.
func (l *sessionLifetime) addWorkerForTest() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.started = true
	l.workers.Add(1)
	l.mu.Unlock()
}
