package navmeshv2

import "testing"

func TestLoaderStatsCountsRetainedRelationships(t *testing.T) {
	loader := &Loader{
		RegionData: map[int16]*RtNavmeshTerrain{
			1: {
				Objects: []RtNavmeshInstObj{
					{EdgeLinks: []RtNavmeshEdgeLink{{}, {LinkedObjID: -1, LinkedObjEdgeID: -1, EdgeID: -1}}},
					{},
				},
				GlobalEdges:   make([]RtNavmeshEdgeGlobal, 3),
				InternalEdges: make([]RtNavmeshEdgeInternal, 4),
				Cells: []RtNavmeshCellQuad{
					{Objects: make([]RtNavmeshInstObj, 5), Edges: make([]RtNavmeshEdge, 6)},
					{Objects: make([]RtNavmeshInstObj, 7), Edges: make([]RtNavmeshEdge, 8)},
				},
			},
			2: nil,
		},
		ObjectData: map[uint32]RtNavmeshObj{
			10: {
				Cells:         make([]RtNavmeshCellTri, 9),
				GlobalEdges:   make([]RtNavmeshEdgeGlobal, 10),
				InternalEdges: make([]RtNavmeshEdgeInternal, 11),
				Grid: RtNavmeshObjGrid{Tiles: []RtNavmeshObjGridTile{
					{Cells: make([]RtNavmeshCellTri, 12), GlobalEdges: make([]RtNavmeshEdgeGlobal, 13), InternalEdges: make([]RtNavmeshEdgeInternal, 14)},
				}},
			},
		},
	}

	got := loader.Stats()
	want := LoaderStats{
		Regions:                        2,
		ObjectMeshes:                   1,
		TerrainInstances:               2,
		TerrainInstancesLinked:         1,
		TerrainInstancesUnlinked:       1,
		TerrainObjectEdgeLinkRecords:   2,
		TerrainObjectEdgeLinks:         1,
		TerrainObjectEdgeLinkSentinels: 1,
		TerrainCells:                   2,
		TerrainGlobalEdges:             3,
		TerrainInternalEdges:           4,
		TerrainCellObjectReferences:    12,
		TerrainCellEdgeReferences:      14,
		ObjectCells:                    9,
		ObjectGlobalEdges:              10,
		ObjectInternalEdges:            11,
		ObjectGridTiles:                1,
		ObjectGridCellReferences:       12,
		ObjectGridEdgeReferences:       27,
	}
	if got != want {
		t.Fatalf("Stats() = %#v, want %#v", got, want)
	}
}

func TestNilLoaderStatsIsEmpty(t *testing.T) {
	var loader *Loader
	if got := loader.Stats(); got != (LoaderStats{}) {
		t.Fatalf("Stats() = %#v, want empty", got)
	}
}
