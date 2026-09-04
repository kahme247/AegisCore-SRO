package service

import (
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type lifecycleWorldFake struct {
	mu       sync.Mutex
	removed  []*model.Item
	removedC chan struct{}
}

func (w *lifecycleWorldFake) RemoveVisibleObject(object model.ISRObject) {
	item, _ := object.(*model.Item)
	w.mu.Lock()
	w.removed = append(w.removed, item)
	w.mu.Unlock()
	select {
	case w.removedC <- struct{}{}:
	default:
	}
}

func TestGroundItemLifecycleReleasesOwnershipBeforeDespawn(t *testing.T) {
	world := &lifecycleWorldFake{removedC: make(chan struct{}, 1)}
	lifecycle := NewGroundItemLifecycle(world)
	item := &model.Item{OwnerUniqueID: 77}
	item.SetUniqueID(9001)
	var released chan struct{}
	lifecycle.ConfigureOwnershipRelease(func(got *model.Item) error {
		if got != item || got.OwnerUniqueID != 0 {
			t.Errorf("release callback item=%p owner=%d", got, got.OwnerUniqueID)
		}
		close(released)
		return nil
	})
	released = make(chan struct{})
	if err := lifecycle.Track(item, 10*time.Millisecond, 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("ownership release did not fire")
	}
	if item.OwnerUniqueID != 0 {
		t.Fatalf("owner = %d, want cleared", item.OwnerUniqueID)
	}
	select {
	case <-world.removedC:
	case <-time.After(time.Second):
		t.Fatal("despawn did not fire")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	if len(world.removed) != 1 || world.removed[0] != item {
		t.Fatalf("removed = %#v, want item once", world.removed)
	}
}

func TestGroundItemLifecycleCancelPreventsCallbacks(t *testing.T) {
	world := &lifecycleWorldFake{removedC: make(chan struct{}, 1)}
	lifecycle := NewGroundItemLifecycle(world)
	item := &model.Item{OwnerUniqueID: 12}
	item.SetUniqueID(9002)
	released := make(chan struct{}, 1)
	lifecycle.ConfigureOwnershipRelease(func(*model.Item) error {
		released <- struct{}{}
		return nil
	})
	if err := lifecycle.Track(item, 10*time.Millisecond, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	lifecycle.Cancel(item)
	time.Sleep(50 * time.Millisecond)
	select {
	case <-released:
		t.Fatal("ownership callback fired after cancellation")
	default:
	}
	select {
	case <-world.removedC:
		t.Fatal("despawn fired after cancellation")
	default:
	}
}

func TestGroundItemLifecycleClosePreventsCallbacks(t *testing.T) {
	world := &lifecycleWorldFake{removedC: make(chan struct{}, 1)}
	lifecycle := NewGroundItemLifecycle(world)
	item := &model.Item{}
	item.SetUniqueID(9003)
	released := make(chan struct{}, 1)
	lifecycle.ConfigureOwnershipRelease(func(*model.Item) error {
		released <- struct{}{}
		return nil
	})
	if err := lifecycle.Track(item, 10*time.Millisecond, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	lifecycle.Close()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-released:
		t.Fatal("ownership callback fired after lifecycle close")
	default:
	}
	select {
	case <-world.removedC:
		t.Fatal("despawn fired after lifecycle close")
	default:
	}
}
