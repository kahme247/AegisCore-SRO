package service

import (
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestDimensionHolePopulationClonesAndReleasesAuthoredSpawnsPerScope(t *testing.T) {
	scope, err := model.NewGameWorldScope(63, 7)
	if err != nil {
		t.Fatal(err)
	}
	base := model.InitSpawnAreaFromSpawnNest(model.Spawn{NestID: 1, RefObjID: 100, NpcCodeName: "MOB_TEST", MaxTotalCount: 4})
	world := &WorldService{
		mutex:  &sync.Mutex{},
		spawns: map[int16][]*model.SpawnArea{27380: {base}},
	}

	if err = world.InstantiateDimensionHoleSpawns(scope, []int16{27380}); err != nil {
		t.Fatalf("InstantiateDimensionHoleSpawns() error = %v", err)
	}
	areas := world.GetSpawnsForRegion(27380)
	if len(areas) != 2 || areas[0] != base || areas[1].GameWorldID != uint32(scope) || areas[1].MaxTotalCount != base.MaxTotalCount {
		t.Fatalf("scoped spawn areas = %#v", areas)
	}
	if err = world.InstantiateDimensionHoleSpawns(scope, []int16{27380}); err != nil || len(world.GetSpawnsForRegion(27380)) != 2 {
		t.Fatalf("duplicate instantiation error=%v areas=%d", err, len(world.GetSpawnsForRegion(27380)))
	}

	world.ReleaseDimensionHoleSpawns(scope)
	areas = world.GetSpawnsForRegion(27380)
	if len(areas) != 1 || areas[0] != base {
		t.Fatalf("released spawn areas = %#v", areas)
	}
}
