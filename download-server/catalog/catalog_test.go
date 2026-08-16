package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func testSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func TestLoadAndOpenVerifiedFile(t *testing.T) {
	root := t.TempDir()
	payload := []byte("prepared-patch")
	if err := os.WriteFile(filepath.Join(root, "patch.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	manifest := fmt.Sprintf(`{"files":[{"id":7,"storage_path":"patch.bin","size":%d,"sha256":"%s"}]}`, len(payload), testSHA256(payload))
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := Load(manifestPath, root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	file, size, err := files.Open(7)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	if size != uint64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
}

func TestLoadRejectsEscapingStoragePath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "files")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(parent, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(fmt.Sprintf(`{"files":[{"id":1,"storage_path":"../outside.bin","size":1,"sha256":"%s"}]}`, testSHA256([]byte("x")))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(manifestPath, root); err == nil {
		t.Fatal("Load() accepted a path outside the storage root")
	}
}

func TestOpenRejectsFileChangedAfterLoad(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "patch.bin")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(fmt.Sprintf(`{"files":[{"id":1,"storage_path":"patch.bin","size":3,"sha256":"%s"}]}`, testSHA256([]byte("one")))), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := Load(manifestPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := files.Open(1); err == nil {
		t.Fatal("Open() accepted a file changed after catalog load")
	}
}

func TestLoadRejectsDuplicateIDsAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "patch.bin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []string{
		fmt.Sprintf(`{"files":[{"id":1,"storage_path":"patch.bin","size":0,"sha256":"%s"},{"id":1,"storage_path":"patch.bin","size":0,"sha256":"%s"}]}`, testSHA256(nil), testSHA256(nil)),
		`{"files":[],"unexpected":true}`,
	}
	for _, manifest := range tests {
		manifestPath := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(manifestPath, root); err == nil {
			t.Fatalf("Load() accepted invalid manifest %s", manifest)
		}
	}
}
