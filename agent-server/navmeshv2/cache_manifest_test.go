package navmeshv2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/g3n/engine/math32"
)

func TestCacheManifestRoundTripAndArtifactValidation(t *testing.T) {
	cacheDir := t.TempDir()
	regionPath := filepath.Join(cacheDir, "regions.gob")
	objectPath := filepath.Join(cacheDir, "objects.gob")
	if err := os.WriteFile(regionPath, []byte("region-cache"), 0o600); err != nil {
		t.Fatalf("write region cache: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("object-cache"), 0o600); err != nil {
		t.Fatalf("write object cache: %v", err)
	}

	archiveSHA := strings.Repeat("a", 64)
	manifest, err := BuildCacheManifest(context.Background(), regionPath, objectPath, NavmeshCacheProfile, archiveSHA)
	if err != nil {
		t.Fatalf("BuildCacheManifest() error = %v", err)
	}
	manifestPath := filepath.Join(cacheDir, "cache_manifest.json")
	if err := WriteCacheManifestAtomic(manifestPath, manifest); err != nil {
		t.Fatalf("WriteCacheManifestAtomic() error = %v", err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest was not published: %v", err)
	}

	validated, err := ValidateCacheManifest(context.Background(), manifestPath, NavmeshCacheProfile, archiveSHA)
	if err != nil {
		t.Fatalf("ValidateCacheManifest() error = %v", err)
	}
	if validated.RegionData.Path != "regions.gob" || validated.ObjectData.Path != "objects.gob" {
		t.Fatalf("validated paths = %#v, want relative artifact names", validated)
	}

	if err := os.WriteFile(regionPath, []byte("region-cachX"), 0o600); err != nil {
		t.Fatalf("tamper region cache: %v", err)
	}
	if _, err := ValidateCacheManifest(context.Background(), manifestPath, NavmeshCacheProfile, archiveSHA); !errors.Is(err, ErrNavmeshCacheChecksum) {
		t.Fatalf("tampered cache error = %v, want checksum error", err)
	}
}

func TestCacheManifestRejectsProfileArchiveAndPathMismatch(t *testing.T) {
	manifest := CacheManifest{
		SchemaVersion: NavmeshCacheManifestSchemaVersion,
		Profile:       NavmeshCacheProfile,
		ArchiveSHA256: strings.Repeat("a", 64),
		RegionData: CacheFile{
			Path:      "../regions.gob",
			SizeBytes: 1,
			SHA256:    strings.Repeat("b", 64),
		},
		ObjectData: CacheFile{
			Path:      "objects.gob",
			SizeBytes: 1,
			SHA256:    strings.Repeat("c", 64),
		},
	}
	if _, err := manifest.CanonicalJSON(); !errors.Is(err, ErrNavmeshCachePath) {
		t.Fatalf("path traversal error = %v, want cache path error", err)
	}

	directory := t.TempDir()
	regionPath := filepath.Join(directory, "regions.gob")
	objectPath := filepath.Join(directory, "objects.gob")
	if err := os.WriteFile(regionPath, []byte("r"), 0o600); err != nil {
		t.Fatalf("write region cache: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("o"), 0o600); err != nil {
		t.Fatalf("write object cache: %v", err)
	}
	built, err := BuildCacheManifest(context.Background(), regionPath, objectPath, NavmeshCacheProfile, strings.Repeat("d", 64))
	if err != nil {
		t.Fatalf("BuildCacheManifest() error = %v", err)
	}
	manifestPath := filepath.Join(directory, "cache_manifest.json")
	if err := WriteCacheManifestAtomic(manifestPath, built); err != nil {
		t.Fatalf("WriteCacheManifestAtomic() error = %v", err)
	}
	if _, err := ValidateCacheManifest(context.Background(), manifestPath, "other-profile", strings.Repeat("d", 64)); !errors.Is(err, ErrNavmeshCacheProfile) {
		t.Fatalf("profile mismatch error = %v, want profile error", err)
	}
	if _, err := ValidateCacheManifest(context.Background(), manifestPath, NavmeshCacheProfile, strings.Repeat("e", 64)); !errors.Is(err, ErrNavmeshCacheArchive) {
		t.Fatalf("archive mismatch error = %v, want archive error", err)
	}
}

func TestReadCacheManifestRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "cache_manifest.json")
	validFields := `{"schema_version":3,"profile":"vsro188","archive_sha256":"` + strings.Repeat("a", 64) + `","region_data":{"path":"regions.gob","size_bytes":1,"sha256":"` + strings.Repeat("b", 64) + `"},"object_data":{"path":"objects.gob","size_bytes":1,"sha256":"` + strings.Repeat("c", 64) + `"}}`
	for name, content := range map[string]string{
		"unknown field":  validFields[:len(validFields)-1] + `,"unexpected":true}`,
		"trailing value": validFields + ` {}`,
	} {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(manifestPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			if _, err := ReadCacheManifest(manifestPath); !errors.Is(err, ErrNavmeshCacheManifest) {
				t.Fatalf("ReadCacheManifest() error = %v, want manifest error", err)
			}
		})
	}
}

func TestSaveAndLoadNavmeshCacheUsesDistinctObjectArtifact(t *testing.T) {
	cachePrefix := filepath.Join(t.TempDir(), "prelinked_navdata.gob")
	terrain := NewRtNavmeshTerrain("region", NewRegionFromInt16(1))
	terrain.Cells = []RtNavmeshCellQuad{{RtNavmeshCellBase: RtNavmeshCellBase{Index: 0}}}
	loader := &Loader{
		RegionData: map[int16]*RtNavmeshTerrain{
			1: &terrain,
		},
		ObjectData: map[uint32]RtNavmeshObj{
			7: {RtNavmeshBase: RtNavmeshBase{Filename: "object"}},
		},
	}
	archiveSHA := strings.Repeat("f", 64)
	if err := loader.SaveNavmeshDataAsGOBWithManifest(context.Background(), cachePrefix, NavmeshCacheProfile, archiveSHA); err != nil {
		t.Fatalf("SaveNavmeshDataAsGOBWithManifest() error = %v", err)
	}
	if _, err := ValidateCacheManifest(context.Background(), CacheManifestPath(cachePrefix), NavmeshCacheProfile, archiveSHA); err != nil {
		t.Fatalf("ValidateCacheManifest() error = %v", err)
	}

	loaded := &Loader{}
	if err := loaded.LoadPrecomputedNavmeshDataFromGOB(cachePrefix); err != nil {
		t.Fatalf("LoadPrecomputedNavmeshDataFromGOB() error = %v", err)
	}
	if loaded.RegionData[1].Filename != "region" {
		t.Fatalf("loaded region = %#v, want region artifact", loaded.RegionData[1])
	}
	if loaded.ObjectData[7].Filename != "object" {
		t.Fatalf("loaded object = %#v, want object artifact", loaded.ObjectData[7])
	}
}

func TestLoadPrecomputedNavmeshDataReturnsMissingFileError(t *testing.T) {
	err := (&Loader{}).LoadPrecomputedNavmeshDataFromGOB(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing cache error = %v, want not-exist error", err)
	}
}

func TestLoadPrecomputedNavmeshDataRejectsInvalidLinksWithoutPublishing(t *testing.T) {
	cachePrefix := filepath.Join(t.TempDir(), "invalid_navdata.gob")
	terrain := NewRtNavmeshTerrain("invalid.nvm", NewRegionFromInt16(1))
	terrain.Cells = []RtNavmeshCellQuad{{RtNavmeshCellBase: RtNavmeshCellBase{Index: 0}}}
	terrain.Objects = []RtNavmeshInstObj{{
		RtNavmeshInstBase: RtNavmeshInstBase{Object: RtNavmeshObj{GlobalEdges: make([]RtNavmeshEdgeGlobal, 1)}},
		EdgeLinks:         []RtNavmeshEdgeLink{{LinkedObjID: 1, LinkedObjEdgeID: 0, EdgeID: 0}},
	}}
	if err := writeGobAtomically(cachePrefix+"_regions", map[int16]*RtNavmeshTerrain{1: &terrain}); err != nil {
		t.Fatalf("write region cache: %v", err)
	}
	if err := writeGobAtomically(cachePrefix+"_objects", map[uint32]RtNavmeshObj{}); err != nil {
		t.Fatalf("write object cache: %v", err)
	}

	originalTerrain := NewRtNavmeshTerrain("original.nvm", NewRegionFromInt16(7))
	originalRegions := map[int16]*RtNavmeshTerrain{7: &originalTerrain}
	originalObjects := map[uint32]RtNavmeshObj{8: NewNavmeshObj("original.bms")}
	loader := &Loader{RegionData: originalRegions, ObjectData: originalObjects}
	err := loader.LoadPrecomputedNavmeshDataFromGOB(cachePrefix)
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("LoadPrecomputedNavmeshDataFromGOB() error = %v, want ErrInvalidNavmeshData", err)
	}
	if !reflect.DeepEqual(loader.RegionData, originalRegions) {
		t.Fatalf("RegionData changed after invalid cache: %#v", loader.RegionData)
	}
	if !reflect.DeepEqual(loader.ObjectData, originalObjects) {
		t.Fatalf("ObjectData changed after invalid cache: %#v", loader.ObjectData)
	}
}

func TestSaveNavmeshDataRejectsInvalidRegionBeforeWriting(t *testing.T) {
	cachePrefix := filepath.Join(t.TempDir(), "invalid_navdata.gob")
	loader := &Loader{RegionData: map[int16]*RtNavmeshTerrain{1: nil}}

	err := loader.saveNavmeshDataAsGOB(cachePrefix)
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("saveNavmeshDataAsGOB() error = %v, want ErrInvalidNavmeshData", err)
	}
	for _, artifact := range []string{cachePrefix + "_regions", cachePrefix + "_objects"} {
		if _, statErr := os.Stat(artifact); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("cache artifact %q exists after validation failure: %v", artifact, statErr)
		}
	}
}

func TestValidateCachedRegionDataRejectsRegionIdentityMismatch(t *testing.T) {
	terrain := NewRtNavmeshTerrain("mismatch.nvm", NewRegionFromInt16(2))
	err := validateCachedRegionData(map[int16]*RtNavmeshTerrain{1: &terrain})
	if !errors.Is(err, ErrInvalidNavmeshData) {
		t.Fatalf("validateCachedRegionData() error = %v, want ErrInvalidNavmeshData", err)
	}
	var loadErr *NavmeshLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("validateCachedRegionData() error type = %T, want *NavmeshLoadError", err)
	}
}

func TestSaveAndLoadLinkedTerrainCacheRebuildsRuntimeEdges(t *testing.T) {
	cachePrefix := filepath.Join(t.TempDir(), "linked_navdata.gob")
	terrain := newTerrainEdgeTestTerrain(1, 1)
	terrain.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Index: 0, Flag: RtNavmeshEdgeFlag((1 << 0) | (1 << 2)),
		SrcCellIndex: 0, DstCellIndex: 65535,
		SrcDirection: 0, DstDirection: -1,
		Line: LineSegment{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(1, 0, 0),
		},
	}}}
	regions := map[int16]*RtNavmeshTerrain{1: terrain}
	if err := linkTerrainEdges(regions, true); err != nil {
		t.Fatalf("linkTerrainEdges() error = %v", err)
	}
	loader := &Loader{RegionData: regions, ObjectData: make(map[uint32]RtNavmeshObj)}
	if err := loader.saveNavmeshDataAsGOB(cachePrefix); err != nil {
		t.Fatalf("saveNavmeshDataAsGOB() error = %v", err)
	}

	loaded := &Loader{}
	if err := loaded.LoadPrecomputedNavmeshDataFromGOB(cachePrefix); err != nil {
		t.Fatalf("LoadPrecomputedNavmeshDataFromGOB() error = %v", err)
	}
	if got := len(loaded.RegionData[1].Cells[0].Edges); got != 1 {
		t.Fatalf("loaded cell edge count = %d, want 1", got)
	}
}
