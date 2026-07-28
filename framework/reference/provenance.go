package reference

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/ferdoran/go-sro/protocol/profiles"
)

const (
	// ManifestSchemaVersion versions the deterministic provenance document,
	// not an imported or published database schema.
	ManifestSchemaVersion = 1

	archiveHeaderVersionOffset   = 30
	archiveHeaderEncryptedOffset = archiveHeaderVersionOffset + 4
	archiveHeaderSize            = archiveHeaderEncryptedOffset + 1
)

var (
	ErrInvalidProfileMetadata = errors.New("invalid reference profile metadata")
	ErrProfileSchema          = errors.New("reference profile schema mismatch")
	ErrProfileName            = errors.New("reference profile name mismatch")
	ErrProfileMetadata        = errors.New("reference profile metadata mismatch")
	ErrArchivePath            = errors.New("reference archive path is required")
	ErrArchiveSize            = errors.New("reference archive size mismatch")
	ErrArchiveChecksum        = errors.New("reference archive checksum mismatch")
	ErrArchiveFormat          = errors.New("reference archive format mismatch")
	ErrArchiveEncryption      = errors.New("reference archive encryption mismatch")
	ErrEvidencePending        = errors.New("reference profile evidence is pending")
	ErrPublishUnavailable     = errors.New("reference dataset publishing is unavailable")
)

// VerificationRequest explicitly separates the checked-in canonical profile
// from the operator-supplied profile. The latter must match the former before
// the archive is read.
type VerificationRequest struct {
	CanonicalProfile profiles.Profile
	Profile          profiles.Profile
	ArchivePath      string
	Sources          SourceManifest
}

// ArchiveProvenance is the immutable identity of the client archive used by a
// future importer. Path is a logical profile path, never the local input path.
type ArchiveProvenance struct {
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	ArchiveFormat string `json:"archive_format"`
	Encrypted     bool   `json:"encrypted"`
}

// ProvenanceManifest is emitted by the verify-only command. It provides an
// auditable input identity, but it is not a published reference dataset.
type ProvenanceManifest struct {
	ManifestSchemaVersion int               `json:"manifest_schema_version"`
	Profile               string            `json:"profile"`
	ProfileStatus         string            `json:"profile_status"`
	Archive               ArchiveProvenance `json:"archive"`
	Sources               []Source          `json:"sources"`
}

// VerifyArchive validates a supplied profile and file against the canonical
// profile. It reads only the fixed archive header and streams the archive into
// SHA-256; it never indexes or decodes PK2 entries and never writes a database.
func VerifyArchive(ctx context.Context, request VerificationRequest) (ProvenanceManifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	format, checksum, err := validateProfile(request.CanonicalProfile, request.Profile)
	if err != nil {
		return ProvenanceManifest{}, err
	}
	if strings.TrimSpace(request.ArchivePath) == "" {
		return ProvenanceManifest{}, ErrArchivePath
	}

	archive, err := os.Open(request.ArchivePath)
	if err != nil {
		return ProvenanceManifest{}, fmt.Errorf("open reference archive: %w", err)
	}
	defer archive.Close()

	info, err := archive.Stat()
	if err != nil {
		return ProvenanceManifest{}, fmt.Errorf("stat reference archive: %w", err)
	}
	if info.Size() != request.CanonicalProfile.DataPK2.SizeBytes {
		return ProvenanceManifest{}, fmt.Errorf("%w: got %d bytes, want %d", ErrArchiveSize, info.Size(), request.CanonicalProfile.DataPK2.SizeBytes)
	}

	version, encrypted, err := readArchiveHeader(archive)
	if err != nil {
		return ProvenanceManifest{}, err
	}
	if version != format {
		return ProvenanceManifest{}, fmt.Errorf("%w: got 0x%08X, want 0x%08X", ErrArchiveFormat, version, format)
	}
	if encrypted != request.CanonicalProfile.DataPK2.Encrypted {
		return ProvenanceManifest{}, fmt.Errorf("%w: got %t, want %t", ErrArchiveEncryption, encrypted, request.CanonicalProfile.DataPK2.Encrypted)
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return ProvenanceManifest{}, fmt.Errorf("rewind reference archive: %w", err)
	}
	actualChecksum, err := hashArchive(ctx, archive)
	if err != nil {
		return ProvenanceManifest{}, err
	}
	if actualChecksum != checksum {
		return ProvenanceManifest{}, fmt.Errorf("%w: got %s, want %s", ErrArchiveChecksum, actualChecksum, checksum)
	}

	var sources []Source
	if request.Sources != nil {
		sources, err = canonicalizeSources(request.Sources.Sources())
		if err != nil {
			return ProvenanceManifest{}, err
		}
	}
	if sources == nil {
		sources = make([]Source, 0)
	}

	return ProvenanceManifest{
		ManifestSchemaVersion: ManifestSchemaVersion,
		Profile:               request.CanonicalProfile.Profile,
		ProfileStatus:         request.CanonicalProfile.Status,
		Archive: ArchiveProvenance{
			Path:          request.CanonicalProfile.DataPK2.Path,
			SizeBytes:     request.CanonicalProfile.DataPK2.SizeBytes,
			SHA256:        checksum,
			ArchiveFormat: canonicalArchiveFormat(format),
			Encrypted:     request.CanonicalProfile.DataPK2.Encrypted,
		},
		Sources: sources,
	}, nil
}

// RequirePublishReady always rejects this scaffold as a publication path. It
// distinguishes the current evidence-pending profile so callers cannot mistake
// successful byte verification for capture-backed dataset readiness.
func (m ProvenanceManifest) RequirePublishReady() error {
	if m.ProfileStatus == profiles.StatusEvidencePending {
		return fmt.Errorf("%w: profile %q", ErrEvidencePending, m.Profile)
	}
	return fmt.Errorf("%w: verification manifests cannot publish datasets", ErrPublishUnavailable)
}

// CanonicalJSON serializes a manifest with normalized checksums and sorted
// sources. The use of structs rather than maps fixes field order as well.
func (m ProvenanceManifest) CanonicalJSON() ([]byte, error) {
	if m.ManifestSchemaVersion != ManifestSchemaVersion {
		return nil, fmt.Errorf("manifest schema version %d, want %d", m.ManifestSchemaVersion, ManifestSchemaVersion)
	}

	checksum, err := normalizeSHA256(m.Archive.SHA256)
	if err != nil {
		return nil, fmt.Errorf("archive checksum: %w", err)
	}
	format, err := parseArchiveFormat(m.Archive.ArchiveFormat)
	if err != nil {
		return nil, err
	}
	sources, err := canonicalizeSources(m.Sources)
	if err != nil {
		return nil, err
	}
	if sources == nil {
		sources = make([]Source, 0)
	}

	canonical := m
	canonical.Archive.SHA256 = checksum
	canonical.Archive.ArchiveFormat = canonicalArchiveFormat(format)
	canonical.Sources = sources
	return json.Marshal(canonical)
}

func validateProfile(canonical, supplied profiles.Profile) (uint32, string, error) {
	if canonical.SchemaVersion != profiles.CurrentSchemaVersion {
		return 0, "", fmt.Errorf("%w: canonical profile has schema %d", ErrInvalidProfileMetadata, canonical.SchemaVersion)
	}
	if canonical.Profile != profiles.VSRO188 {
		return 0, "", fmt.Errorf("%w: canonical profile %q", ErrInvalidProfileMetadata, canonical.Profile)
	}
	if supplied.SchemaVersion != canonical.SchemaVersion {
		return 0, "", fmt.Errorf("%w: got %d, want %d", ErrProfileSchema, supplied.SchemaVersion, canonical.SchemaVersion)
	}
	if supplied.Profile != canonical.Profile {
		return 0, "", fmt.Errorf("%w: got %q, want %q", ErrProfileName, supplied.Profile, canonical.Profile)
	}
	canonicalChecksum, err := normalizeSHA256(canonical.DataPK2.SHA256)
	if err != nil {
		return 0, "", fmt.Errorf("%w: canonical archive checksum: %v", ErrInvalidProfileMetadata, err)
	}
	suppliedChecksum, err := normalizeSHA256(supplied.DataPK2.SHA256)
	if err != nil {
		return 0, "", fmt.Errorf("%w: supplied archive checksum: %v", ErrProfileMetadata, err)
	}
	if suppliedChecksum != canonicalChecksum {
		return 0, "", fmt.Errorf("%w: archive checksum differs from canonical profile", ErrProfileMetadata)
	}

	canonicalFormat, err := parseArchiveFormat(canonical.DataPK2.ArchiveFormat)
	if err != nil {
		return 0, "", fmt.Errorf("%w: canonical archive format: %v", ErrInvalidProfileMetadata, err)
	}
	suppliedFormat, err := parseArchiveFormat(supplied.DataPK2.ArchiveFormat)
	if err != nil {
		return 0, "", fmt.Errorf("%w: supplied archive format: %v", ErrProfileMetadata, err)
	}
	if suppliedFormat != canonicalFormat {
		return 0, "", fmt.Errorf("%w: archive format differs from canonical profile", ErrProfileMetadata)
	}

	// A verification manifest represents this exact pinned client profile, not
	// merely an archive with the same checksum. Normalize the two fields that
	// have presentation-only differences, then require every remaining profile
	// field (client identity, locale/content ID, capture list, and promotion
	// requirements included) to agree with the canonical profile.
	normalizedCanonical := canonical
	normalizedSupplied := supplied
	normalizedCanonical.DataPK2.SHA256 = canonicalChecksum
	normalizedSupplied.DataPK2.SHA256 = suppliedChecksum
	normalizedCanonical.DataPK2.ArchiveFormat = canonicalArchiveFormat(canonicalFormat)
	normalizedSupplied.DataPK2.ArchiveFormat = canonicalArchiveFormat(suppliedFormat)
	if !reflect.DeepEqual(normalizedCanonical, normalizedSupplied) {
		return 0, "", fmt.Errorf("%w: supplied profile differs from canonical metadata", ErrProfileMetadata)
	}
	if canonical.DataPK2.SizeBytes < archiveHeaderSize {
		return 0, "", fmt.Errorf("%w: archive size %d is smaller than header", ErrInvalidProfileMetadata, canonical.DataPK2.SizeBytes)
	}

	return canonicalFormat, canonicalChecksum, nil
}

func readArchiveHeader(archive *os.File) (uint32, bool, error) {
	header := make([]byte, archiveHeaderSize)
	if _, err := io.ReadFull(archive, header); err != nil {
		return 0, false, fmt.Errorf("read reference archive header: %w", err)
	}

	flag := header[archiveHeaderEncryptedOffset]
	if flag != 0 && flag != 1 {
		return 0, false, fmt.Errorf("%w: invalid encryption flag %d", ErrArchiveEncryption, flag)
	}
	return binary.LittleEndian.Uint32(header[archiveHeaderVersionOffset:archiveHeaderEncryptedOffset]), flag == 1, nil
}

func hashArchive(ctx context.Context, archive *os.File) (string, error) {
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		count, err := archive.Read(buffer)
		if count > 0 {
			if _, writeErr := hash.Write(buffer[:count]); writeErr != nil {
				return "", fmt.Errorf("hash reference archive: %w", writeErr)
			}
		}
		if errors.Is(err, io.EOF) {
			return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
		}
		if err != nil {
			return "", fmt.Errorf("read reference archive: %w", err)
		}
	}
}

func normalizeSHA256(raw string) (string, error) {
	checksum := strings.ToUpper(strings.TrimSpace(raw))
	if len(checksum) != sha256.Size*2 {
		return "", fmt.Errorf("want %d hexadecimal characters, got %d", sha256.Size*2, len(checksum))
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return "", fmt.Errorf("invalid hexadecimal checksum: %w", err)
	}
	return checksum, nil
}

func parseArchiveFormat(raw string) (uint32, error) {
	format := strings.TrimSpace(raw)
	if len(format) != len("0x00000000") || !strings.HasPrefix(format, "0x") {
		return 0, fmt.Errorf("invalid archive format %q", raw)
	}
	value, err := strconv.ParseUint(format[2:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid archive format %q: %w", raw, err)
	}
	return uint32(value), nil
}

func canonicalArchiveFormat(format uint32) string {
	return fmt.Sprintf("0x%08X", format)
}
