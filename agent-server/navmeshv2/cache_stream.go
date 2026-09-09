package navmeshv2

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

var regionCacheStreamMagic = []byte("AEGIS-NAV-REGION-1\n")
var objectCacheStreamMagic = []byte("AEGIS-NAV-OBJECT-1\n")

func writeRegionCacheAtomically(path string, regions map[int16]*RtNavmeshTerrain) error {
	return writeFileAtomically(path, func(writer io.Writer) error {
		if _, err := writer.Write(regionCacheStreamMagic); err != nil {
			return err
		}
		encoder := gob.NewEncoder(writer)
		regionIDs := sortedTerrainRegionIDs(regions)
		if err := encoder.Encode(uint32(len(regionIDs))); err != nil {
			return err
		}
		for _, regionID := range regionIDs {
			if err := encoder.Encode(regionID); err != nil {
				return err
			}
			terrain := cloneTerrainForCache(regions[regionID])
			if err := encoder.Encode(terrain); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeObjectCacheAtomically(path string, objects map[uint32]RtNavmeshObj) error {
	return writeFileAtomically(path, func(writer io.Writer) error {
		if _, err := writer.Write(objectCacheStreamMagic); err != nil {
			return err
		}
		encoder := gob.NewEncoder(writer)
		objectIDs := make([]uint32, 0, len(objects))
		for objectID := range objects {
			objectIDs = append(objectIDs, objectID)
		}
		sort.Slice(objectIDs, func(i, j int) bool { return objectIDs[i] < objectIDs[j] })
		if err := encoder.Encode(uint32(len(objectIDs))); err != nil {
			return err
		}
		for _, objectID := range objectIDs {
			if err := encoder.Encode(objectID); err != nil {
				return err
			}
			object := cloneObjectForCache(objects[objectID])
			if err := encoder.Encode(object); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeFileAtomically(path string, write func(io.Writer) error) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".navmesh-cache-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := write(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func readRegionCache(path string) (map[int16]*RtNavmeshTerrain, error) {
	file, stream, err := openCacheStream(path, regionCacheStreamMagic)
	if err != nil {
		return nil, err
	}
	if !stream {
		regions := make(map[int16]*RtNavmeshTerrain)
		if err := readGob(path, &regions); err != nil {
			return nil, err
		}
		return regions, nil
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	var count uint32
	if err := decodeCacheStreamValue(decoder, &count, "region count"); err != nil {
		return nil, err
	}
	if err := validateCacheStreamCount(file, count, RegionsTotal, "regions"); err != nil {
		return nil, err
	}
	regions := make(map[int16]*RtNavmeshTerrain, int(count))
	for i := uint32(0); i < count; i++ {
		var regionID int16
		var terrain RtNavmeshTerrain
		if err := decodeCacheStreamValue(decoder, &regionID, fmt.Sprintf("region %d identifier", i)); err != nil {
			return nil, err
		}
		if err := decodeCacheStreamValue(decoder, &terrain, fmt.Sprintf("region %d terrain", i)); err != nil {
			return nil, err
		}
		if _, exists := regions[regionID]; exists {
			return nil, fmt.Errorf("%w: duplicate region %04x", ErrInvalidNavmeshData, uint16(regionID))
		}
		regions[regionID] = &terrain
	}
	if err := requireCacheStreamEOF(decoder); err != nil {
		return nil, err
	}
	return regions, nil
}

func readObjectCache(path string) (map[uint32]RtNavmeshObj, error) {
	file, stream, err := openCacheStream(path, objectCacheStreamMagic)
	if err != nil {
		return nil, err
	}
	if !stream {
		objects := make(map[uint32]RtNavmeshObj)
		if err := readGob(path, &objects); err != nil {
			return nil, err
		}
		return objects, nil
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	var count uint32
	if err := decodeCacheStreamValue(decoder, &count, "object count"); err != nil {
		return nil, err
	}
	if err := validateCacheStreamCount(file, count, 1<<24, "objects"); err != nil {
		return nil, err
	}
	objects := make(map[uint32]RtNavmeshObj, int(count))
	for i := uint32(0); i < count; i++ {
		var objectID uint32
		var object RtNavmeshObj
		if err := decodeCacheStreamValue(decoder, &objectID, fmt.Sprintf("object %d identifier", i)); err != nil {
			return nil, err
		}
		if err := decodeCacheStreamValue(decoder, &object, fmt.Sprintf("object %d navmesh", i)); err != nil {
			return nil, err
		}
		if _, exists := objects[objectID]; exists {
			return nil, fmt.Errorf("%w: duplicate object %d", ErrInvalidNavmeshData, objectID)
		}
		objects[objectID] = object
	}
	if err := requireCacheStreamEOF(decoder); err != nil {
		return nil, err
	}
	return objects, nil
}

func openCacheStream(path string, magic []byte) (*os.File, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	header := make([]byte, len(magic))
	if _, err := io.ReadFull(file, header); err != nil {
		_ = file.Close()
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !bytes.Equal(header, magic) {
		_ = file.Close()
		return nil, false, nil
	}
	return file, true, nil
}

func validateCacheStreamCount(file *os.File, count uint32, maximum uint64, kind string) error {
	if uint64(count) > maximum {
		return fmt.Errorf("%w: %s count %d exceeds maximum %d", ErrInvalidNavmeshData, kind, count, maximum)
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 0 || uint64(count) > uint64(info.Size()) {
		return fmt.Errorf("%w: %s count %d exceeds cache size %d", ErrInvalidNavmeshData, kind, count, info.Size())
	}
	return nil
}

func requireCacheStreamEOF(decoder *gob.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("%w: cache contains invalid trailing data: %v", ErrInvalidNavmeshData, err)
	}
	return fmt.Errorf("%w: cache contains trailing value", ErrInvalidNavmeshData)
}

func decodeCacheStreamValue(decoder *gob.Decoder, target any, description string) error {
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode %s: %v", ErrInvalidNavmeshData, description, err)
	}
	return nil
}

func cloneTerrainForCache(source *RtNavmeshTerrain) RtNavmeshTerrain {
	clone := *source
	clone.Objects = make([]RtNavmeshInstObj, len(source.Objects))
	for i := range source.Objects {
		clone.Objects[i] = cloneInstanceForCache(source.Objects[i])
	}
	clone.Cells = make([]RtNavmeshCellQuad, len(source.Cells))
	for i := range source.Cells {
		clone.Cells[i] = source.Cells[i]
		clone.Cells[i].Mesh = nil
		clone.Cells[i].Edges = nil
		clone.Cells[i].Objects = make([]RtNavmeshInstObj, len(source.Cells[i].Objects))
		for j := range source.Cells[i].Objects {
			clone.Cells[i].Objects[j] = cloneInstanceForCache(source.Cells[i].Objects[j])
		}
	}
	clone.GlobalEdges = append([]RtNavmeshEdgeGlobal(nil), source.GlobalEdges...)
	for i := range clone.GlobalEdges {
		clearEdgeRuntime(&clone.GlobalEdges[i].RtNavmeshEdgeBase)
	}
	clone.InternalEdges = append([]RtNavmeshEdgeInternal(nil), source.InternalEdges...)
	for i := range clone.InternalEdges {
		clearEdgeRuntime(&clone.InternalEdges[i].RtNavmeshEdgeBase)
	}
	return clone
}

func cloneInstanceForCache(source RtNavmeshInstObj) RtNavmeshInstObj {
	clone := source
	clone.Mesh = nil
	clone.Object = cloneObjectForCache(source.Object)
	clone.EdgeLinks = append([]RtNavmeshEdgeLink(nil), source.EdgeLinks...)
	return clone
}

func cloneObjectForCache(source RtNavmeshObj) RtNavmeshObj {
	clone := source
	clone.Cells = append([]RtNavmeshCellTri(nil), source.Cells...)
	for i := range clone.Cells {
		clone.Cells[i].Mesh = nil
		clone.Cells[i].edges = nil
	}
	clone.GlobalEdges = append([]RtNavmeshEdgeGlobal(nil), source.GlobalEdges...)
	for i := range clone.GlobalEdges {
		clearEdgeRuntime(&clone.GlobalEdges[i].RtNavmeshEdgeBase)
	}
	clone.InternalEdges = append([]RtNavmeshEdgeInternal(nil), source.InternalEdges...)
	for i := range clone.InternalEdges {
		clearEdgeRuntime(&clone.InternalEdges[i].RtNavmeshEdgeBase)
	}
	clone.Events = append([]string(nil), source.Events...)
	clone.Grid = source.Grid
	clone.Grid.object = nil
	clone.Grid.Tiles = make([]RtNavmeshObjGridTile, len(source.Grid.Tiles))
	for i := range source.Grid.Tiles {
		tile := source.Grid.Tiles[i]
		tile.grid = RtNavmeshObjGrid{}
		tile.Cells = append([]RtNavmeshCellTri(nil), source.Grid.Tiles[i].Cells...)
		for j := range tile.Cells {
			tile.Cells[j].Mesh = nil
			tile.Cells[j].edges = nil
		}
		tile.GlobalEdges = append([]RtNavmeshEdgeGlobal(nil), source.Grid.Tiles[i].GlobalEdges...)
		for j := range tile.GlobalEdges {
			clearEdgeRuntime(&tile.GlobalEdges[j].RtNavmeshEdgeBase)
		}
		tile.InternalEdges = append([]RtNavmeshEdgeInternal(nil), source.Grid.Tiles[i].InternalEdges...)
		for j := range tile.InternalEdges {
			clearEdgeRuntime(&tile.InternalEdges[j].RtNavmeshEdgeBase)
		}
		clone.Grid.Tiles[i] = tile
	}
	return clone
}

func clearEdgeRuntime(edge *RtNavmeshEdgeBase) {
	edge.Mesh = nil
	edge.SrcCell = nil
	edge.DstCell = nil
}

func rehydrateCachedNavmeshRuntime(regions map[int16]*RtNavmeshTerrain, objects map[uint32]RtNavmeshObj) {
	for objectID, object := range objects {
		rebindObjectRuntime(&object)
		objects[objectID] = object
	}
	for _, terrain := range regions {
		rehydrateTerrainRuntime(terrain)
	}
}

// rehydrateTerrainRuntime makes every retained instance copy own its runtime
// pointers. Object catalog entries are templates stored by value; copying one
// into a terrain or cell must not leave its grid, cells, or edges pointing at
// the catalog/template object it came from.
func rehydrateTerrainRuntime(terrain *RtNavmeshTerrain) {
	if terrain == nil {
		return
	}
	for i := range terrain.Objects {
		rebindInstanceRuntime(&terrain.Objects[i])
	}
	for i := range terrain.Cells {
		for j := range terrain.Cells[i].Objects {
			rebindInstanceRuntime(&terrain.Cells[i].Objects[j])
		}
	}
}

func rebindInstanceRuntime(instance *RtNavmeshInstObj) {
	rebindObjectRuntime(&instance.Object)
	instance.Mesh = &instance.Object
}

func rebindObjectRuntime(object *RtNavmeshObj) {
	object.Grid.object = object
	for i := range object.Cells {
		object.Cells[i].Mesh = object
		object.Cells[i].edges = nil
	}
	for i := range object.GlobalEdges {
		object.GlobalEdges[i].Mesh = object
		object.GlobalEdges[i].SrcCell = nil
		object.GlobalEdges[i].DstCell = nil
	}
	for i := range object.InternalEdges {
		object.InternalEdges[i].Mesh = object
		object.InternalEdges[i].SrcCell = nil
		object.InternalEdges[i].DstCell = nil
	}
	for i := range object.Grid.Tiles {
		tile := &object.Grid.Tiles[i]
		tile.grid = object.Grid
		// Tiles need their grid's dimensions and owner, not another copy of the
		// complete tile slice. Keeping that slice would recursively retain the
		// entire object graph once per tile.
		tile.grid.Tiles = nil
		for j := range tile.Cells {
			tile.Cells[j].Mesh = object
			tile.Cells[j].edges = nil
		}
		for j := range tile.GlobalEdges {
			tile.GlobalEdges[j].Mesh = object
			tile.GlobalEdges[j].SrcCell = nil
			tile.GlobalEdges[j].DstCell = nil
		}
		for j := range tile.InternalEdges {
			tile.InternalEdges[j].Mesh = object
			tile.InternalEdges[j].SrcCell = nil
			tile.InternalEdges[j].DstCell = nil
		}
	}
}
