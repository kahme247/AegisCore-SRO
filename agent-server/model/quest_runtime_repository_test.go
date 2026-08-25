package model

import (
	"errors"
	"testing"
)

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
	const weaponListItemID = uint32(2200)
	items := map[byte]questInventoryItem{
		13: {id: 1, refID: 999, stack: 1},
		20: {id: 2, refID: weaponListItemID, stack: 1},
	}
	slot, item, ok := findQuestItem(items, weaponListItemID)
	if !ok || slot != 20 || item.id != 2 {
		t.Fatalf("quest item = slot %d item %#v present=%t", slot, item, ok)
	}
}

func TestValidateQuestRewardReferenceAcceptsRecoveredRewards(t *testing.T) {
	const chulsanRewardItemID = uint32(9228)
	weaponList := questRewardReference{IsBasicReward: true, Gold: 205, Experience: 270}
	if err := validateQuestRewardReference(QuestWeaponListID, weaponList); err != nil {
		t.Fatalf("weapon-list reward: %v", err)
	}
	chulsan := questRewardReference{IsBasicReward: true, IsItemReward: true, Experience: 68860, SkillExperience: 10000, ItemRefID: chulsanRewardItemID, ItemQuantity: 1}
	if err := validateQuestRewardReference(QuestChulsanAngerID, chulsan); err != nil {
		t.Fatalf("Chulsan reward: %v", err)
	}
}

func TestValidateQuestRewardReferenceRejectsUnimplementedMechanisms(t *testing.T) {
	const chulsanRewardItemID = uint32(9228)
	tests := []questRewardReference{
		{IsBasicReward: true, IsCheckClass: true},
		{IsBasicReward: true, SelectionCount: 1},
		{IsBasicReward: true, IsItemReward: true, ItemRefID: chulsanRewardItemID, ItemQuantity: 2},
	}
	for index, reward := range tests {
		questID := QuestWeaponListID
		if index == 2 {
			questID = QuestChulsanAngerID
		}
		if err := validateQuestRewardReference(questID, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
			t.Fatalf("case %d error = %v, want ErrQuestReferenceUnsupported", index, err)
		}
	}
}
