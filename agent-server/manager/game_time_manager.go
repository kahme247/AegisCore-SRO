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
	cos           interface{ UpdateFollowers(time.Time) }
}

func (gtm *GameTimeManager) ConfigureCOS(runtime interface{ UpdateFollowers(time.Time) }) {
	gtm.cos = runtime
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
			now := time.Now()
			if gtm.cos != nil {
				gtm.cos.UpdateFollowers(now)
			}
			world.ForEachMovingObject(func(obj model.ICharacter) {
				world.UpdatePosition(obj)
				// WorldService reconciles the changed character immediately after a
				// committed movement/stop transition. Do not restore a polling
				// known-list walk here: it races disconnect cleanup and cannot
				// provide deterministic reciprocal visibility deltas.
			})
			world.FlushDirtyPositionsIfDue(now)
		}
	}
}

func (gtm *GameTimeManager) GetCurrentTick() int64 {
	return int64(time.Now().Sub(gtm.referenceTime) / gtm.rate)
}
