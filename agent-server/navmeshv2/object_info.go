package navmeshv2

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/framework/pk2"
)

const (
	objectIfoHeader       = "JMXVOBJI1000"
	ObjectFilePathDataPk2 = "Data" + string(os.PathSeparator) + "navmesh" + string(os.PathSeparator) + "object.ifo"
)

type ObjectInfoEntry struct {
	Index    uint32
	Flag     uint32
	FilePath string
}

type ObjectInfo struct {
	Objects map[uint32]ObjectInfoEntry
}

func LoadObjectInfo(reader *pk2.Pk2Reader) (ObjectInfo, error) {
	if reader == nil {
		return ObjectInfo{}, &NavmeshLoadError{Operation: "read object info", Path: ObjectFilePathDataPk2, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	objectInfoBytes, err := reader.ReadFile(ObjectFilePathDataPk2)
	if err != nil {
		return ObjectInfo{}, &NavmeshLoadError{Operation: "read object info", Path: ObjectFilePathDataPk2, Err: err}
	}
	return loadObjectInfoContent(ObjectFilePathDataPk2, objectInfoBytes)
}

func loadObjectInfoContent(filename string, objectInfoBytes []byte) (ObjectInfo, error) {
	lines := splitMetadataLines(objectInfoBytes)
	if len(lines) == 0 || string(lines[0]) != objectIfoHeader {
		return ObjectInfo{}, &NavmeshLoadError{Operation: "read object info header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}
	if len(lines) < 2 {
		return ObjectInfo{}, invalidObjectInfo(filename, "entry count is missing")
	}

	entryCount, err := strconv.ParseUint(string(lines[1]), 10, 32)
	if err != nil {
		return ObjectInfo{}, invalidObjectInfo(filename, "invalid entry count %q: %v", lines[1], err)
	}
	availableEntries := len(lines) - 2
	if entryCount > uint64(availableEntries) || entryCount > uint64(math.MaxInt) {
		return ObjectInfo{}, invalidObjectInfo(filename, "entry count %d exceeds %d available lines", entryCount, availableEntries)
	}

	objects := make(map[uint32]ObjectInfoEntry, int(entryCount))
	for i := 0; i < int(entryCount); i++ {
		line := lines[i+2]
		if len(line) < 19 {
			return ObjectInfo{}, invalidObjectInfo(filename, "entry %d has %d bytes, need at least 19", i, len(line))
		}
		index, err := strconv.ParseUint(string(line[:5]), 10, 32)
		if err != nil {
			return ObjectInfo{}, invalidObjectInfo(filename, "invalid index on entry %d: %v", i, err)
		}
		flag, err := strconv.ParseUint(string(line[6:16]), 0, 32)
		if err != nil {
			return ObjectInfo{}, invalidObjectInfo(filename, "invalid flag %q on entry %d: %v", line[6:16], i, err)
		}
		if line[17] != '"' || line[len(line)-1] != '"' {
			return ObjectInfo{}, invalidObjectInfo(filename, "entry %d path is not quoted", i)
		}
		filepath := string(line[18 : len(line)-1])
		if strings.TrimSpace(filepath) == "" {
			return ObjectInfo{}, invalidObjectInfo(filename, "entry %d path is empty", i)
		}
		objectIndex := uint32(index)
		if _, exists := objects[objectIndex]; exists {
			return ObjectInfo{}, invalidObjectInfo(filename, "duplicate object index %d", objectIndex)
		}

		objects[objectIndex] = ObjectInfoEntry{
			Index:    objectIndex,
			Flag:     uint32(flag),
			FilePath: filepath,
		}
	}

	return ObjectInfo{Objects: objects}, nil
}

func invalidObjectInfo(filename, format string, args ...any) error {
	return &NavmeshLoadError{
		Operation: "read object info",
		Path:      filename,
		Err:       fmt.Errorf("%w: %s", ErrInvalidNavmeshData, fmt.Sprintf(format, args...)),
	}
}
