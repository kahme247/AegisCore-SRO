package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalItemMallMedia(t *testing.T) {
	textData := filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata")
	if _, err := os.Stat(textData); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Skip("local Media reference tree is unavailable")
		}
		t.Fatalf("stat local Media reference tree: %v", err)
	}
	rows, err := loadItemMallPlacements(textData)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 235 {
		t.Fatalf("placements = %d, want 235", len(rows))
	}
}
