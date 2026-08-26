package runtime

import (
	"errors"
	"testing"
	"time"
)

func TestLifecycleHappyPathAndSnapshots(t *testing.T) {
	now := time.Date(2026, 7, 19, 1, 2, 3, 0, time.FixedZone("test", 2*60*60))
	lifecycle := newLifecycle(func() time.Time { return now })
	if got := lifecycle.Snapshot(); got.State != Starting || !got.ChangedAt.Equal(now.UTC()) {
		t.Fatalf("initial snapshot = %+v", got)
	}
	for _, next := range []State{Loading, Ready, Draining, Stopped} {
		now = now.Add(time.Second)
		if err := lifecycle.Transition(next, ""); err != nil {
			t.Fatalf("Transition(%s) error = %v", next, err)
		}
	}
	if lifecycle.IsReady() {
		t.Fatal("stopped lifecycle reported ready")
	}
}

func TestLifecycleDegradationRequiresReasonAndReloadBeforeReady(t *testing.T) {
	lifecycle := NewLifecycle()
	if err := lifecycle.Transition(Loading, ""); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Transition(Ready, ""); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Transition(Degraded, ""); err == nil {
		t.Fatal("degradation without a reason was accepted")
	}
	if err := lifecycle.Transition(Degraded, " shard database unavailable "); err != nil {
		t.Fatal(err)
	}
	if got := lifecycle.Snapshot().Reason; got != "shard database unavailable" {
		t.Fatalf("degraded reason = %q", got)
	}
	if err := lifecycle.Transition(Ready, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct degraded-to-ready error = %v, want ErrInvalidTransition", err)
	}
	if err := lifecycle.Transition(Loading, ""); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Transition(Ready, ""); err != nil {
		t.Fatal(err)
	}
	if !lifecycle.IsReady() {
		t.Fatal("recovered lifecycle did not report ready")
	}
}

func TestStoppedLifecycleIsTerminal(t *testing.T) {
	lifecycle := NewLifecycle()
	_ = lifecycle.Transition(Draining, "")
	_ = lifecycle.Transition(Stopped, "")
	if err := lifecycle.Transition(Loading, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition error = %v, want ErrInvalidTransition", err)
	}
}
