package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultPollInterval  = time.Second
	DefaultLeaseDuration = 30 * time.Second
	DefaultBatchSize     = 64
)

type RuntimeStore interface {
	MaterializeDue(context.Context, time.Time, int) (MaterializeResult, error)
	ClaimRuns(context.Context, string, time.Time, int, time.Duration) ([]Run, error)
	MarkSucceeded(context.Context, Run, time.Time) error
	MarkFailed(context.Context, Run, error, RetryPolicy, time.Time) (bool, error)
	RecoverExpired(context.Context, time.Time) (int64, error)
}

type Handler func(context.Context, Run) error

type RuntimeOptions struct {
	Worker        string
	PollInterval  time.Duration
	LeaseDuration time.Duration
	BatchSize     int
	RetryPolicy   RetryPolicy
	Now           func() time.Time
	OnError       func(error)
}

type Runtime struct {
	store    RuntimeStore
	options  RuntimeOptions
	handlers map[string]Handler

	mu      sync.Mutex
	started bool
	done    chan struct{}
}

func NewRuntime(store RuntimeStore, options RuntimeOptions, handlers map[string]Handler) (*Runtime, error) {
	if store == nil {
		return nil, errors.New("scheduler runtime store is required")
	}
	if err := validateWorker(options.Worker); err != nil {
		return nil, err
	}
	if options.PollInterval <= 0 || options.LeaseDuration <= 0 || options.LeaseDuration > 24*time.Hour || options.BatchSize < 1 || options.BatchSize > 1000 {
		return nil, errors.New("scheduler runtime timing or batch options are invalid")
	}
	if err := options.RetryPolicy.Validate(); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	registered := make(map[string]Handler, len(handlers))
	for name, handler := range handlers {
		if name == "" || handler == nil {
			return nil, errors.New("scheduler handler name and implementation are required")
		}
		registered[name] = handler
	}
	return &Runtime{store: store, options: options, handlers: registered}, nil
}

func (runtime *Runtime) Start(ctx context.Context) error {
	if runtime == nil {
		return errors.New("scheduler runtime is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.started {
		return errors.New("scheduler runtime already started")
	}
	if _, err := runtime.store.RecoverExpired(ctx, runtime.options.Now().UTC()); err != nil {
		return fmt.Errorf("recover scheduler leases: %w", err)
	}
	runtime.started = true
	runtime.done = make(chan struct{})
	go runtime.run(ctx)
	return nil
}

func (runtime *Runtime) HasHandler(name string) bool {
	if runtime == nil {
		return false
	}
	_, exists := runtime.handlers[name]
	return exists
}

func (runtime *Runtime) Wait(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	done := runtime.done
	started := runtime.started
	runtime.mu.Unlock()
	if !started || done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (runtime *Runtime) run(ctx context.Context) {
	defer close(runtime.done)
	runtime.cycle(ctx)
	ticker := time.NewTicker(runtime.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.cycle(ctx)
		}
	}
}

func (runtime *Runtime) cycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	now := runtime.options.Now().UTC()
	if _, err := runtime.store.RecoverExpired(ctx, now); err != nil {
		runtime.report(fmt.Errorf("recover scheduler leases: %w", err))
		return
	}
	if _, err := runtime.store.MaterializeDue(ctx, now, runtime.options.BatchSize); err != nil {
		runtime.report(fmt.Errorf("materialize scheduler runs: %w", err))
		return
	}
	runs, err := runtime.store.ClaimRuns(ctx, runtime.options.Worker, now, runtime.options.BatchSize, runtime.options.LeaseDuration)
	if err != nil {
		runtime.report(fmt.Errorf("claim scheduler runs: %w", err))
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		runtime.execute(ctx, run)
	}
}

func (runtime *Runtime) execute(ctx context.Context, run Run) {
	handler, exists := runtime.handlers[run.Handler]
	var runErr error
	if !exists {
		runErr = permanentError{err: fmt.Errorf("scheduler handler %q is not registered", run.Handler)}
	} else {
		runErr = handler(ctx, run)
	}
	finishedAt := runtime.options.Now().UTC()
	if runErr == nil {
		if err := runtime.store.MarkSucceeded(ctx, run, finishedAt); err != nil {
			runtime.report(err)
		}
		return
	}
	if _, err := runtime.store.MarkFailed(ctx, run, runErr, runtime.options.RetryPolicy, finishedAt); err != nil {
		runtime.report(err)
	}
}

func (runtime *Runtime) report(err error) {
	if err != nil && runtime.options.OnError != nil {
		runtime.options.OnError(err)
	}
}

type permanentError struct{ err error }

func (err permanentError) Error() string { return err.err.Error() }
func (err permanentError) Unwrap() error { return err.err }
func (permanentError) Permanent() bool   { return true }
