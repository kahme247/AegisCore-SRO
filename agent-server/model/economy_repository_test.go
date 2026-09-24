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
		{name: "legacy potion stack", maxStack: 50, quantity: 50, want: []int{50}},
		{name: "legacy potion over limit", maxStack: 50, quantity: 51, want: nil},
		{name: "large potion stack", maxStack: 50000, quantity: 50000, want: []int{50000}},
		{name: "large potion over limit", maxStack: 50000, quantity: 50001, want: nil},
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

func TestShopGoodCatalogTabPreservesMediaIndex(t *testing.T) {
	for _, tab := range []byte{0, 1, 2, 4} {
		if got := shopGoodCatalogTab(tab); got != tab {
			t.Fatalf("shopGoodCatalogTab(%d) = %d, want %d", tab, got, tab)
		}
	}
}

func TestResolveStorageMoveAmount(t *testing.T) {
	for _, test := range []struct {
		name                              string
		source, max, target               int
		requested                         uint16
		targetExists, targetSameReference bool
		want                              int
		valid                             bool
	}{
		{name: "whole move to empty", source: 40, max: 50, want: 40, valid: true},
		{name: "split to empty", source: 40, max: 50, requested: 10, want: 10, valid: true},
		{name: "merge", source: 20, max: 50, target: 25, requested: 20, targetExists: true, targetSameReference: true, want: 20, valid: true},
		{name: "merge overflow", source: 20, max: 50, target: 40, requested: 20, targetExists: true, targetSameReference: true},
		{name: "different target", source: 20, max: 50, target: 1, requested: 20, targetExists: true},
		{name: "too much", source: 20, max: 50, requested: 21},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid := resolveStorageMoveAmount(test.source, test.max, test.requested, test.targetExists, test.targetSameReference, test.target)
			if got != test.want || valid != test.valid {
				t.Fatalf("resolveStorageMoveAmount() = %d/%v, want %d/%v", got, valid, test.want, test.valid)
			}
		})
	}
}

func TestValidStorageCapacity(t *testing.T) {
	for _, test := range []struct {
		capacity int
		valid    bool
	}{
		{capacity: 0},
		{capacity: 150, valid: true},
		{capacity: 180, valid: true},
		{capacity: 255, valid: true},
		{capacity: 256},
	} {
		if got := validStorageCapacity(test.capacity); got != test.valid {
			t.Fatalf("validStorageCapacity(%d) = %v, want %v", test.capacity, got, test.valid)
		}
	}
}
