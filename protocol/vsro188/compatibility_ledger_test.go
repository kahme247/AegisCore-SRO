package vsro188

import "testing"

func TestItemCompatibilityCoverageHasUniqueSurfaces(t *testing.T) {
	entries := ItemCompatibilityCoverage()
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		key := entry.Surface + "|" + entry.Operation
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate compatibility ledger entry %q", key)
		}
		seen[key] = struct{}{}
		if entry.Surface == "" || entry.DomainBoundary == "" || entry.Evidence == "" {
			t.Fatalf("incomplete compatibility ledger entry: %+v", entry)
		}
		if entry.Status != CompatibilityImplemented && entry.Status != CompatibilityPartial && entry.Status != CompatibilityFramingOnly && entry.Status != CompatibilityFailClosed {
			t.Fatalf("unknown compatibility status in %+v", entry)
		}
	}
}

func TestItemCompatibilityCoveragePinsHighRiskBoundaries(t *testing.T) {
	entries := ItemCompatibilityCoverage()
	find := func(surface string) CompatibilityCoverage {
		t.Helper()
		for _, entry := range entries {
			if entry.Surface == surface {
				return entry
			}
		}
		t.Fatalf("missing compatibility ledger entry %q", surface)
		return CompatibilityCoverage{}
	}

	if got := find("modern item mall"); got.Status != CompatibilityPartial || got.RequestOpcode != 0x7034 || got.ResponseOpcode != 0xB034 {
		t.Fatalf("modern mall coverage = %+v", got)
	}
	if got := find("specialty/job goods"); got.Status != CompatibilityFramingOnly {
		t.Fatalf("specialty coverage = %+v", got)
	}
	if got := find("magic-cube lifecycle"); got.Status != CompatibilityFailClosed {
		t.Fatalf("magic-cube coverage = %+v", got)
	}
	if got := find("unresolved item-use callbacks"); got.Status != CompatibilityFailClosed {
		t.Fatalf("unresolved item-use coverage = %+v", got)
	}
	if got := find("skill withdrawal"); got.Status != CompatibilityImplemented || got.RequestOpcode != 0x7202 || got.ResponseOpcode != 0xB202 {
		t.Fatalf("skill withdrawal coverage = %+v", got)
	}
	if got := find("mastery withdrawal"); got.Status != CompatibilityImplemented || got.RequestOpcode != 0x7203 || got.ResponseOpcode != 0xB203 {
		t.Fatalf("mastery withdrawal coverage = %+v", got)
	}
}
