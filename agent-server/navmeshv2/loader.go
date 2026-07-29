package navmeshv2

import (
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"github.com/ferdoran/go-sro/agent-server/engine/geo/math"
	"github.com/ferdoran/go-sro/framework/pk2"
	"github.com/ferdoran/go-sro/framework/utils"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
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

func (l *Loader) LoadNavMeshInfos() (err error) {
	defer recoverNavmeshLoad("load metadata", l.NavMeshPath, &err)
	mapProjectInfo := LoadMapProjectInfo(l.Pk2Reader)
	objectInfo := LoadObjectInfo(l.Pk2Reader)
	l.MapProjectInfo = mapProjectInfo
	l.ObjectInfo = objectInfo
	//l.DungeonInfo = LoadDungeonInfo(l.Pk2Reader)
	return nil
}

func (l *Loader) LoadObjectMeshes() (err error) {
	defer recoverNavmeshLoad("load object meshes", l.NavMeshPath, &err)
	for _, objectEntry := range l.ObjectInfo.Objects {
		switch path.Ext(objectEntry.FilePath) {
		case ".bms":
			l.LoadObjectFromPrimMesh(objectEntry)
		case ".bsr":
			l.LoadObjectFromResource(objectEntry)
		case ".cpd":
			l.LoadObjectFromCompound(objectEntry)
		default:
			return &NavmeshLoadError{Operation: "load object mesh", Path: objectEntry.FilePath, Err: ErrUnsupportedObjectMesh}
		}
	}
	return nil
}

func (l *Loader) LoadObjectMesh(objectIndex uint32) (object RtNavmeshObj, err error) {
	defer recoverNavmeshLoad("load object mesh", fmt.Sprintf("index %d", objectIndex), &err)
	if object, exists := l.ObjectData[objectIndex]; exists {
		return object, nil
	}
	objectEntry, exists := l.ObjectInfo.Objects[objectIndex]
	if !exists {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: fmt.Sprintf("index %d", objectIndex), Err: ErrObjectMeshNotFound}
	}
	switch path.Ext(objectEntry.FilePath) {
	case ".bms":
		l.LoadObjectFromPrimMesh(objectEntry)
	case ".bsr":
		l.LoadObjectFromResource(objectEntry)
	case ".cpd":
		l.LoadObjectFromCompound(objectEntry)
	default:
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: objectEntry.FilePath, Err: ErrUnsupportedObjectMesh}
	}

	object, exists = l.ObjectData[objectIndex]
	if !exists {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Path: objectEntry.FilePath, Err: ErrObjectMeshNotFound}
	}
	return object, nil
}

func (l *Loader) LoadTerrainMeshes(progress chan int) error {
	counter := 0
	for regionId, enabled := range l.MapProjectInfo.EnabledRegions {
		if !enabled {
			continue
		}
		regionShortHex := fmt.Sprintf("%x", regionId)
		navMeshHex := fmt.Sprintf("nv_%s.nvm", regionShortHex)
		counter++
		if progress != nil {
			progress <- counter
		}
		fmt.Printf("\rReading %s. Finished [%d / %d] files", navMeshHex, counter, l.MapProjectInfo.ActiveRegionsCount)
		if err := l.LoadTerrainMesh(l.NavMeshPath+string(os.PathSeparator)+navMeshHex, regionId); err != nil {
			return errors.Wrap(err, "failed to load terrain mesh")
		}
	}
	return nil
}

func (l *Loader) LoadTerrainMesh(filepath string, regionId int16) (err error) {
	defer recoverNavmeshLoad("load terrain", filepath, &err)
	fileContent, err := l.Pk2Reader.ReadFile(filepath)
	if err != nil {
		return &NavmeshLoadError{Operation: "read terrain", Path: filepath, Err: err}
	}

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
	objectInstanceCount := binary.LittleEndian.Uint16(fileContent[readIndex : readIndex+2])
	readIndex += 2
	objectInstances := make([]RtNavmeshInstObj, objectInstanceCount)
	for i := 0; i < int(objectInstanceCount); i++ {
		objectInstance, loadErr := l.loadTerrainObjectInstance(fileContent, &readIndex)
		if loadErr != nil {
			return loadErr
		}
		objectInstances[i] = objectInstance
	}
	terrain.Objects = objectInstances

	// 2. Read Cells
	cellCount := binary.LittleEndian.Uint32(fileContent[readIndex : readIndex+4])
	// openCellCount := binary.LittleEndian.Uint32(fileContent[readIndex+4 : readIndex+8])
	readIndex += 8

	cells := make([]RtNavmeshCellQuad, cellCount)
	for i := 0; i < int(cellCount); i++ {
		cells[i] = l.loadTerrainNavigationCell(fileContent, &readIndex, i, &terrain)
	}
	terrain.Cells = cells

	// 3. Read Global Edges
	globalEdgeCount := binary.LittleEndian.Uint32(fileContent[readIndex : readIndex+4])
	globalEdges := make([]RtNavmeshEdgeGlobal, globalEdgeCount)
	readIndex += 4
	for i := 0; i < int(globalEdgeCount); i++ {
		globalEdges[i] = l.loadTerrainGlobalEdge(fileContent, &readIndex, i, &terrain)
	}
	terrain.GlobalEdges = globalEdges

	// 4. Read Internal Edges
	internalEdgeCount := binary.LittleEndian.Uint32(fileContent[readIndex : readIndex+4])
	internalEdges := make([]RtNavmeshEdgeInternal, internalEdgeCount)
	readIndex += 4
	for i := 0; i < int(internalEdgeCount); i++ {
		internalEdges[i] = l.loadTerrainInternalEdge(fileContent, &readIndex, i, &terrain)
	}
	terrain.InternalEdges = internalEdges

	// 5. Read TileMap
	terrain.TileMap = l.loadTerrainTileMap(fileContent, &readIndex)

	// 6. Read Height Map
	terrain.HeightMap = l.loadTerrainHeightMap(fileContent, &readIndex)

	// 7. Read Plane Map
	terrain.PlaneMap = l.loadTerrainPlaneMap(fileContent, &readIndex)

	l.RegionData[regionId] = &terrain

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

func (l *Loader) loadTerrainNavigationCell(content []byte, readIndex *int, index int, terrain *RtNavmeshTerrain) RtNavmeshCellQuad {

	minVectorOffset := *readIndex
	maxVectorOffset := minVectorOffset + 8
	objCountOffset := maxVectorOffset + 8
	*readIndex += 17

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
		Objects: make([]RtNavmeshInstObj, content[objCountOffset]),
		Edges:   make([]RtNavmeshEdge, 0),
	}

	// Object instances
	for i := 0; i < int(content[objCountOffset]); i++ {
		cell.Objects[i] = terrain.Objects[binary.LittleEndian.Uint16(content[*readIndex:*readIndex+2])]
		*readIndex += 2
	}

	return cell
}

func (l *Loader) loadTerrainObjectInstance(fileContent []byte, readIndex *int) (RtNavmeshInstObj, error) {
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
	edgeLinks := make([]RtNavmeshEdgeLink, 0)
	if globalEdgeLinkCount > 0 {
		logrus.Infof("Object %d (%d) has %d global edge links", objectInstance.ID, objectInstance.WorldID, globalEdgeLinkCount)
	} else {
		logrus.Infof("Object %d (%d) has no global edge links", objectInstance.ID, objectInstance.WorldID)
	}
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
	if err := writeGobAtomically(cachePrefix+"_regions", l.RegionData); err != nil {
		return fmt.Errorf("write region cache: %w", err)
	}
	if err := writeGobAtomically(cachePrefix+"_objects", l.ObjectData); err != nil {
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
	regions := make(map[int16]*RtNavmeshTerrain)
	objects := make(map[uint32]RtNavmeshObj)
	decodeStartTime := time.Now()
	if err := readGob(cachePrefix+"_regions", &regions); err != nil {
		return fmt.Errorf("read region cache: %w", err)
	}
	decodeStopTime := time.Now()
	logrus.Debugf("finished loading region navmesh data after %d ms", decodeStopTime.Sub(decodeStartTime).Milliseconds())

	decodeStartTime2 := time.Now()
	if err := readGob(cachePrefix+"_objects", &objects); err != nil {
		return fmt.Errorf("read object cache: %w", err)
	}
	decodeStopTime2 := time.Now()
	logrus.Debugf("finished loading object navmesh data after %d ms", decodeStopTime2.Sub(decodeStartTime2).Milliseconds())

	l.RegionData = regions
	l.ObjectData = objects
	return nil
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

func (l *Loader) LoadObjectFromPrimMesh(entry ObjectInfoEntry) {
	logrus.Tracef("loading object entry %d: %s", entry.Index, entry.FilePath)
	meshFile := LoadMeshFile(l.DataPk2Path+string(os.PathSeparator)+entry.FilePath, l.Pk2Reader)
	object := meshFile.LoadMeshObject()
	l.ObjectData[entry.Index] = object
}

func (l *Loader) LoadObjectFromResource(entry ObjectInfoEntry) {
	logrus.Tracef("loading object entry %d: %s", entry.Index, entry.FilePath)
	res := LoadResource(l.DataPk2Path+string(os.PathSeparator)+entry.FilePath, l.Pk2Reader)
	mesh := LoadMeshFile(l.DataPk2Path+string(os.PathSeparator)+res.NavMeshObjPath, l.Pk2Reader)
	object := mesh.LoadMeshObject()
	l.ObjectData[entry.Index] = object
}

func (l *Loader) LoadObjectFromCompound(entry ObjectInfoEntry) {
	logrus.Tracef("loading object entry %d: %s", entry.Index, entry.FilePath)
	cpd := LoadCompoundFile(l.DataPk2Path+string(os.PathSeparator)+entry.FilePath, l.Pk2Reader)
	res := LoadResource(l.DataPk2Path+string(os.PathSeparator)+cpd.NavMeshObjPath, l.Pk2Reader)
	mesh := LoadMeshFile(l.DataPk2Path+string(os.PathSeparator)+res.NavMeshObjPath, l.Pk2Reader)
	object := mesh.LoadMeshObject()
	l.ObjectData[entry.Index] = object
}
