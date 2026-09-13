package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

type slavePursuitRepositoryFake struct {
	ticks       int
	confirms    int
	characterID int
	eligible    bool
	result      model.SlavePursuitTickResult
	confirmed   model.CharacterQuest
	err         error
}

func (f *slavePursuitRepositoryFake) TickSlavePursuit(_ context.Context, characterID int, eligible bool) (model.SlavePursuitTickResult, error) {
	f.ticks++
	f.characterID, f.eligible = characterID, eligible
	return f.result, f.err
}

func (f *slavePursuitRepositoryFake) ConfirmSlavePursuitSpawn(_ context.Context, characterID int) (model.CharacterQuest, error) {
	f.confirms++
	f.characterID = characterID
	return f.confirmed, f.err
}

type questNPCSpawnerFake struct {
	calls    int
	codeName string
	center   navmeshv2.RtNavmeshPosition
	radius   float32
	err      error
}

func (f *questNPCSpawnerFake) SpawnQuestNPC(codeName string, center navmeshv2.RtNavmeshPosition, radius float32) (*model.NPC, error) {
	f.calls++
	f.codeName, f.center, f.radius = codeName, center, radius
	return &model.NPC{}, f.err
}

func TestNativeQuestManagerSpawnsAndConfirmsSlavePursuer(t *testing.T) {
	quest := model.CharacterQuest{QuestID: model.SlavePursuitQuestID, Status: model.QuestProgressed}
	confirmed := quest
	confirmed.Data1 = uint64(59) << 32
	repository := &slavePursuitRepositoryFake{result: model.SlavePursuitTickResult{Quest: quest, Spawn: true}, confirmed: confirmed}
	spawner := &questNPCSpawnerFake{}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000)}
	player := &model.Player{ID: 44, LifeState: model.Alive, Quests: []model.CharacterQuest{quest}, SRObject: model.SRObject{RtNavmeshPosition: position}}
	player.SetJob(model.PlayerJob{Type: model.PlayerJobNone})
	manager := NewNativeQuestManager(repository, spawner)
	manager.players = func() []*model.Player { return []*model.Player{player} }
	manager.regionLoaded = func(int16) bool { return true }
	now := time.Now()
	manager.Tick(now)
	if repository.ticks != 1 || repository.confirms != 1 || repository.characterID != 44 || !repository.eligible {
		t.Fatalf("repository = %+v", repository)
	}
	if spawner.calls != 1 || spawner.codeName != model.SlavePursuitMobCodeName || spawner.radius != model.SlavePursuitSpawnRadius || spawner.center.Region.ID != position.Region.ID {
		t.Fatalf("spawner = %+v", spawner)
	}
	if got := player.QuestSnapshot(); len(got) != 1 || got[0].Data1 != confirmed.Data1 {
		t.Fatalf("live quests = %+v", got)
	}
	manager.Tick(now.Add(500 * time.Millisecond))
	if repository.ticks != 1 {
		t.Fatalf("sub-second ticks = %d", repository.ticks)
	}
}

func TestNativeQuestManagerLeavesZeroCountdownWhenSpawnFails(t *testing.T) {
	quest := model.CharacterQuest{QuestID: model.SlavePursuitQuestID, Status: model.QuestProgressed}
	repository := &slavePursuitRepositoryFake{result: model.SlavePursuitTickResult{Quest: quest, Spawn: true}}
	spawner := &questNPCSpawnerFake{err: errors.New("world admission failed")}
	player := &model.Player{ID: 44, LifeState: model.Alive, Quests: []model.CharacterQuest{quest}, SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000)}}}
	manager := NewNativeQuestManager(repository, spawner)
	manager.players = func() []*model.Player { return []*model.Player{player} }
	manager.regionLoaded = func(int16) bool { return true }
	manager.Tick(time.Now())
	if repository.confirms != 0 || spawner.calls != 1 {
		t.Fatalf("confirms=%d spawns=%d", repository.confirms, spawner.calls)
	}
}

func TestNativeQuestManagerSuppressesDeadPlayerSpawnEligibility(t *testing.T) {
	quest := model.CharacterQuest{QuestID: model.SlavePursuitQuestID, Status: model.QuestProgressed}
	repository := &slavePursuitRepositoryFake{result: model.SlavePursuitTickResult{Quest: quest}}
	player := &model.Player{ID: 44, LifeState: model.Dead, Quests: []model.CharacterQuest{quest}, SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000)}}}
	manager := NewNativeQuestManager(repository, &questNPCSpawnerFake{})
	manager.players = func() []*model.Player { return []*model.Player{player} }
	manager.regionLoaded = func(int16) bool { return true }
	manager.Tick(time.Now())
	if repository.ticks != 1 || repository.eligible {
		t.Fatalf("repository = %+v", repository)
	}
}

func TestSlavePursuitEligibilityDoesNotRejectActiveJob(t *testing.T) {
	player := &model.Player{LifeState: model.Alive, SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000)}}}
	player.SetJob(model.PlayerJob{Type: model.PlayerJobTrader, Alias: "Trader"})
	if !slavePursuitEligible(player) {
		t.Fatal("active job incorrectly rejected by actor life-state gate")
	}
}
