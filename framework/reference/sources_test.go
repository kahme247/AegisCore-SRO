package reference

import (
	"errors"
	"testing"
)

const checksumA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const checksumB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestSourceManifestCanonicalizesOrderingAndChecksums(t *testing.T) {
	t.Parallel()

	manifest, err := NewSourceManifest([]Source{
		{ID: `PK2\\Z.TXT`, Kind: "PK2-Entry", SHA256: checksumB},
		{ID: "pk2/a.txt", Kind: "pk2-entry", SHA256: checksumA},
	})
	if err != nil {
		t.Fatalf("NewSourceManifest() error = %v", err)
	}

	sources := manifest.Sources()
	if len(sources) != 2 {
		t.Fatalf("source count = %d, want 2", len(sources))
	}
	if sources[0].ID != "pk2/a.txt" || sources[1].ID != "pk2/z.txt" {
		t.Fatalf("source order = %#v, want canonical ID ordering", sources)
	}
	if sources[0].SHA256 != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("source checksum = %q, want uppercase canonical checksum", sources[0].SHA256)
	}

	sources[0].ID = "mutated"
	if manifest.Sources()[0].ID != "pk2/a.txt" {
		t.Fatal("Sources() leaked manifest storage")
	}
}

func TestSourceManifestRejectsDuplicateIDsAndBadChecksums(t *testing.T) {
	t.Parallel()

	_, err := NewSourceManifest([]Source{
		{ID: "pk2/a.txt", Kind: "pk2-entry", SHA256: checksumA},
		{ID: `PK2\\A.TXT`, Kind: "pk2-entry", SHA256: checksumB},
	})
	if !errors.Is(err, ErrDuplicateSource) {
		t.Fatalf("duplicate source error = %v, want duplicate-source error", err)
	}

	_, err = NewSourceManifest([]Source{{ID: "pk2/a.txt", Kind: "pk2-entry", SHA256: "not-a-checksum"}})
	if !errors.Is(err, ErrInvalidSourceChecksum) {
		t.Fatalf("bad checksum error = %v, want invalid-checksum error", err)
	}
}

func TestEmptyDecoderRegistryFailsClosed(t *testing.T) {
	t.Parallel()

	registry, err := NewDecoderRegistry()
	if err != nil {
		t.Fatalf("NewDecoderRegistry() error = %v", err)
	}
	_, err = registry.DecoderFor(Source{ID: "pk2/a.txt", Kind: "pk2-entry", SHA256: checksumA})
	if !errors.Is(err, ErrNoDecoder) {
		t.Fatalf("DecoderFor() error = %v, want no-decoder error", err)
	}
}
