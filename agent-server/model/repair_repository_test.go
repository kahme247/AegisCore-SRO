package model

import "testing"

func TestMaximumItemDurabilityUsesStockVarianceField(t *testing.T) {
	tests := []struct {
		name     string
		lower    float64
		upper    float64
		variance uint64
		typeID3  int
		want     uint32
	}{
		{name: "weapon", lower: 46, upper: 68, variance: uint64(27) << 30, typeID3: TypeID3Weapon, want: 65},
		{name: "armor", lower: 20, upper: 51, variance: uint64(31) << 25, typeID3: 3, want: 51},
		{name: "shield", lower: 40, upper: 71, variance: uint64(0) << 25, typeID3: 4, want: 40},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := maximumItemDurability(test.lower, test.upper, test.variance, test.typeID3); got != test.want {
				t.Fatalf("maximumItemDurability() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestMaximumItemDurabilityRejectsInvalidReferenceRange(t *testing.T) {
	if got := maximumItemDurability(0, 10, 0, TypeID3Weapon); got != 0 {
		t.Fatalf("maximumItemDurability() = %d, want 0", got)
	}
	if got := maximumItemDurability(20, 10, 0, TypeID3Weapon); got != 0 {
		t.Fatalf("maximumItemDurability() = %d, want 0", got)
	}
}
