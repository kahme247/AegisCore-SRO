package navmeshv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// NavmeshCacheManifestSchemaVersion identifies the on-disk cache contract.
	// Version 3 switches region artifacts to pointer-owned terrain records so
	// the runtime can share one immutable terrain object without value copies.
	// It is deliberately independent from the PK2 format version.
	NavmeshCacheManifestSchemaVersion = 3
	// NavmeshCacheProfile is the only client profile this cache can serve.
	NavmeshCacheProfile = "vsro188"
)

var (
	ErrNavmeshCacheManifest       = errors.New("invalid navmesh cache manifest")
	ErrNavmeshCacheProfile        = errors.New("navmesh cache profile mismatch")
	ErrNavmeshCacheArchive        = errors.New("navmesh cache archive checksum mismatch")
	ErrNavmeshCacheArtifact       = errors.New("invalid navmesh cache artifact")
	ErrNavmeshCacheChecksum       = errors.New("navmesh cache artifact checksum mismatch")
	ErrNavmeshCacheSize           = errors.New("navmesh cache artifact size mismatch")
	ErrNavmeshCachePath           = errors.New("invalid navmesh cache artifact path")
	ErrNavmeshCacheDuplicateFiles = errors.New("navmesh cache artifacts must be distinct")
)

// CacheFile identifies one immutable cache artifact relative to the manifest
// directory. Keeping the path relative makes the manifest portable while the
// checksum and size make a stale or partial artifact fail closed.
type CacheFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// CacheManifest binds both generated GOB artifacts to one client profile and
// one exact source archive. The runtime must validate it before decoding any
// cache bytes.
type CacheManifest struct {
	SchemaVersion int       `json:"schema_version"`
	Profile       string    `json:"profile"`
	ArchiveSHA256 string    `json:"archive_sha256"`
	RegionData    CacheFile `json:"region_data"`
	ObjectData    CacheFile `json:"object_data"`
}

// CacheManifestPath returns the manifest path associated with the existing
// region/object prefix convention.
func CacheManifestPath(prefix string) string {
	return prefix + "_manifest.json"
}

// BuildCacheManifest describes two already-written artifacts without decoding
// them. Both files must be reachable from the same manifest directory.
func BuildCacheManifest(ctx context.Context, regionPath, objectPath, profile, archiveSHA256 string) (CacheManifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(profile) == "" {
		return CacheManifest{}, fmt.Errorf("%w: profile is required", ErrNavmeshCacheManifest)
	}
	normalizedArchiveSHA, err := normalizeCacheSHA256(archiveSHA256)
	if err != nil {
		return CacheManifest{}, fmt.Errorf("%w: archive checksum: %v", ErrNavmeshCacheManifest, err)
	}

	manifestDirectory := filepath.Dir(regionPath)
	region, err := describeCacheFile(ctx, manifestDirectory, regionPath)
	if err != nil {
		return CacheManifest{}, fmt.Errorf("region data: %w", err)
	}
	object, err := describeCacheFile(ctx, manifestDirectory, objectPath)
	if err != nil {
		return CacheManifest{}, fmt.Errorf("object data: %w", err)
	}
	if region.Path == object.Path {
		return CacheManifest{}, ErrNavmeshCacheDuplicateFiles
	}

	manifest := CacheManifest{
		SchemaVersion: NavmeshCacheManifestSchemaVersion,
		Profile:       strings.TrimSpace(profile),
		ArchiveSHA256: normalizedArchiveSHA,
		RegionData:    region,
		ObjectData:    object,
	}
	if err := manifest.validateMetadata(); err != nil {
		return CacheManifest{}, err
	}
	return manifest, nil
}

// ReadCacheManifest decodes exactly one strict JSON manifest.
func ReadCacheManifest(path string) (CacheManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return CacheManifest{}, fmt.Errorf("open navmesh cache manifest: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest CacheManifest
	if err := decoder.Decode(&manifest); err != nil {
		return CacheManifest{}, fmt.Errorf("%w: decode: %v", ErrNavmeshCacheManifest, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return CacheManifest{}, fmt.Errorf("%w: more than one JSON value", ErrNavmeshCacheManifest)
		}
		return CacheManifest{}, fmt.Errorf("%w: trailing data: %v", ErrNavmeshCacheManifest, err)
	}
	if err := manifest.validateMetadata(); err != nil {
		return CacheManifest{}, err
	}
	return manifest, nil
}

// CanonicalJSON emits deterministic field order and normalized checksums.
func (m CacheManifest) CanonicalJSON() ([]byte, error) {
	if err := m.validateMetadata(); err != nil {
		return nil, err
	}
	m.ArchiveSHA256, _ = normalizeCacheSHA256(m.ArchiveSHA256)
	m.RegionData.SHA256, _ = normalizeCacheSHA256(m.RegionData.SHA256)
	m.ObjectData.SHA256, _ = normalizeCacheSHA256(m.ObjectData.SHA256)
	return json.Marshal(m)
}

// WriteCacheManifestAtomic writes a complete manifest through a same-directory
// temporary file. The manifest is the final publication marker: if a previous
// artifact write was interrupted, validation still rejects the cache.
func WriteCacheManifestAtomic(path string, manifest CacheManifest) error {
	encoded, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".navmesh-manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create navmesh cache manifest temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write navmesh cache manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync navmesh cache manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close navmesh cache manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish navmesh cache manifest: %w", err)
	}
	removeTemporary = false
	return nil
}

// ValidateCacheManifest verifies the manifest, profile binding, and both
// artifacts before callers decode either GOB file.
func ValidateCacheManifest(ctx context.Context, manifestPath, expectedProfile, expectedArchiveSHA256 string) (CacheManifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	manifest, err := ReadCacheManifest(manifestPath)
	if err != nil {
		return CacheManifest{}, err
	}
	if manifest.Profile != strings.TrimSpace(expectedProfile) {
		return CacheManifest{}, fmt.Errorf("%w: got %q, want %q", ErrNavmeshCacheProfile, manifest.Profile, expectedProfile)
	}
	expectedArchive, err := normalizeCacheSHA256(expectedArchiveSHA256)
	if err != nil {
		return CacheManifest{}, fmt.Errorf("%w: expected archive checksum: %v", ErrNavmeshCacheArchive, err)
	}
	if manifest.ArchiveSHA256 != expectedArchive {
		return CacheManifest{}, fmt.Errorf("%w: got %s, want %s", ErrNavmeshCacheArchive, manifest.ArchiveSHA256, expectedArchive)
	}

	manifestDirectory := filepath.Dir(manifestPath)
	if err := validateCacheFile(ctx, manifestDirectory, manifest.RegionData); err != nil {
		return CacheManifest{}, fmt.Errorf("region data: %w", err)
	}
	if err := validateCacheFile(ctx, manifestDirectory, manifest.ObjectData); err != nil {
		return CacheManifest{}, fmt.Errorf("object data: %w", err)
	}
	return manifest, nil
}

func (m CacheManifest) validateMetadata() error {
	if m.SchemaVersion != NavmeshCacheManifestSchemaVersion {
		return fmt.Errorf("%w: schema version %d, want %d", ErrNavmeshCacheManifest, m.SchemaVersion, NavmeshCacheManifestSchemaVersion)
	}
	if strings.TrimSpace(m.Profile) == "" {
		return fmt.Errorf("%w: profile is required", ErrNavmeshCacheManifest)
	}
	if _, err := normalizeCacheSHA256(m.ArchiveSHA256); err != nil {
		return fmt.Errorf("%w: archive checksum: %v", ErrNavmeshCacheManifest, err)
	}
	if err := validateCacheFileMetadata(m.RegionData); err != nil {
		return fmt.Errorf("region data: %w", err)
	}
	if err := validateCacheFileMetadata(m.ObjectData); err != nil {
		return fmt.Errorf("object data: %w", err)
	}
	if m.RegionData.Path == m.ObjectData.Path {
		return ErrNavmeshCacheDuplicateFiles
	}
	return nil
}

func validateCacheFileMetadata(file CacheFile) error {
	if err := validateCachePath(file.Path); err != nil {
		return err
	}
	if file.SizeBytes < 0 {
		return fmt.Errorf("%w: negative size", ErrNavmeshCacheArtifact)
	}
	if _, err := normalizeCacheSHA256(file.SHA256); err != nil {
		return fmt.Errorf("%w: checksum: %v", ErrNavmeshCacheArtifact, err)
	}
	return nil
}

func describeCacheFile(ctx context.Context, baseDirectory, filePath string) (CacheFile, error) {
	relative, err := relativeCachePath(baseDirectory, filePath)
	if err != nil {
		return CacheFile{}, err
	}
	resolved := filepath.Join(baseDirectory, filepath.FromSlash(relative))
	info, err := os.Lstat(resolved)
	if err != nil {
		return CacheFile{}, fmt.Errorf("stat %q: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return CacheFile{}, fmt.Errorf("%w: %q is not a regular file", ErrNavmeshCacheArtifact, relative)
	}
	checksum, err := hashCacheFile(ctx, resolved)
	if err != nil {
		return CacheFile{}, err
	}
	return CacheFile{Path: relative, SizeBytes: info.Size(), SHA256: checksum}, nil
}

func validateCacheFile(ctx context.Context, baseDirectory string, expected CacheFile) error {
	if err := validateCacheFileMetadata(expected); err != nil {
		return err
	}
	resolved, err := resolveCachePath(baseDirectory, expected.Path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return fmt.Errorf("stat %q: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q is not a regular file", ErrNavmeshCacheArtifact, expected.Path)
	}
	if info.Size() != expected.SizeBytes {
		return fmt.Errorf("%w: %q got %d, want %d", ErrNavmeshCacheSize, expected.Path, info.Size(), expected.SizeBytes)
	}
	checksum, err := hashCacheFile(ctx, resolved)
	if err != nil {
		return err
	}
	if checksum != expected.SHA256 {
		return fmt.Errorf("%w: %q got %s, want %s", ErrNavmeshCacheChecksum, expected.Path, checksum, expected.SHA256)
	}
	return nil
}

func resolveCachePath(baseDirectory, relative string) (string, error) {
	if err := validateCachePath(relative); err != nil {
		return "", err
	}
	base, err := filepath.Abs(baseDirectory)
	if err != nil {
		return "", fmt.Errorf("%w: resolve cache directory: %v", ErrNavmeshCachePath, err)
	}
	resolved, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(relative)))
	if err != nil {
		return "", fmt.Errorf("%w: resolve artifact: %v", ErrNavmeshCachePath, err)
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q escapes cache directory", ErrNavmeshCachePath, relative)
	}
	return resolved, nil
}

func relativeCachePath(baseDirectory, filePath string) (string, error) {
	base, err := filepath.Abs(baseDirectory)
	if err != nil {
		return "", fmt.Errorf("%w: resolve cache directory: %v", ErrNavmeshCachePath, err)
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("%w: resolve artifact: %v", ErrNavmeshCachePath, err)
	}
	relative, err := filepath.Rel(base, absPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: %q escapes cache directory", ErrNavmeshCachePath, filePath)
	}
	relative = filepath.ToSlash(relative)
	if err := validateCachePath(relative); err != nil {
		return "", err
	}
	return relative, nil
}

func validateCachePath(value string) error {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || filepath.IsAbs(filepath.FromSlash(trimmed)) {
		return fmt.Errorf("%w: %q", ErrNavmeshCachePath, value)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(trimmed)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%w: %q", ErrNavmeshCachePath, value)
	}
	return nil
}

func normalizeCacheSHA256(value string) (string, error) {
	checksum := strings.ToUpper(strings.TrimSpace(value))
	if len(checksum) != sha256.Size*2 {
		return "", fmt.Errorf("want %d hexadecimal characters, got %d", sha256.Size*2, len(checksum))
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return "", fmt.Errorf("invalid hexadecimal checksum: %w", err)
	}
	return checksum, nil
}

func hashCacheFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open cache artifact: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if errors.Is(readErr, io.EOF) {
			return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
		}
		if readErr != nil {
			return "", fmt.Errorf("read cache artifact: %w", readErr)
		}
	}
}
