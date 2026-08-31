package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMediaExportScanManifestValidatesFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "manifest.json")
	valid := `{"schema_version":1,"profile":"vsro188","files":[{"path":"server_dep/silkroad/textdata/leveldata.txt","rows":140,"bytes":2,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadMediaExportScanManifest(path)
	if err != nil {
		t.Fatalf("loadMediaExportScanManifest() error = %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Rows != 140 {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestLoadMediaExportScanManifestRejectsUnsafeOrDuplicateFiles(t *testing.T) {
	t.Parallel()

	for name, contents := range map[string]string{
		"escape":         `{"schema_version":1,"profile":"vsro188","files":[{"path":"../outside.txt","rows":1,"bytes":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`,
		"duplicate":      `{"schema_version":1,"profile":"vsro188","files":[{"path":"A.txt","rows":1,"bytes":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"path":"a.txt","rows":1,"bytes":1,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`,
		"uppercase hash": `{"schema_version":1,"profile":"vsro188","files":[{"path":"a.txt","rows":1,"bytes":1,"sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`,
	} {
		name, contents := name, contents
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadMediaExportScanManifest(path); err == nil || strings.TrimSpace(err.Error()) == "" {
				t.Fatalf("loadMediaExportScanManifest() error = %v", err)
			}
		})
	}
}
