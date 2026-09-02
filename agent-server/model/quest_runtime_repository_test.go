package model

import (
	"context"
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
	generic := questRewardReference{IsBasicReward: true, Gold: 100, Experience: 200}
	if err := validateQuestRewardReference(101, generic); err != nil {
		t.Fatalf("generic reward: %v", err)
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

func TestValidateQuestRewardReferenceRejectsEveryUnconfirmedRewardField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*questRewardReference)
	}{
		{name: "skill points", mutate: func(r *questRewardReference) { r.SkillPoints = 1 }},
		{name: "academy points", mutate: func(r *questRewardReference) { r.AcademyPoints = 1 }},
		{name: "hwan", mutate: func(r *questRewardReference) { r.Hwan = 1 }},
		{name: "inventory slots", mutate: func(r *questRewardReference) { r.InventorySlots = 1 }},
		{name: "conditional", mutate: func(r *questRewardReference) { r.IsCheckCondition = true }},
		{name: "country", mutate: func(r *questRewardReference) { r.IsCheckCountry = true }},
		{name: "class", mutate: func(r *questRewardReference) { r.IsCheckClass = true }},
		{name: "gender", mutate: func(r *questRewardReference) { r.IsCheckGender = true }},
		{name: "parameter 1", mutate: func(r *questRewardReference) { r.Param1 = 1 }},
		{name: "parameter 2", mutate: func(r *questRewardReference) { r.Param2 = 1 }},
		{name: "parameter 3", mutate: func(r *questRewardReference) { r.Param3 = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reward := questRewardReference{IsBasicReward: true}
			tc.mutate(&reward)
			if err := validateQuestRewardReference(QuestWeaponListID, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
				t.Fatalf("error = %v, want ErrQuestReferenceUnsupported", err)
			}
		})
	}
}

func TestSelectQuestRewardItemsUsesReferenceItemIdentity(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward:  true,
		IsItemReward:   true,
		SelectionCount: 2,
		Items: []questRewardItemReference{
			{RefID: 1001, Quantity: 1},
			{RefID: 1002, Quantity: 50},
			{RefID: 1003, Quantity: 1},
		},
	}
	selected, err := selectQuestRewardItems(88, reward, []uint32{1003, 1001})
	if err != nil {
		t.Fatalf("select reward items: %v", err)
	}
	if len(selected) != 2 || selected[0].RefID != 1003 || selected[1].RefID != 1001 {
		t.Fatalf("selected items = %#v", selected)
	}
}

func TestSelectQuestRewardItemsRejectsCountDuplicateAndForeignIdentity(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward:  true,
		IsItemReward:   true,
		SelectionCount: 1,
		Items: []questRewardItemReference{
			{RefID: 1001, Quantity: 1},
			{RefID: 1002, Quantity: 1},
		},
	}
	tests := [][]uint32{{}, {1001, 1002}, {9999}}
	for _, entries := range tests {
		if _, err := selectQuestRewardItems(88, reward, entries); !errors.Is(err, ErrQuestStateConflict) {
			t.Fatalf("entries %v error = %v, want ErrQuestStateConflict", entries, err)
		}
	}
	reward.SelectionCount = 2
	if _, err := selectQuestRewardItems(88, reward, []uint32{1001, 1001}); !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("duplicate selection error = %v, want ErrQuestStateConflict", err)
	}
}

func TestSelectQuestRewardItemsRejectsAmbiguousReferenceIdentity(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward:  true,
		IsItemReward:   true,
		SelectionCount: 1,
		Items: []questRewardItemReference{
			{RefID: 1001, Quantity: 1},
			{RefID: 1001, Quantity: 2},
		},
	}
	if _, err := selectQuestRewardItems(88, reward, []uint32{1001}); !errors.Is(err, ErrQuestReferenceUnsupported) {
		t.Fatalf("ambiguous selection error = %v, want ErrQuestReferenceUnsupported", err)
	}
}

func TestSelectQuestRewardDoesNotCoerceEventRewardIDIntoQuestID(t *testing.T) {
	repository := &SQLQuestRuntimeRepository{}
	_, err := repository.SelectQuestReward(context.Background(), 42, 10000, []uint32{7})
	if !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("event reward selection error = %v, want ErrQuestStateConflict", err)
	}
}

func TestAdvanceQuestObjectiveSaturatesAndUsesConfirmedTransitions(t *testing.T) {
	quest := CharacterQuest{QuestID: 10, Status: QuestActive, Data1: 4}
	progressed, changed, err := advanceQuestObjective(quest, 10, 3)
	if err != nil || !changed || progressed.Data1 != 7 || progressed.Status != QuestProgressed {
		t.Fatalf("progress = (%+v, %v, %v)", progressed, changed, err)
	}
	ready, changed, err := advanceQuestObjective(progressed, 10, ^uint64(0))
	if err != nil || !changed || ready.Data1 != 10 || ready.Status != QuestRewardReady {
		t.Fatalf("completion = (%+v, %v, %v)", ready, changed, err)
	}
	if unchanged, changed, err := advanceQuestObjective(ready, 10, 1); err != nil || changed || unchanged != ready {
		t.Fatalf("completed objective changed = (%+v, %v, %v)", unchanged, changed, err)
	}
}

func TestAdvanceQuestObjectiveRejectsInvalidSourceState(t *testing.T) {
	quest := CharacterQuest{QuestID: 10, Status: QuestInactive}
	if _, changed, err := advanceQuestObjective(quest, 1, 1); err == nil || changed {
		t.Fatalf("inactive advance = (changed=%v, err=%v)", changed, err)
	}
}

func TestQuestInventoryContainsAggregatesStacks(t *testing.T) {
	items := map[byte]questInventoryItem{
		13: {refID: 100, stack: 2},
		14: {refID: 100, stack: 3},
		15: {refID: 200, stack: 1},
	}
	if !questInventoryContains(items, map[uint32]uint64{100: 5, 200: 1}) {
		t.Fatal("complete delivery inventory was rejected")
	}
	if questInventoryContains(items, map[uint32]uint64{100: 6}) {
		t.Fatal("insufficient delivery inventory was accepted")
	}
}
