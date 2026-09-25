package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareMediaExportDestinationUsesConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, destination, err := prepareMediaExportDestination(root, "media-export-2026-07-27")
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedRoot != wantRoot || destination != filepath.Join(wantRoot, "media-export-2026-07-27") {
		t.Fatalf("root=%q destination=%q", resolvedRoot, destination)
	}
}

func TestPrepareMediaExportDestinationRejectsExistingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "existing"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareMediaExportDestination(root, "existing"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
}

func TestMediaExportDisplayDestinationShowsHostAddress(t *testing.T) {
	tests := map[string]struct {
		root string
		want string
	}{
		"forward slashes": {root: "C:/Silkroad/MediaExports/", want: "C:/Silkroad/MediaExports/export-1"},
		"backslashes":     {root: `C:\Silkroad\MediaExports\`, want: `C:\Silkroad\MediaExports\export-1`},
		"mixed Windows":   {root: `C:\Silkroad/MediaExports/subdir`, want: `C:\Silkroad\MediaExports\subdir\export-1`},
		"posix":           {root: "/srv/media/", want: "/srv/media/export-1"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := mediaExportDisplayDestination(test.root, "export-1"); got != test.want {
				t.Fatalf("destination = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMediaExportFailureMessageIsBounded(t *testing.T) {
	message := mediaExportFailureMessage(nil, strings.Repeat("x", 2000))
	if len(message) != 1000 {
		t.Fatalf("message length = %d", len(message))
	}
	if got := mediaExportFailureMessage(context.Canceled, "details"); got != "Media export timed out or was cancelled" {
		t.Fatalf("cancelled message = %q", got)
	}
}
