package model

import (
	"errors"
	"testing"
)

func TestPlanInventoryRelocationIsPureAndApplySwapsExactItems(t *testing.T) {
	items := map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
		15: relocationTestItem(202, 2002, TypeInfo{}),
	}

	plan, err := PlanInventoryRelocation(items, 14, 15, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocation() error = %v", err)
	}
	if plan.Action != InventoryRelocationMove || plan.SourceItemID != 101 || plan.TargetItemID != 202 {
		t.Fatalf("plan = %+v, want a move of 101 and 202", plan)
	}
	if got := items[14].ID; got != 101 {
		t.Fatalf("planning changed source item ID to %d", got)
	}
	if got := items[15].ID; got != 202 {
		t.Fatalf("planning changed target item ID to %d", got)
	}

	if err := ApplyInventoryRelocation(items, plan); err != nil {
		t.Fatalf("ApplyInventoryRelocation() error = %v", err)
	}
	if got := items[14].ID; got != 202 {
		t.Fatalf("source slot ID = %d, want 202", got)
	}
	if got := items[15].ID; got != 101 {
		t.Fatalf("target slot ID = %d, want 101", got)
	}
}

func TestCommitRelocationKeepsRuntimeInventoryUntouchedWhenPersistenceFails(t *testing.T) {
	inventory := Inventory{Items: map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
		15: relocationTestItem(202, 2002, TypeInfo{}),
	}}
	persistFailure := errors.New("persist failed")
	persistCalled := false

	_, err := inventory.CommitRelocation(relocationTestPlayer(), 14, 15, func(plan InventoryRelocationPlan) error {
		persistCalled = true
		if got := inventory.Items[14].ID; got != 101 {
			t.Fatalf("source changed before persistence to %d", got)
		}
		if got := inventory.Items[15].ID; got != 202 {
			t.Fatalf("target changed before persistence to %d", got)
		}
		if plan.SourceItemID != 101 || plan.TargetItemID != 202 {
			t.Fatalf("persisted plan = %+v, want source 101 and target 202", plan)
		}
		return persistFailure
	})
	if !errors.Is(err, persistFailure) {
		t.Fatalf("CommitRelocation() error = %v, want wrapped %v", err, persistFailure)
	}
	if !persistCalled {
		t.Fatal("CommitRelocation() did not call persistence")
	}
	if got := inventory.Items[14].ID; got != 101 {
		t.Fatalf("failed persistence changed source item ID to %d", got)
	}
	if got := inventory.Items[15].ID; got != 202 {
		t.Fatalf("failed persistence changed target item ID to %d", got)
	}
}

func TestCommitRelocationAppliesOnlyAfterPersistenceSuccess(t *testing.T) {
	inventory := Inventory{Items: map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
	}}

	plan, err := inventory.CommitRelocation(relocationTestPlayer(), 14, 15, func(plan InventoryRelocationPlan) error {
		if got := inventory.Items[14].ID; got != 101 {
			t.Fatalf("source changed before successful persistence returned: %d", got)
		}
		if _, exists := inventory.Items[15]; exists {
			t.Fatal("target was populated before successful persistence returned")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CommitRelocation() error = %v", err)
	}
	if plan.Action != InventoryRelocationMove || plan.TargetItemID != 0 {
		t.Fatalf("plan = %+v, want an empty-target move", plan)
	}
	if _, exists := inventory.Items[14]; exists {
		t.Fatal("source still exists after committed move")
	}
	if got := inventory.Items[15].ID; got != 101 {
		t.Fatalf("target item ID = %d, want 101", got)
	}
}

func TestPlanInventoryRelocationRetainsExistingEquipRulesWithoutMutating(t *testing.T) {
	weapon := relocationTestItem(101, 1001, TypeInfo{
		TypeID1: TypeID1Item,
		TypeID2: TypeID2Wearable,
		TypeID3: TypeID3Weapon,
		TypeID4: 2,
	})
	weapon.RequiredLevelType1 = 1
	weapon.RequiredLevel1 = 1
	items := map[byte]Item{
		14: weapon,
	}

	plan, err := PlanInventoryRelocation(items, 14, SlotPrimaryWeapon, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocation() error = %v", err)
	}
	if plan.Action != InventoryRelocationEquip {
		t.Fatalf("plan action = %d, want equip", plan.Action)
	}
	if !plan.SourceOneHanded || plan.SourceReferenceID != 1001 {
		t.Fatalf("plan packet metadata = %+v, want one-handed reference 1001", plan)
	}
	if got := items[14].ID; got != 101 {
		t.Fatalf("planning changed equipped candidate ID to %d", got)
	}
}

func TestApplyInventoryRelocationRejectsStaleMapWithoutPartialMutation(t *testing.T) {
	items := map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
		15: relocationTestItem(202, 2002, TypeInfo{}),
	}
	plan, err := PlanInventoryRelocation(items, 14, 15, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocation() error = %v", err)
	}
	items[15] = relocationTestItem(303, 3003, TypeInfo{})

	err = ApplyInventoryRelocation(items, plan)
	if !errors.Is(err, ErrInventoryRelocationRuntimeConflict) {
		t.Fatalf("ApplyInventoryRelocation() error = %v, want runtime conflict", err)
	}
	if got := items[14].ID; got != 101 {
		t.Fatalf("stale apply changed source item ID to %d", got)
	}
	if got := items[15].ID; got != 303 {
		t.Fatalf("stale apply changed target item ID to %d", got)
	}
}

func relocationTestPlayer() *Player {
	return &Player{
		SRObject: SRObject{RefObjectID: 1907},
		Level:    1,
	}
}

func relocationTestItem(id int, referenceID uint32, typeInfo TypeInfo) Item {
	return Item{
		SRObject: SRObject{
			RefObjectID: referenceID,
			TypeInfo:    typeInfo,
		},
		ID: id,
	}
}
