package builder

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func validSpec() BuildSpec {
	return BuildSpec{
		Release: ReleaseSpec{DivisionID: 22, ContentID: 22, ModuleID: 9, ModuleName: "SR_Client", BaseVersion: 188, TargetVersion: 189},
		Files:   []FileSpec{{Source: "media/itemdata.txt", Filename: "itemdata.txt", Path: `media\server_dep\silkroad\textdata`, ImportToPK2: true}},
	}
}

func TestBuildCreatesDeterministicLauncherCompatiblePayload(t *testing.T) {
	sourceRoot := t.TempDir()
	storageRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(sourceRoot, "media"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := []byte("approved itemdata contents\n")
	if err := os.WriteFile(filepath.Join(sourceRoot, "media", "itemdata.txt"), target, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Build(validSpec(), sourceRoot, storageRoot)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	second, err := Build(validSpec(), sourceRoot, storageRoot)
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if first.Files[0].PreparedSHA256 != second.Files[0].PreparedSHA256 || first.Files[0].StorageKey != second.Files[0].StorageKey {
		t.Fatal("identical source did not produce deterministic payload identity")
	}
	payload, err := os.ReadFile(filepath.Join(storageRoot, filepath.FromSlash(first.Files[0].StorageKey)))
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(payload[:4]); got != uint32(len(target)) {
		t.Fatalf("original size = %d, want %d", got, len(target))
	}
	// The launcher skips the uint32 size and two-byte zlib header, then gives
	// the remaining raw stream to DeflateStream.
	reader := flate.NewReader(bytes.NewReader(payload[6:]))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, target) {
		t.Fatalf("decoded payload = %q, want %q", decoded, target)
	}
}

func TestValidateSpecRejectsUnsafeOrNonIncrementalBuilds(t *testing.T) {
	tests := []BuildSpec{validSpec(), validSpec(), validSpec()}
	tests[0].Release.TargetVersion = 190
	tests[1].Files[0].Source = "../secret"
	tests[2].Files = append(tests[2].Files, tests[2].Files[0])
	for _, spec := range tests {
		if err := ValidateSpec(spec); err == nil {
			t.Fatalf("ValidateSpec() accepted %#v", spec)
		}
	}
}
