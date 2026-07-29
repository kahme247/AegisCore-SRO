package model

import "testing"

func TestSRObjectZeroValueUsesSharedFallbackMutex(t *testing.T) {
	first := &SRObject{}
	second := &SRObject{}

	if got := first.mutexForObject(); got != fallbackSRObjectMutex {
		t.Fatalf("zero-value first mutex = %p, want fallback %p", got, fallbackSRObjectMutex)
	}
	if got := second.mutexForObject(); got != fallbackSRObjectMutex {
		t.Fatalf("zero-value second mutex = %p, want fallback %p", got, fallbackSRObjectMutex)
	}

	first.SetUniqueID(42)
	if got := first.GetUniqueID(); got != 42 {
		t.Fatalf("zero-value object unique ID = %d, want 42", got)
	}
}

func TestNewSRObjectUsesDedicatedMutex(t *testing.T) {
	first := NewSRObject()
	second := NewSRObject()

	if first.mutexForObject() == fallbackSRObjectMutex {
		t.Fatal("NewSRObject used the zero-value fallback mutex")
	}
	if second.mutexForObject() == fallbackSRObjectMutex {
		t.Fatal("second NewSRObject used the zero-value fallback mutex")
	}
	if first.mutexForObject() == second.mutexForObject() {
		t.Fatal("NewSRObject instances unexpectedly share a mutex")
	}

	// The pointer is intentionally copy-safe: copied Item and SRObject values
	// retain lock ownership without copying sync.RWMutex state.
	copy := first
	if copy.mutexForObject() != first.mutexForObject() {
		t.Fatal("copied SRObject did not retain its mutex pointer")
	}
}

func TestPlayerPartyMethodsSupportZeroValueSRObject(t *testing.T) {
	player := &Player{}
	party := &Party{Number: 7}

	if err := player.AddToParty(party); err != nil {
		t.Fatalf("AddToParty: %v", err)
	}
	if got := player.GetParty(); got != party {
		t.Fatalf("GetParty = %p, want %p", got, party)
	}
	player.RemoveFromParty()
	if player.HasParty() {
		t.Fatal("RemoveFromParty did not clear the party")
	}
}
