package navmeshv2

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestLoadMeshObjectRejectsCountsThatExceedRemainingFileBeforeAllocation(t *testing.T) {
	tests := []struct {
		name         string
		content      func() []byte
		offset       int
		structOption RtNavmeshStructOption
	}{
		{name: "vertices", content: minimalMeshObjectContent, offset: 0},
		{name: "cells", content: minimalMeshObjectContent, offset: 4},
		{name: "global edges", content: minimalMeshObjectContent, offset: 8},
		{name: "internal edges", content: minimalMeshObjectContent, offset: 12},
		{name: "events", content: minimalEventMeshObjectContent, offset: 16, structOption: RtNavmeshStructOptionEvent},
		{name: "grid tiles", content: minimalMeshObjectContent, offset: 32},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := test.content()
			binary.LittleEndian.PutUint32(content[test.offset:], math.MaxUint32)
			mesh := &MeshFile{
				FileName:     "malformed.bms",
				FileContent:  content,
				StructOption: test.structOption,
			}

			_, err := mesh.LoadMeshObject()
			if !errors.Is(err, ErrInvalidNavmeshData) {
				t.Fatalf("LoadMeshObject() error = %v, want ErrInvalidNavmeshData", err)
			}
			var loadErr *NavmeshLoadError
			if !errors.As(err, &loadErr) {
				t.Fatalf("LoadMeshObject() error type = %T, want *NavmeshLoadError", err)
			}
		})
	}
}

func TestValidateMeshNavmeshOffsetClassifiesMissingAndCorruptOffsets(t *testing.T) {
	if err := validateMeshNavmeshOffset("visual-only.bms", 0, 100); !errors.Is(err, ErrObjectMeshNotFound) {
		t.Fatalf("zero offset error = %v, want ErrObjectMeshNotFound", err)
	}
	for _, offset := range []uint32{1, 101} {
		if err := validateMeshNavmeshOffset("corrupt.bms", offset, 100); !errors.Is(err, ErrInvalidNavmeshData) {
			t.Fatalf("offset %d error = %v, want ErrInvalidNavmeshData", offset, err)
		}
	}
	if err := validateMeshNavmeshOffset("valid.bms", 60, 100); err != nil {
		t.Fatalf("valid offset error = %v", err)
	}
}

func TestLoadMeshObjectRejectsOutOfRangeCellVertex(t *testing.T) {
	content := make([]byte, 4+13+4+8+4+4+20)
	binary.LittleEndian.PutUint32(content[0:], 1)
	binary.LittleEndian.PutUint32(content[17:], 1)
	binary.LittleEndian.PutUint16(content[21:], 1)
	mesh := &MeshFile{FileName: "invalid-cell.bms", FileContent: content}

	_, err := mesh.LoadMeshObject()
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadMeshObject() error = %v, want invalid vertex reference", err)
	}
}

func TestLoadMeshObjectRejectsEventLengthBeyondRemainingFile(t *testing.T) {
	content := minimalEventMeshObjectContent()
	binary.LittleEndian.PutUint32(content[16:], 1)
	binary.LittleEndian.PutUint32(content[20:], math.MaxUint32)
	mesh := &MeshFile{
		FileName:     "invalid-event.bms",
		FileContent:  content,
		StructOption: RtNavmeshStructOptionEvent,
	}

	_, err := mesh.LoadMeshObject()
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadMeshObject() error = %v, want invalid event length", err)
	}
}

func minimalMeshObjectContent() []byte {
	// Four zero counts followed by a zero-sized 20-byte grid header.
	return make([]byte, 4+4+4+4+20)
}

func minimalEventMeshObjectContent() []byte {
	// Four zero counts, an event count, and a zero-sized grid header.
	return make([]byte, 4+4+4+4+4+20)
}
