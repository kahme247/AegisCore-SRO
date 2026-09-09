package navmeshv2

import (
	"fmt"
	"os"
	"strings"

	"github.com/Harvi8/AegisCore/framework/pk2"
	"github.com/Harvi8/AegisCore/framework/utils"
)

type Compound struct {
	OffsetNavMeshObj uint32
	OffsetResObjList uint32
	Int0             uint32
	Int1             uint32
	Int2             uint32
	Int3             uint32
	Int4             uint32
	NavMeshObjPath   string
}

func LoadCompoundFile(filename string, reader *pk2.Pk2Reader) (*Compound, error) {
	filename = strings.ReplaceAll(filename, "\\", string(os.PathSeparator))
	filename = strings.ReplaceAll(filename, "/", string(os.PathSeparator))
	if reader == nil {
		return nil, &NavmeshLoadError{Operation: "read compound", Path: filename, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	fileContent, err := reader.ReadFile(filename)
	if err != nil {
		return nil, &NavmeshLoadError{Operation: "read compound", Path: filename, Err: err}
	}
	return loadCompoundContent(filename, fileContent)
}

func loadCompoundContent(filename string, fileContent []byte) (*Compound, error) {
	if len(fileContent) < 40 {
		return nil, &NavmeshLoadError{Operation: "read compound header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}
	if string(fileContent[:12]) != "JMXVCPD 0101" {
		return nil, &NavmeshLoadError{Operation: "read compound header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}

	cpd := Compound{
		OffsetNavMeshObj: utils.ByteArrayToUint32(fileContent[12:16]),
		OffsetResObjList: utils.ByteArrayToUint32(fileContent[16:20]),
		Int0:             utils.ByteArrayToUint32(fileContent[20:24]),
		Int1:             utils.ByteArrayToUint32(fileContent[24:28]),
		Int2:             utils.ByteArrayToUint32(fileContent[28:32]),
		Int3:             utils.ByteArrayToUint32(fileContent[32:36]),
		Int4:             utils.ByteArrayToUint32(fileContent[36:40]),
	}

	path, err := readWrapperPath(fileContent, cpd.OffsetNavMeshObj, 40, "compound", filename)
	if err != nil {
		return nil, err
	}
	cpd.NavMeshObjPath = path
	return &cpd, nil
}
