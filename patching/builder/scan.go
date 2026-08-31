package builder

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Harvi8/AegisCore/framework/pk2"
)

type ScanResult struct {
	Changed   []FileSpec
	Unchanged int
	Skipped   []string
}

type mediaExportScanManifest struct {
	SchemaVersion       int      `json:"schema_version"`
	Profile             string   `json:"profile"`
	UnsupportedCatalogs []string `json:"unsupported_catalogs,omitempty"`
	Files               []struct {
		Path   string `json:"path"`
		Rows   int    `json:"rows"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// ScanPK2 compares a deployed archive with an approved extracted target tree.
// It creates review candidates only; publication remains a separate admin
// action. Deletions are intentionally unsupported because the existing
// launcher patch format only adds or replaces files.
func ScanPK2(archivePath, targetRoot, pk2Name string) (ScanResult, error) {
	reader, err := pk2.OpenPk2Reader(archivePath)
	if err != nil {
		return ScanResult{}, err
	}
	defer reader.Close()
	reader.CachingEnabled = false
	if err := reader.IndexArchiveSafe(); err != nil {
		return ScanResult{}, fmt.Errorf("index deployed PK2: %w", err)
	}
	root, err := filepath.Abs(targetRoot)
	if err != nil {
		return ScanResult{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return ScanResult{}, err
	}
	pk2Name = strings.ToLower(strings.TrimSpace(pk2Name))
	if pk2Name == "" || strings.ContainsAny(pk2Name, `/\\`) {
		return ScanResult{}, fmt.Errorf("PK2 logical name must be one path segment")
	}

	archiveByLowerPath := make(map[string]string, len(reader.Files))
	archiveRoot := strings.ToLower(reader.Directory.Name) + string(filepath.Separator)
	for path := range reader.Files {
		archiveByLowerPath[strings.ToLower(path)] = path
	}
	result := ScanResult{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			result.Skipped = append(result.Skipped, path)
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// SV.T is advanced by the launcher only after every payload succeeds.
		// Shipping it as an ordinary file could mark a partial update complete.
		if strings.EqualFold(filepath.ToSlash(relative), "SV.T") {
			result.Skipped = append(result.Skipped, filepath.ToSlash(relative))
			return nil
		}
		target, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		archiveKey := archiveRoot + strings.ToLower(relative)
		deployedPath, exists := archiveByLowerPath[archiveKey]
		if exists {
			deployed, err := reader.ReadFile(deployedPath)
			if err != nil {
				return err
			}
			if bytes.Equal(deployed, target) {
				result.Unchanged++
				return nil
			}
		}
		directory := filepath.Dir(relative)
		logicalPath := pk2Name
		if directory != "." {
			logicalPath += `\` + strings.ReplaceAll(directory, string(filepath.Separator), `\`)
		}
		candidate := FileSpec{
			Source:      filepath.ToSlash(relative),
			Filename:    filepath.Base(relative),
			Path:        logicalPath,
			ImportToPK2: true,
		}
		if !printableASCII(candidate.Filename) || !printableASCII(candidate.Path) {
			result.Skipped = append(result.Skipped, "non-ascii:"+candidate.Source)
			return nil
		}
		result.Changed = append(result.Changed, candidate)
		return nil
	})
	if err != nil {
		return ScanResult{}, fmt.Errorf("scan extracted target tree: %w", err)
	}
	slices.SortFunc(result.Changed, func(left, right FileSpec) int { return strings.Compare(left.Source, right.Source) })
	slices.Sort(result.Skipped)
	return result, nil
}

// ScanPK2Export compares only files named by a verified sro-data Media export
// manifest. The manifest itself is never offered to the launcher as a Media
// file.
func ScanPK2Export(archivePath, targetRoot, manifestPath, pk2Name string) (ScanResult, error) {
	manifest, err := loadMediaExportScanManifest(manifestPath)
	if err != nil {
		return ScanResult{}, err
	}
	root, err := resolvedRoot(targetRoot)
	if err != nil {
		return ScanResult{}, fmt.Errorf("resolve Media export root: %w", err)
	}
	reader, err := pk2.OpenPk2Reader(archivePath)
	if err != nil {
		return ScanResult{}, err
	}
	defer reader.Close()
	reader.CachingEnabled = false
	if err := reader.IndexArchiveSafe(); err != nil {
		return ScanResult{}, fmt.Errorf("index deployed PK2: %w", err)
	}
	pk2Name = strings.ToLower(strings.TrimSpace(pk2Name))
	if pk2Name == "" || strings.ContainsAny(pk2Name, `/\\`) {
		return ScanResult{}, fmt.Errorf("PK2 logical name must be one path segment")
	}
	archiveByLowerPath := make(map[string]string, len(reader.Files))
	archiveRoot := strings.ToLower(reader.Directory.Name) + string(filepath.Separator)
	for path := range reader.Files {
		archiveByLowerPath[strings.ToLower(path)] = path
	}
	result := ScanResult{}
	for _, file := range manifest.Files {
		targetPath, err := resolveExistingUnderRoot(root, file.Path)
		if err != nil {
			return ScanResult{}, fmt.Errorf("resolve exported Media file %q: %w", file.Path, err)
		}
		target, err := os.ReadFile(targetPath)
		if err != nil {
			return ScanResult{}, fmt.Errorf("read exported Media file %q: %w", file.Path, err)
		}
		digest := sha256.Sum256(target)
		if int64(len(target)) != file.Bytes || hex.EncodeToString(digest[:]) != file.SHA256 {
			return ScanResult{}, fmt.Errorf("exported Media file %q does not match its manifest", file.Path)
		}
		relative := filepath.FromSlash(file.Path)
		archiveKey := archiveRoot + strings.ToLower(relative)
		deployedPath, exists := archiveByLowerPath[archiveKey]
		if exists {
			deployed, err := reader.ReadFile(deployedPath)
			if err != nil {
				return ScanResult{}, err
			}
			if bytes.Equal(deployed, target) {
				result.Unchanged++
				continue
			}
		}
		directory := filepath.Dir(relative)
		logicalPath := pk2Name
		if directory != "." {
			logicalPath += `\` + strings.ReplaceAll(directory, string(filepath.Separator), `\`)
		}
		candidate := FileSpec{Source: file.Path, Filename: filepath.Base(relative), Path: logicalPath, ImportToPK2: true}
		if !printableASCII(candidate.Filename) || !printableASCII(candidate.Path) {
			result.Skipped = append(result.Skipped, "non-ascii:"+candidate.Source)
			continue
		}
		result.Changed = append(result.Changed, candidate)
	}
	slices.SortFunc(result.Changed, func(left, right FileSpec) int { return strings.Compare(left.Source, right.Source) })
	slices.Sort(result.Skipped)
	return result, nil
}

func loadMediaExportScanManifest(path string) (mediaExportScanManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return mediaExportScanManifest{}, fmt.Errorf("open Media export manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest mediaExportScanManifest
	if err := decoder.Decode(&manifest); err != nil {
		return mediaExportScanManifest{}, fmt.Errorf("decode Media export manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return mediaExportScanManifest{}, errors.New("decode Media export manifest: trailing data")
	}
	if manifest.SchemaVersion != 1 || manifest.Profile != "vsro188" || len(manifest.Files) == 0 {
		return mediaExportScanManifest{}, errors.New("Media export manifest must be non-empty schema 1 for vsro188")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for index, file := range manifest.Files {
		if err := validateRelativePath(file.Path, false); err != nil {
			return mediaExportScanManifest{}, fmt.Errorf("Media export file %d path: %w", index, err)
		}
		path := strings.ToLower(strings.ReplaceAll(file.Path, `\`, "/"))
		if _, exists := seen[path]; exists {
			return mediaExportScanManifest{}, fmt.Errorf("Media export manifest duplicates %q", file.Path)
		}
		seen[path] = struct{}{}
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || len(digest) != sha256.Size || file.SHA256 != strings.ToLower(file.SHA256) || file.Bytes < 0 || file.Rows < 0 {
			return mediaExportScanManifest{}, fmt.Errorf("Media export file %d has invalid metadata", index)
		}
	}
	return manifest, nil
}

func WriteSpec(path string, spec BuildSpec) error {
	if err := ValidateSpec(spec); err != nil {
		return err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
