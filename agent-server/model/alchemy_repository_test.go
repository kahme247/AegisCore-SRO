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
