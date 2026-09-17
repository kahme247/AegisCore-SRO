package model

import "testing"

func TestItemMallStacksSplitsAtReferenceLimit(t *testing.T) {
	got := itemMallStacks(3, 8)
	want := []int{3, 3, 2}
	if len(got) != len(want) {
		t.Fatalf("stacks = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("stacks = %v, want %v", got, want)
		}
	}
	if got := itemMallStacks(0, 1); got != nil {
		t.Fatalf("invalid stack limit returned %v", got)
	}
}

func TestReverseReturnMultiBuyPreservesPackageResultGroups(t *testing.T) {
	got := itemMallPackageStacks(50, 11, 5)
	want := []int{11, 11, 11, 11, 11}
	if len(got) != len(want) {
		t.Fatalf("stacks = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("stacks = %v, want %v", got, want)
		}
	}
}

func TestItemMallPackageStacksSplitsEachOversizedPackageIndependently(t *testing.T) {
	got := itemMallPackageStacks(50, 55, 2)
	want := []int{50, 5, 50, 5}
	if len(got) != len(want) {
		t.Fatalf("stacks = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("stacks = %v, want %v", got, want)
		}
	}
}

func TestItemMallBalanceSelectsAllCurrencies(t *testing.T) {
	balances := ItemMallBalances{Silk: 10, GiftSilk: 20, Point: 30}
	if itemMallBalance(balances, ItemMallSilk) != 10 || itemMallBalance(balances, ItemMallGiftSilk) != 20 || itemMallBalance(balances, ItemMallPoint) != 30 {
		t.Fatalf("currency selection failed for %+v", balances)
	}
}
