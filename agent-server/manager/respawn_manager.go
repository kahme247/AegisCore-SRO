package manager

import (
	"fmt"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"math"
	"math/rand"
	"sync"
	"time"
)

type RespawnManager struct {
	Manager
	referenceDataMutex sync.RWMutex
	referenceData      *model.ReferenceDataCatalog
	gameplayRatesMutex sync.RWMutex
	gameplayRates      interface{ Current() model.GameplayRates }
	rarityMutex        sync.RWMutex
	giantSpawnRatio    int
	rollPercent        func(int) bool
	rollOneIn          func(int) bool
}

const maxRespawnPositionAttempts = 32

type respawnPositionWorld interface {
	NewPosFromGlobalCoordinates(*math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
	CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool
}

type dungeonRespawnPositionWorld interface {
	GetRegion(int16) (navmeshv2.RtNavmeshTerrain, error)
}

var respawnManagerInstance *RespawnManager
var respawnManagerOnce sync.Once

// ErrRespawnReferenceCatalogUnavailable identifies a manager that was started
// without the boot-time immutable reference snapshot.
var ErrRespawnReferenceCatalogUnavailable = errors.New("respawn reference catalog is not configured")

// ErrRespawnReferenceCharacterUnavailable identifies a spawn row whose NPC
// reference does not exist in the configured snapshot.
var ErrRespawnReferenceCharacterUnavailable = errors.New("respawn reference character is not configured")

var ErrScheduledUniqueSpawnUnavailable = errors.New("scheduled unique spawn is unavailable")

func GetRespawnManagerInstance() *RespawnManager {
	respawnManagerOnce.Do(func() {
		respawnManagerInstance = &RespawnManager{
			giantSpawnRatio: 1000,
			rollPercent: func(chance int) bool {
				return chance >= 100 || chance > 0 && rand.Intn(100) < chance
			},
			rollOneIn: func(ratio int) bool {
				return ratio > 0 && rand.Intn(ratio) == 0
			},
		}
		respawnManagerInstance.Name = "RespawnManager"
		respawnManagerInstance.initialDelay = time.Second
		respawnManagerInstance.rate = time.Millisecond * 100
		respawnManagerInstance.runnerFunc = respawnManagerInstance.respawn
	})
	return respawnManagerInstance
}

// ConfigureGiantSpawnRatio mirrors SR_GameServer's GiantMonster_SpawnRatio.
// The value is a denominator (1000 means one giant per 1000 eligible spawns);
// zero disables runtime giant generation. Authored giant/party rows are not
// affected.
func (s *RespawnManager) ConfigureGiantSpawnRatio(ratio int) {
	if s == nil {
		return
	}
	if ratio < 0 {
		ratio = 0
	}
	s.rarityMutex.Lock()
	s.giantSpawnRatio = ratio
	s.rarityMutex.Unlock()
}

func (s *RespawnManager) selectRuntimeRarity(refRarity int, championPercentage int) model.MonsterRarity {
	if refRarity != int(model.MonsterNormal) {
		if refRarity > 0 && refRarity <= int(^uint8(0)) {
			return model.MonsterRarity(refRarity)
		}
		return model.MonsterNormal
	}
	s.rarityMutex.RLock()
	giantRatio, rollOneIn, rollPercent := s.giantSpawnRatio, s.rollOneIn, s.rollPercent
	s.rarityMutex.RUnlock()
	if rollOneIn != nil && rollOneIn(giantRatio) {
		return model.MonsterGiant
	}
	championPercentage = min(max(championPercentage, 0), 100)
	if rollPercent != nil && rollPercent(championPercentage) {
		return model.MonsterChampion
	}
	return model.MonsterNormal
}

// ConfigureReferenceData supplies the immutable catalog produced by the
// successful gamedata boot phase. It deliberately does not consult the legacy
// ReferenceDataService: respawns must not open a database connection after
// startup just to resolve an NPC type.
func (s *RespawnManager) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) error {
	if s == nil || catalog == nil {
		return ErrRespawnReferenceCatalogUnavailable
	}
	s.referenceDataMutex.Lock()
	s.referenceData = catalog
	s.referenceDataMutex.Unlock()
	return nil
}

func (s *RespawnManager) configuredReferenceData() *model.ReferenceDataCatalog {
	if s == nil {
		return nil
	}
	s.referenceDataMutex.RLock()
	catalog := s.referenceData
	s.referenceDataMutex.RUnlock()
	return catalog
}

func (s *RespawnManager) ConfigureGameplayRates(rates interface{ Current() model.GameplayRates }) {
	if s == nil {
		return
	}
	s.gameplayRatesMutex.Lock()
	s.gameplayRates = rates
	s.gameplayRatesMutex.Unlock()
}

func (s *RespawnManager) currentGameplayRates() model.GameplayRates {
	if s == nil {
		return model.GameplayRates{MobCountRatePercent: 100, MobRespawnRatePercent: 100}
	}
	s.gameplayRatesMutex.RLock()
	provider := s.gameplayRates
	s.gameplayRatesMutex.RUnlock()
	if provider == nil {
		return model.GameplayRates{MobCountRatePercent: 100, MobRespawnRatePercent: 100}
	}
	return provider.Current()
}

func (s *RespawnManager) respawn() {
	world := service.GetWorldServiceInstance()
	for s.started {
		select {
		case <-s.ticker.C:
			rates := s.currentGameplayRates()
			for _, regionID := range world.GetRegionIDs() {
				for _, spawnArea := range world.GetSpawnsForRegion(regionID) {
					spawnArea.ApplyGameplayRates(int(rates.MobCountRatePercent), int(rates.MobRespawnRatePercent))
					logrus.Tracef("respawning %s on position R[%d] (%f|%f|%f)", spawnArea.NpcCodeName, spawnArea.Position.Region.ID, spawnArea.Position.Offset.X, spawnArea.Position.Offset.Y, spawnArea.Position.Offset.Z)
					for i := 0; i < spawnArea.TakeDueRespawns(time.Now()); i++ {
						if err := s.spawnNpc(spawnArea); err != nil {
							logrus.Errorf("failed to respawn npc: %v", err)
							spawnArea.RetryRespawn()
						}
					}
				}
			}
		}
	}
}

func (s *RespawnManager) spawnNpc(spawnArea *model.SpawnArea) error {
	npc, err := s.newNPCForSpawn(spawnArea)
	if err != nil {
		return err
	}
	npc.RtNavmeshPosition, err = generateRandomPositionInRadius(
		service.GetWorldServiceInstance(),
		spawnArea,
		randomRespawnOffset,
	)
	if err != nil {
		return err
	}
	service.GetWorldServiceInstance().AddVisibleObject(npc)
	npc.BindSpawnArea(spawnArea)
	spawnArea.AddNPC(npc)
	return nil
}

// SpawnQuestNPC creates a transient quest-owned monster near the player. It
// deliberately does not bind the NPC to a SpawnArea: death/despawn must not
// enqueue an ordinary world respawn for a periodic quest pursuer.
func (s *RespawnManager) SpawnQuestNPC(codeName string, center navmeshv2.RtNavmeshPosition, radius float32) (*model.NPC, error) {
	return s.spawnTransientNPC(codeName, center, radius, nil)
}

// SpawnItemSummonNPC materializes the ordinary monster class authored by the
// summon-scroll catalog. Unique classes are rejected: these event scrolls
// create strong, giant, or party field mobs rather than world uniques.
func (s *RespawnManager) SpawnItemSummonNPC(codeName string, center navmeshv2.RtNavmeshPosition, radius float32, rarity model.MonsterRarity) (*model.NPC, error) {
	if !rarity.Valid() || rarity.IsUniqueBoss() {
		return nil, ErrRespawnReferenceCharacterUnavailable
	}
	return s.spawnTransientNPC(codeName, center, radius, &rarity)
}

func (s *RespawnManager) spawnTransientNPC(codeName string, center navmeshv2.RtNavmeshPosition, radius float32, forcedRarity *model.MonsterRarity) (*model.NPC, error) {
	catalog := s.configuredReferenceData()
	if catalog == nil {
		return nil, ErrRespawnReferenceCatalogUnavailable
	}
	refChar, exists := catalog.CharacterByCodeName(codeName)
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrRespawnReferenceCharacterUnavailable, codeName)
	}
	area := &model.SpawnArea{Spawn: model.Spawn{
		Position: center, RefObjID: uint64(refChar.ID), NpcCodeName: codeName,
		ChampionGenPercentage: 0, AggressType: byte(model.Aggressive), SightRange: 100,
	}}
	npc, err := s.newNPCForSpawnWithRarity(area, forcedRarity)
	if err != nil {
		return nil, err
	}
	world := service.GetWorldServiceInstance()
	npc.RtNavmeshPosition = center
	if radius > 0 {
		origin := center.GetGlobalCoordinates()
		for attempt := 0; attempt < maxRespawnPositionAttempts; attempt++ {
			x, z := questSpawnOffset(radius, rand.Float64(), rand.Float64())
			candidate, resolveErr := world.NewPosFromGlobalCoordinates(math32.NewVector3(origin.X+x, origin.Y, origin.Z+z))
			if resolveErr != nil || candidate.Region.ID != center.Region.ID || !world.CanMoveDirectly(center, candidate) {
				continue
			}
			candidate.Offset.Y = center.Offset.Y
			npc.RtNavmeshPosition = candidate
			break
		}
	}
	world.AddVisibleObject(npc)
	return npc, nil
}

// SpawnServerEventMonster materializes the binary-authored class-3 reward at
// its exact region/local coordinates. SR_GameServer 0x0066FF00 reads the
// reward subtype as RefObjID + RegionID + XYZ and does not use a nest radius.
func (s *RespawnManager) SpawnServerEventMonster(refObjectID uint32, regionID int16, x, y, z float32) (*model.NPC, error) {
	if s == nil || refObjectID == 0 || regionID == 0 {
		return nil, ErrRespawnReferenceCharacterUnavailable
	}
	catalog := s.configuredReferenceData()
	if catalog == nil {
		return nil, ErrRespawnReferenceCatalogUnavailable
	}
	refChar, exists := catalog.Character(refObjectID)
	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrRespawnReferenceCharacterUnavailable, refObjectID)
	}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(regionID), Offset: math32.NewVector3(x, y, z)}
	return s.SpawnQuestNPC(refChar.CodeName, position, 0)
}

// SpawnScheduledUnique creates one transient unique at an authored spawn nest.
// It deliberately does not bind the NPC to the area's ordinary population, so
// an event execution cannot permanently increase automatic respawn counts.
func (s *RespawnManager) SpawnScheduledUnique(codeName string, regionID int16, radius float32) (*model.NPC, error) {
	if s == nil || codeName == "" || regionID == 0 || radius < 0 || radius > 1000 {
		return nil, fmt.Errorf("%w: invalid request", ErrScheduledUniqueSpawnUnavailable)
	}
	catalog := s.configuredReferenceData()
	if catalog == nil {
		return nil, ErrRespawnReferenceCatalogUnavailable
	}
	refChar, exists := catalog.CharacterByCodeName(codeName)
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrRespawnReferenceCharacterUnavailable, codeName)
	}
	if !model.MonsterRarity(refChar.Rarity).IsUniqueBoss() {
		return nil, fmt.Errorf("%w: %s is not a unique monster", ErrScheduledUniqueSpawnUnavailable, codeName)
	}
	world := service.GetWorldServiceInstance()
	for _, area := range world.GetSpawnsForRegion(regionID) {
		if area != nil && area.NpcCodeName == codeName {
			if radius == 0 {
				radius = float32(area.GenerateRadius)
			}
			return s.SpawnQuestNPC(codeName, area.Position, radius)
		}
	}
	return nil, fmt.Errorf("%w: no authored nest for %s in region %d", ErrScheduledUniqueSpawnUnavailable, codeName, regionID)
}

func questSpawnOffset(radius float32, angleUnit, distanceUnit float64) (float32, float32) {
	if radius <= 0 {
		return 0, 0
	}
	angle := angleUnit * 2 * math.Pi
	distance := float64(radius) * distanceUnit
	return float32(math.Cos(angle) * distance), float32(math.Sin(angle) * distance)
}

// newNPCForSpawn resolves the NPC through the configured immutable catalog
// before it touches world state. Keeping the reference lookup separate makes
// a missing row fail closed rather than triggering the legacy per-call DB
// loader during a tick.
func (s *RespawnManager) newNPCForSpawn(spawnArea *model.SpawnArea) (*model.NPC, error) {
	return s.newNPCForSpawnWithRarity(spawnArea, nil)
}

func (s *RespawnManager) newNPCForSpawnWithRarity(spawnArea *model.SpawnArea, forcedRarity *model.MonsterRarity) (*model.NPC, error) {
	if spawnArea == nil {
		return nil, fmt.Errorf("%w: spawn area is nil", ErrRespawnReferenceCharacterUnavailable)
	}
	catalog := s.configuredReferenceData()
	if catalog == nil {
		return nil, ErrRespawnReferenceCatalogUnavailable
	}

	refObjectID := uint32(spawnArea.RefObjID)
	refChar, exists := catalog.Character(refObjectID)
	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrRespawnReferenceCharacterUnavailable, refObjectID)
	}

	npc := &model.NPC{
		SRObject: model.NewSRObject(),
		Type:     "NPC",
		Mutex:    &sync.Mutex{},
	}
	npc.KnownObjectList = model.NewKnownObjectList(npc)
	npc.Name = spawnArea.NpcCodeName
	npc.RefObjectID = refObjectID
	npc.TypeInfo = refChar.TypeInfo
	npc.ConfigureMovementSpeeds(float32(refChar.Speed1), float32(refChar.Speed2))
	if refChar.TypeInfo.IsNPCMob() {
		rarity := s.selectRuntimeRarity(refChar.Rarity, spawnArea.ChampionGenPercentage)
		if forcedRarity != nil {
			rarity = *forcedRarity
		}
		npc.SetRarity(byte(rarity))
		multiplier := rarity.RuntimeStatMultiplier()
		if forcedRarity != nil && rarity.IsParty() {
			multiplier = rarity.BaseClass().RuntimeStatMultiplier()
		}
		experience := uint64(0)
		if refChar.ExpToGive > 0 {
			experience = uint64(refChar.ExpToGive) * uint64(multiplier)
		}
		maxHP := refChar.MaxHP
		if maxHP > 0 && multiplier > 1 {
			maxHP *= multiplier
		}
		npc.ConfigureCombat(maxHP, experience)
		npc.ConfigureDecayTime(refChar.DecayTime)
		npc.SetLifeState(model.Alive)
		npc.ConfigureAI(refChar.Level, spawnArea.AggressType == byte(model.Aggressive), spawnArea.SightRange)
		npc.ConfigureAttackSkills(
			uint32(max(refChar.DefaultSkill1, 0)),
			uint32(max(refChar.DefaultSkill2, 0)),
			uint32(max(refChar.DefaultSkill3, 0)),
			uint32(max(refChar.DefaultSkill4, 0)),
			uint32(max(refChar.DefaultSkill5, 0)),
			uint32(max(refChar.DefaultSkill6, 0)),
			uint32(max(refChar.DefaultSkill7, 0)),
			uint32(max(refChar.DefaultSkill8, 0)),
			uint32(max(refChar.DefaultSkill9, 0)),
			uint32(max(refChar.DefaultSkill10, 0)),
		)
	}
	return npc, nil
}

func randomRespawnOffset(radius float32) float32 {
	return (rand.Float32()*2 - 1) * radius
}

func generateRandomPositionInRadius(
	world respawnPositionWorld,
	s *model.SpawnArea,
	randomOffset func(float32) float32,
) (navmeshv2.RtNavmeshPosition, error) {
	if world == nil {
		return navmeshv2.RtNavmeshPosition{}, errors.New("respawn position world is unavailable")
	}
	if s == nil {
		return navmeshv2.RtNavmeshPosition{}, errors.New("spawn area is nil")
	}

	authoredGlobal := s.Position.GetGlobalCoordinates()
	var authored navmeshv2.RtNavmeshPosition
	var err error
	if navmeshv2.IsDungeonRegion(s.Position.Region.ID) {
		resolver, ok := world.(dungeonRespawnPositionWorld)
		if !ok {
			return navmeshv2.RtNavmeshPosition{}, errors.New("dungeon respawn position resolver is unavailable")
		}
		region, regionErr := resolver.GetRegion(s.Position.Region.ID)
		if regionErr != nil {
			return navmeshv2.RtNavmeshPosition{}, errors.Wrap(regionErr, "authored dungeon respawn region is unavailable")
		}
		cell, cellErr := region.ResolveCell(s.Position.Offset)
		if cellErr != nil {
			return navmeshv2.RtNavmeshPosition{}, errors.Wrap(cellErr, "authored dungeon respawn position is unavailable")
		}
		authored = navmeshv2.RtNavmeshPosition{Region: region.Region, Offset: s.Position.Offset.Clone(), Cell: &cell}
	} else {
		authored, err = world.NewPosFromGlobalCoordinates(authoredGlobal)
		if err != nil {
			return navmeshv2.RtNavmeshPosition{}, errors.Wrap(err, "authored respawn position is unavailable")
		}
	}
	authored.Offset.Y = s.Position.Offset.Y
	authored.Heading = s.Position.Heading

	if s.GenerateRadius <= 0 || randomOffset == nil {
		return authored, nil
	}

	radius := float32(s.GenerateRadius)
	for attempt := 0; attempt < maxRespawnPositionAttempts; attempt++ {
		if navmeshv2.IsDungeonRegion(authored.Region.ID) {
			resolver := world.(dungeonRespawnPositionWorld)
			region, regionErr := resolver.GetRegion(authored.Region.ID)
			if regionErr != nil {
				continue
			}
			offset := math32.NewVector3(
				authored.Offset.X+randomOffset(radius),
				authored.Offset.Y,
				authored.Offset.Z+randomOffset(radius),
			)
			cell, cellErr := region.ResolveCell(offset)
			if cellErr != nil {
				continue
			}
			candidate := navmeshv2.RtNavmeshPosition{Region: region.Region, Offset: offset, Cell: &cell, Heading: s.Position.Heading}
			if world.CanMoveDirectly(authored, candidate) {
				return candidate, nil
			}
			continue
		}
		candidateGlobal := math32.NewVector3(
			authoredGlobal.X+randomOffset(radius),
			authoredGlobal.Y,
			authoredGlobal.Z+randomOffset(radius),
		)
		candidate, err := world.NewPosFromGlobalCoordinates(candidateGlobal)
		if err != nil || candidate.Region.ID != authored.Region.ID {
			continue
		}
		candidate.Offset.Y = s.Position.Offset.Y
		candidate.Heading = s.Position.Heading
		if !world.CanMoveDirectly(authored, candidate) {
			continue
		}
		return candidate, nil
	}

	// The spawn catalog's authored point was resolved during world bootstrap.
	// Falling back to it keeps respawn bounded and inside the loaded region when
	// every randomized candidate crosses a region boundary or an obstacle.
	return authored, nil
}

func removeArrayElement(array []time.Time, index int) []time.Time {
	if index >= len(array) {
		return array
	}
	array[len(array)-1], array[index] = array[index], array[len(array)-1]
	return array[:len(array)-1]
}
