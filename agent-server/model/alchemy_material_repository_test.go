package model

import (
	"math"
	"testing"
)

func TestAlchemyResidueMatchesBoundedRandModulus(t *testing.T) {
	for _, test := range []struct {
		roll    float64
		modulus int
		want    int
	}{{0, 12, 0}, {0.499, 12, 5}, {0.999999, 12, 11}, {1, 12, 11}} {
		if got := alchemyResidue(func() float64 { return test.roll }, test.modulus); got != test.want {
			t.Fatalf("alchemyResidue(%v, %d) = %d, want %d", test.roll, test.modulus, got, test.want)
		}
	}
}

func TestRandomDismantleStoneFiltersFamilyAndDegreeDeterministically(t *testing.T) {
	original := RefItems
	t.Cleanup(func() { RefItems = original })
	RefItems = map[uint32]RefItem{
		30: {RefObject: RefObject{ID: 30, CodeName: "ITEM_ETC_ARCHEMY_ATTRSTONE_ER_04", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 11, TypeID4: 2}}, ItemClass: 4},
		10: {RefObject: RefObject{ID: 10, CodeName: "ITEM_ETC_ARCHEMY_ATTRSTONE_PA_04", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 11, TypeID4: 2}}, ItemClass: 4},
		20: {RefObject: RefObject{ID: 20, CodeName: "ITEM_ETC_ARCHEMY_MAGICSTONE_STR_04", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 11, TypeID4: 1}}, ItemClass: 4},
		40: {RefObject: RefObject{ID: 40, CodeName: "ITEM_ETC_ARCHEMY_ATTRSTONE_HR_05", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 11, TypeID4: 2}}, ItemClass: 5},
	}
	first, ok := randomDismantleStone(func() float64 { return 0 }, 4, []int{2})
	if !ok || first.ID != 10 {
		t.Fatalf("first attribute candidate = %d, %v; want 10", first.ID, ok)
	}
	last, ok := randomDismantleStone(func() float64 { return 0.999999 }, 4, []int{2})
	if !ok || last.ID != 30 {
		t.Fatalf("last attribute candidate = %d, %v; want 30", last.ID, ok)
	}
}

func TestDismantleStoneWeightsMatchDropAssignmentCatalog(t *testing.T) {
	for _, test := range []struct {
		code    string
		typeID4 int
		want    int
	}{
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_01", 1, 30},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_INT_01", 1, 60},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_HR_01", 1, 100},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_01", 7, 300},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_01", 7, 0},
		{"ITEM_ETC_ARCHEMY_ATTRSTONE_CRITICAL_01", 2, 30},
		{"ITEM_ETC_ARCHEMY_ATTRSTONE_PA_01", 2, 60},
		{"ITEM_ETC_ARCHEMY_ATTRSTONE_ER_01", 2, 100},
		{"ITEM_EVENT_ARCHEMY_MAGICSTONE_LUCK_01", 7, 0},
	} {
		ref := RefItem{RefObject: RefObject{CodeName: test.code, TypeInfo: TypeInfo{TypeID4: test.typeID4}}, ItemClass: 1}
		if got := dismantleStoneWeight(ref); got != test.want {
			t.Errorf("dismantleStoneWeight(%s) = %d, want %d", test.code, got, test.want)
		}
	}
}

func TestDismantleBinaryDivisorCatalogs(t *testing.T) {
	if len(dismantleGradeDivisors) != 12 || dismantleGradeDivisors[0] != 75 || dismantleGradeDivisors[11] != 1180 {
		t.Fatalf("element divisors = %v", dismantleGradeDivisors)
	}
	if len(dismantleStoneDivisors) != 12 || dismantleStoneDivisors[0] != 18750 || dismantleStoneDivisors[11] != 294706 {
		t.Fatalf("stone divisors = %v", dismantleStoneDivisors)
	}
}

func TestDismantleMeritOutputMapsScoreToCanonicalRecipe(t *testing.T) {
	weapon := func(itemClass, rarity, setID int) RefItem {
		return RefItem{RefObject: RefObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6}, Rarity: rarity}, ItemClass: itemClass, SetID: setID}
	}
	if got, ok := dismantleMeritCode(Item{}, weapon(1, 0, 0)); ok || got != "" {
		t.Fatalf("normal low-merit weapon produced %q", got)
	}
	if got, ok := dismantleMeritCode(Item{}, weapon(1, 2, 0)); !ok || got != "ITEM_ETC_ARCHEMY_UPPER_REINFORCE_RECIPE_WE_RARE_A_1" {
		t.Fatalf("rare class-one weapon output = %q, %v; want rare A", got, ok)
	}
	if got, ok := dismantleMeritCode(Item{}, weapon(3, 2, 0)); !ok || got != "ITEM_ETC_ARCHEMY_UPPER_REINFORCE_RECIPE_WE_RARE_B_1" {
		t.Fatalf("rare class-three weapon output = %q, %v; want rare B", got, ok)
	}
	if got, ok := dismantleMeritCode(Item{}, weapon(3, 0, 77)); !ok || got != "ITEM_ETC_ARCHEMY_UPPER_REINFORCE_RECIPE_WE_A_1" {
		t.Fatalf("normal set class-three weapon output = %q, %v; want A", got, ok)
	}
}

func TestDismantleVarianceMeritUsesCategoryCoefficients(t *testing.T) {
	maxWeaponVariance := uint64(0)
	for shift := 0; shift <= 30; shift += 5 {
		maxWeaponVariance |= uint64(VarianceStatMask) << shift
	}
	got := dismantleVarianceMerit(maxWeaponVariance, "WE", 0)
	want := (2.0 + 3 + 3 + 2 + 1 + 1 + 1) * 0.6 / 100
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("weapon variance merit = %v, want %v", got, want)
	}
}
