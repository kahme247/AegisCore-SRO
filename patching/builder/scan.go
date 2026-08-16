package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
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
