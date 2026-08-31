// Package captures provides provenance-aware logical-payload fixtures and a
// deterministic replay boundary for protocol compatibility tests. It does
// not parse TCP, encryption, or SQL; those layers must be reconstructed before
// a fixture is promoted beyond logical-payload representation.
package captures

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const CurrentSchemaVersion = "1"

var (
	ErrFixtureInvalid = errors.New("invalid protocol capture fixture")
)

// ClientIdentity records the executable and client-content identity required
// before observed bytes can be treated as stock evidence.
type ClientIdentity struct {
	ExecutableName string `json:"executable_name"`
	SHA256         string `json:"sha256"`
	FileVersion    string `json:"file_version"`
	Locale         string `json:"locale"`
	ContentID      string `json:"content_id"`
}

type ArchiveIdentity struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Format  string `json:"format"`
	Profile string `json:"profile"`
}

type ConnectionIdentity struct {
	Branch        string `json:"branch"`
	TargetService string `json:"target_service"`
}

type Reconstruction struct {
	Details string `json:"details"`
}

// Frame is a logical protocol payload. PayloadHex is the captured or
// reconstructed input. ExpectedPayloadHex is optional for decode-only
// fixtures and required when replay is claiming exact re-encoding.
type Frame struct {
	Index              int    `json:"index"`
	Direction          string `json:"direction"`
	Opcode             uint16 `json:"opcode"`
	LogicalLength      int    `json:"logical_length"`
	PayloadHex         string `json:"payload_hex"`
	ExpectedPayloadHex string `json:"expected_payload_hex,omitempty"`
	EncryptionState    string `json:"encryption_state"`
	EDCState           string `json:"edc_state"`
	MassiveRole        string `json:"massive_role"`
	ChunkLengths       []int  `json:"chunk_lengths,omitempty"`
}

type ExpectedOutcome struct {
	StateTransition string `json:"state_transition"`
	ResponseBytes   string `json:"response_bytes,omitempty"`
	Rejected        bool   `json:"rejected"`
	Notes           string `json:"notes,omitempty"`
}

// Fixture is the checked-in metadata envelope for a sanitized capture. A
// fixture may contain logical payloads now and later be extended with exact
// reconstructed wire bytes without changing its identity fields.
type Fixture struct {
	SchemaVersion              string             `json:"schema_version"`
	FixtureID                  string             `json:"fixture_id"`
	Behavior                   string             `json:"behavior"`
	EvidenceStatus             string             `json:"evidence_status"`
	Client                     ClientIdentity     `json:"client"`
	Data                       ArchiveIdentity    `json:"data_pk2"`
	Connection                 ConnectionIdentity `json:"connection"`
	Representation             string             `json:"representation"`
	PrivateSourceCaptureSHA256 string             `json:"private_source_capture_sha256"`
	PublicFixtureSHA256        string             `json:"public_fixture_sha256"`
	Sanitizer                  string             `json:"sanitizer"`
	SanitizedFields            []string           `json:"sanitized_fields"`
	Reconstruction             Reconstruction     `json:"reconstruction"`
	Frames                     []Frame            `json:"frames"`
	Expected                   []ExpectedOutcome  `json:"expected"`
	ExternalSources            []string           `json:"external_sources"`
	ReviewerConfirmed          bool               `json:"reviewer_confirmed"`
}

// DecodeFixture decodes and validates one JSON fixture. Unknown fields are
// rejected so metadata cannot silently fall out of the acceptance contract.
func DecodeFixture(data []byte) (Fixture, error) {
	var fixture Fixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return Fixture{}, fmt.Errorf("%w: decode JSON: %v", ErrFixtureInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Fixture{}, fmt.Errorf("%w: trailing JSON value", ErrFixtureInvalid)
		}
		return Fixture{}, fmt.Errorf("%w: decode trailing JSON: %v", ErrFixtureInvalid, err)
	}
	if err := fixture.Validate(); err != nil {
		return Fixture{}, err
	}
	return fixture, nil
}

func LoadFixture(path string) (Fixture, error) {
	if strings.TrimSpace(path) == "" {
		return Fixture{}, fmt.Errorf("%w: fixture path is empty", ErrFixtureInvalid)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("read capture fixture: %w", err)
	}
	return DecodeFixture(data)
}

func (f Fixture) Validate() error {
	if f.SchemaVersion != CurrentSchemaVersion || strings.TrimSpace(f.FixtureID) == "" || strings.TrimSpace(f.Behavior) == "" || strings.TrimSpace(f.EvidenceStatus) == "" {
		return fmt.Errorf("%w: missing identity or unsupported schema version", ErrFixtureInvalid)
	}
	if err := validateIdentity(f.Client.SHA256, "client SHA-256"); err != nil {
		return err
	}
	if f.Client.ExecutableName == "" || f.Client.FileVersion == "" || f.Client.Locale == "" || f.Client.ContentID == "" {
		return fmt.Errorf("%w: incomplete client identity", ErrFixtureInvalid)
	}
	if f.Data.Path == "" || f.Data.Size < 0 || f.Data.Format == "" || f.Data.Profile == "" {
		return fmt.Errorf("%w: incomplete Data.pk2 identity", ErrFixtureInvalid)
	}
	if err := validateIdentity(f.Data.SHA256, "Data.pk2 SHA-256"); err != nil {
		return err
	}
	if f.Connection.Branch == "" || f.Connection.TargetService == "" || f.Representation == "" || f.Sanitizer == "" || f.Reconstruction.Details == "" {
		return fmt.Errorf("%w: incomplete capture metadata", ErrFixtureInvalid)
	}
	if err := validateIdentity(f.PrivateSourceCaptureSHA256, "private source SHA-256"); err != nil {
		return err
	}
	if err := validateIdentity(f.PublicFixtureSHA256, "public fixture SHA-256"); err != nil {
		return err
	}
	if len(f.SanitizedFields) == 0 || len(f.Frames) == 0 || len(f.Expected) == 0 || len(f.ExternalSources) == 0 || !f.ReviewerConfirmed {
		return fmt.Errorf("%w: fixture is missing acceptance metadata", ErrFixtureInvalid)
	}
	for index, field := range f.SanitizedFields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: sanitized field %d is empty", ErrFixtureInvalid, index)
		}
	}
	for index, outcome := range f.Expected {
		if strings.TrimSpace(outcome.StateTransition) == "" {
			return fmt.Errorf("%w: expected outcome %d has no state transition", ErrFixtureInvalid, index)
		}
	}
	for index, source := range f.ExternalSources {
		if strings.TrimSpace(source) == "" {
			return fmt.Errorf("%w: external source %d is empty", ErrFixtureInvalid, index)
		}
	}
	if f.Representation != "logical-payload" && f.Representation != "wire-frame" && f.Representation != "decrypted-frame" && f.Representation != "tcp-stream-chunks" {
		return fmt.Errorf("%w: unsupported representation %q", ErrFixtureInvalid, f.Representation)
	}
	for expectedIndex, frame := range f.Frames {
		if frame.Index != expectedIndex || frame.Opcode == 0 || frame.LogicalLength < 0 || frame.Direction == "" || frame.EncryptionState == "" || frame.EDCState == "" || frame.MassiveRole == "" {
			return fmt.Errorf("%w: invalid frame metadata at index %d", ErrFixtureInvalid, expectedIndex)
		}
		if frame.Direction != "client->server" && frame.Direction != "server->client" {
			return fmt.Errorf("%w: invalid frame direction %q", ErrFixtureInvalid, frame.Direction)
		}
		payload, err := decodeHex(frame.PayloadHex)
		if err != nil {
			return fmt.Errorf("%w: frame %d payload: %v", ErrFixtureInvalid, expectedIndex, err)
		}
		if frame.LogicalLength != len(payload) {
			return fmt.Errorf("%w: frame %d logical length %d does not match payload length %d", ErrFixtureInvalid, expectedIndex, frame.LogicalLength, len(payload))
		}
		if frame.ExpectedPayloadHex != "" {
			if _, err = decodeHex(frame.ExpectedPayloadHex); err != nil {
				return fmt.Errorf("%w: frame %d expected payload: %v", ErrFixtureInvalid, expectedIndex, err)
			}
		}
		if f.Representation == "logical-payload" && len(frame.ChunkLengths) != 0 {
			return fmt.Errorf("%w: logical-payload frame %d cannot contain TCP chunk lengths", ErrFixtureInvalid, expectedIndex)
		}
	}
	return nil
}

// ReplayCodec receives one validated logical payload and returns the payload
// produced by the typed codec. It is intentionally generic: package-specific
// decode/encode and state assertions stay in protocol tests, not in the
// capture metadata package.
type ReplayCodec func(Frame, []byte) ([]byte, error)

func Replay(fixture Fixture, codec ReplayCodec) error {
	if codec == nil {
		return fmt.Errorf("%w: replay codec is nil", ErrFixtureInvalid)
	}
	if err := fixture.Validate(); err != nil {
		return err
	}
	for index, frame := range fixture.Frames {
		payload, err := decodeHex(frame.PayloadHex)
		if err != nil {
			return fmt.Errorf("replay frame %d: %w", index, err)
		}
		encoded, err := codec(frame, payload)
		if err != nil {
			return fmt.Errorf("replay frame %d: %w", index, err)
		}
		if frame.ExpectedPayloadHex != "" {
			expected, decodeErr := decodeHex(frame.ExpectedPayloadHex)
			if decodeErr != nil {
				return fmt.Errorf("replay frame %d expected payload: %w", index, decodeErr)
			}
			if hex.EncodeToString(encoded) != hex.EncodeToString(expected) {
				return fmt.Errorf("replay frame %d re-encoding differs: got %x want %x", index, encoded, expected)
			}
		}
	}
	return nil
}

func decodeHex(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("hex payload is empty")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func validateIdentity(value, name string) error {
	if len(value) != 64 {
		return fmt.Errorf("%w: %s must contain 64 hex characters", ErrFixtureInvalid, name)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%w: %s is not hexadecimal", ErrFixtureInvalid, name)
	}
	return nil
}
