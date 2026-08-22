package model

import "testing"

func TestBaseInventorySizeProvidesThreeBagPages(t *testing.T) {
	if got, want := InventoryPages, byte(3); got != want {
		t.Fatalf("inventory pages = %d, want %d", got, want)
	}
	if got, want := BaseInventorySize, byte(109); got != want {
		t.Fatalf("base inventory size = %d, want %d (13 equipment + 3*32 bag slots)", got, want)
	}
}
