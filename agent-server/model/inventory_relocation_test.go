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

func TestInventoryStackMergeMovesRequestedQuantity(t *testing.T) {
	previous := RefItems
	RefItems = map[uint32]RefItem{1001: {StackSize: 100}}
	t.Cleanup(func() { RefItems = previous })

	items := map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
		15: relocationTestItem(202, 1001, TypeInfo{}),
	}
	source, target := items[14], items[15]
	source.StackSize, target.StackSize = 40, 50
	items[14], items[15] = source, target

	plan, err := PlanInventoryRelocationAmount(items, 14, 15, 10, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocationAmount() error = %v", err)
	}
	if plan.Action != InventoryRelocationMerge || plan.TransferQuantity != 10 {
		t.Fatalf("merge plan = %+v", plan)
	}
	if err = ApplyInventoryRelocation(items, plan); err != nil {
		t.Fatalf("ApplyInventoryRelocation() error = %v", err)
	}
	if items[14].StackSize != 30 || items[15].StackSize != 60 {
		t.Fatalf("merged stacks = %d/%d, want 30/60", items[14].StackSize, items[15].StackSize)
	}
}

func TestInventoryStackMergeRemovesConsumedSourceAndRejectsOverflow(t *testing.T) {
	previous := RefItems
	RefItems = map[uint32]RefItem{1001: {StackSize: 100}}
	t.Cleanup(func() { RefItems = previous })

	items := map[byte]Item{
		14: relocationTestItem(101, 1001, TypeInfo{}),
		15: relocationTestItem(202, 1001, TypeInfo{}),
	}
	source, target := items[14], items[15]
	source.StackSize, target.StackSize = 10, 90
	items[14], items[15] = source, target

	plan, err := PlanInventoryRelocationAmount(items, 14, 15, 10, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocationAmount() error = %v", err)
	}
	if err = ApplyInventoryRelocation(items, plan); err != nil {
		t.Fatalf("ApplyInventoryRelocation() error = %v", err)
	}
	if _, exists := items[14]; exists || items[15].StackSize != 100 {
		t.Fatalf("full merge left source=%v target=%d", exists, items[15].StackSize)
	}

	items[14], items[15] = source, target
	if _, err = PlanInventoryRelocationAmount(items, 14, 15, 11, relocationTestPlayer()); !errors.Is(err, ErrInventoryRelocationNotAllowed) {
		t.Fatalf("overflow merge error = %v, want %v", err, ErrInventoryRelocationNotAllowed)
	}
}

func TestInventoryStackSplitPlansRequestedQuantityIntoEmptySlot(t *testing.T) {
	previous := RefItems
	RefItems = map[uint32]RefItem{1001: {StackSize: 100}}
	t.Cleanup(func() { RefItems = previous })

	item := relocationTestItem(101, 1001, TypeInfo{})
	item.StackSize = 40
	items := map[byte]Item{14: item}
	plan, err := PlanInventoryRelocationAmount(items, 14, 15, 10, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocationAmount() error = %v", err)
	}
	if plan.Action != InventoryRelocationSplit || plan.TransferQuantity != 10 || plan.SourceStackBefore != 40 || plan.TargetItemID != 0 {
		t.Fatalf("split plan = %+v", plan)
	}
	if items[14].StackSize != 40 {
		t.Fatalf("planning mutated source stack to %d", items[14].StackSize)
	}
}

func TestInventoryEquipmentMoveIgnoresStockQuantityOne(t *testing.T) {
	previous := RefItems
	RefItems = map[uint32]RefItem{1001: {StackSize: 1}}
	t.Cleanup(func() { RefItems = previous })

	weapon := relocationTestItem(101, 1001, TypeInfo{
		TypeID1: TypeID1Item,
		TypeID2: TypeID2Wearable,
		TypeID3: TypeID3Weapon,
		TypeID4: 2,
	})
	items := map[byte]Item{SlotPrimaryWeapon: weapon}
	plan, err := PlanInventoryRelocationAmount(items, SlotPrimaryWeapon, 19, 1, relocationTestPlayer())
	if err != nil {
		t.Fatalf("PlanInventoryRelocationAmount() error = %v", err)
	}
	if plan.Action != InventoryRelocationUnequip || plan.SourceSlot != SlotPrimaryWeapon || plan.TargetSlot != 19 {
		t.Fatalf("equipment move plan = %+v, want unequip 6 -> 19", plan)
	}
}

func TestInventoryWholeStackQuantityStillPlansOrdinaryMove(t *testing.T) {
	previous := RefItems
	RefItems = map[uint32]RefItem{1001: {StackSize: 100}}
	t.Cleanup(func() { RefItems = previous })

	item := relocationTestItem(101, 1001, TypeInfo{})
	item.StackSize = 40
	plan, err := PlanInventoryRelocationAmount(map[byte]Item{14: item}, 14, 15, 40, relocationTestPlayer())
	if err != nil || plan.Action != InventoryRelocationMove {
		t.Fatalf("whole-stack plan = %+v, error = %v", plan, err)
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

func TestPlanInventoryRelocationAllowsOnlyMatchingJobSuitInExtraSlot(t *testing.T) {
	previous := RefItems
	t.Cleanup(func() { RefItems = previous })
	RefItems = map[uint32]RefItem{
		2001: {RefObject: RefObject{ID: 2001, CodeName: "ITEM_CH_M_TRADE_THIEF_02"}},
		2002: {RefObject: RefObject{ID: 2002, CodeName: "ITEM_CH_M_TRADE_HUNTER_02"}},
	}
	player := relocationTestPlayer()
	player.SetJob(PlayerJob{Type: PlayerJobThief, Level: 1})
	items := map[byte]Item{14: relocationTestItem(101, 2001, TypeInfo{})}
	plan, err := PlanInventoryRelocation(items, 14, SlotExtra, player)
	if err != nil || plan.Action != InventoryRelocationEquip {
		t.Fatalf("matching thief suit plan = %+v, err=%v", plan, err)
	}
	player.Inventory.Items = map[byte]Item{SlotExtra: relocationTestItem(101, 2001, TypeInfo{})}
	if !HasActiveJobSuit(player) {
		t.Fatal("equipped matching thief suit was not recognized as active")
	}
	player.SetJob(PlayerJob{Type: PlayerJobHunter, Level: 1})
	if HasActiveJobSuit(player) {
		t.Fatal("hunter was authorized for a trade transport")
	}
	player.SetJob(PlayerJob{Type: PlayerJobThief, Level: 1})
	items[14] = relocationTestItem(102, 2002, TypeInfo{})
	if _, err = PlanInventoryRelocation(items, 14, SlotExtra, player); !errors.Is(err, ErrInventoryRelocationNotAllowed) {
		t.Fatalf("hunter suit error = %v, want not allowed", err)
	}
}

func TestPlanInventoryRelocationUsesCharacterCapacityWithinWireRange(t *testing.T) {
	weapon := relocationTestItem(101, 1001, TypeInfo{
		TypeID1: TypeID1Item,
		TypeID2: TypeID2Wearable,
		TypeID3: TypeID3Weapon,
		TypeID4: 2,
	})
	weapon.RequiredLevelType1 = 1
	weapon.RequiredLevel1 = 1
	items := map[byte]Item{111: weapon}
	player := relocationTestPlayer()
	player.InventorySize = MaxInventoryWireSize

	plan, err := PlanInventoryRelocation(items, 111, SlotPrimaryWeapon, player)
	if err != nil {
		t.Fatalf("PlanInventoryRelocation() error = %v", err)
	}
	if plan.Action != InventoryRelocationEquip {
		t.Fatalf("plan action = %d, want equip", plan.Action)
	}

	player.InventorySize = BaseInventorySize
	if _, err := PlanInventoryRelocation(items, 111, SlotPrimaryWeapon, player); !errors.Is(err, ErrInventoryRelocationSlotOutOfRange) {
		t.Fatalf("PlanInventoryRelocation() error = %v, want capacity rejection", err)
	}
}

func TestPlanInventoryRelocationRejectsSlotAboveWireCeiling(t *testing.T) {
	player := relocationTestPlayer()
	player.InventorySize = MaxInventoryWireSize
	items := map[byte]Item{111: relocationTestItem(101, 1001, TypeInfo{})}
	if _, err := PlanInventoryRelocation(items, 111, MaxInventoryWireSize, player); !errors.Is(err, ErrInventoryRelocationSlotOutOfRange) {
		t.Fatalf("PlanInventoryRelocation() error = %v, want wire-range rejection", err)
	}
}

func TestCanEquipSecondaryWeaponAllowsMatchingAmmunition(t *testing.T) {
	bow := relocationTestItem(101, 1001, TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: TypeID3Weapon, TypeID4: 6})
	arrow := relocationTestItem(202, 2002, TypeInfo{TypeID1: TypeID1Item, TypeID2: 3, TypeID3: 4, TypeID4: 1})
	arrow.RequiredLevelType1 = 1
	arrow.RequiredLevel1 = 1
	player := relocationTestPlayer()
	player.SRObject.RefObjectID = 1907 // Chinese
	inventory := NewInventory(map[byte]Item{SlotPrimaryWeapon: bow})

	if !inventory.canEquipSecondaryWeapon(arrow, player) {
		t.Fatal("canEquipSecondaryWeapon() rejected arrows with an equipped bow")
	}
	items := map[byte]Item{SlotPrimaryWeapon: bow, 13: arrow}
	plan, err := PlanInventoryRelocation(items, 13, SlotSecondaryWeapon, player)
	if err != nil {
		t.Fatalf("PlanInventoryRelocation() error = %v", err)
	}
	if plan.Action != InventoryRelocationEquip || !plan.SourceAmmunition {
		t.Fatalf("arrow plan = %+v, want ammunition equip", plan)
	}
}

func TestPlanInventoryRelocationDisplacesSecondaryForTwoHandedPrimary(t *testing.T) {
	twoHanded := relocationTestItem(101, 1001, TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: TypeID3Weapon, TypeID4: 4})
	twoHanded.RequiredLevelType1 = 1
	twoHanded.RequiredLevel1 = 1
	shield := relocationTestItem(202, 2002, TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: 4, TypeID4: 1})
	items := map[byte]Item{14: twoHanded, SlotSecondaryWeapon: shield}
	player := relocationTestPlayer()
	player.SRObject.RefObjectID = 1907 // Chinese

	plan, err := PlanInventoryRelocation(items, 14, SlotPrimaryWeapon, player)
	if err != nil {
		t.Fatalf("PlanInventoryRelocation() error = %v", err)
	}
	if len(plan.AdditionalMoves) != 1 {
		t.Fatalf("additional moves = %+v, want one secondary displacement", plan.AdditionalMoves)
	}
	move := plan.AdditionalMoves[0]
	if move.SourceSlot != SlotSecondaryWeapon || move.TargetSlot != 14 || move.SourceItemID != 202 || move.SourceReferenceID != 2002 {
		t.Fatalf("additional move = %+v, want secondary item into vacated source slot", move)
	}
	if err := ApplyInventoryRelocation(items, plan); err != nil {
		t.Fatalf("ApplyInventoryRelocation() error = %v", err)
	}
	if got := items[SlotPrimaryWeapon].ID; got != 101 {
		t.Fatalf("primary item ID = %d, want 101", got)
	}
	if got := items[14].ID; got != 202 {
		t.Fatalf("displaced secondary ID = %d, want 202", got)
	}
	if _, exists := items[SlotSecondaryWeapon]; exists {
		t.Fatal("secondary slot still occupied after two-handed equip")
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
