package service

import (
	"sync"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
)

func TestLoadSpawnDataPreservesInitialHeading(t *testing.T) {
	const (
		regionID = int16(25000)
		heading  = 49152
	)

	terrain := newGlobalPositionTestTerrain(regionID)
	world := &WorldService{
		regions: map[int16]*navmeshv2.RtNavmeshTerrain{regionID: &terrain},
		spawns:  make(map[int16][]*model.SpawnArea),
		mutex:   &sync.Mutex{},
	}

	world.loadSpawnData("Test", []model.SpawnDB{{
		NestID:        1,
		RegionID:      regionID,
		X:             100,
		Y:             20,
		Z:             100,
		Heading:       heading,
		RefObjID:      1001,
		NpcCodeName:   "NPC_TEST",
		MaxTotalCount: 1,
	}})

	spawns := world.GetSpawnsForRegion(regionID)
	if len(spawns) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(spawns))
	}
	if got := spawns[0].Position.Heading; got != heading {
		t.Fatalf("spawn heading = %v, want %d", got, heading)
	}
}
