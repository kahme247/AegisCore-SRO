package navmeshv2

import (
	"fmt"
	"sort"
)

func linkTerrainEdges(regions map[int16]*RtNavmeshTerrain, requireDestinations bool) error {
	if err := validateTerrainEdgeTopology(regions, requireDestinations); err != nil {
		return err
	}
	rebuildTerrainEdgeLinks(regions)
	return nil
}

func validateTerrainEdgeTopology(regions map[int16]*RtNavmeshTerrain, requireDestinations bool) error {
	regionIDs := sortedTerrainRegionIDs(regions)
	for _, regionID := range regionIDs {
		terrain := regions[regionID]
		path := terrainValidationPath(regionID, terrain)
		if terrain == nil {
			return invalidTerrainEdgeTopology(path, "terrain is nil")
		}
		if len(terrain.Cells) == 0 {
			return invalidTerrainEdgeTopology(path, "terrain contains no cells")
		}
		for cellIndex := range terrain.Cells {
			if terrain.Cells[cellIndex].Index != cellIndex {
				return invalidTerrainEdgeTopology(path, "cell %d stores index %d", cellIndex, terrain.Cells[cellIndex].Index)
			}
		}
		for tileIndex := range terrain.TileMap {
			cellIndex := terrain.TileMap[tileIndex].CellIndex
			if cellIndex < 0 || cellIndex >= len(terrain.Cells) {
				return invalidTerrainEdgeTopology(path, "tile %d references cell %d with %d cells", tileIndex, cellIndex, len(terrain.Cells))
			}
		}
		for edgeIndex := range terrain.InternalEdges {
			edge := &terrain.InternalEdges[edgeIndex]
			if edge.Index != edgeIndex {
				return invalidTerrainEdgeTopology(path, "internal edge %d stores index %d", edgeIndex, edge.Index)
			}
			if !validLineSegment(edge.Line) {
				return invalidTerrainEdgeTopology(path, "internal edge %d has invalid line geometry", edgeIndex)
			}
			if !validTerrainCellEndpoint(edge.SrcCellIndex, edge.SrcDirection, len(terrain.Cells)) {
				return invalidTerrainEdgeTopology(
					path,
					"internal edge %d has invalid source cell/direction %d/%d",
					edgeIndex,
					edge.SrcCellIndex,
					edge.SrcDirection,
				)
			}
			if !edge.Flag.IsBlocked() && !validTerrainCellEndpoint(edge.DstCellIndex, edge.DstDirection, len(terrain.Cells)) {
				return invalidTerrainEdgeTopology(
					path,
					"internal edge %d has invalid destination cell/direction %d/%d",
					edgeIndex,
					edge.DstCellIndex,
					edge.DstDirection,
				)
			}
		}
		for edgeIndex := range terrain.GlobalEdges {
			edge := &terrain.GlobalEdges[edgeIndex]
			if edge.Index != edgeIndex {
				return invalidTerrainEdgeTopology(path, "global edge %d stores index %d", edgeIndex, edge.Index)
			}
			if !validLineSegment(edge.Line) {
				return invalidTerrainEdgeTopology(path, "global edge %d has invalid line geometry", edgeIndex)
			}
			if edge.SrcMeshIndex != int(uint16(regionID)) {
				return invalidTerrainEdgeTopology(
					path,
					"global edge %d source region %04x does not match owner %04x",
					edgeIndex,
					uint16(edge.SrcMeshIndex),
					uint16(regionID),
				)
			}
			if !validTerrainCellEndpoint(edge.SrcCellIndex, edge.SrcDirection, len(terrain.Cells)) {
				return invalidTerrainEdgeTopology(
					path,
					"global edge %d has invalid source cell/direction %d/%d",
					edgeIndex,
					edge.SrcCellIndex,
					edge.SrcDirection,
				)
			}
			if edge.DstMeshIndex <= 0 {
				continue
			}
			destination, exists := regions[int16(uint16(edge.DstMeshIndex))]
			if !exists || destination == nil {
				if requireDestinations {
					return invalidTerrainEdgeTopology(path, "global edge %d destination region %04x is missing", edgeIndex, uint16(edge.DstMeshIndex))
				}
				continue
			}
			if !validTerrainCellEndpoint(edge.DstCellIndex, edge.DstDirection, len(destination.Cells)) {
				return invalidTerrainEdgeTopology(
					path,
					"global edge %d has invalid destination %04x cell/direction %d/%d",
					edgeIndex,
					uint16(edge.DstMeshIndex),
					edge.DstCellIndex,
					edge.DstDirection,
				)
			}
		}
	}
	return nil
}

func rebuildTerrainEdgeLinks(regions map[int16]*RtNavmeshTerrain) {
	regionIDs := sortedTerrainRegionIDs(regions)
	for _, regionID := range regionIDs {
		terrain := regions[regionID]
		for cellIndex := range terrain.Cells {
			cell := &terrain.Cells[cellIndex]
			cell.Mesh = terrain
			clear(cell.Edges)
			cell.Edges = cell.Edges[:0]
		}
	}
	for _, regionID := range regionIDs {
		terrain := regions[regionID]
		for edgeIndex := range terrain.InternalEdges {
			edge := &terrain.InternalEdges[edgeIndex]
			edge.Mesh = terrain
			edge.SrcCell = &terrain.Cells[edge.SrcCellIndex]
			edge.DstCell = nil
			addTerrainCellEdge(&terrain.Cells[edge.SrcCellIndex], edge)
			if !edge.Flag.IsBlocked() {
				edge.DstCell = &terrain.Cells[edge.DstCellIndex]
				addTerrainCellEdge(&terrain.Cells[edge.DstCellIndex], edge)
			}
		}
		for edgeIndex := range terrain.GlobalEdges {
			edge := &terrain.GlobalEdges[edgeIndex]
			edge.Mesh = terrain
			edge.SrcCell = &terrain.Cells[edge.SrcCellIndex]
			edge.DstCell = nil
			addTerrainCellEdge(&terrain.Cells[edge.SrcCellIndex], edge)
			if edge.Flag.IsBlocked() || edge.DstMeshIndex <= 0 {
				continue
			}
			destination := regions[int16(uint16(edge.DstMeshIndex))]
			if destination == nil {
				continue
			}
			edge.DstCell = &destination.Cells[edge.DstCellIndex]
			addTerrainCellEdge(&destination.Cells[edge.DstCellIndex], edge)
		}
	}
}

func addTerrainCellEdge(cell *RtNavmeshCellQuad, edge RtNavmeshEdge) {
	for _, existing := range cell.Edges {
		if existing == edge {
			return
		}
	}
	cell.Edges = append(cell.Edges, edge)
}

func validTerrainCellEndpoint(cellIndex int, direction RtNavmeshEdgeDirection, cellCount int) bool {
	return cellIndex >= 0 && cellIndex < cellCount && direction >= 0 && direction <= 3
}

func sortedTerrainRegionIDs(regions map[int16]*RtNavmeshTerrain) []int16 {
	regionIDs := make([]int16, 0, len(regions))
	for regionID := range regions {
		regionIDs = append(regionIDs, regionID)
	}
	sort.Slice(regionIDs, func(i, j int) bool { return uint16(regionIDs[i]) < uint16(regionIDs[j]) })
	return regionIDs
}

func terrainValidationPath(regionID int16, terrain *RtNavmeshTerrain) string {
	if terrain != nil && terrain.Filename != "" {
		return terrain.Filename
	}
	return fmt.Sprintf("region %04x", uint16(regionID))
}

func invalidTerrainEdgeTopology(path, format string, args ...any) error {
	return &NavmeshLoadError{
		Operation: "validate terrain edge topology",
		Path:      path,
		Err:       fmt.Errorf("%w: %s", ErrInvalidNavmeshData, fmt.Sprintf(format, args...)),
	}
}
