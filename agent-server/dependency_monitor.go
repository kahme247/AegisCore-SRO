package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

type dependencyState byte

const (
	dependencyHealthy dependencyState = iota
	dependencySuspect
	dependencyLost
)

const (
	dependencyProbeInterval = 2 * time.Second
	dependencyProbeTimeout  = 1500 * time.Millisecond
	dependencyFailureLimit  = 3
	dependencyDrainTimeout  = 30 * time.Second
)

type dependencySnapshot struct {
	State               dependencyState
	ConsecutiveFailures int
	Reason              string
}

type databasePinger interface {
	PingContext(context.Context) error
}

type dependencyMonitor struct {
	account  databasePinger
	shard    databasePinger
	drain    func(context.Context, string) error
	interval time.Duration
	timeout  time.Duration
	limit    int

	mu       sync.RWMutex
	snapshot dependencySnapshot
	done     chan struct{}
	once     sync.Once
}

func newDependencyMonitor(account, shard *sql.DB, drain func(context.Context, string) error) (*dependencyMonitor, error) {
	if account == nil || shard == nil || drain == nil {
		return nil, errors.New("Agent dependency monitor requires account, shard, and drain callback")
	}
	return &dependencyMonitor{account: account, shard: shard, drain: drain, interval: dependencyProbeInterval, timeout: dependencyProbeTimeout, limit: dependencyFailureLimit, snapshot: dependencySnapshot{State: dependencyHealthy}, done: make(chan struct{})}, nil
}

func (m *dependencyMonitor) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() { go m.run(ctx) })
}

func (m *dependencyMonitor) run(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lost, reason := m.probe(ctx)
			if !lost {
				continue
			}
			drainCtx, cancel := context.WithTimeout(context.Background(), dependencyDrainTimeout)
			_ = m.drain(drainCtx, reason)
			cancel()
			return
		}
	}
}

func (m *dependencyMonitor) probe(ctx context.Context) (bool, string) {
	probeCtx, cancel := context.WithTimeout(ctx, m.timeout)
	accountErr := m.account.PingContext(probeCtx)
	cancel()
	probeCtx, cancel = context.WithTimeout(ctx, m.timeout)
	shardErr := m.shard.PingContext(probeCtx)
	cancel()
	if accountErr == nil && shardErr == nil {
		m.mu.Lock()
		if m.snapshot.State != dependencyLost {
			m.snapshot = dependencySnapshot{State: dependencyHealthy}
		}
		m.mu.Unlock()
		return false, ""
	}
	reason := dependencyFailureReason(accountErr, shardErr)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshot.State == dependencyLost {
		return true, m.snapshot.Reason
	}
	m.snapshot.State = dependencySuspect
	m.snapshot.ConsecutiveFailures++
	m.snapshot.Reason = reason
	if m.snapshot.ConsecutiveFailures < m.limit {
		return false, reason
	}
	m.snapshot.State = dependencyLost
	return true, reason
}

func dependencyFailureReason(accountErr, shardErr error) string {
	switch {
	case accountErr != nil && shardErr != nil:
		return fmt.Sprintf("account and shard databases unavailable: account=%v; shard=%v", accountErr, shardErr)
	case accountErr != nil:
		return fmt.Sprintf("account database unavailable: %v", accountErr)
	default:
		return fmt.Sprintf("shard database unavailable: %v", shardErr)
	}
}

func (m *dependencyMonitor) Snapshot() dependencySnapshot {
	if m == nil {
		return dependencySnapshot{State: dependencyLost, Reason: "dependency monitor is unavailable"}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

func (m *dependencyMonitor) Wait(ctx context.Context) error {
	if m == nil {
		return nil
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
