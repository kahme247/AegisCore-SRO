package service

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/protocol/profiles"
)

func TestWorldBootstrapUsesValidatedV2CacheWithoutOpeningPK2(t *testing.T) {
	cachePrefix := t.TempDir() + "\\prelinked_navdata.gob"
	cacheWriter := &navmeshv2.Loader{
		RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{
			7: {RtNavmeshBase: navmeshv2.RtNavmeshBase{Filename: "cached-region"}},
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
