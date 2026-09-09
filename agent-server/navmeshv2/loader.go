package navmeshv2

import (
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"github.com/Harvi8/AegisCore/agent-server/engine/geo/math"
	"github.com/Harvi8/AegisCore/framework/pk2"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Loader struct {
	Pk2Reader      *pk2.Pk2Reader
	DataPk2Path    string
	NavMeshPath    string
	MapProjectInfo MapProjectInfo
	ObjectInfo     ObjectInfo
	DungeonInfo    DungeonInfo
	ObjectData     map[uint32]RtNavmeshObj
	// RegionData owns one immutable terrain object per region. Keeping pointers
	// here avoids copying the large fixed-size tile/height arrays whenever a
	// caller transfers or looks up a region.
	RegionData map[int16]*RtNavmeshTerrain
}

const (
	terrainObjectInstanceMinBytes = 32
	terrainCellMinBytes           = 17
	terrainGlobalEdgeBytes        = 27
	terrainInternalEdgeBytes      = 23
	terrainTileMapBytes           = TilesTotal * 8
	terrainHeightMapBytes         = VerticesTotal * 4
	terrainPlaneMapBytes          = BlocksTotal * 5
	terrainFixedTailBytes         = terrainTileMapBytes + terrainHeightMapBytes + terrainPlaneMapBytes
)

var navmeshGobRegistration sync.Once

func NewLoader(dataPk2Path string) (*Loader, error) {
	return NewLoaderSafe(dataPk2Path)
}

// NewLoaderSafe opens and indexes a PK2 archive through the checked reader.
// It is the production constructor: malformed archives return an error rather
// than reaching the legacy panic-based reader API.
func NewLoaderSafe(dataPk2Path string) (*Loader, error) {
	reader, err := pk2.OpenPk2Reader(dataPk2Path)
	if err != nil {
		return nil, err
	}
	if err := reader.IndexArchiveSafe(); err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("index pk2 archive: %w", err)
	}
	return newLoader(reader, dataPk2Path), nil
}

func newLoader(reader *pk2.Pk2Reader, dataPk2Path string) *Loader {
	return &Loader{
		Pk2Reader:      reader,
		DataPk2Path:    "Data",
		NavMeshPath:    "Data" + string(os.PathSeparator) + "navmesh",
		MapProjectInfo: MapProjectInfo{},
		ObjectInfo:     ObjectInfo{},
		DungeonInfo:    DungeonInfo{},
		ObjectData:     make(map[uint32]RtNavmeshObj),
		RegionData:     make(map[int16]*RtNavmeshTerrain),
	}
}

// CloseArchive releases the source PK2 once all runtime navmesh data has been
// materialized. RegionData and ObjectData are self-contained after loading;
// retaining the reader would otherwise keep the archive handle, index, and
// decoded file cache alive for the lifetime of the Agent.
func (l *Loader) CloseArchive() error {
	if l == nil || l.Pk2Reader == nil {
		return nil
	}
	reader := l.Pk2Reader
	l.Pk2Reader = nil
	reader.ClearFileCache()
	return reader.Close()
}

func (l *Loader) LoadNavMeshInfos() (err error) {
	defer recoverNavmeshLoad("load metadata", l.NavMeshPath, &err)
	mapProjectInfo, err := LoadMapProjectInfo(l.Pk2Reader)
	if err != nil {
		return err
	}
	objectInfo, err := LoadObjectInfo(l.Pk2Reader)
	if err != nil {
		return err
	}
	l.MapProjectInfo = mapProjectInfo
	l.ObjectInfo = objectInfo
	// dungeonInfo, err := LoadDungeonInfo(l.Pk2Reader)
	return nil
}

func (l *Loader) LoadObjectMeshes() (err error) {
	if l == nil {
		return &NavmeshLoadError{Operation: "load object meshes", Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	defer recoverNavmeshLoad("load object meshes", l.NavMeshPath, &err)
	return l.loadObjectMeshes(l.decodeObjectEntry)
}

func (l *Loader) loadObjectMeshes(decode func(ObjectInfoEntry) (RtNavmeshObj, error)) error {
	if decode == nil {
		return &NavmeshLoadError{Operation: "load object meshes", Err: fmt.Errorf("%w: object decoder is nil", ErrInvalidNavmeshData)}
	}
	indexes := make([]uint32, 0, len(l.ObjectInfo.Objects))
	for index := range l.ObjectInfo.Objects {
		indexes = append(indexes, index)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })

	objects := make(map[uint32]RtNavmeshObj, len(indexes))
	for _, index := range indexes {
		entry := l.ObjectInfo.Objects[index]
		if entry.Index != index {
			return &NavmeshLoadError{
				Operation: "load object mesh catalog",
				Path:      entry.FilePath,
				Err:       fmt.Errorf("%w: catalog index %d does not match entry index %d", ErrInvalidNavmeshData, index, entry.Index),
			}
		}
		object, err := decode(entry)
		if err != nil {
			if errors.Is(err, ErrObjectMeshNotFound) || errors.Is(err, pk2.ErrArchiveFileNotFound) {
				continue
			}
			return fmt.Errorf("load object catalog entry %d %q: %w", index, entry.FilePath, err)
		}
		objects[index] = object
	}
	l.ObjectData = objects
	return nil
}

func (l *Loader) LoadObjectMesh(objectIndex uint32) (object RtNavmeshObj, err error) {
	if l == nil {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: fmt.Sprintf("index %d", objectIndex), Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	defer recoverNavmeshLoad("load object mesh", fmt.Sprintf("index %d", objectIndex), &err)
	if object, exists := l.ObjectData[objectIndex]; exists {
		return object, nil
	}
	objectEntry, exists := l.ObjectInfo.Objects[objectIndex]
	if !exists {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: fmt.Sprintf("index %d", objectIndex), Err: ErrObjectMeshNotFound}
	}
	if objectEntry.Index != objectIndex {
		return RtNavmeshObj{}, &NavmeshLoadError{
			Operation: "load object mesh catalog",
			Path:      objectEntry.FilePath,
			Err:       fmt.Errorf("%w: catalog index %d does not match entry index %d", ErrInvalidNavmeshData, objectIndex, objectEntry.Index),
		}
	}
	object, err = l.decodeObjectEntry(objectEntry)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	if l.ObjectData == nil {
		l.ObjectData = make(map[uint32]RtNavmeshObj)
	}
	l.ObjectData[objectIndex] = object
	return object, nil
}

func (l *Loader) LoadTerrainMeshes(progress chan int) error {
	if l == nil {
		return &NavmeshLoadError{Operation: "load terrain meshes", Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	return l.loadTerrainMeshes(progress, func(staged *Loader, filepath string, regionID int16) error {
		return staged.readTerrainMesh(filepath, regionID)
	})
}

func (l *Loader) loadTerrainMeshes(progress chan<- int, load func(*Loader, string, int16) error) (err error) {
	if load == nil {
		return &NavmeshLoadError{Operation: "load terrain meshes", Err: fmt.Errorf("%w: terrain decoder is nil", ErrInvalidNavmeshData)}
	}
	defer recoverNavmeshLoad("load terrain meshes", l.NavMeshPath, &err)

	regionIDs := make([]int16, 0, len(l.MapProjectInfo.EnabledRegions))
	for regionID, enabled := range l.MapProjectInfo.EnabledRegions {
		if !enabled {
			continue
		}
		regionIDs = append(regionIDs, regionID)
	}
	sort.Slice(regionIDs, func(i, j int) bool { return uint16(regionIDs[i]) < uint16(regionIDs[j]) })

	staged := *l
	staged.RegionData = make(map[int16]*RtNavmeshTerrain, len(regionIDs))
	staged.ObjectData = cloneObjectData(l.ObjectData)
	for i, regionID := range regionIDs {
		filename := fmt.Sprintf("nv_%04x.nvm", uint16(regionID))
		filepath := l.NavMeshPath + string(os.PathSeparator) + filename
		if err := load(&staged, filepath, regionID); err != nil {
			return errors.Wrapf(err, "failed to load terrain mesh %s", filename)
		}
		completed := i + 1
		logrus.WithFields(logrus.Fields{
			"file":      filename,
			"completed": completed,
			"total":     len(regionIDs),
		}).Trace("loaded navmesh terrain")
		if progress != nil {
			progress <- completed
		}
	}
	if err := linkTerrainEdges(staged.RegionData, true); err != nil {
		return err
	}

	l.RegionData = staged.RegionData
	l.ObjectData = staged.ObjectData
	return nil
}

func cloneObjectData(source map[uint32]RtNavmeshObj) map[uint32]RtNavmeshObj {
	clone := make(map[uint32]RtNavmeshObj, len(source))
	for index, object := range source {
		clone[index] = object
	}
	return clone
}

func (l *Loader) LoadTerrainMesh(filepath string, regionId int16) (err error) {
	if l == nil {
		return &NavmeshLoadError{Operation: "load terrain", Path: filepath, Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	defer recoverNavmeshLoad("load terrain", filepath, &err)
	return l.loadTerrainMeshTransaction(filepath, regionId, func(staged *Loader, filepath string, regionID int16) error {
		return staged.readTerrainMesh(filepath, regionID)
	})
}

func (l *Loader) loadTerrainMeshTransaction(filepath string, regionID int16, load func(*Loader, string, int16) error) error {
	if load == nil {
		return &NavmeshLoadError{Operation: "load terrain", Path: filepath, Err: fmt.Errorf("%w: terrain decoder is nil", ErrInvalidNavmeshData)}
	}
	staged := *l
	staged.RegionData = cloneRegionData(l.RegionData)
	staged.ObjectData = cloneObjectData(l.ObjectData)
	if err := load(&staged, filepath, regionID); err != nil {
		return err
	}
	if err := linkTerrainEdges(staged.RegionData, false); err != nil {
		return err
	}
	l.RegionData = staged.RegionData
	l.ObjectData = staged.ObjectData
	return nil
}

func cloneRegionData(source map[int16]*RtNavmeshTerrain) map[int16]*RtNavmeshTerrain {
	clone := make(map[int16]*RtNavmeshTerrain, len(source))
	for regionID, terrain := range source {
		clone[regionID] = terrain
	}
	return clone
}

func (l *Loader) readTerrainMesh(filepath string, regionId int16) error {
	if l.Pk2Reader == nil {
		return &NavmeshLoadError{Operation: "read terrain", Path: filepath, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	fileContent, err := l.Pk2Reader.ReadFile(filepath)
	if err != nil {
		return &NavmeshLoadError{Operation: "read terrain", Path: filepath, Err: err}
	}
	return l.loadTerrainMeshContent(fileContent, filepath, regionId)
}

func (l *Loader) loadTerrainMeshContent(fileContent []byte, filepath string, regionId int16) error {
	readIndex := 0
	if len(fileContent) < 12 {
		return &NavmeshLoadError{Operation: "read terrain header", Path: filepath, Err: ErrInvalidNavmeshHeader}
	}
	signature := string(fileContent[:12])

	if signature != "JMXVNVM 1000" {
		return &NavmeshLoadError{Operation: "read terrain header", Path: filepath, Err: ErrInvalidNavmeshHeader}
	}
	readIndex += 12

	terrain := NewRtNavmeshTerrain(filepath, NewRegionFromInt16(regionId))

	// 1. Read Object Instances
	if err := requireTerrainBytes(fileContent, readIndex, 2, filepath, "object count"); err != nil {
		return err
	}
	objectInstanceCount := binary.LittleEndian.Uint16(fileContent[readIndex : readIndex+2])
	readIndex += 2
	if err := validateTerrainRecordCount(fileContent, readIndex, uint64(objectInstanceCount), terrainObjectInstanceMinBytes, terrainFixedTailBytes+16, filepath, "object instances"); err != nil {
		return err
	}
	objectInstances := make([]RtNavmeshInstObj, objectInstanceCount)
	for i := 0; i < int(objectInstanceCount); i++ {
		objectInstance, loadErr := l.loadTerrainObjectInstance(fileContent, &readIndex)
		if loadErr != nil {
			return loadErr
		}
		objectInstances[i] = objectInstance
	}
	terrain.Objects = objectInstances
	if err := validateTerrainObjectEdgeLinks(terrain.Objects, filepath); err != nil {
		return err
	}

	// 2. Read Cells
	if err := requireTerrainBytes(fileContent, readIndex, 8, filepath, "cell counts"); err != nil {
		return err
	}
	cellCount := binary.LittleEndian.Uint32(fileContent[readIndex : readIndex+4])
	// openCellCount := binary.LittleEndian.Uint32(fileContent[readIndex+4 : readIndex+8])
	readIndex += 8
	if err := validateTerrainRecordCount(fileContent, readIndex, uint64(cellCount), terrainCellMinBytes, terrainFixedTailBytes+8, filepath, "cells"); err != nil {
		return err
	}

	cells := make([]RtNavmeshCellQuad, cellCount)
	for i := 0; i < int(cellCount); i++ {
		cell, err := l.loadTerrainNavigationCell(fileContent, &readIndex, i, &terrain, filepath)
		if err != nil {
			return err
		}
		cells[i] = cell
	}
	terrain.Cells = cells

	// 3. Read Global Edges
	if err := requireTerrainBytes(fileContent, readIndex, 4, filepath, "global edge count"); err != nil {
		return err
	}
	globalEdgeCount := binary.LittleEndian.Uint32(fileContent[readIndex : readIndex+4])
	readIndex += 4
	if err := validateTerrainRecordCount(fileContent, readIndex, uint64(globalEdgeCount), terrainGlobalEdgeBytes, terrainFixedTailBytes+4, filepath, "global edges"); err != nil {
		return err
	}
	globalEdges := make([]RtNavmeshEdgeGlobal, globalEdgeCount)
	for i := 0; i < int(globalEdgeCount); i++ {
		globalEdges[i] = l.loadTerrainGlobalEdge(fileContent, &readIndex, i, &terrain)
	}
	terrain.GlobalEdges = globalEdges

	// 4. Read Internal Edges
	if err := requireTerrainBytes(fileContent, readIndex, 4, filepath, "internal edge count"); err != nil {
		return err
	}
	internalEdgeCount := binary.LittleEndian.Uint32(fileContent[readIndex : readIndex+4])
	readIndex += 4
	if err := validateTerrainRecordCount(fileContent, readIndex, uint64(internalEdgeCount), terrainInternalEdgeBytes, terrainFixedTailBytes, filepath, "internal edges"); err != nil {
		return err
	}
	internalEdges := make([]RtNavmeshEdgeInternal, internalEdgeCount)
	for i := 0; i < int(internalEdgeCount); i++ {
		internalEdges[i] = l.loadTerrainInternalEdge(fileContent, &readIndex, i, &terrain)
	}
	terrain.InternalEdges = internalEdges

	if err := requireTerrainBytes(fileContent, readIndex, terrainFixedTailBytes, filepath, "fixed maps"); err != nil {
		return err
	}

	// 5. Read TileMap
	terrain.TileMap = l.loadTerrainTileMap(fileContent, &readIndex)

	// 6. Read Height Map
	terrain.HeightMap = l.loadTerrainHeightMap(fileContent, &readIndex)

	// 7. Read Plane Map
	terrain.PlaneMap = l.loadTerrainPlaneMap(fileContent, &readIndex)
	// Object meshes are catalog templates copied into terrain- and cell-owned
	// instances. Rebind runtime ownership only after those copies reach their
	// final backing slices.
	rehydrateTerrainRuntime(&terrain)
	if err := validateTerrainInstanceTopology(terrain.Objects, filepath); err != nil {
		return err
	}
	if err := validateTerrainCellObjectReferences(&terrain, filepath); err != nil {
		return err
	}

	if l.RegionData == nil {
		l.RegionData = make(map[int16]*RtNavmeshTerrain)
	}
	l.RegionData[regionId] = &terrain

	return nil
}

func requireTerrainBytes(content []byte, index, needed int, filepath, section string) error {
	if index < 0 || needed < 0 || index > len(content) || needed > len(content)-index {
		remaining := 0
		if index >= 0 && index <= len(content) {
			remaining = len(content) - index
		}
		return &NavmeshLoadError{
			Operation: "read terrain " + section,
			Path:      filepath,
			Err:       fmt.Errorf("%w: need %d bytes at offset %d, have %d", ErrInvalidNavmeshData, needed, index, remaining),
		}
	}
	return nil
}

func validateTerrainRecordCount(content []byte, index int, count uint64, recordBytes, trailingBytes int, filepath, section string) error {
	if err := requireTerrainBytes(content, index, trailingBytes, filepath, section); err != nil {
		return err
	}
	available := len(content) - index - trailingBytes
	maximum := uint64(available / recordBytes)
	if count > maximum {
		return &NavmeshLoadError{
			Operation: "read terrain " + section,
			Path:      filepath,
			Err:       fmt.Errorf("%w: record count %d exceeds maximum %d for %d available bytes", ErrInvalidNavmeshData, count, maximum, available),
		}
	}
	return nil
}

func (l *Loader) loadTerrainPlaneMap(content []byte, readIndex *int) [BlocksTotal]RtNavmeshPlane {
	planeMap := [BlocksTotal]RtNavmeshPlane{}

	for i := 0; i < BlocksTotal; i++ {
		planeMap[i] = RtNavmeshPlane{
			SurfaceType: RtNavmeshSurfaceType(content[*readIndex]),
		}
		*readIndex++
	}
	for i := 0; i < BlocksTotal; i++ {
		planeMap[i].Height = utils.Float32FromByteArray(content[*readIndex : *readIndex+4])
		*readIndex += 4
	}

	return planeMap
}

func (l *Loader) loadTerrainHeightMap(content []byte, readIndex *int) [VerticesTotal]float32 {
	heightMap := [VerticesTotal]float32{}
	for i := 0; i < VerticesTotal; i++ {
		heightMap[i] = utils.Float32FromByteArray(content[*readIndex : *readIndex+4])
		*readIndex += 4
	}
	return heightMap
}

func (l *Loader) loadTerrainTileMap(content []byte, readIndex *int) [TilesTotal]RtNavmeshTile {
	tileMap := [TilesTotal]RtNavmeshTile{}
	for i := 0; i < TilesTotal; i++ {
		cellIdOffset := *readIndex
		flagOffset := cellIdOffset + 4
		textureIndexOffset := flagOffset + 2

		*readIndex += 8

		tileMap[i] = RtNavmeshTile{
			CellIndex: int(binary.LittleEndian.Uint32(content[cellIdOffset:flagOffset])),
			Flag:      RtNavmeshTileFlag(binary.LittleEndian.Uint16(content[flagOffset:textureIndexOffset])),
			TextureID: int16(binary.LittleEndian.Uint16(content[textureIndexOffset : textureIndexOffset+2])),
		}
	}
	return tileMap
}

func (l *Loader) loadTerrainInternalEdge(content []byte, readIndex *int, index int, terrain *RtNavmeshTerrain) RtNavmeshEdgeInternal {
	minVectorOffset := *readIndex
	maxVectorOffset := minVectorOffset + 8
	edgeFlagOffset := maxVectorOffset + 8
	assocDir0Offset := edgeFlagOffset + 1
	assocDir1Offset := assocDir0Offset + 1
	assocCell0Offset := assocDir1Offset + 1
	assocCell1Offset := assocCell0Offset + 2

	*readIndex += 23

	if val := content[edgeFlagOffset] & 32; val != 0 {
		log.Printf("Internal EdgeType & Bit5 %v", val)
	}
	if val := content[edgeFlagOffset] & 64; val != 0 {
		log.Printf("Internal EdgeType & Bit6 %v", val)
	}

	internalEdge := RtNavmeshEdgeInternal{
		RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			RtNavmeshEdgeMeshType: RtNavmeshEdgeMeshTypeTerrain,
			Mesh:                  terrain,
			Index:                 index,
			Line: LineSegment{
				A: math32.NewVector3(utils.Float32FromByteArray(content[minVectorOffset:minVectorOffset+4]), 0, utils.Float32FromByteArray(content[minVectorOffset+4:maxVectorOffset])),
				B: math32.NewVector3(utils.Float32FromByteArray(content[maxVectorOffset:maxVectorOffset+4]), 0, utils.Float32FromByteArray(content[maxVectorOffset+4:edgeFlagOffset])),
			},
			Flag:         RtNavmeshEdgeFlag(content[edgeFlagOffset]),
			SrcDirection: RtNavmeshEdgeDirection(content[assocDir0Offset]),
			DstDirection: RtNavmeshEdgeDirection(content[assocDir1Offset]),
			SrcCellIndex: int(binary.LittleEndian.Uint16(content[assocCell0Offset:assocCell1Offset])),
			DstCellIndex: int(binary.LittleEndian.Uint16(content[assocCell1Offset : assocCell1Offset+2])),
			SrcCell:      nil,
			DstCell:      nil,
		},
	}

	return internalEdge
}

func (l *Loader) loadTerrainGlobalEdge(content []byte, readIndex *int, index int, terrain *RtNavmeshTerrain) RtNavmeshEdgeGlobal {
	minVectorOffset := *readIndex
	maxVectorOffset := minVectorOffset + 8
	edgeFlagOffset := maxVectorOffset + 8
	assocDir0Offset := edgeFlagOffset + 1
	assocDir1Offset := assocDir0Offset + 1
	assocCell0Offset := assocDir1Offset + 1
	assocCell1Offset := assocCell0Offset + 2
	assocRgn0Offset := assocCell1Offset + 2
	assocRgn1Offset := assocRgn0Offset + 2

	*readIndex += 27

	if val := content[edgeFlagOffset] & 32; val != 0 {
		log.Printf("Internal EdgeType & Bit5 %v", val)
	}
	if val := content[edgeFlagOffset] & 64; val != 0 {
		log.Printf("Internal EdgeType & Bit6 %v", val)
	}

	globalEdge := RtNavmeshEdgeGlobal{
		RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			RtNavmeshEdgeMeshType: RtNavmeshEdgeMeshTypeTerrain,
			Mesh:                  terrain,
			Index:                 index,
			Line: LineSegment{
				A: math32.NewVector3(utils.Float32FromByteArray(content[minVectorOffset:minVectorOffset+4]), 0, utils.Float32FromByteArray(content[minVectorOffset+4:maxVectorOffset])),
				B: math32.NewVector3(utils.Float32FromByteArray(content[maxVectorOffset:maxVectorOffset+4]), 0, utils.Float32FromByteArray(content[maxVectorOffset+4:edgeFlagOffset])),
			},
			Flag:         RtNavmeshEdgeFlag(content[edgeFlagOffset]),
			SrcDirection: RtNavmeshEdgeDirection(content[assocDir0Offset]),
			DstDirection: RtNavmeshEdgeDirection(content[assocDir1Offset]),
			SrcCellIndex: int(binary.LittleEndian.Uint16(content[assocCell0Offset:assocCell1Offset])),
			DstCellIndex: int(binary.LittleEndian.Uint16(content[assocCell1Offset:assocRgn0Offset])),
			SrcCell:      nil,
			DstCell:      nil,
		},
		SrcMeshIndex: int(binary.LittleEndian.Uint16(content[assocRgn0Offset:assocRgn1Offset])),
		DstMeshIndex: int(binary.LittleEndian.Uint16(content[assocRgn1Offset : assocRgn1Offset+2])),
	}

	return globalEdge
}

func (l *Loader) loadTerrainNavigationCell(content []byte, readIndex *int, index int, terrain *RtNavmeshTerrain, filepath string) (RtNavmeshCellQuad, error) {
	if readIndex == nil {
		return RtNavmeshCellQuad{}, &NavmeshLoadError{
			Operation: "read terrain cell",
			Path:      filepath,
			Err:       fmt.Errorf("%w: read index is nil", ErrInvalidNavmeshData),
		}
	}
	if terrain == nil {
		return RtNavmeshCellQuad{}, &NavmeshLoadError{
			Operation: "read terrain cell",
			Path:      filepath,
			Err:       fmt.Errorf("%w: terrain is nil", ErrInvalidNavmeshData),
		}
	}
	if err := requireTerrainBytes(content, *readIndex, terrainCellMinBytes, filepath, "cell"); err != nil {
		return RtNavmeshCellQuad{}, err
	}

	minVectorOffset := *readIndex
	maxVectorOffset := minVectorOffset + 8
	objCountOffset := maxVectorOffset + 8
	*readIndex += 17
	objectReferenceCount := int(content[objCountOffset])
	if err := requireTerrainBytes(content, *readIndex, objectReferenceCount*2, filepath, "cell object references"); err != nil {
		return RtNavmeshCellQuad{}, err
	}

	// cell data
	cell := RtNavmeshCellQuad{
		RtNavmeshCellBase: RtNavmeshCellBase{
			Index: index,
			Mesh:  terrain,
		},
		Rect: Rectangle{
			Min: &math32.Vector2{
				X: utils.Float32FromByteArray(content[minVectorOffset : minVectorOffset+4]),
				Y: utils.Float32FromByteArray(content[minVectorOffset+4 : maxVectorOffset]),
			},
			Max: &math32.Vector2{
				X: utils.Float32FromByteArray(content[maxVectorOffset : maxVectorOffset+4]),
				Y: utils.Float32FromByteArray(content[maxVectorOffset+4 : objCountOffset]),
			},
		},
		Objects: make([]RtNavmeshInstObj, objectReferenceCount),
		Edges:   make([]RtNavmeshEdge, 0),
	}

	// Object instances
	for i := 0; i < objectReferenceCount; i++ {
		objectIndex := int(binary.LittleEndian.Uint16(content[*readIndex : *readIndex+2]))
		if objectIndex >= len(terrain.Objects) {
			return RtNavmeshCellQuad{}, &NavmeshLoadError{
				Operation: "read terrain cell object reference",
				Path:      filepath,
				Err: fmt.Errorf(
					"%w: cell %d reference %d selects object %d with %d loaded",
					ErrInvalidNavmeshData,
					index,
					i,
					objectIndex,
					len(terrain.Objects),
				),
			}
		}
		cell.Objects[i] = terrain.Objects[objectIndex]
		*readIndex += 2
	}

	return cell, nil
}

func (l *Loader) loadTerrainObjectInstance(fileContent []byte, readIndex *int) (RtNavmeshInstObj, error) {
	if readIndex == nil {
		return RtNavmeshInstObj{}, &NavmeshLoadError{Operation: "read terrain object instance", Err: fmt.Errorf("%w: read index is nil", ErrInvalidNavmeshData)}
	}
	if err := requireTerrainBytes(fileContent, *readIndex, terrainObjectInstanceMinBytes, "", "object instance"); err != nil {
		return RtNavmeshInstObj{}, err
	}
	objIdOffset := *readIndex
	positionOffset := objIdOffset + 4
	objTypeOffset := positionOffset + 12
	yawOffset := objTypeOffset + 2
	localUidOffset := yawOffset + 4
	short0Offset := localUidOffset + 2
	isLargeOffset := short0Offset + 2
	isStructureOffset := isLargeOffset + 1
	regionIdOffset := isStructureOffset + 1
	globalEdgeLinkCountOffset := regionIdOffset + 2
	*readIndex += 32

	objectIndex := binary.LittleEndian.Uint32(fileContent[objIdOffset:positionOffset])
	object, err := l.LoadObjectMesh(objectIndex)
	if err != nil {
		return RtNavmeshInstObj{}, err
	}
	objectInstance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{
			Mesh:   object,
			Object: object,
			ID:     int16(binary.LittleEndian.Uint16(fileContent[localUidOffset:short0Offset])),
			Position: math32.NewVector3(
				utils.Float32FromByteArray(fileContent[positionOffset:positionOffset+4]),
				utils.Float32FromByteArray(fileContent[positionOffset+4:positionOffset+8]),
				utils.Float32FromByteArray(fileContent[positionOffset+8:objTypeOffset]),
			),
			Rotation:     math.NewQuaternion(-utils.Float32FromByteArray(fileContent[yawOffset:localUidOffset]), 0, 0),
			Scale:        math32.NewVector3(1, 1, 1),
			LocalToWorld: nil,
			WorldToLocal: nil,
		},
		Region: NewRegionFromUint16(binary.LittleEndian.Uint16(fileContent[regionIdOffset:globalEdgeLinkCountOffset])),
	}
	objectInstance.WorldID = int(uint32(objectInstance.Region.ID)<<16 | uint32(uint16(objectInstance.ID)))
	objectInstance.LocalToWorld = math32.NewMatrix4().Compose(objectInstance.GetPosition(), objectInstance.GetRotation(), objectInstance.GetScale())
	objectInstance.WorldToLocal = objectInstance.GetLocalToWorld().Clone()

	err = objectInstance.GetWorldToLocal().GetInverse(objectInstance.GetLocalToWorld())
	if err != nil {
		return RtNavmeshInstObj{}, &NavmeshLoadError{Operation: "invert object transform", Path: fmt.Sprintf("index %d", objectIndex), Err: err}
	}

	globalEdgeLinkCount := binary.LittleEndian.Uint16(fileContent[globalEdgeLinkCountOffset : globalEdgeLinkCountOffset+2])
	if err := requireTerrainBytes(fileContent, *readIndex, int(globalEdgeLinkCount)*6, "", "object edge links"); err != nil {
		return RtNavmeshInstObj{}, err
	}
	edgeLinks := make([]RtNavmeshEdgeLink, 0)
	logrus.WithFields(logrus.Fields{
		"object_id":       objectInstance.ID,
		"object_world_id": objectInstance.WorldID,
		"edge_links":      globalEdgeLinkCount,
	}).Trace("loaded navmesh object edge links")
	for j := 0; j < int(globalEdgeLinkCount); j++ {
		// TODO find out what this is for
		linkedObjIdOffset := *readIndex
		linkedObjEdgeIdOffset := linkedObjIdOffset + 2
		edgeIdOffset := linkedObjEdgeIdOffset + 2

		edgeLink := RtNavmeshEdgeLink{
			LinkedObjID:     int16(utils.ByteArrayToUint16(fileContent[linkedObjIdOffset:linkedObjEdgeIdOffset])),
			LinkedObjEdgeID: int16(utils.ByteArrayToUint16(fileContent[linkedObjEdgeIdOffset:edgeIdOffset])),
			EdgeID:          int16(utils.ByteArrayToUint16(fileContent[edgeIdOffset : edgeIdOffset+2])),
		}
		*readIndex += 6
		edgeLinks = append(edgeLinks, edgeLink)
	}
	objectInstance.EdgeLinks = edgeLinks
	return objectInstance, nil
}

func validateTerrainObjectEdgeLinks(objects []RtNavmeshInstObj, filepath string) error {
	for objectIndex := range objects {
		object := &objects[objectIndex]
		for linkIndex, link := range object.EdgeLinks {
			if link.IsUnmapped() {
				continue
			}
			if link.HasNegativeField() {
				return invalidTerrainObjectEdgeLink(filepath, objectIndex, linkIndex, link, "partially negative link")
			}
			if int(link.EdgeID) >= len(object.Object.GlobalEdges) {
				return invalidTerrainObjectEdgeLink(
					filepath,
					objectIndex,
					linkIndex,
					link,
					fmt.Sprintf("source edge index exceeds %d edges", len(object.Object.GlobalEdges)),
				)
			}
			if int(link.LinkedObjID) >= len(objects) {
				return invalidTerrainObjectEdgeLink(
					filepath,
					objectIndex,
					linkIndex,
					link,
					fmt.Sprintf("linked object index exceeds %d objects", len(objects)),
				)
			}
			linkedObject := &objects[int(link.LinkedObjID)]
			if int(link.LinkedObjEdgeID) >= len(linkedObject.Object.GlobalEdges) {
				return invalidTerrainObjectEdgeLink(
					filepath,
					objectIndex,
					linkIndex,
					link,
					fmt.Sprintf("linked edge index exceeds %d edges", len(linkedObject.Object.GlobalEdges)),
				)
			}
		}
	}
	return nil
}

func invalidTerrainObjectEdgeLink(filepath string, objectIndex, linkIndex int, link RtNavmeshEdgeLink, reason string) error {
	return &NavmeshLoadError{
		Operation: "validate terrain object edge link",
		Path:      filepath,
		Err: fmt.Errorf(
			"%w: object %d link %d (%d,%d,%d): %s",
			ErrInvalidNavmeshData,
			objectIndex,
			linkIndex,
			link.LinkedObjID,
			link.LinkedObjEdgeID,
			link.EdgeID,
			reason,
		),
	}
}

func (l *Loader) SaveNavmeshDataAsGOB(cachePrefix string) {
	if err := l.saveNavmeshDataAsGOB(cachePrefix); err != nil {
		logrus.Errorf("failed to save navmesh data as gob: %v", err)
	}
}

// SaveNavmeshDataAsGOBWithManifest publishes both v2 artifacts before the
// manifest. A stale or interrupted artifact set therefore cannot validate as
// a ready cache.
func (l *Loader) SaveNavmeshDataAsGOBWithManifest(ctx context.Context, cachePrefix, profile, archiveSHA256 string) error {
	if err := l.saveNavmeshDataAsGOB(cachePrefix); err != nil {
		return err
	}
	manifest, err := BuildCacheManifest(ctx, cachePrefix+"_regions", cachePrefix+"_objects", profile, archiveSHA256)
	if err != nil {
		return err
	}
	return WriteCacheManifestAtomic(CacheManifestPath(cachePrefix), manifest)
}

func (l *Loader) saveNavmeshDataAsGOB(cachePrefix string) error {
	if l == nil {
		return errors.New("navmesh loader is nil")
	}
	if strings.TrimSpace(cachePrefix) == "" {
		return errors.New("navmesh cache prefix is required")
	}
	navmeshGobRegistration.Do(func() {
		gob.Register(RtNavmeshObj{})
		gob.Register(RtNavmeshTerrain{})
	})
	if l.RegionData == nil {
		l.RegionData = make(map[int16]*RtNavmeshTerrain)
	}
	if l.ObjectData == nil {
		l.ObjectData = make(map[uint32]RtNavmeshObj)
	}
	if err := validateCachedObjectData(l.ObjectData); err != nil {
		return fmt.Errorf("validate object cache: %w", err)
	}
	if err := validateCachedRegionData(l.RegionData); err != nil {
		return fmt.Errorf("validate region cache: %w", err)
	}
	if err := writeRegionCacheAtomically(cachePrefix+"_regions", l.RegionData); err != nil {
		return fmt.Errorf("write region cache: %w", err)
	}
	if err := writeObjectCacheAtomically(cachePrefix+"_objects", l.ObjectData); err != nil {
		return fmt.Errorf("write object cache: %w", err)
	}
	return nil
}

func (l *Loader) LoadPrecomputedNavmeshDataFromGOB(cachePrefix string) error {
	if l == nil {
		return errors.New("navmesh loader is nil")
	}
	navmeshGobRegistration.Do(func() {
		gob.Register(RtNavmeshObj{})
		gob.Register(RtNavmeshTerrain{})
	})
	logrus.Debugf("loading precomputed navmesh data")
	decodeStartTime := time.Now()
	regions, err := readRegionCache(cachePrefix + "_regions")
	if err != nil {
		return fmt.Errorf("read region cache: %w", err)
	}
	decodeStopTime := time.Now()
	logrus.Debugf("finished loading region navmesh data after %d ms", decodeStopTime.Sub(decodeStartTime).Milliseconds())

	decodeStartTime2 := time.Now()
	objects, err := readObjectCache(cachePrefix + "_objects")
	if err != nil {
		return fmt.Errorf("read object cache: %w", err)
	}
	decodeStopTime2 := time.Now()
	logrus.Debugf("finished loading object navmesh data after %d ms", decodeStopTime2.Sub(decodeStartTime2).Milliseconds())
	rehydrateCachedNavmeshRuntime(regions, objects)
	if err := validateCachedObjectData(objects); err != nil {
		return fmt.Errorf("validate object cache: %w", err)
	}
	if err := validateCachedRegionData(regions); err != nil {
		return fmt.Errorf("validate region cache: %w", err)
	}
	if err := linkTerrainEdges(regions, true); err != nil {
		return fmt.Errorf("link region cache: %w", err)
	}

	l.RegionData = regions
	l.ObjectData = objects
	return nil
}

func validateCachedRegionData(regions map[int16]*RtNavmeshTerrain) error {
	regionIDs := make([]int16, 0, len(regions))
	for regionID := range regions {
		regionIDs = append(regionIDs, regionID)
	}
	sort.Slice(regionIDs, func(i, j int) bool { return uint16(regionIDs[i]) < uint16(regionIDs[j]) })

	for _, regionID := range regionIDs {
		terrain := regions[regionID]
		regionPath := fmt.Sprintf("region %04x", uint16(regionID))
		if terrain == nil {
			return &NavmeshLoadError{
				Operation: "validate cached terrain",
				Path:      regionPath,
				Err:       fmt.Errorf("%w: terrain is nil", ErrInvalidNavmeshData),
			}
		}
		if terrain.Filename != "" {
			regionPath = terrain.Filename
		}
		if terrain.Region.ID != regionID {
			return &NavmeshLoadError{
				Operation: "validate cached terrain",
				Path:      regionPath,
				Err: fmt.Errorf(
					"%w: map key %04x does not match terrain region %04x",
					ErrInvalidNavmeshData,
					uint16(regionID),
					uint16(terrain.Region.ID),
				),
			}
		}
		if err := validateTerrainInstanceTopology(terrain.Objects, regionPath); err != nil {
			return err
		}
		if err := validateTerrainCellObjectReferences(terrain, regionPath); err != nil {
			return err
		}
		if err := validateTerrainObjectEdgeLinks(terrain.Objects, regionPath); err != nil {
			return err
		}
	}
	return validateTerrainEdgeTopology(regions, true)
}

func writeGobAtomically(path string, value interface{}) error {
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
	if err := gob.NewEncoder(temporary).Encode(value); err != nil {
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

func readGob(path string, target interface{}) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("cache contains trailing gob value")
		}
		return err
	}
	return nil
}

func (l *Loader) LoadObjectFromPrimMesh(entry ObjectInfoEntry) error {
	object, err := l.decodeObjectFromPrimMesh(entry)
	if err != nil {
		return err
	}
	l.publishObject(entry.Index, object)
	return nil
}

func (l *Loader) decodeObjectEntry(entry ObjectInfoEntry) (RtNavmeshObj, error) {
	logrus.Tracef("loading object entry %d: %s", entry.Index, entry.FilePath)
	switch strings.ToLower(path.Ext(entry.FilePath)) {
	case ".bms":
		return l.decodeObjectFromPrimMesh(entry)
	case ".bsr":
		return l.decodeObjectFromResource(entry)
	case ".cpd":
		return l.decodeObjectFromCompound(entry)
	default:
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: ErrUnsupportedObjectMesh}
	}
}

func (l *Loader) decodeObjectFromPrimMesh(entry ObjectInfoEntry) (RtNavmeshObj, error) {
	if l == nil {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	meshFile, err := LoadMeshFile(l.DataPk2Path+string(os.PathSeparator)+entry.FilePath, l.Pk2Reader)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	object, err := meshFile.LoadMeshObject()
	if err != nil {
		return RtNavmeshObj{}, err
	}
	return object, nil
}

func (l *Loader) LoadObjectFromResource(entry ObjectInfoEntry) error {
	object, err := l.decodeObjectFromResource(entry)
	if err != nil {
		return err
	}
	l.publishObject(entry.Index, object)
	return nil
}

func (l *Loader) decodeObjectFromResource(entry ObjectInfoEntry) (RtNavmeshObj, error) {
	if l == nil {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	res, err := LoadResource(l.DataPk2Path+string(os.PathSeparator)+entry.FilePath, l.Pk2Reader)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	if strings.TrimSpace(res.NavMeshObjPath) == "" {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: ErrObjectMeshNotFound}
	}
	mesh, err := LoadMeshFile(l.DataPk2Path+string(os.PathSeparator)+res.NavMeshObjPath, l.Pk2Reader)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	object, err := mesh.LoadMeshObject()
	if err != nil {
		return RtNavmeshObj{}, err
	}
	return object, nil
}

func (l *Loader) LoadObjectFromCompound(entry ObjectInfoEntry) error {
	object, err := l.decodeObjectFromCompound(entry)
	if err != nil {
		return err
	}
	l.publishObject(entry.Index, object)
	return nil
}

func (l *Loader) decodeObjectFromCompound(entry ObjectInfoEntry) (RtNavmeshObj, error) {
	if l == nil {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	cpd, err := LoadCompoundFile(l.DataPk2Path+string(os.PathSeparator)+entry.FilePath, l.Pk2Reader)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	if strings.TrimSpace(cpd.NavMeshObjPath) == "" {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: ErrObjectMeshNotFound}
	}
	res, err := LoadResource(l.DataPk2Path+string(os.PathSeparator)+cpd.NavMeshObjPath, l.Pk2Reader)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	if strings.TrimSpace(res.NavMeshObjPath) == "" {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: entry.FilePath, Err: ErrObjectMeshNotFound}
	}
	mesh, err := LoadMeshFile(l.DataPk2Path+string(os.PathSeparator)+res.NavMeshObjPath, l.Pk2Reader)
	if err != nil {
		return RtNavmeshObj{}, err
	}
	object, err := mesh.LoadMeshObject()
	if err != nil {
		return RtNavmeshObj{}, err
	}
	return object, nil
}

func (l *Loader) publishObject(index uint32, object RtNavmeshObj) {
	if l.ObjectData == nil {
		l.ObjectData = make(map[uint32]RtNavmeshObj)
	}
	l.ObjectData[index] = object
}
