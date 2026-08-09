package model

import "testing"

func TestQuestInventorySlotUsesPersistedCapacity(t *testing.T) {
	items := map[byte]questInventoryItem{13: {id: 1}, 14: {id: 2}}
	slot, ok := freeQuestInventorySlot(items, 16)
	if !ok || slot != 15 {
		t.Fatalf("free slot = %d, present=%t; want 15", slot, ok)
	}
	items[15] = questInventoryItem{id: 3}
	if slot, ok := freeQuestInventorySlot(items, 16); ok {
		t.Fatalf("full inventory returned slot %d", slot)
	}
}

func TestFindQuestItemUsesReferenceIdentity(t *testing.T) {
	items := map[byte]questInventoryItem{
		13: {id: 1, refID: 999, stack: 1},
		20: {id: 2, refID: QuestWeaponListItemID, stack: 1},
	}
	slot, item, ok := findQuestItem(items, QuestWeaponListItemID)
	if !ok || slot != 20 || item.id != 2 {
		t.Fatalf("quest item = slot %d item %#v present=%t", slot, item, ok)
	}
}
