package navmeshv2

import (
	"fmt"
	"sort"

	"github.com/g3n/engine/math32"
)

func validateCachedObjectData(objects map[uint32]RtNavmeshObj) error {
	objectIDs := make([]uint32, 0, len(objects))
	for objectID := range objects {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Slice(objectIDs, func(i, j int) bool { return objectIDs[i] < objectIDs[j] })
	for _, objectID := range objectIDs {
		object := objects[objectID]
		path := object.Filename
		if path == "" {
			path = fmt.Sprintf("object %d", objectID)
		}
		if err := validateObjectTopology(&object, path); err != nil {
			return err
		}
	}
	return nil
}

func validateTerrainInstanceTopology(instances []RtNavmeshInstObj, path string) error {
	for instanceIndex := range instances {
		instance := &instances[instanceIndex]
		if !validInstanceTransform(instance) {
			return invalidObjectTopology(path, "instance %d has an invalid transform", instanceIndex)
		}
		if !validRectangle(instance.Object.Grid.Rectangle) {
			return invalidObjectTopology(path, "instance %d has an invalid object grid rectangle", instanceIndex)
		}
		objectPath := instance.Object.Filename
		if objectPath == "" {
			objectPath = fmt.Sprintf("%s instance %d", path, instanceIndex)
		}
		if err := validateObjectTopology(&instance.Object, objectPath); err != nil {
			return err
		}
	}
	return nil
}

func validateTerrainCellObjectReferences(terrain *RtNavmeshTerrain, path string) error {
	if terrain == nil {
		return invalidObjectTopology(path, "terrain is nil")
	}
	instancesByWorldID := make(map[int]*RtNavmeshInstObj, len(terrain.Objects))
	for instanceIndex := range terrain.Objects {
		instance := &terrain.Objects[instanceIndex]
		if _, exists := instancesByWorldID[instance.WorldID]; exists {
			return invalidObjectTopology(path, "duplicate terrain object world ID %d", instance.WorldID)
		}
		instancesByWorldID[instance.WorldID] = instance
	}
	for cellIndex := range terrain.Cells {
		for referenceIndex := range terrain.Cells[cellIndex].Objects {
			reference := &terrain.Cells[cellIndex].Objects[referenceIndex]
			authoritative, exists := instancesByWorldID[reference.WorldID]
			if !exists {
				return invalidObjectTopology(path, "cell %d references unknown object world ID %d", cellIndex, reference.WorldID)
			}
			if reference.ID != authoritative.ID || reference.Region.ID != authoritative.Region.ID ||
				reference.Object.Filename != authoritative.Object.Filename ||
				len(reference.Object.Cells) != len(authoritative.Object.Cells) ||
				len(reference.Object.GlobalEdges) != len(authoritative.Object.GlobalEdges) ||
				len(reference.Object.InternalEdges) != len(authoritative.Object.InternalEdges) ||
				!validInstanceTransform(reference) {
				return invalidObjectTopology(path, "cell %d has inconsistent object reference %d", cellIndex, referenceIndex)
			}
		}
	}
	return nil
}

func validateObjectTopology(object *RtNavmeshObj, path string) error {
	if object == nil {
		return invalidObjectTopology(path, "object is nil")
	}
	for cellIndex := range object.Cells {
		cell := &object.Cells[cellIndex]
		if cell.Index != cellIndex {
			return invalidObjectTopology(path, "cell %d stores index %d", cellIndex, cell.Index)
		}
		if !validTriangle(cell.Triangle) {
			return invalidObjectTopology(path, "cell %d has invalid triangle geometry", cellIndex)
		}
	}
	for edgeIndex := range object.GlobalEdges {
		edge := &object.GlobalEdges[edgeIndex]
		if edge.Index != edgeIndex {
			return invalidObjectTopology(path, "global edge %d stores index %d", edgeIndex, edge.Index)
		}
		if !validLineSegment(edge.Line) {
			return invalidObjectTopology(path, "global edge %d has invalid line geometry", edgeIndex)
		}
		if !validObjectCellIndex(edge.SrcCellIndex, len(object.Cells)) {
			return invalidObjectTopology(path, "global edge %d has invalid source cell %d", edgeIndex, edge.SrcCellIndex)
		}
		if !objectCellIndexIsSentinel(edge.DstCellIndex) && !validObjectCellIndex(edge.DstCellIndex, len(object.Cells)) {
			return invalidObjectTopology(path, "global edge %d has invalid destination cell %d", edgeIndex, edge.DstCellIndex)
		}
	}
	for edgeIndex := range object.InternalEdges {
		edge := &object.InternalEdges[edgeIndex]
		if edge.Index != edgeIndex {
			return invalidObjectTopology(path, "internal edge %d stores index %d", edgeIndex, edge.Index)
		}
		if !validLineSegment(edge.Line) {
			return invalidObjectTopology(path, "internal edge %d has invalid line geometry", edgeIndex)
		}
		if !validObjectCellIndex(edge.SrcCellIndex, len(object.Cells)) || !validObjectCellIndex(edge.DstCellIndex, len(object.Cells)) {
			return invalidObjectTopology(
				path,
				"internal edge %d has invalid source/destination cells %d/%d",
				edgeIndex,
				edge.SrcCellIndex,
				edge.DstCellIndex,
			)
		}
	}
	if err := validateObjectGridTopology(object, path); err != nil {
		return err
	}
	return nil
}

func validateObjectGridTopology(object *RtNavmeshObj, path string) error {
	grid := &object.Grid
	if grid.Width < 0 || grid.Height < 0 {
		return invalidObjectTopology(path, "grid has negative dimensions %dx%d", grid.Width, grid.Height)
	}
	expectedTileCount := uint64(grid.Width) * uint64(grid.Height)
	if expectedTileCount != uint64(len(grid.Tiles)) {
		return invalidObjectTopology(path, "grid dimensions %dx%d do not match %d tiles", grid.Width, grid.Height, len(grid.Tiles))
	}
	if grid.Width == 0 && grid.Height == 0 && len(grid.Tiles) == 0 && grid.Rectangle.Min == nil && grid.Rectangle.Max == nil {
		return nil
	}
	if !validRectangle(grid.Rectangle) {
		return invalidObjectTopology(path, "grid has invalid rectangle geometry")
	}
	for tileIndex := range grid.Tiles {
		tile := &grid.Tiles[tileIndex]
		if tile.Index != tileIndex {
			return invalidObjectTopology(path, "grid tile %d stores index %d", tileIndex, tile.Index)
		}
		if grid.Width == 0 || tile.X != tileIndex%grid.Width || tile.Y != tileIndex/grid.Width {
			return invalidObjectTopology(path, "grid tile %d has invalid coordinates %d/%d", tileIndex, tile.X, tile.Y)
		}
		if !validRectangle(tile.Rectangle) {
			return invalidObjectTopology(path, "grid tile %d has invalid rectangle geometry", tileIndex)
		}
		for referenceIndex := range tile.Cells {
			cellIndex := tile.Cells[referenceIndex].Index
			if !validObjectCellIndex(cellIndex, len(object.Cells)) {
				return invalidObjectTopology(path, "grid tile %d references invalid cell %d", tileIndex, cellIndex)
			}
		}
		for referenceIndex := range tile.GlobalEdges {
			edgeIndex := tile.GlobalEdges[referenceIndex].Index
			if edgeIndex < 0 || edgeIndex >= len(object.GlobalEdges) {
				return invalidObjectTopology(path, "grid tile %d references invalid global edge %d", tileIndex, edgeIndex)
			}
		}
		for referenceIndex := range tile.InternalEdges {
			edgeIndex := tile.InternalEdges[referenceIndex].Index
			if edgeIndex < 0 || edgeIndex >= len(object.InternalEdges) {
				return invalidObjectTopology(path, "grid tile %d references invalid internal edge %d", tileIndex, edgeIndex)
			}
		}
	}
	return nil
}

func validInstanceTransform(instance *RtNavmeshInstObj) bool {
	if instance == nil || !validVector3(instance.Position) || instance.Rotation == nil || !validVector3(instance.Scale) ||
		instance.LocalToWorld == nil || instance.WorldToLocal == nil {
		return false
	}
	if !finiteFloat32(instance.Rotation.X) || !finiteFloat32(instance.Rotation.Y) ||
		!finiteFloat32(instance.Rotation.Z) || !finiteFloat32(instance.Rotation.W) {
		return false
	}
	for _, matrix := range []*math32.Matrix4{instance.LocalToWorld, instance.WorldToLocal} {
		for _, value := range matrix {
			if !finiteFloat32(value) {
				return false
			}
		}
	}
	return true
}

func validObjectCellIndex(index, cellCount int) bool {
	return index >= 0 && index < cellCount
}

func objectCellIndexIsSentinel(index int) bool {
	return index == -1 || index == int(^uint16(0))
}

func invalidObjectTopology(path, format string, args ...any) error {
	return &NavmeshLoadError{
		Operation: "validate object topology",
		Path:      path,
		Err:       fmt.Errorf("%w: %s", ErrInvalidNavmeshData, fmt.Sprintf(format, args...)),
	}
}
