package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/protocol/profiles"
)

func TestWorldBootstrapUsesValidatedV2CacheWithoutOpeningPK2(t *testing.T) {
	cachePrefix := t.TempDir() + "\\prelinked_navdata.gob"
	terrain := navmeshv2.NewRtNavmeshTerrain("cached-region", navmeshv2.NewRegionFromInt16(7))
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0}}}
	cacheWriter := &navmeshv2.Loader{
		RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{
			7: &terrain,
		},
		ObjectData: make(map[uint32]navmeshv2.RtNavmeshObj),
	}
	canonicalProfile, err := profiles.VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}
	if err := cacheWriter.SaveNavmeshDataAsGOBWithManifest(context.Background(), cachePrefix, navmeshv2.NavmeshCacheProfile, canonicalProfile.DataPK2.SHA256); err != nil {
		t.Fatalf("SaveNavmeshDataAsGOBWithManifest() error = %v", err)
	}

	world := newWorldRepositoryWiringTestWorld()
	world.navmeshGobPath = cachePrefix
	world.loader2 = nil
	regions := &worldWiringRegionCatalog{regions: []model.GameServerRegion{{ContinentName: "Test", Regions: []int16{7}}}}
	spawns := &worldWiringSpawnCatalog{}
	positions := &worldWiringPositionRepository{}
	if err := world.ConfigureRepositories(configuredWorldRepositories(regions, spawns, positions)); err != nil {
		t.Fatalf("ConfigureRepositories() error = %v", err)
	}

	if err := world.BootstrapGameServerRegionsContext(context.Background(), 1); err != nil {
		t.Fatalf("BootstrapGameServerRegionsContext() error = %v", err)
	}
	loaded, err := world.lookupRegionPtr(7)
	if err != nil {
		t.Fatalf("lookup loaded region: %v", err)
	}
	if loaded.Filename != "cached-region" {
		t.Fatalf("loaded region = %#v, want validated cache data", loaded)
	}
	if world.loader2 == nil || world.loader2.Pk2Reader != nil {
		t.Fatalf("cache bootstrap opened PK2 reader: loader=%#v", world.loader2)
	}
}

func TestWorldBootstrapDoesNotPublishFailedCacheLoader(t *testing.T) {
	cachePrefix := t.TempDir() + "\\invalid_navdata.gob"
	terrain := navmeshv2.NewRtNavmeshTerrain("cached-region", navmeshv2.NewRegionFromInt16(7))
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0}}}
	cacheWriter := &navmeshv2.Loader{
		RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{7: &terrain},
		ObjectData: make(map[uint32]navmeshv2.RtNavmeshObj),
	}
	canonicalProfile, err := profiles.VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}
	if err := cacheWriter.SaveNavmeshDataAsGOBWithManifest(context.Background(), cachePrefix, navmeshv2.NavmeshCacheProfile, canonicalProfile.DataPK2.SHA256); err != nil {
		t.Fatalf("SaveNavmeshDataAsGOBWithManifest() error = %v", err)
	}
	if err := os.WriteFile(cachePrefix+"_objects", []byte("not a navmesh cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := navmeshv2.BuildCacheManifest(context.Background(), cachePrefix+"_regions", cachePrefix+"_objects", navmeshv2.NavmeshCacheProfile, canonicalProfile.DataPK2.SHA256)
	if err != nil {
		t.Fatalf("BuildCacheManifest() error = %v", err)
	}
	if err := navmeshv2.WriteCacheManifestAtomic(navmeshv2.CacheManifestPath(cachePrefix), manifest); err != nil {
		t.Fatalf("WriteCacheManifestAtomic() error = %v", err)
	}

	world := newWorldRepositoryWiringTestWorld()
	world.navmeshGobPath = cachePrefix
	regions := &worldWiringRegionCatalog{regions: []model.GameServerRegion{{ContinentName: "Test", Regions: []int16{7}}}}
	if err := world.ConfigureRepositories(configuredWorldRepositories(regions, &worldWiringSpawnCatalog{}, &worldWiringPositionRepository{})); err != nil {
		t.Fatalf("ConfigureRepositories() error = %v", err)
	}

	err = world.BootstrapGameServerRegionsContext(context.Background(), 1)
	if !errors.Is(err, ErrNavmeshCacheInvalid) {
		t.Fatalf("BootstrapGameServerRegionsContext() error = %v, want ErrNavmeshCacheInvalid", err)
	}
	if world.loader2 != nil {
		t.Fatalf("failed cache bootstrap published loader %#v", world.loader2)
	}
	if len(world.regions) != 0 {
		t.Fatalf("failed cache bootstrap published %d regions", len(world.regions))
	}
}

func TestWorldBootstrapDoesNotPublishLoaderBeforePartitionAdmission(t *testing.T) {
	cachePrefix := t.TempDir() + "\\incomplete_navdata.gob"
	terrain := navmeshv2.NewRtNavmeshTerrain("cached-region", navmeshv2.NewRegionFromInt16(7))
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0}}}
	cacheWriter := &navmeshv2.Loader{
		RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{7: &terrain},
		ObjectData: make(map[uint32]navmeshv2.RtNavmeshObj),
	}
	canonicalProfile, err := profiles.VSRO188Profile()
	if err != nil {
		t.Fatalf("VSRO188Profile() error = %v", err)
	}
	if err := cacheWriter.SaveNavmeshDataAsGOBWithManifest(context.Background(), cachePrefix, navmeshv2.NavmeshCacheProfile, canonicalProfile.DataPK2.SHA256); err != nil {
		t.Fatalf("SaveNavmeshDataAsGOBWithManifest() error = %v", err)
	}

	world := newWorldRepositoryWiringTestWorld()
	world.navmeshGobPath = cachePrefix
	regions := &worldWiringRegionCatalog{regions: []model.GameServerRegion{{ContinentName: "Test", Regions: []int16{8}}}}
	if err := world.ConfigureRepositories(configuredWorldRepositories(regions, &worldWiringSpawnCatalog{}, &worldWiringPositionRepository{})); err != nil {
		t.Fatalf("ConfigureRepositories() error = %v", err)
	}

	if err := world.BootstrapGameServerRegionsContext(context.Background(), 1); err == nil {
		t.Fatal("BootstrapGameServerRegionsContext() unexpectedly admitted a missing region")
	}
	if world.loader2 != nil {
		t.Fatalf("failed partition admission published loader %#v", world.loader2)
	}
	if len(world.regions) != 0 {
		t.Fatalf("failed partition admission published %d regions", len(world.regions))
	}
}
