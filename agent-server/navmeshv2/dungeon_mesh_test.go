package navmeshv2

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
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

func TestActualTombDOFHeadersWhenDataPK2IsAvailable(t *testing.T) {
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
	for _, regionID := range []int16{-32762, -32763, -32764} {
		metadata, ok := loader.DungeonData[regionID]
		if !ok {
			t.Errorf("Tomb region %d was not decoded", regionID)
			continue
		}
		if metadata.Minimum.X >= metadata.Maximum.X || metadata.Minimum.Y >= metadata.Maximum.Y || metadata.Minimum.Z >= metadata.Maximum.Z {
			t.Errorf("Tomb region %d has invalid bounds: minimum=%v maximum=%v", regionID, metadata.Minimum, metadata.Maximum)
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
