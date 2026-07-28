// Package profiles loads the checked-in client compatibility profiles.
//
// Profiles are data only. In particular, loading a profile does not open a
// client archive or select a runtime dataset.
package profiles

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	// CurrentSchemaVersion is the format version understood by this package.
	CurrentSchemaVersion = 1

	// VSRO188 is the only supported client profile at this stage.
	VSRO188 = "vsro188"

	// StatusEvidencePending marks a profile whose client/capture evidence has
	// not completed the release promotion requirements.
	StatusEvidencePending = "evidence-pending"
)

var ErrInvalidProfile = errors.New("invalid protocol profile")

// Profile describes a pinned client and its client assets. It intentionally
// carries no decoded PK2 content and no database configuration.
type Profile struct {
	SchemaVersion         int      `json:"schema_version"`
	Profile               string   `json:"profile"`
	Status                string   `json:"status"`
	Client                Client   `json:"client"`
	DataPK2               DataPK2  `json:"data_pk2"`
	Locale                *string  `json:"locale"`
	ContentID             *int     `json:"content_id"`
	UnvalidatedContentIDs []int    `json:"unvalidated_content_id_values"`
	Captures              []string `json:"captures"`
	PromotionRequirements []string `json:"promotion_requirements"`
}

// Client identifies the executable associated with a profile. The fields are
// nullable while the profile is evidence-pending.
type Client struct {
	Executable  *string `json:"executable"`
	SHA256      *string `json:"sha256"`
	FileVersion *string `json:"file_version"`
}

// DataPK2 identifies the exact client asset archive used by a profile.
type DataPK2 struct {
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	ArchiveFormat string `json:"archive_format"`
	Encrypted     bool   `json:"encrypted"`
}

//go:embed vsro188.json
var vsro188JSON []byte

// VSRO188Profile returns a fresh copy of the canonical checked-in vSRO 1.88
// profile. Embedding keeps command verification independent of the current
// working directory while preserving the JSON file as the reviewed source.
func VSRO188Profile() (Profile, error) {
	return Parse(vsro188JSON)
}

// LoadFile reads and decodes a profile supplied to an offline tool.
func LoadFile(path string) (Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: read %q: %v", ErrInvalidProfile, path, err)
	}
	return Parse(raw)
}

// Parse decodes exactly one profile JSON document and rejects unknown fields
// so a newer profile format cannot be silently misinterpreted by this tool.
func Parse(raw []byte) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("%w: decode: %v", ErrInvalidProfile, err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Profile{}, fmt.Errorf("%w: more than one JSON value", ErrInvalidProfile)
		}
		return Profile{}, fmt.Errorf("%w: trailing data: %v", ErrInvalidProfile, err)
	}

	return profile, nil
}
