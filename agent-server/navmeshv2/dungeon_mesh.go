package navmeshv2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/Harvi8/AegisCore/framework/pk2"
	"github.com/g3n/engine/math32"
)

const (
	dungeonDOFHeaderSize = 0x60
	dungeonDOFRegionOff  = 0x42
	dungeonDOFBoundsOff  = 0x44
)

var dungeonDOFSignature = []byte("JMXVDOF 0101")
var errNonInstanceDungeonMesh = errors.New("DOF is not a negative-region dungeon instance")

// DungeonMeshMetadata is the authoritative addressing/bounds prefix shared by
// Joymax DOF 0101 files. Collision blocks are decoded separately.
type DungeonMeshMetadata struct {
	ID       int
	FilePath string
	Region   Region
	Minimum  math32.Vector3
	Maximum  math32.Vector3
}

func (l *Loader) LoadDungeonMeshes() error {
	if l == nil || l.Pk2Reader == nil {
		return &NavmeshLoadError{Operation: "load dungeon meshes", Err: fmt.Errorf("%w: loader archive is unavailable", ErrInvalidNavmeshData)}
	}
	loaded := make(map[int16]DungeonMeshMetadata)
	for _, entry := range l.DungeonInfo.Dungeons {
		if entry.Service == 0 {
			continue
		}
		archivePath := l.DataPk2Path + string(os.PathSeparator) + strings.ReplaceAll(entry.FilePath, "\\", string(os.PathSeparator))
		archivePath = resolvePK2PathCaseInsensitive(l.Pk2Reader.Files, archivePath)
		contents, err := l.Pk2Reader.ReadFile(archivePath)
		if err != nil {
			return &NavmeshLoadError{Operation: "read dungeon mesh", Path: archivePath, Err: err}
		}
		metadata, err := decodeDungeonMeshMetadata(entry.ID, archivePath, contents)
		if errors.Is(err, errNonInstanceDungeonMesh) {
			continue
		}
		if err != nil {
			return err
		}
		if existing, duplicate := loaded[metadata.Region.ID]; duplicate {
			loaded[metadata.Region.ID] = mergeDungeonMeshBounds(existing, metadata)
			continue
		}
		loaded[metadata.Region.ID] = metadata
	}
	l.DungeonData = loaded
	return nil
}
func mergeDungeonMeshBounds(existing, additional DungeonMeshMetadata) DungeonMeshMetadata {
	if additional.Minimum.X < existing.Minimum.X {
		existing.Minimum.X = additional.Minimum.X
	}
	if additional.Minimum.Y < existing.Minimum.Y {
		existing.Minimum.Y = additional.Minimum.Y
	}
	if additional.Minimum.Z < existing.Minimum.Z {
		existing.Minimum.Z = additional.Minimum.Z
	}
	if additional.Maximum.X > existing.Maximum.X {
		existing.Maximum.X = additional.Maximum.X
	}
	if additional.Maximum.Y > existing.Maximum.Y {
		existing.Maximum.Y = additional.Maximum.Y
	}
	if additional.Maximum.Z > existing.Maximum.Z {
		existing.Maximum.Z = additional.Maximum.Z
	}
	return existing
}

func resolvePK2PathCaseInsensitive(files map[string]pk2.PackFileEntry, requested string) string {
	normalized := strings.NewReplacer("\\", string(os.PathSeparator), "/", string(os.PathSeparator)).Replace(requested)
	if _, ok := files[normalized]; ok {
		return normalized
	}
	for indexed := range files {
		if strings.EqualFold(indexed, normalized) {
			return indexed
		}
	}
	return normalized
}

// AdmitDungeonRegions publishes bounded DOF regions into the same immutable
// region catalog used by world movement. Detailed AI collision samples can be
// attached to the single root cell as the native AINavData decoder advances;
// bounds remain authoritative immediately and prevent cross-dungeon escape.
func (l *Loader) AdmitDungeonRegions() error {
	if l == nil {
		return &NavmeshLoadError{Operation: "admit dungeon regions", Err: fmt.Errorf("%w: loader is nil", ErrInvalidNavmeshData)}
	}
	if l.RegionData == nil {
		l.RegionData = make(map[int16]*RtNavmeshTerrain)
	}
	for regionID, metadata := range l.DungeonData {
		if _, exists := l.RegionData[regionID]; exists {
			return invalidDungeonInfo(metadata.FilePath, "dungeon region %d conflicts with terrain", regionID)
		}
		terrain := NewRtNavmeshTerrain(metadata.FilePath, metadata.Region)
		minimum, maximum := metadata.Minimum, metadata.Maximum
		terrain.DungeonMinimum = minimum.Clone()
		terrain.DungeonMaximum = maximum.Clone()
		terrain.Cells = []RtNavmeshCellQuad{{RtNavmeshCellBase: RtNavmeshCellBase{Index: 0}}}
		terrain.Cells[0].Mesh = &terrain
		l.RegionData[regionID] = &terrain
	}
	return nil
}

func decodeDungeonMeshMetadata(id int, path string, contents []byte) (DungeonMeshMetadata, error) {
	if len(contents) < dungeonDOFHeaderSize || string(contents[:len(dungeonDOFSignature)]) != string(dungeonDOFSignature) {
		return DungeonMeshMetadata{}, &NavmeshLoadError{Operation: "decode dungeon mesh header", Path: path, Err: fmt.Errorf("%w: unsupported DOF header", ErrInvalidNavmeshData)}
	}
	readFloat := func(offset int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(contents[offset:])) }
	metadata := DungeonMeshMetadata{
		ID: id, FilePath: path,
		Region:  NewRegionFromUint16(binary.LittleEndian.Uint16(contents[dungeonDOFRegionOff:])),
		Minimum: *math32.NewVector3(readFloat(dungeonDOFBoundsOff), readFloat(dungeonDOFBoundsOff+4), readFloat(dungeonDOFBoundsOff+8)),
		Maximum: *math32.NewVector3(readFloat(dungeonDOFBoundsOff+12), readFloat(dungeonDOFBoundsOff+16), readFloat(dungeonDOFBoundsOff+20)),
	}
	if metadata.Region.ID >= 0 {
		return DungeonMeshMetadata{}, &NavmeshLoadError{Operation: "decode dungeon mesh header", Path: path, Err: errNonInstanceDungeonMesh}
	}
	if metadata.Minimum.X > metadata.Maximum.X || metadata.Minimum.Y > metadata.Maximum.Y || metadata.Minimum.Z > metadata.Maximum.Z {
		return DungeonMeshMetadata{}, &NavmeshLoadError{Operation: "decode dungeon mesh header", Path: path, Err: fmt.Errorf("%w: invalid dungeon region or bounds", ErrInvalidNavmeshData)}
	}
	return metadata, nil
}
