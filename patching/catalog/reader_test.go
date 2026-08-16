package catalog

import (
	"errors"
	"testing"
)

func TestValidateReleaseChainSupportsMultipleIncrementalVersions(t *testing.T) {
	links := []releaseLink{{base: 188, version: 189}, {base: 189, version: 190}}
	if err := validateReleaseChain(188, 190, links); err != nil {
		t.Fatalf("188 to 190 chain error = %v", err)
	}
	if err := validateReleaseChain(189, 190, links[1:]); err != nil {
		t.Fatalf("189 to 190 chain error = %v", err)
	}
	if err := validateReleaseChain(190, 190, nil); err != nil {
		t.Fatalf("current 190 chain error = %v", err)
	}
}

func TestValidateReleaseChainRejectsGapsAndWrongOrdering(t *testing.T) {
	tests := [][]releaseLink{
		{{base: 189, version: 190}},
		{{base: 188, version: 190}},
		{{base: 188, version: 189}, {base: 188, version: 189}},
	}
	for _, links := range tests {
		if err := validateReleaseChain(188, 190, links); !errors.Is(err, ErrClientTooOld) {
			t.Fatalf("validateReleaseChain(%+v) error = %v", links, err)
		}
	}
}
