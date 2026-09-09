package navmeshv2

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/g3n/engine/math32"
)

func TestValidateObjectTopologyRejectsCorruptRuntimeGeometry(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RtNavmeshObj)
	}{
		{name: "nil triangle vertex", mutate: func(object *RtNavmeshObj) { object.Cells[0].Triangle.B = nil }},
		{name: "non-finite edge", mutate: func(object *RtNavmeshObj) {
			object.GlobalEdges[0].Line.B.X = float32(math.NaN())
		}},
		{name: "invalid internal destination", mutate: func(object *RtNavmeshObj) {
			object.InternalEdges[0].DstCellIndex = 2
		}},
		{name: "mismatched grid dimensions", mutate: func(object *RtNavmeshObj) { object.Grid.Width = 2 }},
		{name: "invalid tile cell reference", mutate: func(object *RtNavmeshObj) {
			object.Grid.Tiles[0].Cells[0].Index = 3
		}},
		{name: "invalid tile edge reference", mutate: func(object *RtNavmeshObj) {
			object.Grid.Tiles[0].InternalEdges[0].Index = 3
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := validObjectTopologyFixture()
			test.mutate(&object)
			err := validateObjectTopology(&object, "corrupt.bms")
			if !errors.Is(err, ErrInvalidNavmeshData) {
				t.Fatalf("validateObjectTopology() error = %v, want ErrInvalidNavmeshData", err)
			}
			var loadErr *NavmeshLoadError
			if !errors.As(err, &loadErr) || loadErr.Operation != "validate object topology" {
				t.Fatalf("validateObjectTopology() error type/operation = %T/%v", err, loadErr)
			}
		})
	}
}

func TestValidateTerrainInstanceTopologyRejectsInvalidTransform(t *testing.T) {
	instance := validObjectInstanceFixture()
	instance.WorldToLocal = nil
	if err := validateTerrainInstanceTopology([]RtNavmeshInstObj{instance}, "region"); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("validateTerrainInstanceTopology() error = %v, want ErrInvalidNavmeshData", err)
	}
}

func TestValidateTerrainCellObjectReferencesRejectsUnknownOrInconsistentCopies(t *testing.T) {
	instance := validObjectInstanceFixture()
	instance.WorldID = 7
	instance.ID = 3
	instance.Region = NewRegionFromInt16(1)
	terrain := newTerrainEdgeTestTerrain(1, 1)
	terrain.Objects = []RtNavmeshInstObj{instance}
	terrain.Cells[0].Objects = []RtNavmeshInstObj{instance}
	if err := validateTerrainCellObjectReferences(terrain, "region"); err != nil {
		t.Fatalf("valid references error = %v", err)
	}
	terrain.Cells[0].Objects[0].WorldID = 8
	if err := validateTerrainCellObjectReferences(terrain, "region"); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("unknown reference error = %v, want ErrInvalidNavmeshData", err)
	}
}

func TestSaveNavmeshDataRejectsInvalidObjectBeforeWriting(t *testing.T) {
	cachePrefix := filepath.Join(t.TempDir(), "invalid-object")
	object := validObjectTopologyFixture()
	object.Cells[0].Triangle.A = nil
	loader := &Loader{
		RegionData: map[int16]*RtNavmeshTerrain{},
		ObjectData: map[uint32]RtNavmeshObj{7: object},
	}

	err := loader.saveNavmeshDataAsGOB(cachePrefix)
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("saveNavmeshDataAsGOB() error = %v, want ErrInvalidNavmeshData", err)
	}
	for _, artifact := range []string{cachePrefix + "_regions", cachePrefix + "_objects"} {
		if _, statErr := os.Stat(artifact); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("cache artifact %q exists after object validation failure: %v", artifact, statErr)
		}
	}
}

func validObjectInstanceFixture() RtNavmeshInstObj {
	return RtNavmeshInstObj{RtNavmeshInstBase: RtNavmeshInstBase{
		Object:       validObjectTopologyFixture(),
		Position:     math32.NewVector3(0, 0, 0),
		Rotation:     math32.NewQuaternion(0, 0, 0, 1),
		Scale:        math32.NewVector3(1, 1, 1),
		LocalToWorld: math32.NewMatrix4().Identity(),
		WorldToLocal: math32.NewMatrix4().Identity(),
	}}
}

func validObjectTopologyFixture() RtNavmeshObj {
	object := NewNavmeshObj("valid.bms")
	object.Cells = []RtNavmeshCellTri{{
		RtNavmeshCellBase: RtNavmeshCellBase{Index: 0},
		Triangle: Triangle{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(1, 0, 0),
			C: math32.NewVector3(0, 0, 1),
		},
	}}
	object.GlobalEdges = []RtNavmeshEdgeGlobal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Index: 0,
		Line: LineSegment{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(1, 0, 0),
		},
		SrcCellIndex: 0,
		DstCellIndex: int(^uint16(0)),
	}}}
	object.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Index: 0,
		Line: LineSegment{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(0, 0, 1),
		},
		SrcCellIndex: 0,
		DstCellIndex: 0,
	}}}
	object.Grid.Width = 1
	object.Grid.Height = 1
	object.Grid.Rectangle = NewRectangle(0, 0, RtNavmeshObjGridTileWidth, RtNavmeshObjGridTileHeight)
	object.Grid.Tiles = []RtNavmeshObjGridTile{{
		Index:         0,
		X:             0,
		Y:             0,
		Rectangle:     NewRectangle(0, 0, RtNavmeshObjGridTileWidth, RtNavmeshObjGridTileHeight),
		Cells:         append([]RtNavmeshCellTri(nil), object.Cells...),
		GlobalEdges:   append([]RtNavmeshEdgeGlobal(nil), object.GlobalEdges...),
		InternalEdges: append([]RtNavmeshEdgeInternal(nil), object.InternalEdges...),
	}}
	return object
}
