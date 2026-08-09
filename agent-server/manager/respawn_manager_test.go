package manager

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type respawnPositionWorldFake struct {
	resolve func(*math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
	move    func(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool
	calls   int
}

func (f *respawnPositionWorldFake) NewPosFromGlobalCoordinates(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	f.calls++
	return f.resolve(global)
}

func (f *respawnPositionWorldFake) CanMoveDirectly(source, target navmeshv2.RtNavmeshPosition) bool {
	return f.move(source, target)
}

func respawnTestPosition(regionID int16, x, y, z float32) navmeshv2.RtNavmeshPosition {
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(regionID),
		Offset: math32.NewVector3(x, y, z),
	}
}

func respawnResolvedPosition(regionID int16, global *math32.Vector3) navmeshv2.RtNavmeshPosition {
	region := navmeshv2.NewRegionFromInt16(regionID)
	return navmeshv2.RtNavmeshPosition{
		Region: region,
		Offset: math32.NewVector3(
			global.X-float32(region.X)*navmeshv2.RegionWidth,
			global.Y,
			global.Z-float32(region.Y)*navmeshv2.RegionHeight,
		),
	}
}

func TestRespawnManagerBuildsNPCFromConfiguredReferenceCatalog(t *testing.T) {
	snapshot := model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{
			4001: {
				RefObject: model.RefObject{
					ID:       4001,
					CodeName: "MOB_REFERENCE",
					TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 9},
				},
				DefaultSkill1: 160,
			},
		},
	}
	respawn := &RespawnManager{}
	if err := respawn.ConfigureReferenceData(model.NewReferenceDataCatalog(snapshot)); err != nil {
		t.Fatalf("ConfigureReferenceData: %v", err)
	}

	npc, err := respawn.newNPCForSpawn(&model.SpawnArea{Spawn: model.Spawn{
		RefObjID:    4001,
		NpcCodeName: "MOB_SPAWN_NAME",
		AggressType: byte(model.Aggressive),
	}})
	if err != nil {
		t.Fatalf("newNPCForSpawn: %v", err)
	}
	if npc.RefObjectID != 4001 || npc.Name != "MOB_SPAWN_NAME" {
		t.Fatalf("npc identity = id:%d name:%q; want 4001 and MOB_SPAWN_NAME", npc.RefObjectID, npc.Name)
	}
	if got := npc.GetTypeInfo(); got != (model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 9}) {
		t.Fatalf("npc type info = %#v; want configured reference type", got)
	}
	if got := npc.Rarity(); got != 0 {
		t.Fatalf("npc rarity = %d; want normal rarity 0", got)
	}
	if npc.GetKnownObjectList() == nil {
		t.Fatal("npc has no known-object list")
	}
	if got := npc.AttackSkillID(); got != 160 {
		t.Fatalf("npc attack skill = %d; want reference default 160", got)
	}
	if _, aggressive, _, _ := npc.AIProfile(); !aggressive {
		t.Fatal("AggressType=1 spawn was not configured as aggressive")
	}
}

func TestRespawnManagerRejectsUnconfiguredAndMissingReferences(t *testing.T) {
	spawnArea := &model.SpawnArea{Spawn: model.Spawn{RefObjID: 4001}}

	t.Run("unconfigured", func(t *testing.T) {
		respawn := &RespawnManager{}
		if _, err := respawn.newNPCForSpawn(spawnArea); !errors.Is(err, ErrRespawnReferenceCatalogUnavailable) {
			t.Fatalf("newNPCForSpawn error = %v; want unconfigured catalog", err)
		}
	})

	t.Run("missing reference", func(t *testing.T) {
		respawn := &RespawnManager{}
		if err := respawn.ConfigureReferenceData(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{})); err != nil {
			t.Fatalf("ConfigureReferenceData: %v", err)
		}
		if _, err := respawn.newNPCForSpawn(spawnArea); !errors.Is(err, ErrRespawnReferenceCharacterUnavailable) {
			t.Fatalf("newNPCForSpawn error = %v; want missing reference", err)
		}
	})
}

func TestRespawnManagerRejectsNilReferenceCatalog(t *testing.T) {
	respawn := &RespawnManager{}
	if err := respawn.ConfigureReferenceData(nil); !errors.Is(err, ErrRespawnReferenceCatalogUnavailable) {
		t.Fatalf("ConfigureReferenceData(nil) error = %v; want unavailable catalog", err)
	}
}

func TestGenerateRandomPositionKeepsAuthoredRegionAndHeading(t *testing.T) {
	spawn := &model.SpawnArea{Spawn: model.Spawn{
		Position:       respawnTestPosition(25000, 100, 12, 200),
		GenerateRadius: 50,
	}}
	spawn.Position.Heading = 321

	world := &respawnPositionWorldFake{
		resolve: func(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
			return respawnResolvedPosition(25000, global), nil
		},
		move: func(_, _ navmeshv2.RtNavmeshPosition) bool { return true },
	}
	offsets := []float32{10, -20}
	nextOffset := 0
	position, err := generateRandomPositionInRadius(world, spawn, func(float32) float32 {
		value := offsets[nextOffset]
		nextOffset++
		return value
	})
	if err != nil {
		t.Fatalf("generateRandomPositionInRadius: %v", err)
	}
	if position.Region.ID != spawn.Position.Region.ID {
		t.Fatalf("region = %d; want authored region %d", position.Region.ID, spawn.Position.Region.ID)
	}
	if position.Offset.X != 110 || position.Offset.Z != 180 {
		t.Fatalf("offset = (%g,%g); want (110,180)", position.Offset.X, position.Offset.Z)
	}
	if position.Offset.Y != 12 || position.Heading != 321 {
		t.Fatalf("vertical/heading = (%g,%g); want (12,321)", position.Offset.Y, position.Heading)
	}
}

func TestGenerateRandomPositionFallsBackAfterBoundedCrossRegionAttempts(t *testing.T) {
	spawn := &model.SpawnArea{Spawn: model.Spawn{
		Position:       respawnTestPosition(25000, 100, 12, 200),
		GenerateRadius: 500,
	}}
	spawn.Position.Heading = 45

	world := &respawnPositionWorldFake{
		resolve: func(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
			regionID := int16(25000)
			authoredGlobal := spawn.Position.GetGlobalCoordinates()
			if global.X != authoredGlobal.X || global.Z != authoredGlobal.Z {
				regionID = 25001
			}
			return respawnResolvedPosition(regionID, global), nil
		},
		move: func(_, _ navmeshv2.RtNavmeshPosition) bool {
			t.Fatal("cross-region candidate reached motion validation")
			return false
		},
	}
	position, err := generateRandomPositionInRadius(world, spawn, func(float32) float32 { return 1 })
	if err != nil {
		t.Fatalf("generateRandomPositionInRadius: %v", err)
	}
	if world.calls != maxRespawnPositionAttempts+1 {
		t.Fatalf("resolve calls = %d; want authored point plus %d attempts", world.calls, maxRespawnPositionAttempts)
	}
	if position.Region.ID != 25000 || position.Offset.X != 100 || position.Offset.Z != 200 {
		t.Fatalf("fallback = region:%d (%g,%g); want authored point", position.Region.ID, position.Offset.X, position.Offset.Z)
	}
}

func TestGenerateRandomPositionRejectsObstructedCandidates(t *testing.T) {
	spawn := &model.SpawnArea{Spawn: model.Spawn{
		Position:       respawnTestPosition(25000, 100, 12, 200),
		GenerateRadius: 25,
	}}
	world := &respawnPositionWorldFake{
		resolve: func(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
			return respawnResolvedPosition(25000, global), nil
		},
		move: func(_, _ navmeshv2.RtNavmeshPosition) bool { return false },
	}
	position, err := generateRandomPositionInRadius(world, spawn, func(float32) float32 { return 1 })
	if err != nil {
		t.Fatalf("generateRandomPositionInRadius: %v", err)
	}
	if world.calls != maxRespawnPositionAttempts+1 {
		t.Fatalf("resolve calls = %d; want authored point plus %d attempts", world.calls, maxRespawnPositionAttempts)
	}
	if position.Region.ID != 25000 || position.Offset.X != 100 || position.Offset.Z != 200 {
		t.Fatalf("fallback = region:%d (%g,%g); want authored point", position.Region.ID, position.Offset.X, position.Offset.Z)
	}
}
