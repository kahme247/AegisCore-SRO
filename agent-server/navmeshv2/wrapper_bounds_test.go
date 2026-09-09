package navmeshv2

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"testing"
)

func TestLoadWrapperContentRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name    string
		content func() []byte
		load    func([]byte) error
		want    error
	}{
		{
			name:    "resource truncated header",
			content: func() []byte { return make([]byte, 63) },
			load:    loadResourceContentError,
			want:    ErrInvalidNavmeshHeader,
		},
		{
			name: "resource wrong signature",
			content: func() []byte {
				content := make([]byte, 68)
				copy(content, "JMXVRES 9999")
				return content
			},
			load: loadResourceContentError,
			want: ErrInvalidNavmeshHeader,
		},
		{
			name: "resource offset before body",
			content: func() []byte {
				content := validResourceContent("")
				binary.LittleEndian.PutUint32(content[40:], 12)
				return content
			},
			load: loadResourceContentError,
			want: ErrInvalidNavmeshData,
		},
		{
			name: "resource offset beyond file",
			content: func() []byte {
				content := validResourceContent("")
				binary.LittleEndian.PutUint32(content[40:], math.MaxUint32)
				return content
			},
			load: loadResourceContentError,
			want: ErrInvalidNavmeshData,
		},
		{
			name: "resource offset has truncated length",
			content: func() []byte {
				content := validResourceContent("")
				binary.LittleEndian.PutUint32(content[40:], uint32(len(content)-3))
				return content
			},
			load: loadResourceContentError,
			want: ErrInvalidNavmeshData,
		},
		{
			name: "compound path length beyond file",
			content: func() []byte {
				content := validCompoundContent("")
				binary.LittleEndian.PutUint32(content[40:], math.MaxUint32)
				return content
			},
			load: loadCompoundContentError,
			want: ErrInvalidNavmeshData,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.load(test.content())
			if !errors.Is(err, test.want) {
				t.Fatalf("load wrapper error = %v, want %v", err, test.want)
			}
			var loadErr *NavmeshLoadError
			if !errors.As(err, &loadErr) {
				t.Fatalf("load wrapper error type = %T, want *NavmeshLoadError", err)
			}
		})
	}
}

func TestLoadWrapperContentReadsAndNormalizesNavmeshPaths(t *testing.T) {
	resource, err := loadResourceContent("valid.bsr", validResourceContent("navmesh\\object.bms"))
	if err != nil {
		t.Fatalf("loadResourceContent() error = %v", err)
	}
	if want := "navmesh" + string(os.PathSeparator) + "object.bms"; resource.NavMeshObjPath != want {
		t.Fatalf("Resource.NavMeshObjPath = %q, want %q", resource.NavMeshObjPath, want)
	}

	compound, err := loadCompoundContent("valid.cpd", validCompoundContent("resource/object.bsr"))
	if err != nil {
		t.Fatalf("loadCompoundContent() error = %v", err)
	}
	if want := "resource" + string(os.PathSeparator) + "object.bsr"; compound.NavMeshObjPath != want {
		t.Fatalf("Compound.NavMeshObjPath = %q, want %q", compound.NavMeshObjPath, want)
	}
}

func TestLoadWrappersRejectNilPK2Reader(t *testing.T) {
	_, resourceErr := LoadResource("missing.bsr", nil)
	if !errors.Is(resourceErr, ErrInvalidNavmeshData) {
		t.Fatalf("LoadResource() error = %v, want ErrInvalidNavmeshData", resourceErr)
	}
	_, compoundErr := LoadCompoundFile("missing.cpd", nil)
	if !errors.Is(compoundErr, ErrInvalidNavmeshData) {
		t.Fatalf("LoadCompoundFile() error = %v, want ErrInvalidNavmeshData", compoundErr)
	}
}

func loadResourceContentError(content []byte) error {
	_, err := loadResourceContent("test.bsr", content)
	return err
}

func loadCompoundContentError(content []byte) error {
	_, err := loadCompoundContent("test.cpd", content)
	return err
}

func validResourceContent(path string) []byte {
	content := make([]byte, 64+4+len(path))
	copy(content, "JMXVRES 0109")
	binary.LittleEndian.PutUint32(content[40:], 64)
	binary.LittleEndian.PutUint32(content[64:], uint32(len(path)))
	copy(content[68:], path)
	return content
}

func validCompoundContent(path string) []byte {
	content := make([]byte, 40+4+len(path))
	copy(content, "JMXVCPD 0101")
	binary.LittleEndian.PutUint32(content[12:], 40)
	binary.LittleEndian.PutUint32(content[40:], uint32(len(path)))
	copy(content[44:], path)
	return content
}
