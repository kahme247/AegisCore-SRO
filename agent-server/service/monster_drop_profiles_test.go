package service

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestDropProfileFamilyClassificationMatchesRetainedItemFamilies(t *testing.T) {
	tests := []struct {
		type2, type3, type4, rarity int
		want                        DropProfileFamily
	}{
		{1, 6, 2, 0, DropProfileEquip},
		{1, 6, 2, 2, DropProfileRareEquip},
		{3, 1, 1, 0, DropProfileRecover},
		{3, 2, 6, 0, DropProfileCure},
		{3, 3, 1, 0, DropProfileScroll},
		{3, 4, 1, 0, DropProfileAmmo},
		{3, 10, 1, 0, DropProfileReinforce},
		{3, 11, 1, 0, DropProfileMagicStone},
		{3, 11, 2, 0, DropProfileAttributeStone},
		{3, 11, 3, 0, DropProfileTablet},
		{2, 1, 1, 0, ""},
	}
	for _, test := range tests {
		if got := ClassifyDropProfileFamily(test.type2, test.type3, test.type4, test.rarity); got != test.want {
			t.Fatalf("classify %d/%d/%d rarity %d = %q, want %q", test.type2, test.type3, test.type4, test.rarity, got, test.want)
		}
	}
}

func TestDropProfileAssignmentUsesRelativeWeightThenInclusiveAbsoluteGate(t *testing.T) {
	catalog := NewDropProfileCatalog(nil, []DropItemAssignment{
		{SourceRow: 20, Family: DropProfileRecover, ReferenceItemID: 200, Relative: 70, Absolute: 50, AssignedGroup: 1, DropCount: 5, Country: 1},
		{SourceRow: 10, Family: DropProfileRecover, ReferenceItemID: 100, Relative: 30, Absolute: 50, AssignedGroup: 1, DropCount: 1, Country: 1},
	}, nil)

	selected, accepted := catalog.RollAssignment(DropProfileRecover, 1, 1, &scriptedDropRandom{ints: []int{30, 50}})
	if !accepted || selected.ReferenceItemID != 200 || selected.DropCount != 5 {
		t.Fatalf("selected = %+v accepted=%t", selected, accepted)
	}
	selected, accepted = catalog.RollAssignment(DropProfileRecover, 1, 1, &scriptedDropRandom{ints: []int{0, 51}})
	if accepted || selected.ReferenceItemID != 100 {
		t.Fatalf("failed absolute selection = %+v accepted=%t", selected, accepted)
	}
}

func TestDropProfileClassSelectionsAreSortedAndReturnedByValue(t *testing.T) {
	catalog := NewDropProfileCatalog([]DropClassSelection{
		{Family: DropProfileEquip, MonsterLevel: 10, Group: 2, Probability: 0.2},
		{Family: DropProfileEquip, MonsterLevel: 10, Group: 1, Probability: 0.1},
	}, nil, nil)
	rows := catalog.ClassSelections(DropProfileEquip, 10)
	if len(rows) != 2 || rows[0].Group != 1 || rows[1].Group != 2 {
		t.Fatalf("class rows = %+v", rows)
	}
	rows[0].Group = 99
	if catalog.ClassSelections(DropProfileEquip, 10)[0].Group != 1 {
		t.Fatal("class selection snapshot was mutable")
	}
}

func TestDropProfileClassGroupUsesCumulativeNativeMillionMap(t *testing.T) {
	catalog := NewDropProfileCatalog([]DropClassSelection{
		{Family: DropProfileRecover, MonsterLevel: 10, Group: 0, Probability: 0.1},
		{Family: DropProfileRecover, MonsterLevel: 10, Group: 1, Probability: 0.1},
		{Family: DropProfileRecover, MonsterLevel: 10, Group: 2, Probability: 0.000001},
	}, nil, nil)
	if group, ok := catalog.RollClassGroup(DropProfileRecover, 10, 1, 1, &scriptedDropRandom{ints: []int{1, 0}}); !ok || group != 0 {
		t.Fatalf("low class roll = group %d ok=%t", group, ok)
	}
	if group, ok := catalog.RollClassGroup(DropProfileRecover, 10, 1, 1, &scriptedDropRandom{ints: []int{0, 4}}); !ok || group != 1 {
		t.Fatalf("second cumulative class roll = group %d ok=%t", group, ok)
	}
	if _, ok := catalog.RollClassGroup(DropProfileRecover, 10, 1, 1, &scriptedDropRandom{ints: []int{0, 10}}); ok {
		t.Fatal("roll above the cumulative map was accepted")
	}
	if group, ok := catalog.RollClassGroup(DropProfileRecover, 10, 3, 1, &scriptedDropRandom{ints: []int{0, 10}}); !ok || group != 1 {
		t.Fatalf("factor-adjusted class roll = group %d ok=%t", group, ok)
	}
}

func TestDropProfileOptionLevelUsesCumulativeMillionThresholdAndClamp(t *testing.T) {
	catalog := NewDropProfileCatalog(nil, nil, []DropOptLevelSelection{
		{Level: 0, Probability: 0.5},
		{Level: 1, Probability: 0.9},
		{Level: 12, Probability: 1},
	})
	if got := catalog.RollOptLevel(&scriptedDropRandom{ints: []int{0, 0}}); got != 0 {
		t.Fatalf("low option roll = %d", got)
	}
	if got := catalog.RollOptLevel(&scriptedDropRandom{ints: []int{32767, 32767}}); got != 1 {
		t.Fatalf("middle option roll = %d", got)
	}
	if got := catalog.RollOptLevel(&scriptedDropRandom{ints: []int{0, 30}}); got != 7 {
		t.Fatalf("high option roll clamp = %d", got)
	}
}

func TestGeneratedEquipmentVarianceUsesThreeDrawMinimumPerFiveBitSlot(t *testing.T) {
	accessoryRolls := make([]int, 6)
	for index := range accessoryRolls {
		accessoryRolls[index] = 32767
	}
	variance, durability, ok := GenerateEquipmentBaseState(DropItemAssignment{
		Family: DropProfileEquip, TypeID3: 5,
	}, &scriptedDropRandom{ints: accessoryRolls})
	if !ok || variance != 0x3ff || durability != 1 {
		t.Fatalf("accessory base state = variance %#x durability %d ok=%t", variance, durability, ok)
	}

	armorRolls := []int{16384, 16384, 16384}
	armorRolls = append(armorRolls, make([]int, 15)...)
	variance, durability, ok = GenerateEquipmentBaseState(DropItemAssignment{
		Family: DropProfileEquip, TypeID3: 1, DurabilityLower: 10, DurabilityUpper: 41,
	}, &scriptedDropRandom{ints: armorRolls})
	if !ok || variance&0x1f != 15 || durability != 25 {
		t.Fatalf("armor base state = variance %#x durability %d ok=%t", variance, durability, ok)
	}
}

func TestGenerateEquipmentMagicOptionsNativeAdmissionWeightAndValue(t *testing.T) {
	catalog := NewDropProfileCatalog(nil, nil, nil, []DropMagicOptionCandidate{
		{Country: 0, TypeID3: 6, TypeID4: 2, AssignmentSlot: 0, OptionID: 5, CodeName: "MATTR_INT", DropRatio: 1, ParamType: 7, PackedValue: 0x00010002},
		{Country: 0, TypeID3: 6, TypeID4: 2, AssignmentSlot: 1, OptionID: 65, CodeName: "MATTR_NOT_REPARABLE", DropRatio: 1, PackedValue: 1},
	})
	assignment := DropItemAssignment{Family: DropProfileRareEquip, Country: 0, TypeID3: 6, TypeID4: 2, MaxMagicOptions: 2}
	random := &scriptedDropRandom{ints: []int{5, 5, 5, 5, 0, 0, 0, 0, 1, 0, 0}}
	options := catalog.GenerateEquipmentMagicOptions(assignment, random)
	if len(options) != 1 || options[0] != (model.MagicOption{Type: 5, Value: 1, ParamType: 7}) {
		t.Fatalf("magic options = %+v", options)
	}

	assignment.Family = DropProfileEquip
	if options := catalog.GenerateEquipmentMagicOptions(assignment, &scriptedDropRandom{ints: []int{31}}); len(options) != 0 {
		t.Fatalf("normal equipment above admission gate produced %+v", options)
	}
}
