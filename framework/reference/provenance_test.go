package reference

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/protocol/profiles"
)

func TestVerifyArchiveProducesDeterministicProvenance(t *testing.T) {
	t.Parallel()

	archivePath, profile := fixtureArchive(t, 0x01000002, true, []byte("fixture payload"))
	sources, err := NewSourceManifest([]Source{
		{ID: "pk2/z.txt", Kind: "pk2-entry", SHA256: checksumB},
		{ID: "pk2/a.txt", Kind: "pk2-entry", SHA256: checksumA},
	})
	if err != nil {
		t.Fatalf("NewSourceManifest() error = %v", err)
	}

	manifest, err := VerifyArchive(context.Background(), VerificationRequest{
		CanonicalProfile: profile,
		Profile:          profile,
		ArchivePath:      archivePath,
		Sources:          sources,
	})
	if err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}
	if manifest.Profile != profiles.VSRO188 || manifest.Archive.SHA256 != profile.DataPK2.SHA256 {
		t.Fatalf("manifest = %#v, want profile/archive identity", manifest)
	}
	if len(manifest.Sources) != 2 || manifest.Sources[0].ID != "pk2/a.txt" {
		t.Fatalf("manifest sources = %#v, want canonical source ordering", manifest.Sources)
	}

	encoded, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	expected := `{"manifest_schema_version":1,"profile":"vsro188","profile_status":"evidence-pending","archive":{"path":"Data/Data.pk2","size_bytes":50,"sha256":"` + profile.DataPK2.SHA256 + `","archive_format":"0x01000002","encrypted":true},"sources":[{"id":"pk2/a.txt","kind":"pk2-entry","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"pk2/z.txt","kind":"pk2-entry","sha256":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"}]}`
	if string(encoded) != expected {
		t.Fatalf("CanonicalJSON() = %s\nwant %s", encoded, expected)
	}
}

func TestVerifyArchiveRejectsProfileSchemaAndNameMismatch(t *testing.T) {
	t.Parallel()

	archivePath, profile := fixtureArchive(t, 0x01000002, true, nil)

	schemaMismatch := profile
	schemaMismatch.SchemaVersion++
	_, err := VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: profile, Profile: schemaMismatch, ArchivePath: archivePath})
	if !errors.Is(err, ErrProfileSchema) {
		t.Fatalf("schema mismatch error = %v, want schema mismatch", err)
	}

	nameMismatch := profile
	nameMismatch.Profile = "other"
	_, err = VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: profile, Profile: nameMismatch, ArchivePath: archivePath})
	if !errors.Is(err, ErrProfileName) {
		t.Fatalf("name mismatch error = %v, want profile name mismatch", err)
	}
}

func TestVerifyArchiveRejectsAnyCanonicalProfileMetadataMismatch(t *testing.T) {
	t.Parallel()

	archivePath, profile := fixtureArchive(t, 0x01000002, true, nil)
	metadataMismatch := profile
	locale := "en-US"
	metadataMismatch.Locale = &locale

	_, err := VerifyArchive(context.Background(), VerificationRequest{
		CanonicalProfile: profile,
		Profile:          metadataMismatch,
		ArchivePath:      archivePath,
	})
	if !errors.Is(err, ErrProfileMetadata) {
		t.Fatalf("metadata mismatch error = %v, want profile metadata mismatch", err)
	}
}

func TestVerifyArchiveRejectsSizeHashAndDeclaredHeaderMismatch(t *testing.T) {
	t.Parallel()

	archivePath, profile := fixtureArchive(t, 0x01000002, true, nil)

	sizeMismatch := profile
	sizeMismatch.DataPK2.SizeBytes++
	_, err := VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: sizeMismatch, Profile: sizeMismatch, ArchivePath: archivePath})
	if !errors.Is(err, ErrArchiveSize) {
		t.Fatalf("size mismatch error = %v, want archive size mismatch", err)
	}

	hashMismatch := profile
	hashMismatch.DataPK2.SHA256 = checksumA
	_, err = VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: hashMismatch, Profile: hashMismatch, ArchivePath: archivePath})
	if !errors.Is(err, ErrArchiveChecksum) {
		t.Fatalf("checksum mismatch error = %v, want archive checksum mismatch", err)
	}

	formatMismatch := profile
	formatMismatch.DataPK2.ArchiveFormat = "0x01000003"
	_, err = VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: formatMismatch, Profile: formatMismatch, ArchivePath: archivePath})
	if !errors.Is(err, ErrArchiveFormat) {
		t.Fatalf("format mismatch error = %v, want archive format mismatch", err)
	}

	encryptionMismatch := profile
	encryptionMismatch.DataPK2.Encrypted = false
	_, err = VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: encryptionMismatch, Profile: encryptionMismatch, ArchivePath: archivePath})
	if !errors.Is(err, ErrArchiveEncryption) {
		t.Fatalf("encryption mismatch error = %v, want archive encryption mismatch", err)
	}
}

func TestEvidencePendingManifestCannotPublish(t *testing.T) {
	t.Parallel()

	archivePath, profile := fixtureArchive(t, 0x01000002, true, nil)
	manifest, err := VerifyArchive(context.Background(), VerificationRequest{CanonicalProfile: profile, Profile: profile, ArchivePath: archivePath})
	if err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}
	if !errors.Is(manifest.RequirePublishReady(), ErrEvidencePending) {
		t.Fatalf("RequirePublishReady() = %v, want evidence-pending error", manifest.RequirePublishReady())
	}
}

func fixtureArchive(t *testing.T, format uint32, encrypted bool, payload []byte) (string, profiles.Profile) {
	t.Helper()

	archive := make([]byte, archiveHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(archive[archiveHeaderVersionOffset:archiveHeaderEncryptedOffset], format)
	if encrypted {
		archive[archiveHeaderEncryptedOffset] = 1
	}
	copy(archive[archiveHeaderSize:], payload)

	path := filepath.Join(t.TempDir(), "Data.pk2")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatalf("write archive fixture: %v", err)
	}

	profile, err := profiles.VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}
	checksum := sha256.Sum256(archive)
	profile.DataPK2.SizeBytes = int64(len(archive))
	profile.DataPK2.SHA256 = strings.ToUpper(fmtChecksum(checksum[:]))
	profile.DataPK2.ArchiveFormat = "0x01000002"
	profile.DataPK2.Encrypted = encrypted
	return path, profile
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
