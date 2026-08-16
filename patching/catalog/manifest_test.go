package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestServingManifestMergesReleaseFilesAndVerificationChecksHashes(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	storageRoot := filepath.Join(root, "files")
	if err := os.MkdirAll(filepath.Join(storageRoot, "189"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("prepared")
	digest := sha256.Sum256(payload)
	if err := os.WriteFile(filepath.Join(storageRoot, "189", "payload.patch"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	first := Release{Files: []File{{ID: 4, StorageKey: "188/old.patch", PreparedSize: 3, PreparedSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	if err := WriteServingManifest(manifestPath, first); err != nil {
		t.Fatal(err)
	}
	second := Release{Files: []File{{ID: 7, StorageKey: "189/payload.patch", PreparedSize: uint64(len(payload)), PreparedSHA256: hex.EncodeToString(digest[:])}}}
	if err := WriteServingManifest(manifestPath, second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest ServingManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].ID != 4 || manifest.Files[1].ID != 7 {
		t.Fatalf("merged manifest = %#v", manifest.Files)
	}
	if err := VerifyReleaseStorage(second, manifestPath, storageRoot); err != nil {
		t.Fatalf("VerifyReleaseStorage() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageRoot, "189", "payload.patch"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseStorage(second, manifestPath, storageRoot); err == nil {
		t.Fatal("VerifyReleaseStorage() accepted a tampered payload")
	}
}
