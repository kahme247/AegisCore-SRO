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

func TestJobCompatibilityCoverageImplementsSafeTradeLifecycle(t *testing.T) {
	entries := JobCompatibilityCoverage()
	var safeTrade CompatibilityCoverage
	for _, entry := range entries {
		if entry.Surface == "tri-job safe trade" {
			safeTrade = entry
			break
		}
	}
	if safeTrade.Status != CompatibilityImplemented || safeTrade.ResponseOpcode != 0x34D5 {
		t.Fatalf("safe-trade coverage = %+v", safeTrade)
	}
	for _, opcode := range []uint16{0x70E1, 0x70E2, 0x70E3, 0x70E4, 0x70E5, 0x70E6} {
		found := false
		for _, entry := range entries {
			if entry.RequestOpcode == opcode {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing JOB request opcode 0x%04X", opcode)
		}
	}
	for _, entry := range entries {
		if entry.Status != CompatibilityImplemented {
			t.Fatalf("JOB coverage is not implemented: %+v", entry)
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
	if got := find("token package purchase"); got.Status != CompatibilityImplemented || got.Operation != "0x2B" {
		t.Fatalf("token package coverage = %+v", got)
	}
	if got := find("invalid specialty operation"); got.Status != CompatibilityFailClosed || got.Operation != "0x2C" {
		t.Fatalf("invalid specialty operation coverage = %+v", got)
	}
	if got := find("magic-cube lifecycle"); got.Status != CompatibilityPartial || got.RequestOpcode != 0x7034 || got.ResponseOpcode != 0xB034 || got.Operation != "request 0x27/0x28; response 0x27/0x28/0x29" {
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
	if got := find("storage open"); got.Status != CompatibilityImplemented || got.RequestOpcode != 0x703C || got.ResponseOpcode != 0xB03C {
		t.Fatalf("storage-open coverage = %+v", got)
	}
	if got := find("ranged ammunition update"); got.Status != CompatibilityImplemented || got.ResponseOpcode != 0x3057 {
		t.Fatalf("ranged-ammunition coverage = %+v", got)
	}
	if got := find("ground-item alternate pickup rejection"); got.Status != CompatibilityImplemented || got.RequestOpcode != 0x703F || got.ResponseOpcode != 0xB03F {
		t.Fatalf("unknown inventory selector coverage = %+v", got)
	}
}
