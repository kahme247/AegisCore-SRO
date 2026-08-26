package service

import (
	"errors"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

func TestValidateWorldPartitionAcceptsUniqueOwnership(t *testing.T) {
	partition, err := validateWorldPartition([]model.GameServerRegion{
		{ContinentName: "West", Regions: []int16{9, 7}},
		{ContinentName: "East", Regions: []int16{8}},
	})
	if err != nil {
		t.Fatalf("validateWorldPartition() error = %v", err)
	}
	if len(partition.regions) != 3 || partition.regions[0].id != 7 || partition.regions[1].id != 8 || partition.regions[2].id != 9 {
		t.Fatalf("partition regions = %#v, want deterministic IDs [7 8 9]", partition.regions)
	}
}

func TestValidateWorldPartitionRejectsDuplicateWithinContinent(t *testing.T) {
	_, err := validateWorldPartition([]model.GameServerRegion{{ContinentName: "West", Regions: []int16{7, 7}}})
	if !errors.Is(err, ErrWorldPartitionInvalid) {
		t.Fatalf("validateWorldPartition() error = %v, want %v", err, ErrWorldPartitionInvalid)
	}
}

func TestValidateWorldPartitionRejectsDuplicateAcrossContinents(t *testing.T) {
	_, err := validateWorldPartition([]model.GameServerRegion{
		{ContinentName: "West", Regions: []int16{7}},
		{ContinentName: "East", Regions: []int16{7}},
	})
	if !errors.Is(err, ErrWorldPartitionInvalid) {
		t.Fatalf("validateWorldPartition() error = %v, want %v", err, ErrWorldPartitionInvalid)
	}
}

func TestValidateWorldPartitionRejectsUnsupportedDungeon(t *testing.T) {
	_, err := validateWorldPartition([]model.GameServerRegion{{ContinentName: "Dungeon", Regions: []int16{-1}}})
	if !errors.Is(err, ErrWorldPartitionInvalid) {
		t.Fatalf("validateWorldPartition() error = %v, want %v", err, ErrWorldPartitionInvalid)
	}
}

func TestAddRegionsMissingTerrainDoesNotPartiallyMutateWorld(t *testing.T) {
	world := &WorldService{
		mutex: &sync.Mutex{},
		loader2: &navmeshv2.Loader{RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{
			7: {RtNavmeshBase: navmeshv2.RtNavmeshBase{Filename: "seven"}},
		}},
		regions:      make(map[int16]*navmeshv2.RtNavmeshTerrain),
		regionOwners: make(map[int16]string),
	}
	if err := world.AddRegions("West", 7, 8); err == nil {
		t.Fatal("AddRegions() error = nil, want missing terrain failure")
	}
	if len(world.regions) != 0 || len(world.regionOwners) != 0 {
		t.Fatalf("failed partition mutated world: regions=%v owners=%v", world.regions, world.regionOwners)
	}
}

func TestAddRegionsNeverOverwritesExistingOwner(t *testing.T) {
	terrain := &navmeshv2.RtNavmeshTerrain{RtNavmeshBase: navmeshv2.RtNavmeshBase{Filename: "seven"}}
	world := &WorldService{
		mutex:        &sync.Mutex{},
		loader2:      &navmeshv2.Loader{RegionData: map[int16]*navmeshv2.RtNavmeshTerrain{7: terrain}},
		regions:      make(map[int16]*navmeshv2.RtNavmeshTerrain),
		regionOwners: make(map[int16]string),
	}
	if err := world.AddRegions("West", 7); err != nil {
		t.Fatalf("first AddRegions() error = %v", err)
	}
	if err := world.AddRegions("East", 7); !errors.Is(err, ErrWorldPartitionInvalid) {
		t.Fatalf("second AddRegions() error = %v, want %v", err, ErrWorldPartitionInvalid)
	}
	if world.regionOwners[7] != "West" || world.regions[7] != terrain {
		t.Fatalf("existing owner was overwritten: owner=%q terrain=%p", world.regionOwners[7], world.regions[7])
	}
}
