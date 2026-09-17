package navmeshv2

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/Harvi8/AegisCore/framework/pk2"
)

const (
	DungeonInfoEntryPattern    = `^(0|1)\t([0-9]+)\t\"([^\"]+)\"$`
	DungeonInfoFilePathDataPk2 = "Data" + string(os.PathSeparator) + "dungeon" + string(os.PathSeparator) + "Dungeoninfo.txt"
)

type DungeonInfo struct {
	Dungeons []DungeonInfoEntry
}

type DungeonInfoEntry struct {
	Service  int
	ID       int
	FilePath string
}

var entryPattern = regexp.MustCompile(DungeonInfoEntryPattern)

func LoadDungeonInfo(reader *pk2.Pk2Reader) (DungeonInfo, error) {
	if reader == nil {
		return DungeonInfo{}, &NavmeshLoadError{Operation: "read dungeon info", Path: DungeonInfoFilePathDataPk2, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	dungeonInfoBytes, err := reader.ReadFile(DungeonInfoFilePathDataPk2)
	if err != nil {
		return DungeonInfo{}, &NavmeshLoadError{Operation: "read dungeon info", Path: DungeonInfoFilePathDataPk2, Err: err}
	}
	return loadDungeonInfoContent(DungeonInfoFilePathDataPk2, dungeonInfoBytes)
}

func loadDungeonInfoContent(filename string, dungeonInfoBytes []byte) (DungeonInfo, error) {
	lines := splitMetadataLines(dungeonInfoBytes)
	if len(lines) == 0 || len(lines[0]) == 0 {
		return DungeonInfo{}, invalidDungeonInfo(filename, "header line is empty")
	}

	entries := make([]DungeonInfoEntry, 0, len(lines)-1)
	for i, lineBytes := range lines[1:] {
		matches := entryPattern.FindSubmatch(lineBytes)
		if len(matches) != 4 {
			return DungeonInfo{}, invalidDungeonInfo(filename, "entry %d does not match the expected format", i)
		}

		service, err := strconv.Atoi(string(matches[1]))
		if err != nil {
			return DungeonInfo{}, invalidDungeonInfo(filename, "invalid service on entry %d: %v", i, err)
		}
		id, err := strconv.Atoi(string(matches[2]))
		if err != nil {
			return DungeonInfo{}, invalidDungeonInfo(filename, "invalid ID on entry %d: %v", i, err)
		}

		entries = append(entries, DungeonInfoEntry{
			Service:  service,
			ID:       id,
			FilePath: string(matches[3]),
		})
	}

	return DungeonInfo{Dungeons: entries}, nil
}

func invalidDungeonInfo(filename, format string, args ...any) error {
	return &NavmeshLoadError{
		Operation: "read dungeon info",
		Path:      filename,
		Err:       fmt.Errorf("%w: %s", ErrInvalidNavmeshData, fmt.Sprintf(format, args...)),
	}
}
