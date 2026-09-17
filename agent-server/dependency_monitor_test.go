package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scriptedPinger struct {
	errors []error
	index  int
}

func (p *scriptedPinger) PingContext(context.Context) error {
	if len(p.errors) == 0 {
		return nil
	}
	index := p.index
	if index >= len(p.errors) {
		index = len(p.errors) - 1
	}
	p.index++
	return p.errors[index]
}

func testDependencyMonitor(account, shard databasePinger) *dependencyMonitor {
	return &dependencyMonitor{account: account, shard: shard, drain: func(context.Context, string) error { return nil }, timeout: time.Second, limit: 3, snapshot: dependencySnapshot{State: dependencyHealthy}, done: make(chan struct{})}
}

func TestDependencyMonitorRequiresConsecutiveFailuresAndResetsOnRecovery(t *testing.T) {
	unavailable := errors.New("unavailable")
	account := &scriptedPinger{errors: []error{unavailable, unavailable, nil, unavailable, unavailable, unavailable}}
	monitor := testDependencyMonitor(account, &scriptedPinger{})
	for probe := 1; probe <= 6; probe++ {
		lost, _ := monitor.probe(context.Background())
		if probe < 6 && lost {
			t.Fatalf("probe %d reported dependency loss early", probe)
		}
		if probe == 6 && !lost {
			t.Fatal("third consecutive failure did not report dependency loss")
		}
	}
	snapshot := monitor.Snapshot()
	if snapshot.State != dependencyLost || snapshot.ConsecutiveFailures != 3 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestDependencyMonitorIdentifiesShardFailure(t *testing.T) {
	monitor := testDependencyMonitor(&scriptedPinger{}, &scriptedPinger{errors: []error{errors.New("connection refused")}})
	monitor.limit = 1
	lost, reason := monitor.probe(context.Background())
	if !lost || reason != "shard database unavailable: connection refused" {
		t.Fatalf("probe = (%t,%q)", lost, reason)
	}
}
