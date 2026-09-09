package navmeshv2

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Harvi8/AegisCore/framework/pk2"
)

func TestNewLoaderSafeRejectsMalformedArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.pk2")
	if err := os.WriteFile(path, make([]byte, pk2.HeaderSize-1), 0o600); err != nil {
		t.Fatal(err)
	}
	loader, err := NewLoaderSafe(path)
	if loader != nil {
		t.Fatal("malformed archive unexpectedly returned a loader")
	}
	if !errors.Is(err, pk2.ErrArchiveHeader) {
		t.Fatalf("expected checked archive-header error, got %v", err)
	}
}

func TestLoaderCloseArchiveReleasesAndDetachesReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.pk2")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reader := pk2.NewPk2Reader(path)
	reader.FileCache["cached"] = []byte("payload")
	loader := newLoader(&reader, path)

	if err := loader.CloseArchive(); err != nil {
		t.Fatalf("CloseArchive() error = %v", err)
	}
	if loader.Pk2Reader != nil {
		t.Fatal("CloseArchive() retained PK2 reader")
	}
	if len(reader.FileCache) != 0 {
		t.Fatalf("CloseArchive() retained %d cached files", len(reader.FileCache))
	}
	if _, err := reader.ReadFile("cached"); !errors.Is(err, pk2.ErrArchiveClosed) {
		t.Fatalf("ReadFile() after CloseArchive error = %v, want ErrArchiveClosed", err)
	}
	if err := loader.CloseArchive(); err != nil {
		t.Fatalf("second CloseArchive() error = %v", err)
	}
}
