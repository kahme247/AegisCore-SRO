package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMediaCatalogImportFilesUsesWhitelistAndPreservesFields(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeRows := func(name string, rows [][]string) {
		t.Helper()
		encoded, err := encodeMediaRows(rows)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRows("characterdata_5000.txt", [][]string{{"1", "2", "NPC_TEST", ""}})
	writeRows("magicoption.txt", [][]string{{"1", "5", "MATTR_INT"}})
	writeRows("leveldata.txt", [][]string{{"1", "118"}})

	files, err := loadMediaCatalogImportFiles(directory)
	if err != nil {
		t.Fatalf("loadMediaCatalogImportFiles() error = %v", err)
	}
	if len(files) != 2 || files[0].Name != "characterdata_5000.txt" || files[1].Name != "magicoption.txt" {
		t.Fatalf("files = %#v", files)
	}
	if got := files[0].Rows[0]; len(got) != 4 || got[2] != "NPC_TEST" || got[3] != "" {
		t.Fatalf("preserved row = %#v", got)
	}
}

func TestPassthroughMediaCatalogWhitelist(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"characterdata_45000.txt", "magicoption.txt", "teleportlink.txt"} {
		if !isPassthroughMediaCatalog(name) {
			t.Fatalf("isPassthroughMediaCatalog(%q) = false", name)
		}
	}
	for _, name := range []string{"leveldata.txt", "../outside.txt", "characterdata_99999.txt", "Media.pk2"} {
		if isPassthroughMediaCatalog(name) {
			t.Fatalf("isPassthroughMediaCatalog(%q) = true", name)
		}
	}
}

func TestLocalMediaContainsAllPassthroughCatalogs(t *testing.T) {
	textData := filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata")
	if _, err := os.Stat(textData); err != nil {
		t.Skip("local Media textdata tree is unavailable")
	}
	files, err := loadMediaCatalogImportFiles(textData)
	if err != nil {
		t.Fatalf("load local Media catalogs: %v", err)
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		seen[file.Name] = true
	}
	for _, name := range unsupportedMediaCatalogs {
		if !seen[name] {
			t.Errorf("local passthrough catalog %s was not loaded", name)
		}
	}
}
