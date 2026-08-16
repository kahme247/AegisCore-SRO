package catalog

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type ServingManifest struct {
	Files []ServingFile `json:"files"`
}

type ServingFile struct {
	ID          uint32 `json:"id"`
	StoragePath string `json:"storage_path"`
	Size        uint64 `json:"size"`
	SHA256      string `json:"sha256"`
}

func WriteServingManifest(path string, release Release) error {
	manifest := ServingManifest{}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &manifest); err != nil {
			return fmt.Errorf("decode existing serving manifest: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing serving manifest: %w", err)
	}
	byID := make(map[uint32]ServingFile, len(manifest.Files)+len(release.Files))
	for _, file := range manifest.Files {
		if file.ID == 0 {
			return errors.New("existing serving manifest contains a zero file ID")
		}
		if _, duplicate := byID[file.ID]; duplicate {
			return fmt.Errorf("existing serving manifest contains duplicate file ID %d", file.ID)
		}
		byID[file.ID] = file
	}
	for _, file := range release.Files {
		byID[file.ID] = ServingFile{ID: file.ID, StoragePath: file.StorageKey, Size: file.PreparedSize, SHA256: file.PreparedSHA256}
	}
	manifest.Files = manifest.Files[:0]
	for _, existing := range byID {
		manifest.Files = append(manifest.Files, existing)
	}
	slices.SortFunc(manifest.Files, func(left, right ServingFile) int { return cmp.Compare(left.ID, right.ID) })
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".manifest-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func VerifyReleaseStorage(release Release, manifestPath, storageRoot string) error {
	root, err := filepath.Abs(storageRoot)
	if err != nil {
		return fmt.Errorf("resolve storage root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve storage root symlinks: %w", err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read serving manifest: %w", err)
	}
	var manifest ServingManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode serving manifest: %w", err)
	}
	entries := make(map[uint32]ServingFile, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, duplicate := entries[file.ID]; duplicate {
			return fmt.Errorf("serving manifest contains duplicate file ID %d", file.ID)
		}
		entries[file.ID] = file
	}
	for _, file := range release.Files {
		entry, ok := entries[file.ID]
		if !ok || entry.StoragePath != file.StorageKey || entry.Size != file.PreparedSize || entry.SHA256 != file.PreparedSHA256 {
			return fmt.Errorf("serving manifest does not match patch file ID %d", file.ID)
		}
		if filepath.IsAbs(filepath.FromSlash(file.StorageKey)) || filepath.VolumeName(filepath.FromSlash(file.StorageKey)) != "" {
			return fmt.Errorf("prepared file ID %d has an absolute storage key", file.ID)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(file.StorageKey)))
		if err != nil {
			return fmt.Errorf("resolve prepared file ID %d: %w", file.ID, err)
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("prepared file ID %d escapes the storage root", file.ID)
		}
		opened, err := os.Open(resolved)
		if err != nil {
			return fmt.Errorf("open prepared file ID %d: %w", file.ID, err)
		}
		hash := sha256.New()
		copied, copyErr := io.Copy(hash, opened)
		closeErr := opened.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if uint64(copied) != file.PreparedSize || hex.EncodeToString(hash.Sum(nil)) != file.PreparedSHA256 {
			return fmt.Errorf("prepared file ID %d does not match database metadata", file.ID)
		}
	}
	return nil
}
