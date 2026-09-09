package navmeshv2

import (
	"errors"
	"reflect"
	"testing"
)

func TestLoadObjectMeshesIsOrderedAndRollsBackOnFailure(t *testing.T) {
	decodeErr := errors.New("decode failed")
	original := map[uint32]RtNavmeshObj{77: NewNavmeshObj("existing")}
	loader := &Loader{
		ObjectInfo: ObjectInfo{Objects: map[uint32]ObjectInfoEntry{
			9: {Index: 9, FilePath: "nine.bms"},
			2: {Index: 2, FilePath: "two.bms"},
			5: {Index: 5, FilePath: "five.bms"},
		}},
		ObjectData: original,
	}
	var decoded []uint32

	err := loader.loadObjectMeshes(func(entry ObjectInfoEntry) (RtNavmeshObj, error) {
		decoded = append(decoded, entry.Index)
		if entry.Index == 5 {
			return RtNavmeshObj{}, decodeErr
		}
		return NewNavmeshObj(entry.FilePath), nil
	})

	if !errors.Is(err, decodeErr) {
		t.Fatalf("loadObjectMeshes() error = %v, want decode failure", err)
	}
	if want := []uint32{2, 5}; !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decode order = %v, want %v", decoded, want)
	}
	if !reflect.DeepEqual(loader.ObjectData, original) {
		t.Fatalf("ObjectData = %#v, want original cache %#v", loader.ObjectData, original)
	}
}

func TestLoadObjectMeshesPublishesCompleteReplacement(t *testing.T) {
	loader := &Loader{
		ObjectInfo: ObjectInfo{Objects: map[uint32]ObjectInfoEntry{
			9: {Index: 9, FilePath: "nine.bms"},
			2: {Index: 2, FilePath: "two.bms"},
		}},
		ObjectData: map[uint32]RtNavmeshObj{77: NewNavmeshObj("stale")},
	}
	var decoded []uint32

	err := loader.loadObjectMeshes(func(entry ObjectInfoEntry) (RtNavmeshObj, error) {
		decoded = append(decoded, entry.Index)
		return NewNavmeshObj(entry.FilePath), nil
	})
	if err != nil {
		t.Fatalf("loadObjectMeshes() error = %v", err)
	}
	if want := []uint32{2, 9}; !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decode order = %v, want %v", decoded, want)
	}
	if len(loader.ObjectData) != 2 || loader.ObjectData[2].Filename != "two.bms" || loader.ObjectData[9].Filename != "nine.bms" {
		t.Fatalf("ObjectData = %#v, want complete new cache", loader.ObjectData)
	}
	if _, exists := loader.ObjectData[77]; exists {
		t.Fatal("ObjectData retained stale object 77")
	}
}

func TestLoadObjectMeshesSkipsCatalogEntriesWithoutNavmesh(t *testing.T) {
	loader := &Loader{
		ObjectInfo: ObjectInfo{Objects: map[uint32]ObjectInfoEntry{
			2: {Index: 2, FilePath: "visual-only.bsr"},
			9: {Index: 9, FilePath: "walkable.bms"},
		}},
		ObjectData: map[uint32]RtNavmeshObj{77: NewNavmeshObj("stale")},
	}
	err := loader.loadObjectMeshes(func(entry ObjectInfoEntry) (RtNavmeshObj, error) {
		if entry.Index == 2 {
			return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: ErrObjectMeshNotFound}
		}
		return NewNavmeshObj(entry.FilePath), nil
	})
	if err != nil {
		t.Fatalf("loadObjectMeshes() error = %v", err)
	}
	if len(loader.ObjectData) != 1 || loader.ObjectData[9].Filename != "walkable.bms" {
		t.Fatalf("ObjectData = %#v, want only navmesh entry 9", loader.ObjectData)
	}
}

func TestLoadObjectMeshesRejectsCatalogIndexMismatchBeforeDecode(t *testing.T) {
	loader := &Loader{ObjectInfo: ObjectInfo{Objects: map[uint32]ObjectInfoEntry{
		2: {Index: 3, FilePath: "mismatch.bms"},
	}}}
	decodeCalled := false

	err := loader.loadObjectMeshes(func(ObjectInfoEntry) (RtNavmeshObj, error) {
		decodeCalled = true
		return RtNavmeshObj{}, nil
	})
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("loadObjectMeshes() error = %v, want ErrInvalidNavmeshData", err)
	}
	if decodeCalled {
		t.Fatal("decoder called for mismatched catalog entry")
	}
	var loadErr *NavmeshLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("loadObjectMeshes() error type = %T, want *NavmeshLoadError", err)
	}
}

func TestObjectMeshLoadingRejectsNilLoader(t *testing.T) {
	var loader *Loader
	if err := loader.LoadObjectMeshes(); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadObjectMeshes() error = %v, want ErrInvalidNavmeshData", err)
	}
	if _, err := loader.LoadObjectMesh(1); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadObjectMesh() error = %v, want ErrInvalidNavmeshData", err)
	}
}
