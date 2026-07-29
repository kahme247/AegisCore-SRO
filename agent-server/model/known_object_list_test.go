package model

import "testing"

func TestKnownObjectListReturnsDetachedSnapshot(t *testing.T) {
	owner := &SRObject{UniqueID: 1}
	other := &SRObject{UniqueID: 2}
	list := NewKnownObjectList(owner)
	if !list.AddObject(other) {
		t.Fatal("AddObject(other) = false, want true")
	}

	snapshot := list.GetKnownObjects()
	delete(snapshot, other.GetUniqueID())
	if !list.Knows(other) {
		t.Fatal("mutating a known-object snapshot changed the authoritative list")
	}
}

func TestCharKnownObjectListReturnsDetachedSnapshot(t *testing.T) {
	owner := &SRObject{UniqueID: 1}
	other := &SRObject{UniqueID: 2}
	list := NewCharKnownObjectList(owner)
	if !list.AddObject(other) {
		t.Fatal("AddObject(other) = false, want true")
	}

	snapshot := list.GetKnownObjects()
	delete(snapshot, other.GetUniqueID())
	if !list.Knows(other) {
		t.Fatal("mutating a character known-object snapshot changed the authoritative list")
	}
}

func TestCharKnownObjectListDoesNotDespawnUndeliveredObject(t *testing.T) {
	owner := &SRObject{UniqueID: 1}
	other := &SRObject{UniqueID: 2}
	list := NewCharKnownObjectList(owner)
	if !list.AddObject(other) || !list.RemoveObject(other) {
		t.Fatal("could not add and remove pending object")
	}
	if spawning := list.GetObjectsToSpawn(); len(spawning) != 0 {
		t.Fatalf("removed pending spawn remained queued: %#v", spawning)
	}
	if despawning := list.GetObjectsToDespawn(); len(despawning) != 0 {
		t.Fatalf("undelivered object queued a despawn: %#v", despawning)
	}
}

func TestCharKnownObjectListTracksClientDelivery(t *testing.T) {
	owner := &SRObject{UniqueID: 1}
	other := &SRObject{UniqueID: 2}
	list := NewCharKnownObjectList(owner)
	list.AddObject(other)
	spawning := list.GetObjectsToSpawn()
	if list.ClientKnows(other.GetUniqueID()) {
		t.Fatal("client knew object before spawn delivery")
	}
	list.MarkSpawned(spawning)
	if !list.ClientKnows(other.GetUniqueID()) {
		t.Fatal("client delivery was not recorded")
	}
	list.RemoveObject(other)
	despawning := list.GetObjectsToDespawn()
	list.MarkDespawned(despawning)
	if list.ClientKnows(other.GetUniqueID()) {
		t.Fatal("client still knew object after despawn delivery")
	}
}
