package navmeshv2

import (
	"encoding/gob"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/g3n/engine/math32"
)

func TestLoadPrecomputedNavmeshDataSupportsLegacyMapCaches(t *testing.T) {
	cachePrefix := filepath.Join(t.TempDir(), "legacy_navdata.gob")
	terrain := newTerrainEdgeTestTerrain(1, 1)
	object := NewNavmeshObj("legacy.bms")
	object.Cells = []RtNavmeshCellTri{{
		RtNavmeshCellBase: RtNavmeshCellBase{Index: 0},
		Triangle: Triangle{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(1, 0, 0),
			C: math32.NewVector3(0, 0, 1),
		},
	}}
	object.Grid.Width = 1
	object.Grid.Height = 1
	object.Grid.Rectangle = NewRectangle(0, 0, RtNavmeshObjGridTileWidth, RtNavmeshObjGridTileHeight)
	object.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Index: 0,
		Line: LineSegment{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(1, 0, 0),
		},
		SrcCellIndex: 0,
		DstCellIndex: 0,
	}}}
	object.Grid.Tiles = []RtNavmeshObjGridTile{{
		Index:         0,
		Rectangle:     NewRectangle(0, 0, RtNavmeshObjGridTileWidth, RtNavmeshObjGridTileHeight),
		Cells:         append([]RtNavmeshCellTri(nil), object.Cells...),
		InternalEdges: append([]RtNavmeshEdgeInternal(nil), object.InternalEdges...),
	}}
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{
			Object:       object,
			Position:     math32.NewVector3(0, 0, 0),
			Rotation:     math32.NewQuaternion(0, 0, 0, 1),
			Scale:        math32.NewVector3(1, 1, 1),
			LocalToWorld: math32.NewMatrix4().Identity(),
			WorldToLocal: math32.NewMatrix4().Identity(),
		},
		Region: NewRegionFromInt16(1),
	}
	terrain.Objects = []RtNavmeshInstObj{instance}
	terrain.Cells[0].Objects = []RtNavmeshInstObj{instance}

	if err := writeGobAtomically(cachePrefix+"_regions", map[int16]*RtNavmeshTerrain{1: terrain}); err != nil {
		t.Fatalf("write legacy region cache: %v", err)
	}
	if err := writeGobAtomically(cachePrefix+"_objects", map[uint32]RtNavmeshObj{7: object}); err != nil {
		t.Fatalf("write legacy object cache: %v", err)
	}

	loaded := &Loader{}
	if err := loaded.LoadPrecomputedNavmeshDataFromGOB(cachePrefix); err != nil {
		t.Fatalf("LoadPrecomputedNavmeshDataFromGOB() error = %v", err)
	}
	loadedTerrain := loaded.RegionData[1]
	if loadedTerrain.Cells[0].Mesh != loadedTerrain {
		t.Fatal("legacy terrain cell ownership was not rehydrated")
	}
	assertCachedInstanceRuntime(t, &loadedTerrain.Objects[0])
	assertCachedInstanceRuntime(t, &loadedTerrain.Cells[0].Objects[0])
	loadedObject := loaded.ObjectData[7]
	if loadedObject.Grid.object == nil || loadedObject.Cells[0].Mesh == nil {
		t.Fatal("legacy object catalog runtime ownership was not rehydrated")
	}
}

func TestReadRegionCacheRejectsCorruptStreamRecords(t *testing.T) {
	tests := []struct {
		name  string
		write func(*gob.Encoder) error
	}{
		{
			name: "excessive count",
			write: func(encoder *gob.Encoder) error {
				return encoder.Encode(uint32(RegionsTotal + 1))
			},
		},
		{
			name: "duplicate identifier",
			write: func(encoder *gob.Encoder) error {
				terrain := *newTerrainEdgeTestTerrain(1, 1)
				for _, value := range []any{uint32(2), int16(1), terrain, int16(1), terrain} {
					if err := encoder.Encode(value); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			name: "trailing value",
			write: func(encoder *gob.Encoder) error {
				if err := encoder.Encode(uint32(0)); err != nil {
					return err
				}
				return encoder.Encode(uint32(99))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "regions")
			if err := writeCacheTestStream(path, regionCacheStreamMagic, test.write); err != nil {
				t.Fatalf("write cache stream: %v", err)
			}
			if _, err := readRegionCache(path); !errors.Is(err, ErrInvalidNavmeshData) {
				t.Fatalf("readRegionCache() error = %v, want ErrInvalidNavmeshData", err)
			}
		})
	}
}

func TestReadObjectCacheRejectsDuplicateIdentifier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "objects")
	err := writeCacheTestStream(path, objectCacheStreamMagic, func(encoder *gob.Encoder) error {
		object := NewNavmeshObj("duplicate.bms")
		for _, value := range []any{uint32(2), uint32(7), object, uint32(7), object} {
			if err := encoder.Encode(value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write cache stream: %v", err)
	}
	if _, err := readObjectCache(path); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("readObjectCache() error = %v, want ErrInvalidNavmeshData", err)
	}
}

func assertCachedInstanceRuntime(t *testing.T, instance *RtNavmeshInstObj) {
	t.Helper()
	if instance.Mesh != &instance.Object || instance.Object.Grid.object != &instance.Object {
		t.Fatal("cached instance object ownership was not rehydrated")
	}
	if instance.Object.Cells[0].Mesh != &instance.Object {
		t.Fatal("cached instance cell ownership was not rehydrated")
	}
	if instance.Object.InternalEdges[0].Mesh != &instance.Object {
		t.Fatal("cached instance edge ownership was not rehydrated")
	}
	if instance.Object.Grid.Tiles[0].grid.object != &instance.Object || instance.Object.Grid.Tiles[0].grid.Width != 1 {
		t.Fatal("cached instance grid-tile ownership was not rehydrated")
	}
	if instance.Object.Grid.Tiles[0].grid.Tiles != nil {
		t.Fatal("cached grid tile recursively retained the complete grid")
	}
	if len(instance.Object.Grid.Tiles[0].Cells) != 1 || len(instance.Object.Grid.Tiles[0].InternalEdges) != 1 {
		t.Fatal("cached grid tile lost its spatial cell or internal-edge references")
	}
	if instance.Object.Grid.Tiles[0].InternalEdges[0].Mesh != &instance.Object {
		t.Fatal("cached grid-tile edge ownership was not rehydrated")
	}
}

func writeCacheTestStream(path string, magic []byte, write func(*gob.Encoder) error) error {
	return writeFileAtomically(path, func(writer io.Writer) error {
		if _, err := writer.Write(magic); err != nil {
			return err
		}
		return write(gob.NewEncoder(writer))
	})
}
