package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ferdoran/go-sro/protocol/profiles"
)

func TestRunVerifyEmitsManifestWithoutPublishing(t *testing.T) {
	t.Parallel()

	archive := make([]byte, 35)
	binary.LittleEndian.PutUint32(archive[30:34], 0x01000002)
	archive[34] = 1
	archivePath := filepath.Join(t.TempDir(), "Data.pk2")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	profile, err := profiles.VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}
	checksum := sha256.Sum256(archive)
	profile.DataPK2.SizeBytes = int64(len(archive))
	profile.DataPK2.SHA256 = strings.ToUpper(fmtChecksum(checksum[:]))
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	encodedProfile, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	if err := os.WriteFile(profilePath, encodedProfile, 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	var output bytes.Buffer
	err = runWithCanonicalProfile(context.Background(), []string{"verify", "--profile", profilePath, "--archive", archivePath}, &output, profile)
	if err != nil {
		t.Fatalf("runWithCanonicalProfile() error = %v", err)
	}
	if !strings.Contains(output.String(), `"manifest_schema_version":1`) || strings.Contains(output.String(), "publish") {
		t.Fatalf("verify output = %q, want only a provenance manifest", output.String())
	}
}

func TestRunRejectsAnythingButExplicitVerifyArguments(t *testing.T) {
	t.Parallel()

	canonical, err := profiles.VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}
	for name, arguments := range map[string][]string{
		"no subcommand":       nil,
		"unknown subcommand":  {"import"},
		"missing flags":       {"verify"},
		"positional argument": {"verify", "unexpected"},
	} {
		name, arguments := name, arguments
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := runWithCanonicalProfile(context.Background(), arguments, &bytes.Buffer{}, canonical)
			if !errors.Is(err, errUsage) {
				t.Fatalf("runWithCanonicalProfile(%q) error = %v, want usage error", arguments, err)
			}
		})
	}
}

func fmtChecksum(value []byte) string {
	const digits = "0123456789abcdef"
	buffer := make([]byte, len(value)*2)
	for index, current := range value {
		buffer[index*2] = digits[current>>4]
		buffer[index*2+1] = digits[current&0x0F]
	}
	return string(buffer)
}
