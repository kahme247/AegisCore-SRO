package model

import (
	"reflect"
	"testing"
)

func TestPurchaseStacks(t *testing.T) {
	tests := []struct {
		name     string
		maxStack int
		quantity uint16
		want     []int
	}{
		{name: "equipment", maxStack: 1, quantity: 1, want: []int{1}},
		{name: "equipment over limit", maxStack: 1, quantity: 2, want: nil},
		{name: "single potion stack", maxStack: 50, quantity: 50, want: []int{50}},
		{name: "potion over limit", maxStack: 50, quantity: 51, want: nil},
		{name: "missing limit", maxStack: 0, quantity: 1, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := purchaseStacks(test.maxStack, test.quantity); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("purchaseStacks(%d, %d) = %v, want %v", test.maxStack, test.quantity, got, test.want)
			}
		})
	}
}
