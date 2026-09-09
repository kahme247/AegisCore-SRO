package service

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/config"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/Harvi8/AegisCore/protocol/profiles"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	ErrRegionNotExisting          = errors.New("region does not exist")
	ErrPlayerAlreadyConnected     = errors.New("player is already connected")
	ErrPlayerIdentityInvalid      = errors.New("player identity is invalid")
	ErrNeighbourPlanesUnavailable = errors.New("neighbour planes are not implemented")
	ErrGlobalPositionInvalid      = errors.New("global position is invalid")
	ErrGlobalPositionOutOfBounds  = errors.New("global position is outside the normal world grid")
	// ErrWorldRepositoryConfiguration is returned when production startup
	// attempts to configure only part of the world persistence boundary. The
	// live Agent must use one owned shard pool for every world catalog and
	// position checkpoint, rather than silently mixing it with compatibility
	// wrappers that open a connection per operation.
	ErrWorldRepositoryConfiguration = errors.New("world repository configuration is incomplete")
	// ErrWorldPositionPersistenceBusy prevents replacement of a queue that
	// still owns dirty snapshots. Repository wiring is a startup operation and
	// must never discard an in-flight position update.
	ErrWorldPositionPersistenceBusy = errors.New("world position persistence has pending writes")
	ErrNavmeshCacheInvalid          = errors.New("prepared navmesh cache is invalid")
)

const (
	normalWorldRegionColumns = 1 << navmeshv2.XSize
	normalWorldRegionRows    = 1 << navmeshv2.YSize
)

type WorldService struct {
	visibleObjects       map[uint32]model.ISRObject
	playersByUniqueId    map[uint32]*model.Player
	playersByCharName    map[string]*model.Player
	playersByCharacterID map[int]*model.Player
	npcsByUniqueId       map[uint32]*model.NPC
	pets                 map[uint32]model.ISRObject // TODO Move to own type+
	// Regions are immutable after bootstrap. Store pointers so lookups do not
	// copy each region's fixed tile/plane/height arrays.
	regions map[int16]*navmeshv2.RtNavmeshTerrain
	// regionOwners records the validated continent/runtime partition. It is
	// populated atomically with regions and immutable after bootstrap.
	regionOwners  map[int16]string
	movingObjects map[uint32]model.ICharacter
	// movingObjectSnapshot changes only when movement membership changes. The
	// game-time loop can iterate this stable slice without cloning a map every
	// tick.
	movingObjectSnapshot   []model.ICharacter
	visibleObjectsByRegion map[int16][]model.ISRObject
	uniqueIdCounter        uint32
	spawns                 map[int16][]*model.SpawnArea
	mutex                  *sync.Mutex
	// visibilityMutex serializes the short visibility reconciliation and
	// disconnect teardown transactions. It is deliberately separate from the
	// world membership mutex: known-list operations can acquire their own
	// locks, so they must run after the membership snapshot is taken without
	// blocking movement, admission, or shutdown from acquiring w.mutex.
	visibilityMutex          sync.Mutex
	loader2                  *navmeshv2.Loader
	navmeshGobPath           string
	positionPersistence      *PositionPersistenceService
	positionCheckpoint       time.Duration
	regionCatalog            model.GameServerRegionRepository
	spawnCatalog             model.ContinentSpawnRepository
	teleportGateCatalog      model.TeleportGateRepository
	gameplayRates            interface{ Current() model.GameplayRates }
	referenceData            *model.ReferenceDataCatalog
	mountedTransportResolver func(*model.Player) (*model.COS, bool)
}

// ConfigureMountedTransportResolver connects world movement to the COS
// lifecycle without making WorldService own COS activation or persistence.
// Production configures it once before the network listener starts.
func (w *WorldService) ConfigureMountedTransportResolver(resolver func(*model.Player) (*model.COS, bool)) {
	if w == nil {
		return
	}
	w.mountedTransportResolver = resolver
}

func (w *WorldService) MountedTransport(player *model.Player) (*model.COS, bool) {
	if w == nil || player == nil || w.mountedTransportResolver == nil {
		return nil, false
	}
	return w.mountedTransportResolver(player)
}

func (w *WorldService) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) {
	if w == nil {
		return
	}
	w.visibilityMutex.Lock()
	w.referenceData = catalog
	w.visibilityMutex.Unlock()
}

// WorldRepositories is the live Agent persistence boundary for world
// bootstrap and checkpointing. Direct unit tests may leave WorldService with
// its compatibility defaults, but NewAgentServer supplies all three
// repositories from its long-lived shard pool before bootstrap begins.
type WorldRepositories struct {
	RegionCatalog model.GameServerRegionRepository
	SpawnCatalog  model.ContinentSpawnRepository
	Positions     model.CharacterPositionRepository
	TeleportGates model.TeleportGateRepository
}

func (w *WorldService) ObjectCount() int {
	if w == nil || w.mutex == nil {
		return 0
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return len(w.visibleObjects)
}

func (w *WorldService) ConfigureGameplayRates(rates interface{ Current() model.GameplayRates }) {
	if w == nil || w.mutex == nil {
		return
	}
	w.mutex.Lock()
	w.gameplayRates = rates
	w.mutex.Unlock()
}

// snapshotTerrain detaches the collection backing that a terrain value carries
// in addition to its fixed-size arrays. Terrain is an interconnected navmesh
// graph, so this is deliberately a collection snapshot rather than a full
// deep-copy of every interface and pointer reachable from a cell or edge.
// Callers can safely reorder or replace returned collection elements without
// mutating WorldService's backing slices.
func snapshotTerrain(terrain navmeshv2.RtNavmeshTerrain) navmeshv2.RtNavmeshTerrain {
	snapshot := terrain

	snapshot.Objects = append([]navmeshv2.RtNavmeshInstObj(nil), terrain.Objects...)
	for i, object := range terrain.Objects {
		snapshot.Objects[i].EdgeLinks = append([]navmeshv2.RtNavmeshEdgeLink(nil), object.EdgeLinks...)
	}

	snapshot.Cells = append([]navmeshv2.RtNavmeshCellQuad(nil), terrain.Cells...)
	for i, cell := range terrain.Cells {
		snapshot.Cells[i].Edges = append([]navmeshv2.RtNavmeshEdge(nil), cell.Edges...)
		snapshot.Cells[i].Objects = append([]navmeshv2.RtNavmeshInstObj(nil), cell.Objects...)
	}

	snapshot.GlobalEdges = append([]navmeshv2.RtNavmeshEdgeGlobal(nil), terrain.GlobalEdges...)
	snapshot.InternalEdges = append([]navmeshv2.RtNavmeshEdgeInternal(nil), terrain.InternalEdges...)
	return snapshot
}

var worldServiceInstance *WorldService
var worldServiceOnce sync.Once

func GetWorldServiceInstance() *WorldService {
	worldServiceOnce.Do(func() {
		checkpointSeconds := viper.GetInt(config.PositionCheckpointSeconds)
		worldServiceInstance = &WorldService{
			visibleObjects:         make(map[uint32]model.ISRObject),
			playersByUniqueId:      make(map[uint32]*model.Player),
			playersByCharName:      make(map[string]*model.Player),
			playersByCharacterID:   make(map[int]*model.Player),
			npcsByUniqueId:         make(map[uint32]*model.NPC),
			pets:                   make(map[uint32]model.ISRObject),
			regions:                make(map[int16]*navmeshv2.RtNavmeshTerrain),
			regionOwners:           make(map[int16]string),
			movingObjects:          make(map[uint32]model.ICharacter),
			visibleObjectsByRegion: make(map[int16][]model.ISRObject),
			spawns:                 make(map[int16][]*model.SpawnArea),
			// UniqueID 0 is reserved as the unauthenticated/no-player sentinel in
			// session cleanup and protocol packets. Never allocate it to a world
			// player.
			uniqueIdCounter:     1,
			mutex:               &sync.Mutex{},
			navmeshGobPath:      viper.GetString(config.AgentPrelinkedNavdataFile),
			positionPersistence: NewPositionPersistenceService(databaseCharacterPositionRepository{}, time.Duration(checkpointSeconds)*time.Second),
			positionCheckpoint:  time.Duration(checkpointSeconds) * time.Second,
		}
	})

	return worldServiceInstance
}

// ConfigureRepositories replaces the singleton's test-compatible database
// adapters with injected repositories owned by AgentServer. It intentionally
// requires every dependency: allowing only the region catalog or position
// writer through would hide a legacy sql.Open path in the live process.
//
// Configuration is safe only before world traffic begins. If a previous queue
// still owns dirty snapshots, the method fails without changing any
// dependency so shutdown can drain those snapshots first.
func (w *WorldService) ConfigureRepositories(repositories WorldRepositories) error {
	if w == nil || w.mutex == nil || repositories.RegionCatalog == nil || repositories.SpawnCatalog == nil || repositories.Positions == nil || repositories.TeleportGates == nil {
		return ErrWorldRepositoryConfiguration
	}

	positionPersistence := NewPositionPersistenceService(
		NewModelCharacterPositionRepositoryAdapter(repositories.Positions),
		w.positionCheckpointInterval(),
	)

	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.positionPersistence != nil {
		stats := w.positionPersistence.Stats()
		if stats.Pending != 0 || stats.InFlight != 0 {
			return ErrWorldPositionPersistenceBusy
		}
	}
	w.regionCatalog = repositories.RegionCatalog
	w.spawnCatalog = repositories.SpawnCatalog
	w.teleportGateCatalog = repositories.TeleportGates
	w.positionPersistence = positionPersistence
	return nil
}

func (w *WorldService) positionCheckpointInterval() time.Duration {
	if w == nil || w.positionCheckpoint <= 0 {
		return time.Duration(viper.GetInt(config.PositionCheckpointSeconds)) * time.Second
	}
	return w.positionCheckpoint
}

func (w *WorldService) repositories() (model.GameServerRegionRepository, model.ContinentSpawnRepository, model.TeleportGateRepository) {
	if w == nil || w.mutex == nil {
		return nil, nil, nil
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.regionCatalog, w.spawnCatalog, w.teleportGateCatalog
}

func (w *WorldService) GetObjectByUniqueId(objectUniqueId uint32) (model.ISRObject, error) {
	return w.index().objectByUniqueID(objectUniqueId)
}

func (w *WorldService) GetPlayerByUniqueId(playerUniqueId uint32) (*model.Player, error) {
	return w.index().playerByUniqueID(playerUniqueId)
}

func (w *WorldService) GetPlayerByCharacterID(characterID int) (*model.Player, error) {
	return w.index().playerByCharacterID(characterID)
}

func (w *WorldService) GetNPCs() []*model.NPC {
	return w.index().npcs()
}

func (w *WorldService) AddPlayer(p *model.Player) error {
	return w.index().addPlayer(p)
}

func (w *WorldService) nextUniqueIDLocked() (uint32, error) {
	// A uint32 wrap is extraordinarily unlikely in normal operation, but skip
	// zero and any still-live UID rather than turning a long-running process
	// into a silent map overwrite.
	for attempts := uint64(0); attempts < uint64(^uint32(0)); attempts++ {
		if w.uniqueIdCounter == 0 {
			w.uniqueIdCounter = 1
		}
		uid := w.uniqueIdCounter
		w.uniqueIdCounter++
		if _, exists := w.visibleObjects[uid]; !exists {
			return uid, nil
		}
	}
	return 0, errors.New("world unique ID space is exhausted")
}

func (w *WorldService) AddVisibleObject(o model.ISRObject) {
	if o == nil {
		return
	}
	w.mutex.Lock()
	uid, err := w.nextUniqueIDLocked()
	if err != nil {
		w.mutex.Unlock()
		log.Errorf("failed to allocate world object ID: %v", err)
		return
	}
	w.visibleObjects[uid] = o
	o.SetUniqueID(uid)
	w.visibleObjectsByRegion[o.GetNavmeshPosition().Region.ID] = append(w.visibleObjectsByRegion[o.GetNavmeshPosition().Region.ID], o)
	if npc, ok := o.(*model.NPC); ok {
		w.npcsByUniqueId[uid] = npc
	}
	w.mutex.Unlock()
	w.reconcileVisibleObject(o)
}

// RemoveVisibleObject atomically removes a non-player world object and then
// lets each player's known-object list queue the normal group-despawn packet.
func (w *WorldService) RemoveVisibleObject(object model.ISRObject) {
	if w == nil || w.mutex == nil || object == nil {
		return
	}
	uid := object.GetUniqueID()
	w.visibilityMutex.Lock()
	w.mutex.Lock()
	if current, exists := w.visibleObjects[uid]; !exists || current != object {
		w.mutex.Unlock()
		w.visibilityMutex.Unlock()
		return
	}
	owners := make([]model.ISRObject, 0, len(w.visibleObjects))
	for _, owner := range w.visibleObjects {
		owners = append(owners, owner)
	}
	for regionID, objects := range w.visibleObjectsByRegion {
		filtered := objects[:0]
		for _, candidate := range objects {
			if candidate != object {
				filtered = append(filtered, candidate)
			}
		}
		w.visibleObjectsByRegion[regionID] = filtered
	}
	delete(w.visibleObjects, uid)
	delete(w.npcsByUniqueId, uid)
	delete(w.movingObjects, uid)
	w.rebuildMovingObjectSnapshotLocked()
	w.mutex.Unlock()
	for _, owner := range owners {
		if known := owner.GetKnownObjectList(); known != nil {
			known.RemoveObject(object)
		}
	}
	w.visibilityMutex.Unlock()
}

func (w *WorldService) PlayerDisconnected(uid uint32, name string) {
	w.playerDisconnected(uid, name, true)
}

// PlayerDisconnectedAfterSave removes world state without scheduling a second
// non-atomic position write. The caller must have completed its terminal save
// attempt first.
func (w *WorldService) PlayerDisconnectedAfterSave(uid uint32, name string) {
	w.playerDisconnected(uid, name, false)
}

func (w *WorldService) playerDisconnected(uid uint32, _ string, checkpointPosition bool) {
	if w == nil || w.mutex == nil {
		return
	}

	// A reconciler cannot be allowed to snapshot this player, then re-add it to
	// a reciprocal known list after disconnect has removed world membership.
	// Serialize the membership removal and out-of-lock known-list cleanup with
	// visibility reconciliation, while keeping the world mutex limited to its
	// own maps.
	w.visibilityMutex.Lock()
	w.mutex.Lock()
	player := w.playersByUniqueId[uid]
	knownObjectOwners := make([]model.ISRObject, 0, len(w.visibleObjects))
	for _, object := range w.visibleObjects {
		knownObjectOwners = append(knownObjectOwners, object)
	}
	if player != nil {
		for regionID, objects := range w.visibleObjectsByRegion {
			filtered := objects[:0]
			for _, object := range objects {
				// AddPlayer is the only source of player region membership, so
				// this identity check avoids calling arbitrary object methods
				// while the world membership mutex is held.
				if regionPlayer, isPlayer := object.(*model.Player); isPlayer && regionPlayer == player {
					continue
				}
				filtered = append(filtered, object)
			}
			w.visibleObjectsByRegion[regionID] = filtered
		}
	}
	delete(w.visibleObjects, uid)
	delete(w.playersByUniqueId, uid)
	if player != nil {
		// Always remove the indexes for the player we actually resolved by UID.
		// A session can retain stale display-name data while it is closing; using
		// that caller-provided name here could otherwise free another player's
		// active reservation.
		delete(w.playersByCharName, player.CharName)
		delete(w.playersByCharacterID, player.ID)
	}
	delete(w.movingObjects, uid)
	w.rebuildMovingObjectSnapshotLocked()
	w.mutex.Unlock()
	if player != nil {
		// Known-object lists are independently synchronized and may trigger
		// work outside the world service. Membership is already gone above,
		// so prune them after releasing the world lock.
		for _, object := range knownObjectOwners {
			if knownList := object.GetKnownObjectList(); knownList != nil {
				knownList.RemoveObject(player)
			}
		}
	}
	w.visibilityMutex.Unlock()
	if player != nil && checkpointPosition {
		if w.positionPersistence != nil {
			w.positionPersistence.MarkDirty(player)
			if err := w.positionPersistence.FlushPlayer(uid); err != nil {
				log.Warnf("position for disconnected player %d remains queued: %v", uid, err)
			}
		}
	}
}

// PrepareDisconnectSave removes any queued or older in-flight position write
// before the terminal snapshot is captured.
func (w *WorldService) PrepareDisconnectSave(ctx context.Context, playerUID uint32) error {
	if w == nil || w.positionPersistence == nil {
		return ErrPositionPersistenceNoRepository
	}
	return w.positionPersistence.DiscardPlayer(ctx, playerUID)
}

// BootstrapGameServerRegionsContext loads the live world without constructing
// a detached copy of every region. Production startup does not consume such a
// snapshot, and creating one temporarily duplicates the largest retained data
// structure in the Agent.
func (w *WorldService) BootstrapGameServerRegionsContext(ctx context.Context, gameServerID int) error {
	return w.bootstrapGameServerRegionsContext(ctx, gameServerID)
}

func (w *WorldService) bootstrapGameServerRegionsContext(ctx context.Context, gameServerID int) error {
	if w == nil {
		return ErrWorldRepositoryConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	regionCatalog, _, teleportGateCatalog := w.repositories()
	var gsRegions []model.GameServerRegion
	var err error
	if regionCatalog == nil {
		// Keep the historical no-argument path for small direct tests. Agent
		// startup configures regionCatalog and never takes this fallback.
		gsRegions = model.GetRegionsForGameServer(gameServerID)
	} else {
		gsRegions, err = regionCatalog.GetRegionsForGameServer(ctx, gameServerID)
		if err != nil {
			return fmt.Errorf("load game-server region catalog: %w", err)
		}
	}
	partition, err := validateWorldPartition(gsRegions)
	if err != nil {
		return err
	}

	// Query every spawn catalog before changing the world. An injected catalog
	// failure must not look like an empty continent or leave a partially
	// bootstrapped world behind for a listener that has not yet started.
	type continentCatalog struct {
		name    string
		regions []int16
		spawns  []model.SpawnDB
	}
	continents := make([]continentCatalog, 0, len(gsRegions))
	for _, region := range gsRegions {
		spawns, err := w.loadSpawnsForContinent(ctx, region.ContinentName)
		if err != nil {
			return fmt.Errorf("load spawn catalog for continent %q: %w", region.ContinentName, err)
		}
		continents = append(continents, continentCatalog{
			name:    region.ContinentName,
			regions: append([]int16(nil), region.Regions...),
			spawns:  spawns,
		})
	}
	var teleportGates []model.TeleportGateSpawn
	if teleportGateCatalog != nil {
		teleportGates, err = teleportGateCatalog.GetTeleportGates(ctx)
		if err != nil {
			return fmt.Errorf("load teleport gate catalog: %w", err)
		}
	}

	log.Info("loading game server regions")
	measurement := beginNavmeshLoadMeasurement()
	loadSource := "raw"

	cachePrefix := w.navmeshGobPath
	cachePaths := []string{
		cachePrefix + "_regions",
		cachePrefix + "_objects",
		navmeshv2.CacheManifestPath(cachePrefix),
	}
	cachePresent, err := anyRegularPathExists(cachePaths...)
	if err != nil {
		return fmt.Errorf("inspect prepared navmesh cache: %w", err)
	}
	if cachePresent {
		loadSource = "prepared-gob"
		canonicalProfile, profileErr := profiles.VSRO188Profile()
		if profileErr != nil {
			return fmt.Errorf("load canonical navmesh profile: %w", profileErr)
		}
		if _, err := navmeshv2.ValidateCacheManifest(ctx, navmeshv2.CacheManifestPath(cachePrefix), profiles.VSRO188, canonicalProfile.DataPK2.SHA256); err != nil {
			return fmt.Errorf("%w: %v", ErrNavmeshCacheInvalid, err)
		}
		log.Infof("loading prepared v2 navmesh cache")
	}

	// Build navmesh state off to the side. Publishing a loader before all
	// decoding and partition checks finish makes a retry observe partial data.
	// A preloaded loader is retained for direct tests and compatibility callers;
	// production startup enters with no loader and takes one of the staged paths.
	w.mutex.Lock()
	loader := w.loader2
	w.mutex.Unlock()
	if _, stageErr := stageWorldPartitionFromLoader(loader, partition); stageErr == nil {
		loadSource = "resident"
	} else {
		// Do not trust a loader left incomplete by an older failed bootstrap or
		// supplied by a compatibility caller. Reload it transactionally.
		loader = nil
	}
	if loader == nil {
		if cachePresent {
			loader = &navmeshv2.Loader{
				ObjectData: make(map[uint32]navmeshv2.RtNavmeshObj),
				RegionData: make(map[int16]*navmeshv2.RtNavmeshTerrain),
			}
			if err := loader.LoadPrecomputedNavmeshDataFromGOB(cachePrefix); err != nil {
				return fmt.Errorf("%w: decode cache: %v", ErrNavmeshCacheInvalid, err)
			}
		} else {
			log.Infof("prepared v2 navdata cache does not exist. loading from data then")
			var loaderErr error
			loader, loaderErr = navmeshv2.NewLoaderSafe(viper.GetString(config.AgentDataPath))
			if loaderErr != nil {
				return fmt.Errorf("load raw navmesh archive: %w", loaderErr)
			}
			archiveOpen := true
			defer func() {
				if archiveOpen {
					_ = loader.CloseArchive()
				}
			}()
			if err := loader.LoadNavMeshInfos(); err != nil {
				return fmt.Errorf("load raw navmesh metadata: %w", err)
			}
			if err := loader.LoadTerrainMeshes(nil); err != nil {
				return fmt.Errorf("load raw navmesh terrain: %w", err)
			}
			if err := loader.CloseArchive(); err != nil {
				return fmt.Errorf("close raw navmesh archive: %w", err)
			}
			archiveOpen = false
		}
	}
	measurement.log(loadSource, loader.Stats())
	loadedRegions, err := stageWorldPartitionFromLoader(loader, partition)
	if err != nil {
		return err
	}
	if err := w.commitWorldBootstrap(partition, loadedRegions, loader); err != nil {
		return err
	}

	for _, continent := range continents {
		w.loadSpawnData(continent.name, continent.spawns)
	}
	if err := w.loadTeleportGates(teleportGates); err != nil {
		return err
	}

	log.Info("finished loading game server regions")
	return nil
}

// LoadGameServerRegionsContext preserves the detached-snapshot API for callers
// that consume it. Production startup uses BootstrapGameServerRegionsContext to
// avoid creating and immediately discarding a complete world copy.
func (w *WorldService) LoadGameServerRegionsContext(ctx context.Context, gameServerID int) (map[int16]navmeshv2.RtNavmeshTerrain, error) {
	if err := w.bootstrapGameServerRegionsContext(ctx, gameServerID); err != nil {
		return nil, err
	}
	return w.GetRegions(), nil
}

// LoadGameServerRegions preserves the previous no-error API for direct unit
// tests and compatibility callers. Live startup calls
// LoadGameServerRegionsContext and treats a returned error as fatal.
func (w *WorldService) LoadGameServerRegions(gameServerID int) map[int16]navmeshv2.RtNavmeshTerrain {
	regions, err := w.LoadGameServerRegionsContext(context.Background(), gameServerID)
	if err != nil {
		log.Errorf("failed to load game server regions: %v", err)
		return nil
	}
	return regions
}

func anyRegularPathExists(paths ...string) (bool, error) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("cache path %q is not a regular file", path)
		}
		return true, nil
	}
	return false, nil
}

func (w *WorldService) AddRegions(continent string, regions ...int16) error {
	log.Debugf("loading regions for %s", continent)
	partition, err := validateWorldPartition([]model.GameServerRegion{{ContinentName: continent, Regions: regions}})
	if err != nil {
		return err
	}
	loadedRegions, err := w.stageWorldPartition(partition)
	if err != nil {
		return err
	}
	if err := w.commitWorldPartition(partition, loadedRegions); err != nil {
		return err
	}
	log.Infof("Finished loading regions for %s\n", continent)
	return nil
}

func (w *WorldService) GetRegion(regionId int16) (navmeshv2.RtNavmeshTerrain, error) {
	region, err := w.lookupRegionPtr(regionId)
	if err != nil {
		return navmeshv2.RtNavmeshTerrain{}, err
	}
	return snapshotTerrain(*region), nil
}

// lookupRegionPtr returns the immutable world-owned terrain for internal
// calculations. Callers must not mutate the returned terrain after bootstrap.
func (w *WorldService) lookupRegionPtr(regionId int16) (*navmeshv2.RtNavmeshTerrain, error) {
	return w.regionStore().lookup(regionId)
}

// lookupRegion preserves the value-returning compatibility path. Hot world
// calculations use lookupRegionPtr to avoid copying fixed terrain arrays.
func (w *WorldService) lookupRegion(regionId int16) (navmeshv2.RtNavmeshTerrain, error) {
	region, err := w.lookupRegionPtr(regionId)
	if err != nil {
		return navmeshv2.RtNavmeshTerrain{}, err
	}
	return *region, nil
}

func (w *WorldService) GetRegions() map[int16]navmeshv2.RtNavmeshTerrain {
	return w.regionStore().snapshots()
}

// GetRegionIDs returns the lightweight region catalog used by periodic world
// managers. It intentionally avoids copying every region's navmesh arrays.
func (w *WorldService) GetRegionIDs() []int16 {
	return w.regionStore().ids()
}

func regionNotExistingError(regionID int16) error {
	return fmt.Errorf("%w: %d", ErrRegionNotExisting, regionID)
}

func (w *WorldService) GetSpawnsForRegion(regionId int16) []*model.SpawnArea {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return append([]*model.SpawnArea(nil), w.spawns[regionId]...)
}

// LoadSpawnDataForContinentContext loads one continent through the configured
// catalog repository. Callers that need startup-grade error handling should
// use this method instead of the compatibility wrapper below.
func (w *WorldService) LoadSpawnDataForContinentContext(ctx context.Context, continent string) error {
	if w == nil {
		return ErrWorldRepositoryConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	spawns, err := w.loadSpawnsForContinent(ctx, continent)
	if err != nil {
		return fmt.Errorf("load spawn catalog for continent %q: %w", continent, err)
	}
	w.loadSpawnData(continent, spawns)
	return nil
}

// LoadSpawnDataForContinent preserves the legacy direct-call surface. Live
// bootstrap uses LoadGameServerRegionsContext, which propagates injected
// repository failures before it mutates the world.
func (w *WorldService) LoadSpawnDataForContinent(continent string) {
	if err := w.LoadSpawnDataForContinentContext(context.Background(), continent); err != nil {
		log.Errorf("failed to load spawn data for continent %q: %v", continent, err)
	}
}

func (w *WorldService) loadSpawnsForContinent(ctx context.Context, continent string) ([]model.SpawnDB, error) {
	_, spawnCatalog, _ := w.repositories()
	if spawnCatalog == nil {
		// Compatibility fallback for direct tests. NewAgentServer configures an
		// injected catalog before the live world bootstrap can run.
		return model.GetSpawnsForContinent(continent), nil
	}
	spawns, err := spawnCatalog.GetSpawnsForContinent(ctx, continent)
	if err != nil {
		return nil, err
	}
	return spawns, nil
}

func (w *WorldService) loadSpawnData(continent string, spawns []model.SpawnDB) {
	log.Infof("initialising %d spawns", len(spawns))
	countRate := 100
	respawnRate := 100
	w.mutex.Lock()
	gameplayRates := w.gameplayRates
	w.mutex.Unlock()
	if gameplayRates != nil {
		rates := gameplayRates.Current()
		countRate = int(rates.MobCountRatePercent)
		respawnRate = int(rates.MobRespawnRatePercent)
	}
	spawnsByRegion := make(map[int16][]*model.SpawnArea)
	for _, spawn := range spawns {
		reg, err := w.lookupRegionPtr(spawn.RegionID)
		if err != nil {
			log.Debugf("found spawn for non-existent region: %d", spawn.RegionID)
			continue
		}
		spawnPos := math32.NewVector3(spawn.X, spawn.Y, spawn.Z)
		cell, err := reg.ResolveCell(spawnPos)
		if err != nil {
			log.Error(err)
		}
		s := model.Spawn{
			Position: navmeshv2.RtNavmeshPosition{
				Cell:     &cell,
				Instance: nil,
				Region:   reg.Region,
				Offset:   spawnPos,
				// wInitialDir is already encoded in the client-facing heading
				// units used by RtNavmeshPosition and WritePosition. Preserve it
				// so stationary NPCs face the direction authored in the shard DB.
				Heading: float32(spawn.Heading),
			},
			RefObjID:              spawn.RefObjID,
			NpcCodeName:           spawn.NpcCodeName,
			Radius:                spawn.Radius,
			GenerateRadius:        spawn.GenerateRadius,
			ChampionGenPercentage: spawn.ChampionGenPercentage,
			MaxTotalCount:         spawn.MaxTotalCount,
			DelayTimeMin:          spawn.DelayTimeMin,
			DelayTimeMax:          spawn.DelayTimeMax,
			SightRange:            spawn.SightRange,
			AggressType:           spawn.AggressType,
		}
		spawnsByRegion[reg.Region.ID] = append(spawnsByRegion[reg.Region.ID], model.InitSpawnAreaFromSpawnNestWithRates(s, countRate, respawnRate))
	}

	// Do not expose partially appended spawn slices to the respawn loop. The
	// SpawnArea instances themselves remain shared world entities; only this
	// collection's backing storage is protected and snapshotted.
	w.mutex.Lock()
	if w.spawns == nil {
		w.spawns = make(map[int16][]*model.SpawnArea)
	}
	for regionID, areas := range spawnsByRegion {
		w.spawns[regionID] = append(w.spawns[regionID], areas...)
	}
	w.mutex.Unlock()
}

func (w *WorldService) loadTeleportGates(spawns []model.TeleportGateSpawn) error {
	loaded := make([]*model.TeleportGate, 0, len(spawns))
	for _, spawn := range spawns {
		region, err := w.lookupRegionPtr(spawn.RegionID)
		if err != nil {
			// The catalog contains gates for every stock city, while one Agent may
			// own only a subset of continents. Gates outside this world are skipped.
			continue
		}
		offset := math32.NewVector3(spawn.X, spawn.Y, spawn.Z)
		cell, err := region.ResolveCell(offset)
		if err != nil {
			return fmt.Errorf("resolve teleport gate %s position: %w", spawn.CodeName, err)
		}
		gate := &model.TeleportGate{SRObject: model.NewSRObject()}
		gate.RefObjectID = spawn.RefObjectID
		gate.TeleportID = spawn.TeleportID
		gate.TaxRate = spawn.TaxRate
		gate.TaxActive = spawn.TaxActive
		gate.Name = spawn.CodeName
		gate.Type = "TeleportGate"
		gate.TypeInfo = model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1, TypeID4: 0}
		gate.RtNavmeshPosition = navmeshv2.RtNavmeshPosition{
			Cell: &cell, Region: region.Region, Offset: offset, Heading: spawn.Heading,
		}
		loaded = append(loaded, gate)
	}
	for _, gate := range loaded {
		w.AddVisibleObject(gate)
	}
	log.Infof("initialised %d teleport gates", len(loaded))
	return nil
}

func (w *WorldService) RegisterMovingCharacter(char model.ICharacter, movementData model.MovementData) model.MovementData {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	registered := char.BeginMovement(movementData)
	char.SetMotionState(model.Running)
	w.movingObjects[char.GetUniqueID()] = char
	w.rebuildMovingObjectSnapshotLocked()
	log.Debugf("registered moving character %d generation %d", char.GetUniqueID(), registered.Generation)
	return registered
}

// rebuildMovingObjectSnapshotLocked publishes a stable iteration slice. The
// caller must hold w.mutex. Replacing the slice, rather than mutating it in
// place, lets an in-progress game-time tick finish against its old snapshot.
func (w *WorldService) rebuildMovingObjectSnapshotLocked() {
	snapshot := make([]model.ICharacter, 0, len(w.movingObjects))
	for _, object := range w.movingObjects {
		snapshot = append(snapshot, object)
	}
	w.movingObjectSnapshot = snapshot
}

// ForEachMovingObject iterates a stable movement snapshot without allocating
// a map on every game-time tick.
func (w *WorldService) ForEachMovingObject(fn func(model.ICharacter)) {
	if w == nil || fn == nil || w.mutex == nil {
		return
	}
	w.mutex.Lock()
	if w.movingObjectSnapshot == nil && len(w.movingObjects) != 0 {
		w.rebuildMovingObjectSnapshotLocked()
	}
	snapshot := w.movingObjectSnapshot
	w.mutex.Unlock()

	for _, object := range snapshot {
		fn(object)
	}
}

func (w *WorldService) GetMovingObjects() map[uint32]model.ICharacter {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	newMap := make(map[uint32]model.ICharacter)

	for k, v := range w.movingObjects {
		newMap[k] = v
	}
	return newMap
}

func (w *WorldService) RemoveMovingObject(uid uint32) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	delete(w.movingObjects, uid)
	w.rebuildMovingObjectSnapshotLocked()
	log.Debugf("Removing moving object %d", uid)
}

func (w *WorldService) NewPosFromGlobalCoordinates(vPos *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	if vPos == nil || math.IsNaN(float64(vPos.X)) || math.IsNaN(float64(vPos.Z)) || math.IsInf(float64(vPos.X), 0) || math.IsInf(float64(vPos.Z), 0) {
		return navmeshv2.RtNavmeshPosition{}, ErrGlobalPositionInvalid
	}

	// Normal-world regions occupy an unsigned 8-bit X column and the lower
	// seven bits of Z. Values with Z's high bit set denote dungeon addressing;
	// this global-coordinate path deliberately does not manufacture that
	// representation. Validate the continuous normal-world bounds before any
	// integer conversion so negative or oversized coordinates cannot truncate or
	// wrap into a different region.
	if vPos.X < 0 || vPos.Z < 0 || vPos.X >= float32(model.WorldWidth) || vPos.Z >= float32(model.WorldHeight) {
		return navmeshv2.RtNavmeshPosition{}, fmt.Errorf("%w: x=%g z=%g", ErrGlobalPositionOutOfBounds, vPos.X, vPos.Z)
	}

	regionX := int(math.Floor(float64(vPos.X) / navmeshv2.RegionWidth))
	regionZ := int(math.Floor(float64(vPos.Z) / navmeshv2.RegionHeight))
	if regionX < 0 || regionX >= normalWorldRegionColumns || regionZ < 0 || regionZ >= normalWorldRegionRows {
		return navmeshv2.RtNavmeshPosition{}, fmt.Errorf("%w: region x=%d z=%d", ErrGlobalPositionOutOfBounds, regionX, regionZ)
	}

	regionId := utils.XAndZToInt16(byte(regionX), byte(regionZ))
	region, err := w.lookupRegionPtr(regionId)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, err
	}

	pX := vPos.X - float32(regionX)*float32(navmeshv2.RegionWidth)
	pZ := vPos.Z - float32(regionZ)*float32(navmeshv2.RegionHeight)
	// Preserve the client/character vertical coordinate while resolving the
	// server-authoritative horizontal region and cell. Navmesh heights are not
	// encoded in the same vertical scale as the SRO position packets.
	pos := math32.NewVector3(pX, vPos.Y, pZ)
	cell, err := region.ResolveCell(pos)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, err
	}

	return navmeshv2.RtNavmeshPosition{
		Cell:     &cell,
		Instance: nil,
		Region:   region.Region,
		Offset:   pos,
	}, nil
}

func (w *WorldService) GetVisibleObjectsForRegion(regionId int16) []model.ISRObject {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return append([]model.ISRObject(nil), w.visibleObjectsByRegion[regionId]...)
}

// GetActivePlayers returns a detached, UID-sorted snapshot of players whose
// clients completed character loading. Staged players must not receive world
// spawn packets before their bootstrap and GameReady exchange finishes.
func (w *WorldService) GetActivePlayers() []*model.Player {
	if w == nil || w.mutex == nil {
		return nil
	}
	w.mutex.Lock()
	players := make([]*model.Player, 0, len(w.playersByUniqueId))
	for _, player := range w.playersByUniqueId {
		if player != nil && player.GetSession() != nil && player.GetSession().IsInWorld() {
			players = append(players, player)
		}
	}
	w.mutex.Unlock()
	sort.Slice(players, func(i, j int) bool { return players[i].GetUniqueID() < players[j].GetUniqueID() })
	return players
}

func (w *WorldService) GetKnownObjectsAroundObject(object model.ISRObject) map[uint32]model.ISRObject {
	knownObjects := make(map[uint32]model.ISRObject)
	if object == nil {
		return knownObjects
	}

	// Snapshot all candidate slices while the world mutex is held, then perform
	// object-level position reads after releasing it. This prevents a visibility
	// scan from racing a region add/remove or retaining the world lock while an
	// object's own synchronization is acquired.
	objectPosition := object.GetNavmeshPosition()
	objectUniqueID := object.GetUniqueID()
	objectGameWorldID := object.GetGameWorldID()
	visibleObjects := w.snapshotVisibleObjectsForNeighbourRegions(objectPosition.Region.ID)
	for _, otherObject := range visibleObjects {
		if otherObject == nil || objectUniqueID == otherObject.GetUniqueID() {
			continue
		}
		if otherObject.GetGameWorldID() != objectGameWorldID {
			continue
		}

		if subject, ok := otherObject.(*model.Player); ok {
			state := subject.GetBodyState()
			if state == model.Stealth || state == model.Invisible || state == model.GmInvisible {
				observer, playerObserver := object.(*model.Player)
				if !playerObserver || !w.playerCanSee(observer, subject) {
					continue
				}
			}
		}
		known := object.GetKnownObjectList() != nil && object.GetKnownObjectList().Knows(otherObject)
		if w.DistanceTo(objectPosition, otherObject.GetNavmeshPosition()) <= objectVisibilityRange(otherObject, known) {
			knownObjects[otherObject.GetUniqueID()] = otherObject
		}
	}
	return knownObjects
}

// snapshotVisibleObjectsForNeighbourRegions returns a detached list of the
// current candidates. It deliberately does not return the region-owned slices
// themselves: callers may iterate without racing a movement or disconnect that
// rewrites region membership.
func (w *WorldService) snapshotVisibleObjectsForNeighbourRegions(regionId int16) []model.ISRObject {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	visibleObjects := make([]model.ISRObject, 0)
	for _, neighbourRegionID := range w.getNeighbourRegionIDsLocked(regionId) {
		visibleObjects = append(visibleObjects, w.visibleObjectsByRegion[neighbourRegionID]...)
	}
	return visibleObjects
}

// GetNeighbourPlanes explicitly reports that neighbour-plane discovery is not
// implemented. It used to log-panic for an absent region, which turned an
// invalid lookup into process-wide failure even though callers can recover.
func (w *WorldService) GetNeighbourPlanes(position navmeshv2.RtNavmeshPosition) error {
	_, err := w.lookupRegionPtr(position.Region.ID)
	if err != nil {
		return err
	}
	return ErrNeighbourPlanesUnavailable
}

func (w *WorldService) GetNeighbourRegions(regionId int16) []navmeshv2.RtNavmeshTerrain {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	regions := w.getNeighbourRegionsLocked(regionId)
	for i, region := range regions {
		regions[i] = snapshotTerrain(region)
	}
	return regions
}

// getNeighbourRegionsLocked has the original coordinate-wrap semantics and is
// shared by public neighbour snapshots and known-object scans. Callers must
// hold w.mutex.
func (w *WorldService) getNeighbourRegionsLocked(regionId int16) []navmeshv2.RtNavmeshTerrain {
	regions := make([]navmeshv2.RtNavmeshTerrain, 0)
	for _, neighbourRegionID := range w.getNeighbourRegionIDsLocked(regionId) {
		if reg, exists := w.regions[neighbourRegionID]; exists {
			regions = append(regions, *reg)
		}
	}
	return regions
}

func (w *WorldService) getNeighbourRegionIDsLocked(regionId int16) []int16 {
	regionIDs := make([]int16, 0, 9)
	x, z := utils.Int16ToXAndZ(regionId)

	for x1 := x - 1; x1 <= x+1; x1++ {
		for z1 := z - 1; z1 <= z+1; z1++ {
			neighbourRegionID := utils.XAndZToInt16(byte(x1), byte(z1))
			if _, exists := w.regions[neighbourRegionID]; exists {
				regionIDs = append(regionIDs, neighbourRegionID)
			}
		}
	}
	return regionIDs
}

func (movement movementCoordinator) updatePosition(p model.ICharacter) bool {
	w := movement.world
	movementData := p.GetMovementData()
	if movementData == nil {
		w.RemoveMovingObject(p.GetUniqueID())
		return true
	}

	currentTime := movement.now()
	movementSpeed := movementData.SpeedOverride
	if movementSpeed <= 0 {
		movementSpeed = p.GetMovementSpeed()
	}
	deltaTime := currentTime.Sub(movementData.UpdateTime)

	if deltaTime <= 0 {
		// Position was just updated
		return false
	}

	curPos := p.GetNavmeshPosition()
	curWorldVec := curPos.GetGlobalCoordinates()
	var walkVector *math32.Vector3
	nextPosIsTarget := false

	if movementData.HasDestination {
		targetWorldVec := movementData.TargetPosition.GetGlobalCoordinates()
		walkVector = targetWorldVec.Clone().Sub(curWorldVec.Clone())
		if walkVector.LengthSq() <= 0.0001 {
			// The initiating client already owns the exact B021 destination.
			// Sending B023 on normal arrival can rewind it across a region seam.
			return w.commitMovementStep(p, movementData.Generation, movementData.TargetPosition, currentTime, true, "arrived", false)
		}
		walkVector.Normalize()
	} else {
		radians := model.HeadingToRadians(movementData.DirectionAngle)
		x := math32.Cos(radians)
		z := math32.Sin(radians)

		walkVector = math32.NewVector3(x, 0, z) // already normalized
	}
	nextPosVec := curWorldVec.Clone().Add(walkVector.MultiplyScalar(movementSpeed * float32(deltaTime.Seconds())))

	newPos, err := w.NewPosFromGlobalCoordinates(nextPosVec)

	if err != nil {
		log.Warn(errors.Wrap(err, "failed to calculate new player position"))
		return w.StopMovingCharacter(p, curPos, "invalid-position", true)
	}

	if movementData.HasDestination && w.DistanceToSquared(curPos, newPos) >= w.DistanceToSquared(curPos, movementData.TargetPosition) {
		newPos = movementData.TargetPosition
		nextPosIsTarget = true
	}
	currentTerrain, err := w.lookupRegionPtr(curPos.Region.ID)
	if err != nil {
		log.Warn(err)
		return w.StopMovingCharacter(p, curPos, "missing-current-region", true)
	}
	curCell, err := currentTerrain.ResolveCell(curPos.Offset)
	if err != nil {
		log.Warn(err)
		return w.StopMovingCharacter(p, curPos, "invalid-current-cell", true)
	}
	nextTerrain, err := w.lookupRegionPtr(newPos.Region.ID)
	if err != nil {
		log.Warn(err)
		return w.StopMovingCharacter(p, curPos, "missing-next-region", true)
	}
	nextCell, err := nextTerrain.ResolveCell(newPos.Offset)
	if err != nil {
		log.Warn(err)
		return w.StopMovingCharacter(p, curPos, "invalid-next-cell", true)
	}
	newPos.Heading = model.RadiansToHeading(math32.Atan2(walkVector.Z, walkVector.X))

	nextObjects := nextTerrain.Objects
	if nextTerrain == currentTerrain {
		nextObjects = nil
	}
	hasCollision, collision, inObject, objectPosition := navmeshv2.FindObjectCollisions(curPos, newPos, currentTerrain.Objects, nextObjects)
	if hasCollision {
		// Do not correct to the exact contact line. That line belongs to the
		// blocked boundary, so a player corrected there can collide again while
		// trying to walk back (notably at Jangan's town gates). Keeping the last
		// verified position leaves the player on the walkable side of the gate.
		log.Debugf("object collision at %s with object world_id=%d local_id=%d edge=%d edge_flag=%d contact=%v",
			curPos.String(), collision.ObjectWorldID, collision.ObjectID, collision.ContactEdge.GetIndex(),
			collision.ContactEdge.GetFlag(), collision.VectorGlobal)
		return w.commitMovementStep(p, movementData.Generation, curPos, currentTime, true, "object-collision", true)
	}

	if inObject && objectPosition != nil {
		// The object transform only supplies the walkable height. Keep the
		// canonical terrain region/offset already resolved from global X/Z.
		newPos.Offset.Y = objectPosition.Offset.Y
	}

	needsTerrainProbe := curPos.Region.ID != newPos.Region.ID ||
		curCell.Index != nextCell.Index ||
		navmeshv2.HorizontalDistance(curPos, newPos) > directMotionProbeLength
	if !inObject && needsTerrainProbe {
		var collisionErr error
		hasCollision, collision, collisionErr = w.findTerrainCollisionAlongPath(curPos, newPos)
		if collisionErr != nil {
			log.Warn(errors.Wrap(collisionErr, "failed to validate terrain motion path"))
			return w.StopMovingCharacter(p, curPos, "invalid-terrain-path", true)
		}
		if hasCollision {
			// See the object-collision path above: the contact point is not a
			// safe standing position for a blocked terrain edge.
			log.Debugf("terrain collision at %s with edge flag %d", curPos.String(), collision.ContactEdge.GetFlag())
			return w.commitMovementStep(p, movementData.Generation, curPos, currentTime, true, "terrain-collision", true)
		}
	}

	// Check terrain collisions

	// Reaching an accepted destination is not a correction. Reserve B023 for
	// collision/invalid-position stops so normal arrival cannot visibly snap
	// the stock client back to the server's last 20 Hz sample.
	return w.commitMovementStep(p, movementData.Generation, newPos, currentTime, nextPosIsTarget, map[bool]string{true: "arrived", false: "moving"}[nextPosIsTarget], false)
}

func (w *WorldService) commitMovementStep(p model.ICharacter, generation uint64, newPosition navmeshv2.RtNavmeshPosition, updateTime time.Time, completed bool, reason string, correction bool) bool {
	oldPosition := p.GetNavmeshPosition()
	regionChanged := oldPosition.Region.ID != newPosition.Region.ID
	activeMovement := p.GetMovementData()
	hasDestination := activeMovement != nil && activeMovement.Generation == generation && activeMovement.HasDestination
	player, mounted := w.mountedMovementPair(p)
	var mountedOldPosition navmeshv2.RtNavmeshPosition
	mountedRegionChanged := false
	if mounted != nil {
		mountedOldPosition = mounted.GetNavmeshPosition()
		mountedRegionChanged = mountedOldPosition.Region.ID != newPosition.Region.ID
	}
	w.mutex.Lock()
	committed := false
	if completed {
		committed = p.FinalizeMovement(generation, newPosition, reason)
	} else {
		committed = p.CommitMovementStep(generation, newPosition, updateTime)
	}
	if !committed {
		w.mutex.Unlock()
		return false
	}
	if regionChanged {
		w.removeVisibleObjectFromRegionLocked(p, oldPosition.Region.ID)
		w.addVisibleObjectToRegionLocked(p, newPosition.Region.ID)
	}
	if completed {
		p.SetMotionState(model.NoMotion)
		delete(w.movingObjects, p.GetUniqueID())
		w.rebuildMovingObjectSnapshotLocked()
	}
	if mounted != nil {
		mounted.SetNavmeshPosition(newPosition)
		mounted.SetMotionState(p.GetMotionState())
		if mountedRegionChanged {
			w.removeVisibleObjectFromRegionLocked(mounted, mountedOldPosition.Region.ID)
			w.addVisibleObjectToRegionLocked(mounted, newPosition.Region.ID)
		}
	}
	w.mutex.Unlock()
	if player != nil {
		w.positionPersistence.MarkDirty(player)
	}
	traceMovementFor(p, "commit uid=%d generation=%d completed=%t reason=%s position=%s", p.GetUniqueID(), generation, completed, reason, newPosition.String())
	// While mounted, the client moves and corrects the transport entity. The
	// player model is visually attached to it by B0CB, so publishing a player
	// B023 here would detach or rewind the rider to the old transport position.
	if correction || (regionChanged && !hasDestination) {
		w.sendMovementPositionUpdate(p, player, mounted)
	}
	// Visibility is reconciled from both members of the mounted pair. Their
	// region membership and coordinates were committed atomically above.
	_ = w.ReconcileCharacterVisibility(p)
	if mounted != nil {
		_ = w.ReconcileCharacterVisibility(mounted)
	}
	return completed
}

func (w *WorldService) StopMovingCharacter(p model.ICharacter, position navmeshv2.RtNavmeshPosition, reason string, correction bool) bool {
	oldPosition := p.GetNavmeshPosition()
	regionChanged := oldPosition.Region.ID != position.Region.ID
	player, mounted := w.mountedMovementPair(p)
	var mountedOldPosition navmeshv2.RtNavmeshPosition
	mountedRegionChanged := false
	if mounted != nil {
		mountedOldPosition = mounted.GetNavmeshPosition()
		mountedRegionChanged = mountedOldPosition.Region.ID != position.Region.ID
	}
	w.mutex.Lock()
	generation := p.CancelMovement(position, reason)
	p.SetMotionState(model.NoMotion)
	delete(w.movingObjects, p.GetUniqueID())
	w.rebuildMovingObjectSnapshotLocked()
	if regionChanged {
		w.removeVisibleObjectFromRegionLocked(p, oldPosition.Region.ID)
		w.addVisibleObjectToRegionLocked(p, position.Region.ID)
	}
	if mounted != nil {
		mounted.SetNavmeshPosition(position)
		mounted.SetMotionState(model.NoMotion)
		if mountedRegionChanged {
			w.removeVisibleObjectFromRegionLocked(mounted, mountedOldPosition.Region.ID)
			w.addVisibleObjectToRegionLocked(mounted, position.Region.ID)
		}
	}
	w.mutex.Unlock()
	if player != nil {
		w.positionPersistence.MarkDirty(player)
		if reason == "teleport" || regionChanged {
			if err := w.positionPersistence.FlushPlayer(player.GetUniqueID()); err != nil {
				log.Warnf("forced position remains queued for player %d: %v", player.GetUniqueID(), err)
			}
		}
	}
	traceMovementFor(p, "stop uid=%d generation=%d reason=%s position=%s", p.GetUniqueID(), generation, reason, position.String())
	if correction {
		w.sendMovementPositionUpdate(p, player, mounted)
	}
	_ = w.ReconcileCharacterVisibility(p)
	if mounted != nil {
		_ = w.ReconcileCharacterVisibility(mounted)
	}
	return true
}

func (w *WorldService) mountedMovementPair(character model.ICharacter) (*model.Player, *model.COS) {
	player, ok := character.(*model.Player)
	if !ok {
		return nil, nil
	}
	transport, mounted := w.MountedTransport(player)
	if !mounted {
		return player, nil
	}
	return player, transport
}

func (w *WorldService) sendMovementPositionUpdate(character model.ICharacter, player *model.Player, mounted *model.COS) {
	if player == nil || mounted == nil {
		character.SendPositionUpdate()
		return
	}
	packet := model.BuildPositionUpdatePacket(mounted)
	player.SendToSelfAndKnown(&packet)
}

func (w *WorldService) AddVisibleObjectToRegion(p model.ICharacter, regionId int16) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.addVisibleObjectToRegionLocked(p, regionId)
}

func (w *WorldService) addVisibleObjectToRegionLocked(p model.ICharacter, regionId int16) {
	alreadyInList := false
	for _, obj := range w.visibleObjectsByRegion[regionId] {
		if obj.GetUniqueID() == p.GetUniqueID() {
			alreadyInList = true
		}
	}
	if !alreadyInList {
		w.visibleObjectsByRegion[regionId] = append(w.visibleObjectsByRegion[regionId], p)
	}
}

func (w *WorldService) RemoveVisibleObjectFromRegion(p model.ICharacter, regionId int16) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.removeVisibleObjectFromRegionLocked(p, regionId)
}

func (w *WorldService) removeVisibleObjectFromRegionLocked(p model.ICharacter, regionId int16) {
	for i, obj := range w.visibleObjectsByRegion[regionId] {
		if obj.GetUniqueID() == p.GetUniqueID() {
			s := w.visibleObjectsByRegion[regionId]
			s[i] = s[len(s)-1]
			w.visibleObjectsByRegion[regionId] = s[:len(s)-1]
			return
		}
	}
}

func (w *WorldService) GetMovementData(p model.ICharacter) *model.MovementData {
	return p.GetMovementData()
}

func (w *WorldService) FlushDirtyPositionsIfDue(now time.Time) {
	w.positionPersistence.FlushDue(now)
}

func (w *WorldService) FlushPlayerPosition(playerUID uint32) error {
	return w.positionPersistence.FlushPlayer(playerUID)
}

// DrainPositionPersistence flushes every queued checkpoint using the caller's
// shutdown context. AgentServer invokes it after sessions have stopped adding
// movement updates and before it closes the shard pool that backs the injected
// repository.
func (w *WorldService) DrainPositionPersistence(ctx context.Context) error {
	if w == nil || w.mutex == nil {
		return ErrWorldRepositoryConfiguration
	}
	w.mutex.Lock()
	positionPersistence := w.positionPersistence
	w.mutex.Unlock()
	if positionPersistence == nil {
		return ErrPositionPersistenceNoRepository
	}
	return positionPersistence.Drain(ctx)
}

func (w *WorldService) DistanceToSquared(from, to navmeshv2.RtNavmeshPosition) float32 {
	vFromGlobal := from.GetGlobalCoordinates()
	vToGlobal := to.GetGlobalCoordinates()

	dx := vFromGlobal.X - vToGlobal.X
	dy := vFromGlobal.Y - vToGlobal.Y
	dz := vFromGlobal.Z - vToGlobal.Z

	return dx*dx + dy*dy + dz*dz
}

func (w *WorldService) DistanceTo(from, to navmeshv2.RtNavmeshPosition) float32 {
	return math32.Sqrt(w.DistanceToSquared(from, to))
}

func (w *WorldService) GetPlayerByCharName(charName string) (*model.Player, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if player, exists := w.playersByCharName[charName]; exists {
		return player, nil
	}

	return nil, errors.New(fmt.Sprintf("player with char name %s does not exist", charName))
}

func (w *WorldService) Broadcast(packet network.Packet) {
	w.mutex.Lock()
	players := make([]*model.Player, 0, len(w.playersByUniqueId))
	for _, player := range w.playersByUniqueId {
		players = append(players, player)
	}
	w.mutex.Unlock()

	// Sender.Send can wait for a slow socket. The world mutex protects only the
	// membership index, never network I/O; a disconnect or movement update may
	// therefore progress while this broadcast is still draining.
	for _, player := range players {
		if player == nil {
			continue
		}
		if session := player.GetSession(); session != nil {
			_ = session.Send(packet)
		}
	}
}
