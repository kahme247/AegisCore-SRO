package builder

import (
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type BuildSpec struct {
	Release ReleaseSpec `json:"release"`
	Files   []FileSpec  `json:"files"`
}

type ReleaseSpec struct {
	DivisionID     uint8  `json:"division_id"`
	ContentID      uint8  `json:"content_id"`
	ModuleID       uint8  `json:"module_id"`
	ModuleName     string `json:"module_name"`
	BaseVersion    uint32 `json:"base_version"`
	TargetVersion  uint32 `json:"target_version"`
	DisplayVersion string `json:"display_version"`
	Description    string `json:"description"`
}

type FileSpec struct {
	Source      string `json:"source"`
	Filename    string `json:"filename"`
	Path        string `json:"path"`
	ImportToPK2 bool   `json:"import_to_pk2"`
}

type PreparedFile struct {
	Filename       string `json:"filename"`
	Path           string `json:"path"`
	ImportToPK2    bool   `json:"import_to_pk2"`
	StorageKey     string `json:"storage_key"`
	PreparedSize   uint64 `json:"prepared_size"`
	PreparedSHA256 string `json:"prepared_sha256"`
	TargetSize     uint64 `json:"target_size"`
	TargetSHA256   string `json:"target_sha256"`
}

type BuildResult struct {
	Release ReleaseSpec    `json:"release"`
	Files   []PreparedFile `json:"files"`
}

func LoadSpec(path string) (BuildSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return BuildSpec{}, fmt.Errorf("open patch build spec: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var spec BuildSpec
	if err := decoder.Decode(&spec); err != nil {
		return BuildSpec{}, fmt.Errorf("decode patch build spec: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return BuildSpec{}, errors.New("decode patch build spec: multiple JSON values")
		}
		return BuildSpec{}, fmt.Errorf("decode patch build spec trailing data: %w", err)
	}
	if err := ValidateSpec(spec); err != nil {
		return BuildSpec{}, err
	}
	return spec, nil
}

func ValidateSpec(spec BuildSpec) error {
	if spec.Release.DivisionID == 0 || spec.Release.ContentID == 0 || spec.Release.ModuleID == 0 {
		return errors.New("release division_id, content_id, and module_id must be non-zero")
	}
	if strings.TrimSpace(spec.Release.ModuleName) != spec.Release.ModuleName || spec.Release.ModuleName == "" {
		return errors.New("release module_name must be non-blank and trimmed")
	}
	if !printableASCII(spec.Release.ModuleName) {
		return errors.New("release module_name must use printable ASCII")
	}
	if spec.Release.BaseVersion == 0 || spec.Release.TargetVersion != spec.Release.BaseVersion+1 {
		return errors.New("target_version must be exactly base_version + 1")
	}
	if len(spec.Files) == 0 {
		return errors.New("patch build spec must contain at least one approved file")
	}
	if len(spec.Files) > math.MaxUint16+1 {
		return errors.New("patch build spec exceeds the uint16 file ordinal limit")
	}
	seen := make(map[string]struct{}, len(spec.Files))
	for index, file := range spec.Files {
		if err := validateRelativePath(file.Source, false); err != nil {
			return fmt.Errorf("file %d source: %w", index, err)
		}
		if file.Filename == "" || filepath.Base(file.Filename) != file.Filename || strings.ContainsAny(file.Filename, `/\\`) {
			return fmt.Errorf("file %d filename must contain only a base filename", index)
		}
		if !printableASCII(file.Filename) || !printableASCII(file.Path) {
			return fmt.Errorf("file %d launcher filename and path must use printable ASCII", index)
		}
		if err := validateLogicalPath(file.Path); err != nil {
			return fmt.Errorf("file %d path: %w", index, err)
		}
		if file.ImportToPK2 && file.Path == "" {
			return fmt.Errorf("file %d imported into PK2 must name the PK2 as the first path segment", index)
		}
		key := strings.ToLower(strings.ReplaceAll(file.Path, "/", `\`) + `\` + file.Filename)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate patch target %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func Build(spec BuildSpec, sourceRoot, storageRoot string) (BuildResult, error) {
	if err := ValidateSpec(spec); err != nil {
		return BuildResult{}, err
	}
	sourceRoot, err := resolvedRoot(sourceRoot)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resolve patch source root: %w", err)
	}
	storageRoot, err = absoluteRoot(storageRoot)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resolve patch storage root: %w", err)
	}
	result := BuildResult{Release: spec.Release, Files: make([]PreparedFile, 0, len(spec.Files))}
	for index, fileSpec := range spec.Files {
		source, err := resolveExistingUnderRoot(sourceRoot, fileSpec.Source)
		if err != nil {
			return BuildResult{}, fmt.Errorf("resolve approved source file %d: %w", index, err)
		}
		prepared, err := prepareFile(source, storageRoot, spec.Release.TargetVersion, fileSpec)
		if err != nil {
			return BuildResult{}, fmt.Errorf("prepare approved source file %d: %w", index, err)
		}
		result.Files = append(result.Files, prepared)
	}
	return result, nil
}

func prepareFile(source, storageRoot string, version uint32, spec FileSpec) (PreparedFile, error) {
	input, err := os.Open(source)
	if err != nil {
		return PreparedFile{}, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return PreparedFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || uint64(info.Size()) > math.MaxUint32 {
		return PreparedFile{}, errors.New("source must be a regular file no larger than uint32")
	}

	temporaryDirectory := filepath.Join(storageRoot, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(temporaryDirectory, 0o750); err != nil {
		return PreparedFile{}, err
	}
	temporary, err := os.CreateTemp(temporaryDirectory, ".building-*.patch")
	if err != nil {
		return PreparedFile{}, err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	targetHash := sha256.New()
	preparedHash := sha256.New()
	written := io.MultiWriter(temporary, preparedHash)
	var sizeHeader [4]byte
	binary.LittleEndian.PutUint32(sizeHeader[:], uint32(info.Size()))
	if _, err := written.Write(sizeHeader[:]); err != nil {
		return PreparedFile{}, err
	}
	compressor, err := zlib.NewWriterLevel(written, zlib.BestCompression)
	if err != nil {
		return PreparedFile{}, err
	}
	copied, copyErr := io.Copy(compressor, io.TeeReader(input, targetHash))
	closeErr := compressor.Close()
	if copyErr != nil || closeErr != nil {
		return PreparedFile{}, errors.Join(copyErr, closeErr)
	}
	if copied != info.Size() {
		return PreparedFile{}, fmt.Errorf("source changed while reading: expected %d bytes, read %d", info.Size(), copied)
	}
	if err := temporary.Sync(); err != nil {
		return PreparedFile{}, err
	}
	if err := temporary.Close(); err != nil {
		return PreparedFile{}, err
	}
	preparedInfo, err := os.Stat(temporaryPath)
	if err != nil {
		return PreparedFile{}, err
	}
	if preparedInfo.Size() < 0 || uint64(preparedInfo.Size()) > math.MaxUint32 {
		return PreparedFile{}, errors.New("prepared payload exceeds the legacy uint32 size field")
	}
	preparedDigest := hex.EncodeToString(preparedHash.Sum(nil))
	storageKey := filepath.ToSlash(filepath.Join(fmt.Sprintf("%d", version), preparedDigest+".patch"))
	finalPath := filepath.Join(storageRoot, filepath.FromSlash(storageKey))
	if existing, err := os.ReadFile(finalPath); err == nil {
		existingHash := sha256.Sum256(existing)
		if hex.EncodeToString(existingHash[:]) != preparedDigest || int64(len(existing)) != preparedInfo.Size() {
			return PreparedFile{}, errors.New("immutable prepared payload collision")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return PreparedFile{}, err
	} else if err := os.Rename(temporaryPath, finalPath); err != nil {
		return PreparedFile{}, err
	}
	return PreparedFile{
		Filename:       spec.Filename,
		Path:           strings.ReplaceAll(spec.Path, "/", `\`),
		ImportToPK2:    spec.ImportToPK2,
		StorageKey:     storageKey,
		PreparedSize:   uint64(preparedInfo.Size()),
		PreparedSHA256: preparedDigest,
		TargetSize:     uint64(info.Size()),
		TargetSHA256:   hex.EncodeToString(targetHash.Sum(nil)),
	}, nil
}

func ValidateResult(result BuildResult) error {
	if err := ValidateSpec(BuildSpec{Release: result.Release, Files: buildResultFileSpecs(result.Files)}); err != nil {
		return err
	}
	for index, file := range result.Files {
		if file.PreparedSize == 0 || file.PreparedSize > math.MaxUint32 || file.TargetSize > math.MaxUint32 {
			return fmt.Errorf("prepared file %d has an invalid legacy size", index)
		}
		for _, digest := range []string{file.PreparedSHA256, file.TargetSHA256} {
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
				return fmt.Errorf("prepared file %d has an invalid SHA-256", index)
			}
		}
		if err := validateRelativePath(file.StorageKey, false); err != nil {
			return fmt.Errorf("prepared file %d storage key: %w", index, err)
		}
	}
	return nil
}

func buildResultFileSpecs(files []PreparedFile) []FileSpec {
	result := make([]FileSpec, 0, len(files))
	for _, file := range files {
		result = append(result, FileSpec{Source: file.StorageKey, Filename: file.Filename, Path: file.Path, ImportToPK2: file.ImportToPK2})
	}
	return result
}

func resolvedRoot(path string) (string, error) {
	root, err := absoluteRoot(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func absoluteRoot(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path must not be blank")
	}
	return filepath.Abs(path)
}

func resolveExistingUnderRoot(root, relative string) (string, error) {
	if err := validateRelativePath(relative, false); err != nil {
		return "", err
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	relativeToRoot, err := filepath.Rel(root, candidate)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes configured root")
	}
	return candidate, nil
}

func validateRelativePath(path string, allowEmpty bool) error {
	path = strings.ReplaceAll(path, `\`, "/")
	if path == "" {
		if allowEmpty {
			return nil
		}
		return errors.New("path must not be empty")
	}
	if filepath.IsAbs(filepath.FromSlash(path)) || filepath.VolumeName(filepath.FromSlash(path)) != "" {
		return errors.New("path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return errors.New("path must stay beneath its root")
	}
	return nil
}

func validateLogicalPath(path string) error {
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, `\`) || strings.HasSuffix(path, `\`) || strings.HasSuffix(path, "/") {
		return errors.New("logical path must not start or end with a separator")
	}
	return validateRelativePath(path, true)
}

func printableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7E {
			return false
		}
	}
	return true
}
