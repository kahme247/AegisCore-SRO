package navmeshv2

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestLoadTerrainMeshRejectsCountsThatExceedRemainingFileBeforeAllocation(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		write  func([]byte)
	}{
		{
			name:   "object instances",
			offset: 12,
			write:  func(field []byte) { binary.LittleEndian.PutUint16(field, math.MaxUint16) },
		},
		{
			name:   "cells",
			offset: 14,
			write:  func(field []byte) { binary.LittleEndian.PutUint32(field, math.MaxUint32) },
		},
		{
			name:   "global edges",
			offset: 22,
			write:  func(field []byte) { binary.LittleEndian.PutUint32(field, math.MaxUint32) },
		},
		{
			name:   "internal edges",
			offset: 26,
			write:  func(field []byte) { binary.LittleEndian.PutUint32(field, math.MaxUint32) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := minimalTerrainContent()
			test.write(content[test.offset:])
			loader := newLoader(nil, "test.pk2")

			err := loader.loadTerrainMeshContent(content, "nv_test.nvm", 1)
			if !errors.Is(err, ErrInvalidNavmeshData) {
				t.Fatalf("loadTerrainMeshContent() error = %v, want ErrInvalidNavmeshData", err)
			}
			var loadErr *NavmeshLoadError
			if !errors.As(err, &loadErr) {
				t.Fatalf("loadTerrainMeshContent() error type = %T, want *NavmeshLoadError", err)
			}
			if len(loader.RegionData) != 0 {
				t.Fatalf("invalid terrain was published: %+v", loader.RegionData)
			}
		})
	}
}

func TestLoadTerrainMeshRejectsTruncatedFixedMaps(t *testing.T) {
	content := minimalTerrainContent()
	content = content[:len(content)-1]
	loader := newLoader(nil, "test.pk2")

	err := loader.loadTerrainMeshContent(content, "nv_test.nvm", 1)
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("loadTerrainMeshContent() error = %v, want ErrInvalidNavmeshData", err)
	}
}

func TestLoadTerrainMeshRejectsInvalidCellObjectReference(t *testing.T) {
	const headerAndObjectCountBytes = 12 + 2
	const cellCountBytes = 8
	const objectReferenceBytes = 2
	const edgeCountBytes = 4 + 4
	content := make([]byte, headerAndObjectCountBytes+cellCountBytes+terrainCellMinBytes+objectReferenceBytes+edgeCountBytes+terrainFixedTailBytes)
	copy(content, "JMXVNVM 1000")
	binary.LittleEndian.PutUint32(content[headerAndObjectCountBytes:], 1)
	cellOffset := headerAndObjectCountBytes + cellCountBytes
	content[cellOffset+terrainCellMinBytes-1] = 1
	// Object index zero is invalid because this fixture declares no terrain
	// object instances. The parser must reject it without indexing the slice.
	binary.LittleEndian.PutUint16(content[cellOffset+terrainCellMinBytes:], 0)

	loader := newLoader(nil, "test.pk2")
	err := loader.loadTerrainMeshContent(content, "nv_test.nvm", 1)
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("loadTerrainMeshContent() error = %v, want ErrInvalidNavmeshData", err)
	}
	var loadErr *NavmeshLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("loadTerrainMeshContent() error type = %T, want *NavmeshLoadError", err)
	}
	if len(loader.RegionData) != 0 {
		t.Fatalf("invalid terrain was published: %+v", loader.RegionData)
	}
}

func minimalTerrainContent() []byte {
	const prefixBytes = 12 + 2 + 8 + 4 + 4
	content := make([]byte, prefixBytes+terrainFixedTailBytes)
	copy(content, "JMXVNVM 1000")
	return content
}
