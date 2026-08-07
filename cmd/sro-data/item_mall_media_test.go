package main

import (
	"path/filepath"
	"testing"
)

func TestCheckedInItemMallMedia(t *testing.T) {
	rows, err := loadItemMallPlacements(filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 235 {
		t.Fatalf("placements = %d, want 235", len(rows))
	}
}
