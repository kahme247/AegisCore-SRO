package navmeshv2

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/Harvi8/AegisCore/framework/pk2"
)

const (
	RegionsX = 256
	RegionsY = 256

	RegionsTotal = RegionsX * RegionsY

	DungeonMask2        = ((1 << 1) - 1) << DungeonOffset
	SignatureByteLength = 12
	DataByteLength      = 12
	TotalByteLength     = SignatureByteLength + DataByteLength + RegionsTotal/8
	MapFilePathDataPk2  = "Data" + string(os.PathSeparator) + "navmesh" + string(os.PathSeparator) + "mapinfo.mfo"
)

type MapProjectInfo struct {
	MapWidth           uint16
	MapHeight          uint16
	Short2             uint16
	Short3             uint16
	Short4             uint16
	Short5             uint16
	ActiveRegionsCount int
	MapRegions         []byte
	EnabledRegions     map[int16]bool
}

func LoadMapProjectInfo(reader *pk2.Pk2Reader) (MapProjectInfo, error) {
	if reader == nil {
		return MapProjectInfo{}, &NavmeshLoadError{Operation: "read map info", Path: MapFilePathDataPk2, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	mapInfoBytes, err := reader.ReadFile(MapFilePathDataPk2)
	if err != nil {
		return MapProjectInfo{}, &NavmeshLoadError{Operation: "read map info", Path: MapFilePathDataPk2, Err: err}
	}
	return loadMapProjectInfoContent(MapFilePathDataPk2, mapInfoBytes)
}

func loadMapProjectInfoContent(filename string, mapInfoBytes []byte) (MapProjectInfo, error) {
	if length := len(mapInfoBytes); length != TotalByteLength {
		return MapProjectInfo{}, &NavmeshLoadError{
			Operation: "read map info",
			Path:      filename,
			Err:       fmt.Errorf("%w: file size %d, want %d", ErrInvalidNavmeshData, length, TotalByteLength),
		}
	}

	if string(mapInfoBytes[:SignatureByteLength]) != "JMXVMFO 1000" {
		return MapProjectInfo{}, &NavmeshLoadError{Operation: "read map info header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}

	mapWidthBytes := mapInfoBytes[SignatureByteLength : SignatureByteLength+2]
	mapHeightBytes := mapInfoBytes[SignatureByteLength+2 : SignatureByteLength+4]
	short2Bytes := mapInfoBytes[SignatureByteLength+4 : SignatureByteLength+6]
	short3Bytes := mapInfoBytes[SignatureByteLength+6 : SignatureByteLength+8]
	short4Bytes := mapInfoBytes[SignatureByteLength+8 : SignatureByteLength+10]
	short5Bytes := mapInfoBytes[SignatureByteLength+10 : SignatureByteLength+12]
	totalRegionBytes := mapInfoBytes[SignatureByteLength+12 : TotalByteLength]

	mapProjectInfo := MapProjectInfo{
		MapWidth:           binary.LittleEndian.Uint16(mapWidthBytes),
		MapHeight:          binary.LittleEndian.Uint16(mapHeightBytes),
		Short2:             binary.LittleEndian.Uint16(short2Bytes),
		Short3:             binary.LittleEndian.Uint16(short3Bytes),
		Short4:             binary.LittleEndian.Uint16(short4Bytes),
		Short5:             binary.LittleEndian.Uint16(short5Bytes),
		ActiveRegionsCount: 0,
		MapRegions:         totalRegionBytes,
		EnabledRegions:     make(map[int16]bool),
	}
	if mapProjectInfo.MapWidth > RegionsX || mapProjectInfo.MapHeight > RegionsY {
		return MapProjectInfo{}, &NavmeshLoadError{
			Operation: "read map info dimensions",
			Path:      filename,
			Err: fmt.Errorf(
				"%w: dimensions %dx%d exceed %dx%d region bitmap",
				ErrInvalidNavmeshData,
				mapProjectInfo.MapWidth,
				mapProjectInfo.MapHeight,
				RegionsX,
				RegionsY,
			),
		}
	}

	for z := 0; z < int(mapProjectInfo.MapHeight); z++ {
		for x := 0; x < int(mapProjectInfo.MapWidth); x++ {
			if IsEnabled(byte(x), byte(z), mapProjectInfo) {
				regionID := int16(binary.LittleEndian.Uint16([]byte{byte(x), byte(z)}))
				mapProjectInfo.ActiveRegionsCount++
				mapProjectInfo.EnabledRegions[regionID] = true
			}
		}
	}

	return mapProjectInfo, nil
}

func IsEnabled(x, z byte, mapProjectInfo MapProjectInfo) bool {
	regionShort := binary.LittleEndian.Uint16([]byte{x, z})
	if (regionShort&DungeonMask2)>>DungeonOffset != 0 {
		// It's a dungeon
		return false
	}

	if int(x) >= int(mapProjectInfo.MapWidth) || int(z) >= int(mapProjectInfo.MapHeight) {
		return false
	}
	regionIndex := int(regionShort >> 3)
	if regionIndex >= len(mapProjectInfo.MapRegions) {
		return false
	}
	return (mapProjectInfo.MapRegions[regionIndex] & byte(uint16(128>>(regionShort%8)))) != 0
}
