package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMediaExportProducesDeterministicDBBackedFiles(t *testing.T) {
	t.Parallel()

	levelRaw, err := json.Marshal([]string{"1", "old-exp", "old-sp", "old-u1", "old-u2", "old-job1", "old-job2", "old-job3", "preserved"})
	if err != nil {
		t.Fatal(err)
	}
	skillFields := make([]string, 117)
	for i := range skillFields {
		skillFields[i] = "old"
	}
	skillRaw, err := json.Marshal(skillFields)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "Media-export")
	manifest, err := writeMediaExport(root,
		[]mediaExportLevel{{Level: 1, Experience: 118, SkillPoints: 2, DropGoldMin: 10, DropGoldMax: 20, Unknown1: 3, Unknown2: 4, JobExperience1: 5, JobExperience2: 6, JobExperience3: 7, Raw: string(levelRaw)}},
		[]mediaExportSkill{
			{ID: 4999, Raw: string(skillRaw), Values: map[int]string{0: "1", 1: "4999", 3: "SKILL_ONE"}},
			{ID: 5000, Raw: string(skillRaw), Values: map[int]string{0: "1", 1: "5000", 3: "SKILL_TWO"}},
		},
		[]mediaExportCatalog{{Name: "questdata.txt", Rows: [][]string{{"1", "27", "QNO_TEST"}}}})
	if err != nil {
		t.Fatalf("writeMediaExport() error = %v", err)
	}
	if len(manifest.Files) != 8 {
		t.Fatalf("manifest files = %d, want 8", len(manifest.Files))
	}

	textData := filepath.Join(root, filepath.FromSlash(mediaTextDataPath))
	levelText := mustReadClientText(t, filepath.Join(textData, "leveldata.txt"))
	if levelText != "1\t118\t2\t3\t4\t5\t6\t7\tpreserved\r\n" {
		t.Fatalf("leveldata.txt = %q", levelText)
	}
	if goldText := mustReadClientText(t, filepath.Join(textData, "levelgold.txt")); goldText != "1\t10\t20\r\n" {
		t.Fatalf("levelgold.txt = %q", goldText)
	}
	if listText := mustReadClientText(t, filepath.Join(textData, "skilldata.txt")); listText != "skilldata_5000.txt\r\nskilldata_10000.txt\r\n" {
		t.Fatalf("skilldata.txt = %q", listText)
	}
	if first := mustReadClientText(t, filepath.Join(textData, "skilldata_5000.txt")); !strings.Contains(first, "\t4999\told\tSKILL_ONE\t") {
		t.Fatalf("skilldata_5000.txt did not contain overlaid DB values")
	}
	if second := mustReadClientText(t, filepath.Join(textData, "skilldata_10000.txt")); !strings.Contains(second, "\t5000\told\tSKILL_TWO\t") {
		t.Fatalf("skilldata_10000.txt did not contain overlaid DB values")
	}
	plain, err := os.ReadFile(filepath.Join(textData, "skilldata_5000.txt"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := os.ReadFile(filepath.Join(textData, "skilldata_5000enc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encrypted, encryptSkillData(plain)) {
		t.Fatal("skilldata_5000enc.txt does not match the legacy encryption transform")
	}

	manifestBytes, err := os.ReadFile(filepath.Join(root, "media-export-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var diskManifest mediaExportManifest
	if err := json.Unmarshal(manifestBytes, &diskManifest); err != nil {
		t.Fatal(err)
	}
	for _, file := range diskManifest.Files {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatalf("read manifest file %q: %v", file.Path, err)
		}
		hash := sha256.Sum256(contents)
		if got := hex.EncodeToString(hash[:]); got != file.SHA256 {
			t.Fatalf("%s SHA-256 = %s, want %s", file.Path, got, file.SHA256)
		}
	}
}

func TestEncryptSkillDataKnownVector(t *testing.T) {
	t.Parallel()

	got := encryptSkillData([]byte{0, 1, 2, 3, 254, 255})
	if hex.EncodeToString(got) != "e3b3f333172f0d000a002f002f0065006e0063007200790070007400" {
		t.Fatalf("encryptSkillData() = %x", got)
	}
}

func TestLocalSkillEncryptionMatchesMedia(t *testing.T) {
	// This local pair has matching source/encrypted lengths. Other extracted
	// chunks can come from different client snapshots and are not valid pairs.
	plainPath := filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata", "skilldata_20000.txt")
	encryptedPath := filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata", "skilldata_20000enc.txt")
	plain, plainErr := os.ReadFile(plainPath)
	want, encryptedErr := os.ReadFile(encryptedPath)
	if errors.Join(plainErr, encryptedErr) != nil {
		t.Skip("local Media skill encryption reference files are unavailable")
	}
	if got := encryptSkillData(plain); !bytes.Equal(got, want) {
		t.Fatalf("encrypted local skill file differs: got %x, want %x", sha256.Sum256(got), sha256.Sum256(want))
	}
}

func TestExportMediaRequiresNewOutputDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	err := exportMedia(context.Background(), []string{"--output", directory}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("exportMedia() error = %v, want existing-directory error", err)
	}
}

func TestRawMediaFieldsRejectsIncompleteRows(t *testing.T) {
	t.Parallel()

	_, err := rawMediaFields(`["one"]`, 2)
	if err == nil || !strings.Contains(err.Error(), "want at least 2") {
		t.Fatalf("rawMediaFields() error = %v", err)
	}
	if _, err := rawMediaFields(`not-json`, 1); err == nil {
		t.Fatal("rawMediaFields() accepted invalid JSON")
	}
}

func TestEncodeMediaRowsRejectsFieldSeparators(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"tab\tvalue", "line\nvalue", "return\rvalue"} {
		if _, err := encodeMediaRows([][]string{{value}}); err == nil {
			t.Fatalf("encodeMediaRows(%q) accepted a field separator", value)
		}
	}
}

func mustReadClientText(t *testing.T, path string) string {
	t.Helper()
	contents, err := readClientTextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
