package navmeshv2

import (
	"bytes"
	"errors"
	"testing"

	"github.com/g3n/engine/math32"
)

func TestResolveCellRejectsInvalidRuntimeInputs(t *testing.T) {
	position := math32.NewVector3(1, 0, 1)
	if _, err := (*RtNavmeshTerrain)(nil).ResolveCell(position); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("nil terrain ResolveCell() error = %v, want ErrInvalidNavmeshData", err)
	}
	terrain := NewRtNavmeshTerrain("invalid.nvm", NewRegionFromInt16(1))
	if _, err := terrain.ResolveCell(nil); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("nil position ResolveCell() error = %v, want ErrInvalidNavmeshData", err)
	}
	terrain.TileMap[0].CellIndex = 4
	if _, err := terrain.ResolveCell(position); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("invalid tile ResolveCell() error = %v, want ErrInvalidNavmeshData", err)
	}
}

func TestResolveHeightClampsPointsOutsideTerrain(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})
	terrain.HeightMap[0] = 11
	terrain.HeightMap[VerticesTotal-1] = 22

	if got := terrain.ResolveHeight(math32.NewVector3(-TileWidth, 0, -TileHeight)); got != 11 {
		t.Fatalf("negative boundary height = %v, want 11", got)
	}
	if got := terrain.ResolveHeight(math32.NewVector3(TerrainWidth+TileWidth, 0, TerrainHeight+TileHeight)); got != 22 {
		t.Fatalf("positive boundary height = %v, want 22", got)
	}
}

func TestResolveHeightInterpolatesAdjacentVertices(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})
	terrain.HeightMap[0] = 0
	terrain.HeightMap[1] = 20
	terrain.HeightMap[VerticesY] = 10
	terrain.HeightMap[VerticesY+1] = 30

	got := terrain.ResolveHeight(math32.NewVector3(TileWidth/2, 0, TileHeight/2))
	if got != 15 {
		t.Fatalf("interpolated height = %v, want 15", got)
	}
}

func TestResolveCellRejectsExclusiveUpperBoundary(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})

	for _, pos := range []*math32.Vector3{
		math32.NewVector3(TerrainWidth, 0, 0),
		math32.NewVector3(0, 0, TerrainHeight),
	} {
		if _, err := terrain.ResolveCell(pos); err == nil {
			t.Fatalf("ResolveCell(%v) succeeded at exclusive terrain boundary", pos)
		}
	}
}

func TestDetectUnmappedObjectEdgesIsPureAndExportIsRepeatable(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})
	terrain.Cells = []RtNavmeshCellQuad{{
		RtNavmeshCellBase: RtNavmeshCellBase{Index: 0, Mesh: &terrain},
		Rect:              NewRectangle(0, 0, TerrainWidth, TerrainHeight),
	}}
	object := NewNavmeshObj("test.bms")
	object.GlobalEdges = []RtNavmeshEdgeGlobal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Index:        4,
		Line:         LineSegment{A: math32.NewVector3(10, 0, 10), B: math32.NewVector3(20, 0, 10)},
		SrcCellIndex: 0,
		DstCellIndex: -1,
	}}}
	terrain.Objects = []RtNavmeshInstObj{{
		RtNavmeshInstBase: RtNavmeshInstBase{
			Object:       object,
			Position:     math32.NewVector3(0, 0, 0),
			Rotation:     math32.NewQuaternion(0, 0, 0, 1),
			LocalToWorld: math32.NewMatrix4().Identity(),
		},
		Region: Region{},
	}}

	derived := terrain.DetectUnmappedObjectEdges()
	if len(derived) != 1 {
		t.Fatalf("derived edges = %d, want 1", len(derived))
	}
	if derived[0].SrcCell != &terrain.Cells[0] {
		t.Fatal("derived edge does not reference the terrain-owned source cell")
	}
	if len(terrain.GlobalEdges) != 0 || len(terrain.Cells[0].Objects) != 0 {
		t.Fatalf("detection mutated terrain: edges=%d objects=%d", len(terrain.GlobalEdges), len(terrain.Cells[0].Objects))
	}

	first := terrain.ToJson()
	second := terrain.ToJson()
	if !bytes.Equal(first, second) {
		t.Fatal("repeated terrain export produced different output")
	}
	if len(terrain.GlobalEdges) != 0 || len(terrain.Cells[0].Objects) != 0 {
		t.Fatalf("export mutated terrain: edges=%d objects=%d", len(terrain.GlobalEdges), len(terrain.Cells[0].Objects))
	}
}
