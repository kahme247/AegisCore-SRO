package navmeshv2

import (
	"errors"
	"testing"

	"github.com/g3n/engine/math32"
)

func TestLinkTerrainEdgesBuildsDeterministicCellRelationships(t *testing.T) {
	first := newTerrainEdgeTestTerrain(1, 2)
	second := newTerrainEdgeTestTerrain(2, 1)
	first.InternalEdges = []RtNavmeshEdgeInternal{
		{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			Index: 0, Flag: RtNavmeshEdgeFlag(1 << 2),
			SrcCellIndex: 0, DstCellIndex: 1,
			SrcDirection: 0, DstDirection: 2,
			Line: LineSegment{
				A: math32.NewVector3(20, 0, -1),
				B: math32.NewVector3(20, 0, 1),
			},
		}},
		{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			Index: 1, Flag: RtNavmeshEdgeFlag((1 << 1) | (1 << 2)),
			SrcCellIndex: 1, DstCellIndex: 65535,
			SrcDirection: 1, DstDirection: -1,
			Line: LineSegment{
				A: math32.NewVector3(5, 0, -1),
				B: math32.NewVector3(5, 0, 1),
			},
		}},
	}
	first.GlobalEdges = []RtNavmeshEdgeGlobal{{
		RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			Index: 0, Flag: RtNavmeshEdgeFlag(1 << 3),
			SrcCellIndex: 0, DstCellIndex: 0,
			SrcDirection: 3, DstDirection: 1,
			Line: LineSegment{
				A: math32.NewVector3(30, 0, -1),
				B: math32.NewVector3(30, 0, 1),
			},
		},
		SrcMeshIndex: 1,
		DstMeshIndex: 2,
	}}
	regions := map[int16]*RtNavmeshTerrain{2: second, 1: first}

	if err := linkTerrainEdges(regions, true); err != nil {
		t.Fatalf("linkTerrainEdges() error = %v", err)
	}
	assertTerrainEdgeLinkCounts(t, first, second)
	if first.InternalEdges[0].SrcCell != &first.Cells[0] || first.InternalEdges[0].DstCell != &first.Cells[1] {
		t.Fatal("unblocked internal edge did not resolve both cells")
	}
	if first.InternalEdges[1].SrcCell != &first.Cells[1] || first.InternalEdges[1].DstCell != nil {
		t.Fatal("blocked internal edge did not retain source-only linkage")
	}
	if first.GlobalEdges[0].SrcCell != &first.Cells[0] || first.GlobalEdges[0].DstCell != &second.Cells[0] {
		t.Fatal("global edge did not resolve its cross-region cells")
	}
	if first.Cells[0].Mesh != first || second.Cells[0].Mesh != second {
		t.Fatal("cell mesh ownership was not rebound to authoritative terrains")
	}
	current := RtNavmeshPosition{Region: first.Region, Offset: math32.NewVector3(0, 0, 0)}
	next := RtNavmeshPosition{Region: first.Region, Offset: math32.NewVector3(10, 0, 0)}
	collides, collision := FindTerrainCollisions(current, next, first.Cells[1], first.Cells[0])
	if !collides || collision.ContactEdge != &first.InternalEdges[1] {
		t.Fatalf("linked terrain collision = collides:%t edge:%v, want blocked internal edge", collides, collision.ContactEdge)
	}

	if err := linkTerrainEdges(regions, true); err != nil {
		t.Fatalf("second linkTerrainEdges() error = %v", err)
	}
	assertTerrainEdgeLinkCounts(t, first, second)
}

func TestLinkTerrainEdgesAllowsMissingDestinationForPartialViewerSet(t *testing.T) {
	terrain := newTerrainEdgeTestTerrain(1, 1)
	terrain.GlobalEdges = []RtNavmeshEdgeGlobal{{
		RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			Index: 0, SrcCellIndex: 0, DstCellIndex: 0,
			SrcDirection: 0, DstDirection: 2,
			Line: LineSegment{
				A: math32.NewVector3(0, 0, 0),
				B: math32.NewVector3(1, 0, 0),
			},
		},
		SrcMeshIndex: 1,
		DstMeshIndex: 2,
	}}

	if err := linkTerrainEdges(map[int16]*RtNavmeshTerrain{1: terrain}, false); err != nil {
		t.Fatalf("linkTerrainEdges() error = %v", err)
	}
	if len(terrain.Cells[0].Edges) != 1 || terrain.GlobalEdges[0].SrcCell != &terrain.Cells[0] || terrain.GlobalEdges[0].DstCell != nil {
		t.Fatalf("partial global linkage = edges:%d src:%p dst:%p", len(terrain.Cells[0].Edges), terrain.GlobalEdges[0].SrcCell, terrain.GlobalEdges[0].DstCell)
	}
}

func TestValidateTerrainEdgeTopologyRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name    string
		regions func() map[int16]*RtNavmeshTerrain
	}{
		{
			name: "empty terrain",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := NewRtNavmeshTerrain("empty.nvm", NewRegionFromInt16(1))
				return map[int16]*RtNavmeshTerrain{1: &terrain}
			},
		},
		{
			name: "invalid line geometry",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
					Index: 0, SrcCellIndex: 0, SrcDirection: 0,
					Line: LineSegment{A: math32.NewVector3(0, 0, 0)},
				}}}
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
		{
			name: "invalid cell index",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.Cells[0].Index = 2
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
		{
			name: "invalid tile cell",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.TileMap[4].CellIndex = 1
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
		{
			name: "invalid internal source",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
					Index: 0, SrcCellIndex: 1, SrcDirection: 0,
				}}}
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
		{
			name: "invalid internal destination",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
					Index: 0, SrcCellIndex: 0, SrcDirection: 0, DstCellIndex: 1, DstDirection: 2,
				}}}
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
		{
			name: "global owner mismatch",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.GlobalEdges = []RtNavmeshEdgeGlobal{{
					RtNavmeshEdgeBase: RtNavmeshEdgeBase{Index: 0, SrcCellIndex: 0, SrcDirection: 0},
					SrcMeshIndex:      2,
				}}
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
		{
			name: "missing global destination",
			regions: func() map[int16]*RtNavmeshTerrain {
				terrain := newTerrainEdgeTestTerrain(1, 1)
				terrain.GlobalEdges = []RtNavmeshEdgeGlobal{{
					RtNavmeshEdgeBase: RtNavmeshEdgeBase{
						Index: 0, SrcCellIndex: 0, DstCellIndex: 0,
						SrcDirection: 0, DstDirection: 2,
					},
					SrcMeshIndex: 1,
					DstMeshIndex: 2,
				}}
				return map[int16]*RtNavmeshTerrain{1: terrain}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTerrainEdgeTopology(test.regions(), true)
			if !errors.Is(err, ErrInvalidNavmeshData) {
				t.Fatalf("validateTerrainEdgeTopology() error = %v, want ErrInvalidNavmeshData", err)
			}
			var loadErr *NavmeshLoadError
			if !errors.As(err, &loadErr) {
				t.Fatalf("validateTerrainEdgeTopology() error type = %T, want *NavmeshLoadError", err)
			}
		})
	}
}

func newTerrainEdgeTestTerrain(regionID int16, cellCount int) *RtNavmeshTerrain {
	terrain := NewRtNavmeshTerrain("test.nvm", NewRegionFromInt16(regionID))
	terrain.Cells = make([]RtNavmeshCellQuad, cellCount)
	for cellIndex := range terrain.Cells {
		terrain.Cells[cellIndex].Index = cellIndex
	}
	return &terrain
}

func assertTerrainEdgeLinkCounts(t *testing.T, first, second *RtNavmeshTerrain) {
	t.Helper()
	if got := len(first.Cells[0].Edges); got != 2 {
		t.Fatalf("first cell 0 edge count = %d, want 2", got)
	}
	if got := len(first.Cells[1].Edges); got != 2 {
		t.Fatalf("first cell 1 edge count = %d, want 2", got)
	}
	if got := len(second.Cells[0].Edges); got != 1 {
		t.Fatalf("second cell 0 edge count = %d, want 1", got)
	}
}
