// Package reference contains offline, runtime-independent reference-data
// provenance contracts. It deliberately does not parse PK2 contents, import
// reference rows, or publish a database dataset.
package reference

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

var (
	// ErrInvalidSource identifies a malformed logical source descriptor.
	ErrInvalidSource = errors.New("invalid reference source")
	// ErrInvalidSourceChecksum identifies a source descriptor without a
	// canonical SHA-256 checksum.
	ErrInvalidSourceChecksum = errors.New("invalid reference source checksum")
	// ErrDuplicateSource prevents a source manifest from ambiguously naming the
	// same logical source more than once.
	ErrDuplicateSource = errors.New("duplicate reference source")
	// ErrDuplicateDecoder prevents two decoders from claiming one source kind.
	ErrDuplicateDecoder = errors.New("duplicate reference source decoder")
	// ErrNoDecoder is returned instead of attempting unverified PK2 parsing.
	ErrNoDecoder = errors.New("no reference source decoder registered")
)

// Source is a checksum-pinned logical input to a future offline importer.
// ID is deliberately logical rather than an absolute filesystem path so a
// provenance manifest remains portable and deterministic.
type Source struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

// SourceManifest is the small boundary future extraction code must consume.
// It carries only immutable source identities/checksums, never open files.
type SourceManifest interface {
	Sources() []Source
}

// StaticSourceManifest is an immutable, canonical source manifest suitable
// for use by an offline importer once capture-backed decoders exist.
type StaticSourceManifest struct {
	sources []Source
}

// NewSourceManifest validates, canonicalizes, sorts, and defensively copies
// source descriptors. Duplicate logical IDs are rejected after normalization.
func NewSourceManifest(sources []Source) (StaticSourceManifest, error) {
	canonical, err := canonicalizeSources(sources)
	if err != nil {
		return StaticSourceManifest{}, err
	}
	return StaticSourceManifest{sources: canonical}, nil
}

// Sources returns a copy so callers cannot mutate a manifest after it has
// been constructed.
func (m StaticSourceManifest) Sources() []Source {
	return append([]Source(nil), m.sources...)
}

func canonicalizeSources(sources []Source) ([]Source, error) {
	canonical := make([]Source, 0, len(sources))
	for _, source := range sources {
		normalized, err := normalizeSource(source)
		if err != nil {
			return nil, err
		}
		canonical = append(canonical, normalized)
	}

	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].ID < canonical[right].ID
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].ID == canonical[index].ID {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateSource, canonical[index].ID)
		}
	}

	return canonical, nil
}

func normalizeSource(source Source) (Source, error) {
	identifier := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(source.ID, "\\", "/")))
	identifier = path.Clean(identifier)
	if identifier == "." || identifier == "" || strings.HasPrefix(identifier, "../") || strings.HasPrefix(identifier, "/") {
		return Source{}, fmt.Errorf("%w: source id %q", ErrInvalidSource, source.ID)
	}

	kind := strings.ToLower(strings.TrimSpace(source.Kind))
	if kind == "" {
		return Source{}, fmt.Errorf("%w: source %q has no kind", ErrInvalidSource, source.ID)
	}

	checksum, err := normalizeSHA256(source.SHA256)
	if err != nil {
		return Source{}, fmt.Errorf("%w: source %q: %v", ErrInvalidSourceChecksum, source.ID, err)
	}

	return Source{ID: identifier, Kind: kind, SHA256: checksum}, nil
}

// SourceDecoder is intentionally narrow: decoders are explicit offline
// dependencies, and registration alone does not provide persistence or
// publishing capabilities.
type SourceDecoder interface {
	SourceKind() string
	Decode(context.Context, Source) error
}

// DecoderRegistry resolves an explicitly registered decoder by source kind.
// There is intentionally no default PK2 content decoder.
type DecoderRegistry interface {
	DecoderFor(Source) (SourceDecoder, error)
}

// Registry is an explicit decoder registry. Constructing it with no decoders
// is the safe default for this evidence-pending profile.
type Registry struct {
	decoders map[string]SourceDecoder
}

// NewDecoderRegistry creates a registry from explicitly supplied decoders.
func NewDecoderRegistry(decoders ...SourceDecoder) (*Registry, error) {
	registry := &Registry{decoders: make(map[string]SourceDecoder, len(decoders))}
	for _, decoder := range decoders {
		if decoder == nil {
			return nil, fmt.Errorf("%w: nil decoder", ErrInvalidSource)
		}

		kind := strings.ToLower(strings.TrimSpace(decoder.SourceKind()))
		if kind == "" {
			return nil, fmt.Errorf("%w: decoder has no source kind", ErrInvalidSource)
		}
		if _, exists := registry.decoders[kind]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateDecoder, kind)
		}
		registry.decoders[kind] = decoder
	}
	return registry, nil
}

// DecoderFor returns a clear error rather than falling back to the legacy PK2
// reader, which is not a safe offline importer boundary.
func (r *Registry) DecoderFor(source Source) (SourceDecoder, error) {
	if r != nil {
		if decoder, ok := r.decoders[strings.ToLower(strings.TrimSpace(source.Kind))]; ok {
			return decoder, nil
		}
	}
	return nil, fmt.Errorf("%w: source kind %q", ErrNoDecoder, source.Kind)
}
