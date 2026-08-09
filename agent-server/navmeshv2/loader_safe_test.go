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
