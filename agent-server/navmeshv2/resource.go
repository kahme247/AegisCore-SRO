package navmeshv2

import (
	"fmt"
	"os"
	"strings"

	"github.com/Harvi8/AegisCore/framework/pk2"
	"github.com/Harvi8/AegisCore/framework/utils"
)

type Resource struct {
	OffsetMaterial           uint32
	OffsetMesh               uint32
	OffsetPrimBranch         uint32
	OffsetAnimation          uint32
	OffsetPrimMeshGroup      uint32
	OffsetPrimAnimationGroup uint32
	OffsetModPalette         uint32
	OffsetNavMeshObj         uint32
	Int0                     uint32
	Int1                     uint32
	Int2                     uint32
	Int3                     uint32
	Int4                     uint32
	NavMeshObjPath           string
}

func LoadResource(filename string, reader *pk2.Pk2Reader) (*Resource, error) {
	filename = strings.ReplaceAll(filename, "\\", string(os.PathSeparator))
	filename = strings.ReplaceAll(filename, "/", string(os.PathSeparator))
	if reader == nil {
		return nil, &NavmeshLoadError{Operation: "read resource", Path: filename, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	fileContent, err := reader.ReadFile(filename)
	if err != nil {
		return nil, &NavmeshLoadError{Operation: "read resource", Path: filename, Err: err}
	}
	return loadResourceContent(filename, fileContent)
}

func loadResourceContent(filename string, fileContent []byte) (*Resource, error) {
	if len(fileContent) < 64 {
		return nil, &NavmeshLoadError{Operation: "read resource header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}
	if string(fileContent[:12]) != "JMXVRES 0109" {
		return nil, &NavmeshLoadError{Operation: "read resource header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}

	res := Resource{
		OffsetMaterial:           utils.ByteArrayToUint32(fileContent[12:16]),
		OffsetMesh:               utils.ByteArrayToUint32(fileContent[16:20]),
		OffsetPrimBranch:         utils.ByteArrayToUint32(fileContent[20:24]),
		OffsetAnimation:          utils.ByteArrayToUint32(fileContent[24:28]),
		OffsetPrimMeshGroup:      utils.ByteArrayToUint32(fileContent[28:32]),
		OffsetPrimAnimationGroup: utils.ByteArrayToUint32(fileContent[32:36]),
		OffsetModPalette:         utils.ByteArrayToUint32(fileContent[36:40]),
		OffsetNavMeshObj:         utils.ByteArrayToUint32(fileContent[40:44]),
		Int0:                     utils.ByteArrayToUint32(fileContent[44:48]),
		Int1:                     utils.ByteArrayToUint32(fileContent[48:52]),
		Int2:                     utils.ByteArrayToUint32(fileContent[52:56]),
		Int3:                     utils.ByteArrayToUint32(fileContent[56:60]),
		Int4:                     utils.ByteArrayToUint32(fileContent[60:64]),
	}

	path, err := readWrapperPath(fileContent, res.OffsetNavMeshObj, 64, "resource", filename)
	if err != nil {
		return nil, err
	}
	res.NavMeshObjPath = path
	return &res, nil
}

func readWrapperPath(fileContent []byte, offset uint32, minimumOffset int, kind, filename string) (string, error) {
	fileSize := uint64(len(fileContent))
	pathOffset := uint64(offset)
	if pathOffset < uint64(minimumOffset) || pathOffset > fileSize || fileSize-pathOffset < 4 {
		return "", &NavmeshLoadError{
			Operation: "read " + kind + " navmesh path",
			Path:      filename,
			Err:       fmt.Errorf("%w: offset %d is outside file size %d", ErrInvalidNavmeshData, offset, len(fileContent)),
		}
	}
	strLen := uint64(utils.ByteArrayToUint32(fileContent[int(pathOffset) : int(pathOffset)+4]))
	pathStart := pathOffset + 4
	if strLen > fileSize-pathStart {
		return "", &NavmeshLoadError{
			Operation: "read " + kind + " navmesh path",
			Path:      filename,
			Err:       fmt.Errorf("%w: path length %d exceeds remaining %d bytes", ErrInvalidNavmeshData, strLen, fileSize-pathStart),
		}
	}
	str := string(fileContent[int(pathStart):int(pathStart+strLen)])
	str = strings.ReplaceAll(str, "\\", string(os.PathSeparator))
	str = strings.ReplaceAll(str, "/", string(os.PathSeparator))
	return str, nil
}
