package model

import (
	"errors"
	"testing"
)

func alchemyRef(type3, type4, itemClass, param1 int) RefItem {
	return RefItem{
		RefObject: RefObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: func() int {
			if type3 == 10 {
				return 3
			}
			return 1
		}(), TypeID3: type3, TypeID4: type4}},
		ItemClass: itemClass,
		Param1:    param1,
	}
}

func TestDecodeMagicOptionRange(t *testing.T) {
	for _, test := range []struct {
		packed       uint32
		minimum, max uint32
	}{
		{packed: 65538, minimum: 1, max: 2},
		{packed: 9830650, minimum: 150, max: 250},
	} {
		minimum, maximum, ok := decodeMagicOptionRange(test.packed)
		if !ok || minimum != test.minimum || maximum != test.max {
			t.Fatalf("decodeMagicOptionRange(%d) = %d..%d, %v; want %d..%d", test.packed, minimum, maximum, ok, test.minimum, test.max)
		}
	}
}

func TestRollMagicOptionValueStaysInsideInclusiveRange(t *testing.T) {
	if got := rollMagicOptionValue(1, 2, 0); got != 1 {
		t.Fatalf("minimum roll = %d, want 1", got)
	}
	if got := rollMagicOptionValue(1, 2, 0.999999); got != 2 {
		t.Fatalf("maximum roll = %d, want 2", got)
	}
}

func TestValidateElixirMaterialsMatchesTargetFamilyAndDegree(t *testing.T) {
	targets := []struct {
		name       string
		type3      int
		elixirKind int
	}{
		{"weapon", 6, alchemyElixirClassWeapon},
		{"shield", 4, alchemyElixirClassShield},
		{"accessory", 5, alchemyElixirClassAccessory},
		{"protector", 2, alchemyElixirClassProtector},
	}
	for _, test := range targets {
		t.Run(test.name, func(t *testing.T) {
			target := alchemyRef(test.type3, 1, 16, 0) // degree 6
			elixir := alchemyRef(10, 1, 0, test.elixirKind)
			powder := alchemyRef(10, 2, 6, 0)
			if err := validateElixirMaterials(target, elixir, &powder); err != nil {
				t.Fatalf("validateElixirMaterials() error = %v", err)
			}
			wrongPowder := powder
			wrongPowder.ItemClass = 5
			if err := validateElixirMaterials(target, elixir, &wrongPowder); !errors.Is(err, ErrAlchemyUnsupported) {
				t.Fatalf("wrong powder error = %v, want ErrAlchemyUnsupported", err)
			}
			wrongElixir := elixir
			wrongElixir.Param1++
			if err := validateElixirMaterials(target, wrongElixir, nil); !errors.Is(err, ErrAlchemyUnsupported) {
				t.Fatalf("wrong elixir error = %v, want ErrAlchemyUnsupported", err)
			}
		})
	}
}

func TestRemainingAlchemyStack(t *testing.T) {
	if got := remainingAlchemyStack(Item{StackSize: 1}); got != 0 {
		t.Fatalf("one-item stack remaining = %d, want 0", got)
	}
	if got := remainingAlchemyStack(Item{StackSize: 7}); got != 6 {
		t.Fatalf("seven-item stack remaining = %d, want 6", got)
	}
}

func TestEnhancementProbabilityAndAttributeMutation(t *testing.T) {
	ref := RefItem{Param2: 0x19140f0a, Param7: 0x0a0a0a0a, Param12: 0x0a050505}
	if got := reinforcementChance(ref, 0); got != 25 {
		t.Fatalf("+0 reinforcement chance = %d, want 25", got)
	}
	if got := reinforcementChance(ref, 4); got != 10 {
		t.Fatalf("+4 reinforcement chance = %d, want 10", got)
	}
	if !enhancementSucceeds(30, 0.29) || enhancementSucceeds(30, 0.30) {
		t.Fatal("reinforcement residue boundary does not match rand()%100 < chance")
	}
	target := alchemyRef(6, 2, 1, 0)
	variance, ok := applyAttributeStone(0, target, "NATTR_CRITICAL")
	if !ok || variance&VarianceStatMask != VarianceStatMask {
		t.Fatalf("critical attribute variance = %#x, %v", variance, ok)
	}
	if _, ok = applyAttributeStone(0, target, "NATTR_UNKNOWN"); ok {
		t.Fatal("unknown attribute stone unexpectedly applied")
	}
}
