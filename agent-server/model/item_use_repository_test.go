package model

import "testing"

func TestInventoryItemTypeMatchesVSRO188Formula(t *testing.T) {
	ref := RefItem{RefObject: RefObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 1}}}
	if got := InventoryItemType(ref); got != 0x08EC {
		t.Fatalf("InventoryItemType() = %#04x, want 0x08EC for HP recovery", got)
	}
	ref.IsCashItem = true
	if got := InventoryItemType(ref); got != 0x08ED {
		t.Fatalf("cash InventoryItemType() = %#04x, want 0x08ED", got)
	}
}

func TestRecoveryValuesAppliesReferenceFlatAndPercentValuesWithCaps(t *testing.T) {
	ref := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 3},
	}}
	ref.Param1, ref.Param2, ref.Param3, ref.Param4 = 120, 25, 50, 25
	hp, mp, changed := recoveryValues(ref, ItemUseVitals{HP: 10, MP: 20, MaxHP: 200, MaxMP: 200})
	if !changed || hp != 180 || mp != 120 {
		t.Fatalf("recoveryValues() = (%d, %d, %v), want (180, 120, true)", hp, mp, changed)
	}
	if hp, mp, changed = recoveryValues(ref, ItemUseVitals{HP: 200, MP: 200, MaxHP: 200, MaxMP: 200}); changed || hp != 200 || mp != 200 {
		t.Fatalf("full recoveryValues() = (%d, %d, %v), want no effect", hp, mp, changed)
	}
}

func TestRecoveryValuesRejectsNonRecoveryExpendables(t *testing.T) {
	ref := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 1}}}
	if hp, mp, ok := recoveryValues(ref, ItemUseVitals{HP: 50, MP: 50, MaxHP: 200, MaxMP: 200}); ok || hp != 50 || mp != 50 {
		t.Fatalf("scroll recoveryValues() = (%d, %d, %v), want unchanged unsupported item", hp, mp, ok)
	}
}
