package model

import (
	"math/rand"
	"sync"
	"time"
)

type SpawnArea struct {
	Spawn
	NPCs               map[uint32]*NPC
	NPCDied            chan uint32
	RespawnTimes       []time.Time
	tickRate           int
	respawnRatePercent int
	targetCount        int
	mutex              sync.Mutex
}

// AddNPC records a live spawn without exposing the spawn maps to concurrent
// combat and respawn ticks.
func (s *SpawnArea) AddNPC(npc *NPC) {
	if s == nil || npc == nil {
		return
	}
	s.mutex.Lock()
	if s.NPCs == nil {
		s.NPCs = make(map[uint32]*NPC)
	}
	s.NPCs[npc.GetUniqueID()] = npc
	s.mutex.Unlock()
}

func (s *SpawnArea) DrainNPCs() []*NPC {
	if s == nil {
		return nil
	}
	s.mutex.Lock()
	npcs := make([]*NPC, 0, len(s.NPCs))
	for _, npc := range s.NPCs {
		npcs = append(npcs, npc)
	}
	s.NPCs = make(map[uint32]*NPC)
	s.RespawnTimes = nil
	s.targetCount = 0
	s.mutex.Unlock()
	return npcs
}

// MarkNPCDead removes one live spawn and schedules exactly one replacement.
func (s *SpawnArea) MarkNPCDead(uniqueID uint32) bool {
	if s == nil || uniqueID == 0 {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, exists := s.NPCs[uniqueID]; !exists {
		return false
	}
	delete(s.NPCs, uniqueID)
	if len(s.NPCs)+len(s.RespawnTimes) < s.targetCount {
		s.RespawnTimes = append(s.RespawnTimes, s.calculateRespawnTime())
	}
	return true
}

// TakeDueRespawns atomically removes and returns the number of replacements
// due on this manager tick.
func (s *SpawnArea) TakeDueRespawns(now time.Time) int {
	if s == nil {
		return 0
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	due := 0
	pending := s.RespawnTimes[:0]
	for _, respawnTime := range s.RespawnTimes {
		if !now.Before(respawnTime) {
			due++
			continue
		}
		pending = append(pending, respawnTime)
	}
	s.RespawnTimes = pending
	return due
}

func (s *SpawnArea) RetryRespawn() {
	if s == nil {
		return
	}
	s.mutex.Lock()
	s.RespawnTimes = append(s.RespawnTimes, time.Now().Add(time.Second))
	s.mutex.Unlock()
}

func InitSpawnAreaFromSpawnNest(spawn Spawn) *SpawnArea {
	return InitSpawnAreaFromSpawnNestWithRates(spawn, 100, 100)
}

// InitSpawnAreaFromSpawnNestWithRates preserves the DB-authored spawn as the
// 100% baseline while allowing a server-wide count and respawn multiplier.
func InitSpawnAreaFromSpawnNestWithRates(spawn Spawn, countRatePercent, respawnRatePercent int) *SpawnArea {
	if countRatePercent <= 0 {
		countRatePercent = 100
	}
	if respawnRatePercent <= 0 {
		respawnRatePercent = 100
	}
	area := &SpawnArea{
		Spawn:              spawn,
		NPCs:               make(map[uint32]*NPC),
		NPCDied:            make(chan uint32, 100),
		RespawnTimes:       make([]time.Time, 0),
		respawnRatePercent: respawnRatePercent,
	}

	initialSpawnTime := time.Now()

	spawnCount := (spawn.MaxTotalCount*countRatePercent + 99) / 100
	area.targetCount = spawnCount
	for i := 0; i < spawnCount; i++ {
		area.RespawnTimes = append(area.RespawnTimes, initialSpawnTime)
	}

	//go area.Respawn()
	return area
}

// ApplyGameplayRates reconciles pending population against a new server-wide
// target. Living mobs above a reduced target retire naturally when they die.
func (s *SpawnArea) ApplyGameplayRates(countRatePercent, respawnRatePercent int) {
	if s == nil {
		return
	}
	if countRatePercent <= 0 {
		countRatePercent = 100
	}
	if respawnRatePercent <= 0 {
		respawnRatePercent = 100
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.respawnRatePercent = respawnRatePercent
	s.targetCount = (s.MaxTotalCount*countRatePercent + 99) / 100
	population := len(s.NPCs) + len(s.RespawnTimes)
	if population < s.targetCount {
		for deficit := s.targetCount - population; deficit > 0; deficit-- {
			s.RespawnTimes = append(s.RespawnTimes, s.calculateRespawnTime())
		}
		return
	}
	excess := population - s.targetCount
	if excess > len(s.RespawnTimes) {
		excess = len(s.RespawnTimes)
	}
	if excess > 0 {
		s.RespawnTimes = s.RespawnTimes[:len(s.RespawnTimes)-excess]
	}
}

func (s *SpawnArea) calculateRespawnTime() time.Time {
	deathTime := time.Now()
	delayDiff := s.DelayTimeMax - s.DelayTimeMin
	respawnSeconds := s.DelayTimeMin
	if delayDiff > 0 {
		respawnSeconds = rand.Intn(delayDiff) + s.DelayTimeMin
	}
	if respawnSeconds <= 0 {
		respawnSeconds = 1
	}
	rate := s.respawnRatePercent
	if rate <= 0 {
		rate = 100
	}
	respawnDelay := time.Second * time.Duration(respawnSeconds) * 100 / time.Duration(rate)
	if respawnDelay < 100*time.Millisecond {
		respawnDelay = 100 * time.Millisecond
	}

	return deathTime.Add(respawnDelay)
}
