package navmeshv2

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmesh"
)

func TestLoadNavMeshInfosReturnsTypedMetadataError(t *testing.T) {
	loader := newLoader(nil, "test.pk2")
	err := loader.LoadNavMeshInfos()
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadNavMeshInfos() error = %v, want ErrInvalidNavmeshData", err)
	}
	var loadErr *NavmeshLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("LoadNavMeshInfos() error = %T, want *NavmeshLoadError", err)
	}
	if loadErr.Operation != "read map info" {
		t.Fatalf("operation = %q, want read map info", loadErr.Operation)
	}
}

func TestLoadObjectMeshReturnsTypedMissingError(t *testing.T) {
	loader := newLoader(nil, "test.pk2")
	_, err := loader.LoadObjectMesh(42)
	if !errors.Is(err, ErrObjectMeshNotFound) {
		t.Fatalf("LoadObjectMesh() error = %v, want ErrObjectMeshNotFound", err)
	}
	var loadErr *NavmeshLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("LoadObjectMesh() error = %T, want *NavmeshLoadError", err)
	}
}

func TestObjectGridLoadReturnsNotImplemented(t *testing.T) {
	var grid RtNavmeshObjGrid
	if err := grid.Load(navmesh.Loader{}); !errors.Is(err, ErrNavmeshNotImplemented) {
		t.Fatalf("Load() error = %v, want ErrNavmeshNotImplemented", err)
	}
}
