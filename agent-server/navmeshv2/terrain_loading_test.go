package navmeshv2

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestLoadTerrainMeshesIsOrderedAndRollsBackBothCaches(t *testing.T) {
	loadErr := errors.New("terrain decode failed")
	originalRegion := NewRtNavmeshTerrain("existing.nvm", NewRegionFromInt16(77))
	originalRegions := map[int16]*RtNavmeshTerrain{77: &originalRegion}
	originalObjects := map[uint32]RtNavmeshObj{88: NewNavmeshObj("existing.bms")}
	loader := &Loader{
		NavMeshPath: "Data" + string(os.PathSeparator) + "navmesh",
		MapProjectInfo: MapProjectInfo{EnabledRegions: map[int16]bool{
			0x0102: true,
			0x0002: true,
			0x0001: false,
		}},
		RegionData: originalRegions,
		ObjectData: originalObjects,
	}
	progress := make(chan int, 2)
	var loaded []int16
	var paths []string

	err := loader.loadTerrainMeshes(progress, func(staged *Loader, filepath string, regionID int16) error {
		loaded = append(loaded, regionID)
		paths = append(paths, filepath)
		terrain := NewRtNavmeshTerrain(filepath, NewRegionFromInt16(regionID))
		staged.RegionData[regionID] = &terrain
		staged.ObjectData[uint32(uint16(regionID))] = NewNavmeshObj(filepath + ".bms")
		if regionID == 0x0102 {
			return loadErr
		}
		return nil
	})

	if !errors.Is(err, loadErr) {
		t.Fatalf("loadTerrainMeshes() error = %v, want terrain decode failure", err)
	}
	if want := []int16{0x0002, 0x0102}; !reflect.DeepEqual(loaded, want) {
		t.Fatalf("load order = %v, want %v", loaded, want)
	}
	wantPaths := []string{
		loader.NavMeshPath + string(os.PathSeparator) + "nv_0002.nvm",
		loader.NavMeshPath + string(os.PathSeparator) + "nv_0102.nvm",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("load paths = %v, want %v", paths, wantPaths)
	}
	if !reflect.DeepEqual(loader.RegionData, originalRegions) {
		t.Fatalf("RegionData changed after failure: %#v", loader.RegionData)
	}
	if !reflect.DeepEqual(loader.ObjectData, originalObjects) {
		t.Fatalf("ObjectData changed after failure: %#v", loader.ObjectData)
	}
	close(progress)
	if got := drainProgress(progress); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("progress = %v, want [1] for the completed region only", got)
	}
}

func TestLoadTerrainMeshesPublishesCompleteRegionSetAndStagedObjects(t *testing.T) {
	staleRegion := NewRtNavmeshTerrain("stale.nvm", NewRegionFromInt16(77))
	loader := &Loader{
		NavMeshPath: "navmesh",
		MapProjectInfo: MapProjectInfo{EnabledRegions: map[int16]bool{
			9: true,
			2: true,
		}},
		RegionData: map[int16]*RtNavmeshTerrain{77: &staleRegion},
		ObjectData: map[uint32]RtNavmeshObj{88: NewNavmeshObj("preloaded.bms")},
	}
	progress := make(chan int, 2)

	err := loader.loadTerrainMeshes(progress, func(staged *Loader, filepath string, regionID int16) error {
		terrain := NewRtNavmeshTerrain(filepath, NewRegionFromInt16(regionID))
		terrain.Cells = []RtNavmeshCellQuad{{RtNavmeshCellBase: RtNavmeshCellBase{Index: 0}}}
		staged.RegionData[regionID] = &terrain
		staged.ObjectData[uint32(regionID)] = NewNavmeshObj(filepath + ".bms")
		return nil
	})
	if err != nil {
		t.Fatalf("loadTerrainMeshes() error = %v", err)
	}
	if len(loader.RegionData) != 2 || loader.RegionData[2] == nil || loader.RegionData[9] == nil {
		t.Fatalf("RegionData = %#v, want regions 2 and 9", loader.RegionData)
	}
	if _, exists := loader.RegionData[77]; exists {
		t.Fatal("RegionData retained stale region 77")
	}
	if len(loader.ObjectData) != 3 || loader.ObjectData[2].Filename == "" || loader.ObjectData[9].Filename == "" || loader.ObjectData[88].Filename != "preloaded.bms" {
		t.Fatalf("ObjectData = %#v, want staged objects plus preloaded object", loader.ObjectData)
	}
	close(progress)
	if got := drainProgress(progress); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("progress = %v, want [1 2]", got)
	}
}

func TestLoadTerrainMeshTransactionRollsBackLazyObjectsOnFailure(t *testing.T) {
	loadErr := errors.New("invalid terrain tail")
	originalRegion := NewRtNavmeshTerrain("existing.nvm", NewRegionFromInt16(77))
	originalRegions := map[int16]*RtNavmeshTerrain{77: &originalRegion}
	originalObjects := map[uint32]RtNavmeshObj{88: NewNavmeshObj("existing.bms")}
	loader := &Loader{RegionData: originalRegions, ObjectData: originalObjects}

	err := loader.loadTerrainMeshTransaction("new.nvm", 2, func(staged *Loader, filepath string, regionID int16) error {
		terrain := NewRtNavmeshTerrain(filepath, NewRegionFromInt16(regionID))
		terrain.Cells = []RtNavmeshCellQuad{{RtNavmeshCellBase: RtNavmeshCellBase{Index: 0}}}
		staged.RegionData[regionID] = &terrain
		staged.ObjectData[2] = NewNavmeshObj("lazy.bms")
		return loadErr
	})

	if !errors.Is(err, loadErr) {
		t.Fatalf("loadTerrainMeshTransaction() error = %v, want terrain failure", err)
	}
	if !reflect.DeepEqual(loader.RegionData, originalRegions) {
		t.Fatalf("RegionData changed after failure: %#v", loader.RegionData)
	}
	if !reflect.DeepEqual(loader.ObjectData, originalObjects) {
		t.Fatalf("ObjectData changed after failure: %#v", loader.ObjectData)
	}
}

func TestLoadTerrainMeshTransactionCommitsBothCaches(t *testing.T) {
	loader := &Loader{}
	err := loader.loadTerrainMeshTransaction("new.nvm", 2, func(staged *Loader, filepath string, regionID int16) error {
		terrain := NewRtNavmeshTerrain(filepath, NewRegionFromInt16(regionID))
		terrain.Cells = []RtNavmeshCellQuad{{RtNavmeshCellBase: RtNavmeshCellBase{Index: 0}}}
		staged.RegionData[regionID] = &terrain
		staged.ObjectData[2] = NewNavmeshObj("lazy.bms")
		return nil
	})
	if err != nil {
		t.Fatalf("loadTerrainMeshTransaction() error = %v", err)
	}
	if loader.RegionData[2] == nil || loader.RegionData[2].Filename != "new.nvm" {
		t.Fatalf("RegionData = %#v, want committed terrain", loader.RegionData)
	}
	if loader.ObjectData[2].Filename != "lazy.bms" {
		t.Fatalf("ObjectData = %#v, want committed lazy object", loader.ObjectData)
	}
}

func TestTerrainLoadingRejectsNilDependencies(t *testing.T) {
	var loader *Loader
	if err := loader.LoadTerrainMeshes(nil); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadTerrainMeshes() error = %v, want ErrInvalidNavmeshData", err)
	}
	if err := loader.LoadTerrainMesh("missing.nvm", 1); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadTerrainMesh() error = %v, want ErrInvalidNavmeshData", err)
	}

	loader = &Loader{}
	if err := loader.LoadTerrainMesh("missing.nvm", 1); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadTerrainMesh() nil reader error = %v, want ErrInvalidNavmeshData", err)
	}
	if err := loader.loadTerrainMeshes(nil, nil); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("loadTerrainMeshes() nil decoder error = %v, want ErrInvalidNavmeshData", err)
	}
	if err := loader.loadTerrainMeshTransaction("missing.nvm", 1, nil); !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("loadTerrainMeshTransaction() nil decoder error = %v, want ErrInvalidNavmeshData", err)
	}
}

func drainProgress(progress <-chan int) []int {
	var values []int
	for value := range progress {
		values = append(values, value)
	}
	return values
}
