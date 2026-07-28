package profiles

import (
	"errors"
	"testing"
)

func TestVSRO188ProfileLoadsEmbeddedCanonicalProfile(t *testing.T) {
	t.Parallel()

	profile, err := VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}

	if profile.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", profile.SchemaVersion, CurrentSchemaVersion)
	}
	if profile.Profile != VSRO188 {
		t.Fatalf("profile = %q, want %q", profile.Profile, VSRO188)
	}
	if profile.Status != StatusEvidencePending {
		t.Fatalf("status = %q, want %q", profile.Status, StatusEvidencePending)
	}
}

func TestParseRejectsUnknownOrMultipleValues(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"unknown field":   `{"schema_version":1,"unknown":true}`,
		"multiple values": `{} {}`,
	} {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(raw))
			if !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("Parse(%q) error = %v, want invalid-profile error", raw, err)
			}
		})
	}
}
