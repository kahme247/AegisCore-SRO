package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

var errWorldRepositoryWiring = errors.New("world repository wiring failure")

type worldWiringRegionCatalog struct {
	regions []model.GameServerRegion
	err     error
	calls   int
}

func (r *worldWiringRegionCatalog) GetRegionsForGameServer(_ context.Context, _ int) ([]model.GameServerRegion, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return append([]model.GameServerRegion(nil), r.regions...), nil
}

type worldWiringSpawnCatalog struct {
	spawns []model.SpawnDB
	err    error
	calls  int
}

type worldWiringTeleportGateCatalog struct{}

func (worldWiringTeleportGateCatalog) GetTeleportGates(context.Context) ([]model.TeleportGateSpawn, error) {
	return nil, nil
}

func (r *worldWiringSpawnCatalog) GetSpawnsForContinent(_ context.Context, _ string) ([]model.SpawnDB, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return append([]model.SpawnDB(nil), r.spawns...), nil
}

type worldWiringPositionRepository struct {
	calls       int
	contextSeen context.Context
	characterID int
	regionID    int16
}

type worldWiringNoopPositionRepository struct{}

func (worldWiringNoopPositionRepository) UpdateCharacterPosition(int, int16, float32, float32, float32) error {
	return nil
}

func (r *worldWiringPositionRepository) UpdateCharacterPosition(ctx context.Context, characterID int, regionID int16, _, _, _ float32) error {
	r.calls++
	r.contextSeen = ctx
	r.characterID = characterID
	r.regionID = regionID
	return nil
}

func newWorldRepositoryWiringTestWorld() *WorldService {
	return &WorldService{
		mutex:               &sync.Mutex{},
		positionCheckpoint:  time.Second,
		positionPersistence: NewPositionPersistenceService(worldWiringNoopPositionRepository{}, time.Hour),
	}
}

func configuredWorldRepositories(region model.GameServerRegionRepository, spawn model.ContinentSpawnRepository, position model.CharacterPositionRepository) WorldRepositories {
	return WorldRepositories{
		RegionCatalog: region,
		SpawnCatalog:  spawn,
		Positions:     position,
		TeleportGates: worldWiringTeleportGateCatalog{},
	}
}

func TestWorldConfigureRepositoriesRequiresCompleteBoundary(t *testing.T) {
	world := newWorldRepositoryWiringTestWorld()
	if err := world.ConfigureRepositories(WorldRepositories{}); !errors.Is(err, ErrWorldRepositoryConfiguration) {
		t.Fatalf("ConfigureRepositories error = %v, want %v", err, ErrWorldRepositoryConfiguration)
	}
}

func TestWorldBootstrapPropagatesInjectedRegionCatalogError(t *testing.T) {
	world := newWorldRepositoryWiringTestWorld()
	regions := &worldWiringRegionCatalog{err: errWorldRepositoryWiring}
	spawns := &worldWiringSpawnCatalog{}
	positions := &worldWiringPositionRepository{}
	if err := world.ConfigureRepositories(configuredWorldRepositories(regions, spawns, positions)); err != nil {
		t.Fatalf("ConfigureRepositories: %v", err)
	}

	_, err := world.LoadGameServerRegionsContext(context.Background(), 1)
	if !errors.Is(err, errWorldRepositoryWiring) {
		t.Fatalf("LoadGameServerRegionsContext error = %v, want wrapped injected error", err)
	}
	if regions.calls != 1 {
		t.Fatalf("region catalog calls = %d, want 1", regions.calls)
	}
	if spawns.calls != 0 {
		t.Fatalf("spawn catalog calls = %d after region failure, want 0", spawns.calls)
	}
}

func TestWorldSpawnLoadPropagatesInjectedCatalogError(t *testing.T) {
	world := newWorldRepositoryWiringTestWorld()
	regions := &worldWiringRegionCatalog{}
	spawns := &worldWiringSpawnCatalog{err: errWorldRepositoryWiring}
	positions := &worldWiringPositionRepository{}
	if err := world.ConfigureRepositories(configuredWorldRepositories(regions, spawns, positions)); err != nil {
		t.Fatalf("ConfigureRepositories: %v", err)
	}

	err := world.LoadSpawnDataForContinentContext(context.Background(), "TestContinent")
	if !errors.Is(err, errWorldRepositoryWiring) {
		t.Fatalf("LoadSpawnDataForContinentContext error = %v, want wrapped injected error", err)
	}
	if spawns.calls != 1 {
		t.Fatalf("spawn catalog calls = %d, want 1", spawns.calls)
	}
}

func TestWorldDrainPositionPersistenceUsesInjectedRepository(t *testing.T) {
	world := newWorldRepositoryWiringTestWorld()
	regions := &worldWiringRegionCatalog{}
	spawns := &worldWiringSpawnCatalog{}
	positions := &worldWiringPositionRepository{}
	if err := world.ConfigureRepositories(configuredWorldRepositories(regions, spawns, positions)); err != nil {
		t.Fatalf("ConfigureRepositories: %v", err)
	}

	player := &model.Player{
		ID: 91,
		SRObject: model.SRObject{
			UniqueID: 19,
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
				Region: navmeshv2.Region{ID: 25000},
				Offset: math32.NewVector3(10, 20, 30),
			},
		},
	}
	if err := world.positionPersistence.TryMarkDirty(player); err != nil {
		t.Fatalf("TryMarkDirty: %v", err)
	}

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "agent-shutdown")
	if err := world.DrainPositionPersistence(ctx); err != nil {
		t.Fatalf("DrainPositionPersistence: %v", err)
	}
	if positions.calls != 1 || positions.characterID != player.ID || positions.regionID != 25000 {
		t.Fatalf("position persistence calls = %d character=%d region=%d, want 1/%d/25000", positions.calls, positions.characterID, positions.regionID, player.ID)
	}
	if positions.contextSeen == nil || positions.contextSeen.Value(contextKey{}) != "agent-shutdown" {
		t.Fatalf("position repository did not receive shutdown context: %v", positions.contextSeen)
	}
}
