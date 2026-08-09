package manager

import (
	"github.com/Harvi8/AegisCore/agent-server/config"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/spf13/viper"
	"sync"
	"time"
)

type GameTimeManager struct {
	Manager
	referenceTime time.Time
}

var gameTimeManagerInstance *GameTimeManager
var gameTimeManagerOnce sync.Once

func GetGameTimeManagerInstance() *GameTimeManager {
	gameTimeManagerOnce.Do(func() {
		now := time.Now()
		gameTimeManagerInstance = &GameTimeManager{
			referenceTime: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()),
		}
		gameTimeManagerInstance.Name = "GameTimeManager"
		gameTimeManagerInstance.initialDelay = time.Second
		gameTimeManagerInstance.rate = time.Second / time.Duration(viper.GetInt(config.GameTimeTicksPerSecond))
		gameTimeManagerInstance.runnerFunc = gameTimeManagerInstance.moveObjects
	})
	return gameTimeManagerInstance
}

func (gtm *GameTimeManager) moveObjects() {
	world := service.GetWorldServiceInstance()
	for gtm.started {
		select {
		case <-gtm.ticker.C:
			world.ForEachMovingObject(func(obj model.ICharacter) {
				world.UpdatePosition(obj)
				// WorldService reconciles the changed character immediately after a
				// committed movement/stop transition. Do not restore a polling
				// known-list walk here: it races disconnect cleanup and cannot
				// provide deterministic reciprocal visibility deltas.
			})
			world.FlushDirtyPositionsIfDue(time.Now())
		}
	}
}

func (gtm *GameTimeManager) GetCurrentTick() int64 {
	return int64(time.Now().Sub(gtm.referenceTime) / gtm.rate)
}
