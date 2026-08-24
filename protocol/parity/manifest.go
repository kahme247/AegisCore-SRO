// Package parity defines the machine-readable evidence and acceptance contract
// for the vSRO 1.188 GameServer/ShardManager parity program.
package parity

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

//go:embed vsro188.json
var vsro188Manifest []byte

type Status string

const (
	StatusUnknown     Status = "unknown"
	StatusEvidenced   Status = "evidenced"
	StatusImplemented Status = "implemented"
	StatusVerified    Status = "verified"
	StatusReplaced    Status = "replaced"
	StatusBlocked     Status = "blocked"
)

type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	Target        string       `json:"target"`
	Binaries      []Binary     `json:"binaries"`
	Catalogs      []Catalog    `json:"catalogs"`
	Requirements  []string     `json:"completion_requirements"`
	Capabilities  []Capability `json:"capabilities"`
}

type Binary struct {
	Service string `json:"service"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}

// Catalog describes a recovered inventory and the exact uniqueness rule used
// to audit it. Filter is optional; it lets procedures and tables share one CSV.
type Catalog struct {
	ID             string `json:"id"`
	Source         string `json:"source"`
	UniqueField    string `json:"unique_field"`
	FilterField    string `json:"filter_field,omitempty"`
	FilterValue    string `json:"filter_value,omitempty"`
	ExpectedUnique int    `json:"expected_unique"`
}

type Evidence struct {
	Source     string `json:"source"`
	Locator    string `json:"locator"`
	Confidence string `json:"confidence"`
}

type Capability struct {
	ID             string     `json:"id"`
	Family         string     `json:"family"`
	Name           string     `json:"name"`
	EnabledDefault bool       `json:"enabled_default"`
	Status         Status     `json:"status"`
	Evidence       []Evidence `json:"evidence"`
	Implementation []string   `json:"implementation,omitempty"`
	Tests          []string   `json:"tests,omitempty"`
	Replacement    string     `json:"replacement,omitempty"`
}

type GateIssue struct {
	CapabilityID string
	Reason       string
}

type CatalogAudit struct {
	ID       string
	Expected int
	Actual   int
}

type BinaryAudit struct {
	Service  string
	File     string
	Expected string
	Actual   string
}

func LoadVSRO188() (Manifest, error) {
	return Decode(bytes.NewReader(vsro188Manifest))
}

func Decode(reader io.Reader) (Manifest, error) {
	if reader == nil {
		return Manifest{}, errors.New("parity manifest reader is nil")
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode parity manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("unsupported parity schema version %d", m.SchemaVersion)
	}
	if m.Target != "vsro188" {
		return fmt.Errorf("unsupported parity target %q", m.Target)
	}
	if len(m.Binaries) == 0 || len(m.Catalogs) == 0 || len(m.Capabilities) == 0 {
		return errors.New("binaries, catalogs, and capabilities are required")
	}
	if len(m.Requirements) == 0 {
		return errors.New("completion requirements are required")
	}

	binaries := make(map[string]struct{}, len(m.Binaries))
	for _, binary := range m.Binaries {
		if binary.Service == "" || binary.File == "" || !validSHA256(binary.SHA256) {
			return fmt.Errorf("invalid binary record for %q", binary.Service)
		}
		if _, duplicate := binaries[binary.Service]; duplicate {
			return fmt.Errorf("duplicate binary service %q", binary.Service)
		}
		binaries[binary.Service] = struct{}{}
	}

	catalogs := make(map[string]struct{}, len(m.Catalogs))
	for _, catalog := range m.Catalogs {
		if catalog.ID == "" || catalog.Source == "" || catalog.UniqueField == "" || catalog.ExpectedUnique <= 0 {
			return fmt.Errorf("invalid catalog %q", catalog.ID)
		}
		if (catalog.FilterField == "") != (catalog.FilterValue == "") {
			return fmt.Errorf("catalog %q has an incomplete filter", catalog.ID)
		}
		if _, duplicate := catalogs[catalog.ID]; duplicate {
			return fmt.Errorf("duplicate catalog ID %q", catalog.ID)
		}
		catalogs[catalog.ID] = struct{}{}
	}

	capabilities := make(map[string]struct{}, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if err := validateCapability(capability); err != nil {
			return fmt.Errorf("capability %q: %w", capability.ID, err)
		}
		if _, duplicate := capabilities[capability.ID]; duplicate {
			return fmt.Errorf("duplicate capability ID %q", capability.ID)
		}
		capabilities[capability.ID] = struct{}{}
	}
	return nil
}

// Gate reports why the manifest cannot yet claim parity. Disabled-by-default
// legacy features are still in scope, so completion is required for every row.
func (m Manifest) Gate() []GateIssue {
	issues := make([]GateIssue, 0)
	for _, capability := range m.Capabilities {
		switch capability.Status {
		case StatusVerified:
			if len(capability.Implementation) == 0 || len(capability.Tests) == 0 {
				issues = append(issues, GateIssue{CapabilityID: capability.ID, Reason: "verified capability lacks implementation or tests"})
			}
		case StatusReplaced:
			if capability.Replacement == "" || len(capability.Tests) == 0 {
				issues = append(issues, GateIssue{CapabilityID: capability.ID, Reason: "replacement lacks rationale or tests"})
			}
		default:
			issues = append(issues, GateIssue{CapabilityID: capability.ID, Reason: fmt.Sprintf("status is %s", capability.Status)})
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].CapabilityID < issues[j].CapabilityID })
	return issues
}

// AuditCatalog counts unique values using the manifest's declared CSV rule.
// It deliberately does not interpret extracted names as confirmed semantics.
func AuditCatalog(reader io.Reader, catalog Catalog) (CatalogAudit, error) {
	if reader == nil {
		return CatalogAudit{}, errors.New("catalog reader is nil")
	}
	csvReader := csv.NewReader(reader)
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		return CatalogAudit{}, fmt.Errorf("read catalog %q header: %w", catalog.ID, err)
	}
	indexes := make(map[string]int, len(header))
	for index, name := range header {
		indexes[name] = index
	}
	uniqueIndex, exists := indexes[catalog.UniqueField]
	if !exists {
		return CatalogAudit{}, fmt.Errorf("catalog %q lacks unique field %q", catalog.ID, catalog.UniqueField)
	}
	filterIndex := -1
	if catalog.FilterField != "" {
		var found bool
		filterIndex, found = indexes[catalog.FilterField]
		if !found {
			return CatalogAudit{}, fmt.Errorf("catalog %q lacks filter field %q", catalog.ID, catalog.FilterField)
		}
	}

	values := make(map[string]struct{})
	for row := 2; ; row++ {
		record, readErr := csvReader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return CatalogAudit{}, fmt.Errorf("read catalog %q row %d: %w", catalog.ID, row, readErr)
		}
		if filterIndex >= 0 && record[filterIndex] != catalog.FilterValue {
			continue
		}
		value := strings.TrimSpace(record[uniqueIndex])
		if value != "" {
			values[value] = struct{}{}
		}
	}
	return CatalogAudit{ID: catalog.ID, Expected: catalog.ExpectedUnique, Actual: len(values)}, nil
}

func (a CatalogAudit) Matches() bool { return a.Expected == a.Actual }

// AuditBinary verifies that the immutable evidence file is exactly the binary
// named by the manifest. A recorded digest without checking the current file
// is metadata, not provenance proof.
func AuditBinary(reader io.Reader, binary Binary) (BinaryAudit, error) {
	if reader == nil {
		return BinaryAudit{}, errors.New("binary evidence reader is nil")
	}
	if binary.Service == "" || binary.File == "" || !validSHA256(binary.SHA256) {
		return BinaryAudit{}, fmt.Errorf("invalid binary record for %q", binary.Service)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return BinaryAudit{}, fmt.Errorf("hash binary evidence %q: %w", binary.File, err)
	}
	return BinaryAudit{
		Service: binary.Service, File: binary.File, Expected: strings.ToLower(binary.SHA256),
		Actual: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (a BinaryAudit) Matches() bool { return a.Expected == a.Actual }

func validateCapability(capability Capability) error {
	if capability.ID == "" || capability.Family == "" || capability.Name == "" {
		return errors.New("ID, family, and name are required")
	}
	switch capability.Status {
	case StatusUnknown, StatusEvidenced, StatusImplemented, StatusVerified, StatusReplaced, StatusBlocked:
	default:
		return fmt.Errorf("invalid status %q", capability.Status)
	}
	if len(capability.Evidence) == 0 {
		return errors.New("evidence is required")
	}
	for index, evidence := range capability.Evidence {
		if evidence.Source == "" || evidence.Locator == "" || evidence.Confidence == "" {
			return fmt.Errorf("evidence %d is incomplete", index)
		}
	}
	if capability.Status == StatusImplemented && len(capability.Implementation) == 0 {
		return errors.New("implemented status requires implementation references")
	}
	return nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode parity manifest: trailing JSON value")
		}
		return fmt.Errorf("decode parity manifest trailing data: %w", err)
	}
	return nil
}
