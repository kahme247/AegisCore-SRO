package navmeshv2

// LoaderStats describes retained navmesh cardinality without estimating Go
// object sizes. Heap profiles remain the authority for byte ownership.
type LoaderStats struct {
	Regions                        int
	ObjectMeshes                   int
	TerrainInstances               int
	TerrainInstancesLinked         int
	TerrainInstancesUnlinked       int
	TerrainObjectEdgeLinkRecords   int
	TerrainObjectEdgeLinks         int
	TerrainObjectEdgeLinkSentinels int
	TerrainCells                   int
	TerrainGlobalEdges             int
	TerrainInternalEdges           int
	TerrainCellObjectReferences    int
	TerrainCellEdgeReferences      int
	ObjectCells                    int
	ObjectGlobalEdges              int
	ObjectInternalEdges            int
	ObjectGridTiles                int
	ObjectGridCellReferences       int
	ObjectGridEdgeReferences       int
}

func (l *Loader) Stats() LoaderStats {
	if l == nil {
		return LoaderStats{}
	}
	stats := LoaderStats{
		Regions:      len(l.RegionData),
		ObjectMeshes: len(l.ObjectData),
	}
	for _, terrain := range l.RegionData {
		if terrain == nil {
			continue
		}
		stats.TerrainInstances += len(terrain.Objects)
		for i := range terrain.Objects {
			effectiveLinks := 0
			for _, link := range terrain.Objects[i].EdgeLinks {
				stats.TerrainObjectEdgeLinkRecords++
				if link.IsUnmapped() {
					stats.TerrainObjectEdgeLinkSentinels++
					continue
				}
				effectiveLinks++
			}
			stats.TerrainObjectEdgeLinks += effectiveLinks
			if effectiveLinks == 0 {
				stats.TerrainInstancesUnlinked++
			} else {
				stats.TerrainInstancesLinked++
			}
		}
		stats.TerrainCells += len(terrain.Cells)
		stats.TerrainGlobalEdges += len(terrain.GlobalEdges)
		stats.TerrainInternalEdges += len(terrain.InternalEdges)
		for i := range terrain.Cells {
			stats.TerrainCellObjectReferences += len(terrain.Cells[i].Objects)
			stats.TerrainCellEdgeReferences += len(terrain.Cells[i].Edges)
		}
	}
	for _, object := range l.ObjectData {
		stats.ObjectCells += len(object.Cells)
		stats.ObjectGlobalEdges += len(object.GlobalEdges)
		stats.ObjectInternalEdges += len(object.InternalEdges)
		stats.ObjectGridTiles += len(object.Grid.Tiles)
		for i := range object.Grid.Tiles {
			tile := &object.Grid.Tiles[i]
			stats.ObjectGridCellReferences += len(tile.Cells)
			stats.ObjectGridEdgeReferences += len(tile.GlobalEdges) + len(tile.InternalEdges)
		}
	}
	return stats
}
