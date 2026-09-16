package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRuntimeCycleExecutesAndCompletesClaimedRun(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	store := &runtimeStoreStub{runs: []Run{{ID: 7, ScheduleID: "notice-1", Handler: "test", Payload: []byte(`{}`)}}}
	called := make(chan Run, 1)
	runtime := newTestRuntime(t, store, now, map[string]Handler{
		"test": func(_ context.Context, run Run) error {
			called <- run
			return nil
		},
	})

	runtime.cycle(context.Background())

	select {
	case run := <-called:
		if run.ID != 7 || run.Attempt != 1 || run.LeaseOwner != "agent-test" {
			t.Fatalf("handler run = %#v", run)
		}
	default:
		t.Fatal("scheduler handler was not called")
	}
	if len(store.succeeded) != 1 || len(store.failed) != 0 {
		t.Fatalf("succeeded=%d failed=%d", len(store.succeeded), len(store.failed))
	}
}

func TestRuntimeCycleMarksUnknownHandlerPermanent(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	store := &runtimeStoreStub{runs: []Run{{ID: 8, ScheduleID: "unknown-1", Handler: "missing", Payload: []byte(`{}`)}}}
	runtime := newTestRuntime(t, store, now, nil)

	runtime.cycle(context.Background())

	if len(store.failed) != 1 {
		t.Fatalf("failed runs = %d, want 1", len(store.failed))
	}
	var permanent interface{ Permanent() bool }
	if !errors.As(store.failed[0], &permanent) || !permanent.Permanent() {
		t.Fatalf("unknown handler error = %v, want permanent", store.failed[0])
	}
}

func TestRuntimeStartRecoversLeasesAndStopsWithContext(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	store := &runtimeStoreStub{}
	runtime := newTestRuntime(t, store, now, nil)
	ctx, cancel := context.WithCancel(context.Background())

	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := runtime.Wait(waitCtx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	store.mu.Lock()
	recoveries := store.recoveries
	store.mu.Unlock()
	if recoveries < 1 {
		t.Fatal("expired scheduler leases were not recovered")
	}
}

func newTestRuntime(t *testing.T, store RuntimeStore, now time.Time, handlers map[string]Handler) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(store, RuntimeOptions{
		Worker:        "agent-test",
		PollInterval:  time.Hour,
		LeaseDuration: time.Minute,
		BatchSize:     10,
		RetryPolicy:   RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Minute},
		Now:           func() time.Time { return now },
	}, handlers)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}

type runtimeStoreStub struct {
	mu         sync.Mutex
	runs       []Run
	succeeded  []Run
	failed     []error
	recoveries int
}

func (*runtimeStoreStub) MaterializeDue(context.Context, time.Time, int) (MaterializeResult, error) {
	return MaterializeResult{}, nil
}

func (store *runtimeStoreStub) ClaimRuns(_ context.Context, worker string, now time.Time, _ int, lease time.Duration) ([]Run, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	runs := append([]Run(nil), store.runs...)
	store.runs = nil
	for index := range runs {
		runs[index].Attempt++
		runs[index].LeaseOwner = worker
		runs[index].LeaseUntil = now.Add(lease)
	}
	return runs, nil
}

func (store *runtimeStoreStub) MarkSucceeded(_ context.Context, run Run, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.succeeded = append(store.succeeded, run)
	return nil
}

func (store *runtimeStoreStub) MarkFailed(_ context.Context, _ Run, runErr error, _ RetryPolicy, _ time.Time) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failed = append(store.failed, runErr)
	return true, nil
}

func (store *runtimeStoreStub) RecoverExpired(context.Context, time.Time) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recoveries++
	return 0, nil
}
