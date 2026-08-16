package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrFileNotFound = errors.New("patch file ID is not present in the catalog")

type manifest struct {
	Files []manifestFile `json:"files"`
}

type manifestFile struct {
	ID          uint32 `json:"id"`
	StoragePath string `json:"storage_path"`
	Size        uint64 `json:"size"`
	SHA256      string `json:"sha256"`
}

type record struct {
	path    string
	size    uint64
	modTime time.Time
}

// FileCatalog maps client-visible numeric IDs to operator-controlled files.
// Client input is never interpreted as a filesystem path.
type FileCatalog struct {
	root    string
	records map[uint32]record
}

type PreparedFile interface {
	io.Reader
	io.Seeker
	io.Closer
}

func Load(manifestPath, storageRoot string) (*FileCatalog, error) {
	root, err := filepath.Abs(storageRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve patch storage root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve patch storage root symlinks: %w", err)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read patch manifest: %w", err)
	}
	var decoded manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode patch manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode patch manifest: multiple JSON values")
		}
		return nil, fmt.Errorf("decode patch manifest trailing data: %w", err)
	}

	catalog := &FileCatalog{root: root, records: make(map[uint32]record, len(decoded.Files))}
	for _, file := range decoded.Files {
		if file.ID == 0 {
			return nil, errors.New("patch manifest file ID must be non-zero")
		}
		if _, duplicate := catalog.records[file.ID]; duplicate {
			return nil, fmt.Errorf("patch manifest contains duplicate file ID %d", file.ID)
		}
		resolved, err := resolveUnderRoot(root, file.StoragePath)
		if err != nil {
			return nil, fmt.Errorf("validate patch file %d: %w", file.ID, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat patch file %d: %w", file.ID, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("patch file %d is not a regular file", file.ID)
		}
		if uint64(info.Size()) != file.Size {
			return nil, fmt.Errorf("patch file %d size mismatch: manifest=%d actual=%d", file.ID, file.Size, info.Size())
		}
		decodedHash, err := hex.DecodeString(file.SHA256)
		if err != nil || len(decodedHash) != sha256.Size || file.SHA256 != strings.ToLower(file.SHA256) {
			return nil, fmt.Errorf("patch file %d has an invalid SHA-256", file.ID)
		}
		opened, err := os.Open(resolved)
		if err != nil {
			return nil, fmt.Errorf("open patch file %d for hashing: %w", file.ID, err)
		}
		hasher := sha256.New()
		_, copyErr := io.Copy(hasher, opened)
		closeErr := opened.Close()
		if copyErr != nil || closeErr != nil {
			return nil, fmt.Errorf("hash patch file %d: %w", file.ID, errors.Join(copyErr, closeErr))
		}
		if hex.EncodeToString(hasher.Sum(nil)) != file.SHA256 {
			return nil, fmt.Errorf("patch file %d SHA-256 mismatch", file.ID)
		}
		catalog.records[file.ID] = record{path: resolved, size: file.Size, modTime: info.ModTime()}
	}
	return catalog, nil
}

// Open returns a verified prepared payload. It rechecks the file because a
// manifest loaded at startup must not silently serve a later partial write.
func (c *FileCatalog) Open(id uint32) (PreparedFile, uint64, error) {
	if c == nil {
		return nil, 0, errors.New("patch catalog is nil")
	}
	record, ok := c.records[id]
	if !ok {
		return nil, 0, ErrFileNotFound
	}
	resolved, err := filepath.EvalSymlinks(record.path)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve patch file %d: %w", id, err)
	}
	if err := ensureUnderRoot(c.root, resolved); err != nil {
		return nil, 0, fmt.Errorf("validate patch file %d: %w", id, err)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, 0, fmt.Errorf("open patch file %d: %w", id, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, fmt.Errorf("stat patch file %d: %w", id, err)
	}
	if !info.Mode().IsRegular() || uint64(info.Size()) != record.size || !info.ModTime().Equal(record.modTime) {
		_ = file.Close()
		return nil, 0, fmt.Errorf("patch file %d changed after catalog load", id)
	}
	return file, record.size, nil
}

func resolveUnderRoot(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", errors.New("storage_path must be a non-empty relative path")
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, filepath.Clean(relative)))
	if err != nil {
		return "", err
	}
	if err := ensureUnderRoot(root, candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func ensureUnderRoot(root, candidate string) error {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("storage_path escapes the configured root")
	}
	return nil
}
