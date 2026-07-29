package model

import (
	"errors"
	"sync"
	"testing"
)

func TestPlayerCOSStateValidatesOwnershipAndIsIdempotent(t *testing.T) {
	var state PlayerCOSState
	if _, err := state.SetMounted(100, true); !errors.Is(err, ErrNoActiveTransport) {
		t.Fatalf("mount without transport error = %v", err)
	}
	if err := state.ActivateTransport(100, 200, false); err != nil {
		t.Fatalf("ActivateTransport: %v", err)
	}
	if _, err := state.SetMounted(101, true); !errors.Is(err, ErrTransportNotOwned) {
		t.Fatalf("foreign transport error = %v", err)
	}
	first, err := state.SetMounted(100, true)
	if err != nil {
		t.Fatalf("first mount: %v", err)
	}
	second, err := state.SetMounted(100, true)
	if err != nil {
		t.Fatalf("duplicate mount: %v", err)
	}
	if first != second || !second.Mounted || second.TransportUniqueID != 100 || second.TransportRefObjectID != 200 {
		t.Fatalf("mount snapshots = %#v, %#v", first, second)
	}
	if _, err = state.SetMounted(100, false); err != nil {
		t.Fatalf("dismount: %v", err)
	}
	if snapshot := state.Snapshot(); snapshot.Mounted {
		t.Fatalf("state remained mounted: %#v", snapshot)
	}
}

func TestPlayerCOSStateConcurrentDuplicateMount(t *testing.T) {
	var state PlayerCOSState
	if err := state.ActivateTransport(100, 200, false); err != nil {
		t.Fatal(err)
	}
	const workers = 64
	var wg sync.WaitGroup
	errorsFound := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := state.SetMounted(100, true)
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("duplicate mount error: %v", err)
		}
	}
	if snapshot := state.Snapshot(); !snapshot.Mounted {
		t.Fatalf("final state = %#v", snapshot)
	}
}
