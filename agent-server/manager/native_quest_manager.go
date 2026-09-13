package manager

import (
	"context"
	"errors"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/sirupsen/logrus"
)

type slavePursuitRepository interface {
	TickSlavePursuit(context.Context, int, bool) (model.SlavePursuitTickResult, error)
	ConfirmSlavePursuitSpawn(context.Context, int) (model.CharacterQuest, error)
}

type questNPCSpawner interface {
	SpawnQuestNPC(string, navmeshv2.RtNavmeshPosition, float32) (*model.NPC, error)
}

type NativeQuestManager struct {
	repository   slavePursuitRepository
	spawner      questNPCSpawner
	players      func() []*model.Player
	regionLoaded func(int16) bool
	nextTick     time.Time
}

func NewNativeQuestManager(repository slavePursuitRepository, spawner questNPCSpawner) *NativeQuestManager {
	return &NativeQuestManager{
		repository: repository, spawner: spawner,
		players: func() []*model.Player { return service.GetWorldServiceInstance().GetActivePlayers() },
		regionLoaded: func(regionID int16) bool {
			_, err := service.GetWorldServiceInstance().GetRegion(regionID)
			return err == nil
		},
	}
}

func (m *NativeQuestManager) Tick(now time.Time) {
	if m == nil || m.repository == nil || m.spawner == nil || (!m.nextTick.IsZero() && now.Before(m.nextTick)) {
		return
	}
	m.nextTick = now.Add(time.Second)
	if m.players == nil {
		return
	}
	for _, player := range m.players() {
		if player == nil || !hasActiveSlavePursuit(player) {
			continue
		}
		position := player.GetNavmeshPosition()
		eligible := slavePursuitEligible(player) && m.regionLoaded != nil && m.regionLoaded(position.Region.ID)
		result, err := m.repository.TickSlavePursuit(context.Background(), player.ID, eligible)
		if err != nil {
			if !errors.Is(err, model.ErrSlavePursuitState) {
				logrus.WithError(err).WithField("character_id", player.ID).Error("QNO_RM_SLAVE2_2 periodic tick failed")
			}
			continue
		}
		player.CommitQuestMutation(result.Quest)
		if result.Spawn {
			if _, err = m.spawner.SpawnQuestNPC(model.SlavePursuitMobCodeName, position, model.SlavePursuitSpawnRadius); err != nil {
				logrus.WithError(err).WithField("character_id", player.ID).Error("QNO_RM_SLAVE2_2 pursuer spawn failed")
				continue
			}
			confirmed, confirmErr := m.repository.ConfirmSlavePursuitSpawn(context.Background(), player.ID)
			if confirmErr != nil {
				logrus.WithError(confirmErr).WithField("character_id", player.ID).Error("QNO_RM_SLAVE2_2 pursuer reset failed")
				continue
			}
			player.CommitQuestMutation(confirmed)
		}
	}
}

func slavePursuitEligible(player *model.Player) bool {
	if player == nil {
		return false
	}
	return player.GetNavmeshPosition().Region.ID != 0 && player.GetLifeState() != model.Dead
}

func hasActiveSlavePursuit(player *model.Player) bool {
	for _, quest := range player.QuestSnapshot() {
		if quest.QuestID == model.SlavePursuitQuestID && quest.Status.ClientActive() {
			return true
		}
	}
	return false
}
