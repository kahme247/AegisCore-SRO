package parity

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestAuditBinaryDetectsEvidenceMismatch(t *testing.T) {
	payload := []byte("legacy-binary")
	want := fmt.Sprintf("%x", sha256.Sum256(payload))
	audit, err := AuditBinary(bytes.NewReader(payload), Binary{Service: "game-server", File: "SR_GameServer.exe", SHA256: want})
	if err != nil || !audit.Matches() {
		t.Fatalf("AuditBinary() = %+v, %v", audit, err)
	}
	mismatch, err := AuditBinary(strings.NewReader("different"), Binary{Service: "game-server", File: "SR_GameServer.exe", SHA256: want})
	if err != nil || mismatch.Matches() {
		t.Fatalf("mismatched AuditBinary() = %+v, %v", mismatch, err)
	}
}

func TestEmbeddedVSRO188ManifestIsValidAndNotPrematurelyComplete(t *testing.T) {
	manifest, err := LoadVSRO188()
	if err != nil {
		t.Fatalf("LoadVSRO188() error = %v", err)
	}
	if got, want := len(manifest.Binaries), 8; got != want {
		t.Fatalf("binary count = %d, want %d", got, want)
	}
	if got, want := len(manifest.Catalogs), 3; got != want {
		t.Fatalf("catalog count = %d, want %d", got, want)
	}
	if issues := manifest.Gate(); len(issues) == 0 {
		t.Fatal("incomplete parity program passed its completion gate")
	}
}

func TestAuditCatalogUsesDeclaredUniquenessAndFilter(t *testing.T) {
	catalog := Catalog{ID: "procedures", UniqueField: "object", FilterField: "kind", FilterValue: "procedure", ExpectedUnique: 2}
	audit, err := AuditCatalog(strings.NewReader("kind,object\nprocedure,_One\ntable,_One\nprocedure,_Two\nprocedure,_One\n"), catalog)
	if err != nil {
		t.Fatalf("AuditCatalog() error = %v", err)
	}
	if !audit.Matches() || audit.Actual != 2 {
		t.Fatalf("audit = %+v, want matching count 2", audit)
	}
}

func TestDecodeRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	for _, input := range []string{
		`{"schema_version":1,"target":"vsro188","unknown":true}`,
		`{"schema_version":1,"target":"vsro188"} {}`,
	} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Fatalf("Decode(%q) accepted invalid JSON contract", input)
		}
	}
}

func TestGateRequiresTestsForVerifiedAndReplacedCapabilities(t *testing.T) {
	manifest := Manifest{Capabilities: []Capability{
		{ID: "verified", Status: StatusVerified, Implementation: []string{"implementation.go"}},
		{ID: "replaced", Status: StatusReplaced, Replacement: "secure adapter"},
	}}
	issues := manifest.Gate()
	if got, want := len(issues), 2; got != want {
		t.Fatalf("issue count = %d, want %d", got, want)
	}
}
