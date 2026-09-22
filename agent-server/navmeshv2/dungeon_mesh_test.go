package navmeshv2

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/g3n/engine/math32"
)

func TestDecodeDungeonMeshMetadataReadsSignedRegionAndBounds(t *testing.T) {
	data := make([]byte, dungeonDOFHeaderSize)
	copy(data, dungeonDOFSignature)
	binary.LittleEndian.PutUint16(data[dungeonDOFRegionOff:], 0x8006)
	values := []float32{-11719, -47, 5742, -11000, 72, 6000}
	for i, value := range values {
		binary.LittleEndian.PutUint32(data[dungeonDOFBoundsOff+i*4:], math.Float32bits(value))
	}
	got, err := decodeDungeonMeshMetadata(6, "jinsi_floor02.dof", data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Region.ID != -32762 || got.Minimum.X != -11719 || got.Maximum.Z != 6000 {
		t.Fatalf("metadata = %+v", got)
	}
}
func TestMergeDungeonMeshBoundsEnclosesSharedRegionDOFs(t *testing.T) {
	existing := DungeonMeshMetadata{
		ID: 1, Region: NewRegionFromInt16(-32762),
		Minimum: *vector(-100, -20, 30), Maximum: *vector(40, 50, 60),
	}
	additional := DungeonMeshMetadata{
		ID: 2, Region: NewRegionFromInt16(-32762),
		Minimum: *vector(-120, -10, 20), Maximum: *vector(30, 70, 80),
	}
	got := mergeDungeonMeshBounds(existing, additional)
	if got.Minimum != *vector(-120, -20, 20) || got.Maximum != *vector(40, 70, 80) {
		t.Fatalf("merged bounds = %v..%v", got.Minimum, got.Maximum)
	}
}

func TestActualEnabledDungeonDOFHeadersWhenDataPK2IsAvailable(t *testing.T) {
	dataPK2 := filepath.Join("..", "..", "Data", "Data.pk2")
	if _, err := os.Stat(dataPK2); err != nil {
		t.Skip("local Data.pk2 is not available")
	}
	loader, err := NewLoaderSafe(dataPK2)
	if err != nil {
		t.Fatal(err)
	}
	defer loader.CloseArchive()
	loader.DungeonInfo, err = LoadDungeonInfo(loader.Pk2Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.LoadDungeonMeshes(); err != nil {
		t.Fatal(err)
	}
	for _, entry := range loader.DungeonInfo.Dungeons {
		if entry.Service == 0 {
			continue
		}
		archivePath := loader.DataPk2Path + string(os.PathSeparator) + strings.ReplaceAll(entry.FilePath, "\\", string(os.PathSeparator))
		archivePath = resolvePK2PathCaseInsensitive(loader.Pk2Reader.Files, archivePath)
		contents, readErr := loader.Pk2Reader.ReadFile(archivePath)
		if readErr != nil {
			t.Fatalf("read enabled DOF %q: %v", entry.FilePath, readErr)
		}
		metadata, decodeErr := decodeDungeonMeshMetadata(entry.ID, archivePath, contents)
		if errors.Is(decodeErr, errNonInstanceDungeonMesh) {
			continue
		}
		if decodeErr != nil {
			t.Fatalf("decode enabled DOF %q: %v", entry.FilePath, decodeErr)
		}
		admitted, ok := loader.DungeonData[metadata.Region.ID]
		if !ok {
			t.Errorf("enabled DOF %q region %d was not admitted", entry.FilePath, metadata.Region.ID)
			continue
		}
		if admitted.Minimum.X > metadata.Minimum.X || admitted.Minimum.Y > metadata.Minimum.Y || admitted.Minimum.Z > metadata.Minimum.Z ||
			admitted.Maximum.X < metadata.Maximum.X || admitted.Maximum.Y < metadata.Maximum.Y || admitted.Maximum.Z < metadata.Maximum.Z {
			t.Errorf("enabled DOF %q bounds %v..%v are not enclosed by admitted region %v..%v", entry.FilePath, metadata.Minimum, metadata.Maximum, admitted.Minimum, admitted.Maximum)
		}
	}
	for _, regionID := range []int16{-32767, -32762, -32763, -32764} {
		metadata, ok := loader.DungeonData[regionID]
		if !ok {
			t.Errorf("fixed dungeon region %d was not decoded", regionID)
			continue
		}
		if metadata.Minimum.X >= metadata.Maximum.X || metadata.Minimum.Y >= metadata.Maximum.Y || metadata.Minimum.Z >= metadata.Maximum.Z {
			t.Errorf("fixed dungeon region %d has invalid bounds: minimum=%v maximum=%v", regionID, metadata.Minimum, metadata.Maximum)
		}
	}
	cave := loader.DungeonData[-32767]
	for name, position := range map[string]*math32.Vector3{
		"event-zone arrival":     vector(1011, 0, -862),
		"reverse-return arrival": vector(1115, 0, -935),
	} {
		if position.X < cave.Minimum.X || position.X > cave.Maximum.X ||
			position.Y < cave.Minimum.Y || position.Y > cave.Maximum.Y ||
			position.Z < cave.Minimum.Z || position.Z > cave.Maximum.Z {
			t.Errorf("Stone Cave %s %v is outside DOF bounds %v..%v", name, position, cave.Minimum, cave.Maximum)
		}
	}
}

func TestAdmitDungeonRegionResolvesLocalNegativeCoordinates(t *testing.T) {
	loader := &Loader{
		RegionData: make(map[int16]*RtNavmeshTerrain),
		DungeonData: map[int16]DungeonMeshMetadata{
			-32762: {ID: 6, Region: NewRegionFromInt16(-32762), Minimum: *vector(-12000, -100, 5000), Maximum: *vector(-11000, 100, 6000)},
		},
	}
	if err := loader.AdmitDungeonRegions(); err != nil {
		t.Fatal(err)
	}
	terrain := loader.RegionData[-32762]
	if terrain == nil {
		t.Fatal("dungeon region was not admitted")
	}
	if _, err := terrain.ResolveCell(vector(-11719, -47, 5742)); err != nil {
		t.Fatalf("resolve valid dungeon position: %v", err)
	}
	if _, err := terrain.ResolveCell(vector(0, 0, 0)); err == nil {
		t.Fatal("out-of-bounds dungeon position resolved")
	}
}

func vector(x, y, z float32) *math32.Vector3 { return math32.NewVector3(x, y, z) }
