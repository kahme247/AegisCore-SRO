package model

import "testing"

func TestInventorySnapshotDetachesItemsAndSynchronization(t *testing.T) {
	inventory := NewInventory(map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
	})

	snapshot := inventory.Snapshot()
	if snapshot.mutex == nil || snapshot.mutex == inventory.mutex {
		t.Fatal("Snapshot() did not create independent synchronization")
	}
	if got := snapshot.Items[14].ID; got != 101 {
		t.Fatalf("snapshot item ID = %d, want 101", got)
	}

	mutex := inventory.synchronizationMutex()
	mutex.Lock()
	inventory.Items[14] = relocationTestItem(202, 2002, TypeInfo{})
	mutex.Unlock()

	if got := snapshot.Items[14].ID; got != 101 {
		t.Fatalf("snapshot item ID after source mutation = %d, want 101", got)
	}
}

func TestZeroValueInventorySnapshotIsUsable(t *testing.T) {
	var inventory Inventory
	snapshot := inventory.Snapshot()
	if snapshot.mutex == nil {
		t.Fatal("zero-value snapshot has no synchronization")
	}
	if snapshot.Items == nil {
		t.Fatal("zero-value snapshot has nil item map")
	}
}
