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
	alchemy       interface{ TickAlchemy() }
	watchdogStop  chan struct{}
}

func (gtm *GameTimeManager) ConfigureCOS(runtime interface{ UpdateFollowers(time.Time) }) {
	gtm.cos = runtime
}

func (gtm *GameTimeManager) ConfigureAlchemy(runtime interface{ TickAlchemy() }) {
	gtm.alchemy = runtime
}

var gameTimeManagerInstance *GameTimeManager
var gameTimeManagerOnce sync.Once

func GetGameTimeManagerInstance() *GameTimeManager {
	gameTimeManagerOnce.Do(func() {
		now := time.Now()
		ticksPerSecond := viper.GetInt(config.GameTimeTicksPerSecond)
		if ticksPerSecond <= 0 {
			ticksPerSecond = 10
		}
		gameTimeManagerInstance = &GameTimeManager{
			referenceTime: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()),
		}
		gameTimeManagerInstance.Name = "GameTimeManager"
		gameTimeManagerInstance.initialDelay = time.Second
		gameTimeManagerInstance.rate = time.Second / time.Duration(ticksPerSecond)
		gameTimeManagerInstance.runnerFunc = gameTimeManagerInstance.moveObjects
	})
	return gameTimeManagerInstance
}

func StopGameTimeWatchdog() {
	if gameTimeManagerInstance != nil {
		gameTimeManagerInstance.StopWatchdog()
	}
}

func (gtm *GameTimeManager) moveObjects() {
	world := service.GetWorldServiceInstance()
	for gtm.started {
		select {
		case <-gtm.ticker.C:
			now := time.Now()
			startedAt := now
			if gtm.alchemy != nil {
				gtm.alchemy.TickAlchemy()
			}
			if gtm.cos != nil {
				gtm.cos.UpdateFollowers(now)
			}
			moving := 0
			world.ForEachMovingObject(func(obj model.ICharacter) {
				moving++
				world.UpdatePosition(obj)
				// WorldService reconciles the changed character immediately after a
				// committed movement/stop transition. Do not restore a polling
				// known-list walk here: it races disconnect cleanup and cannot
				// provide deterministic reciprocal visibility deltas.
			})
			world.FlushDirtyPositionsIfDue(now)
			recordGameUpdate(now, time.Since(startedAt), moving)
		}
	}
}

func (gtm *GameTimeManager) StartWatchdog() {
	if gtm == nil || gtm.watchdogStop != nil {
		return
	}
	gtm.watchdogStop = make(chan struct{})
	go func(stop <-chan struct{}) {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				checkGameUpdateWatchdog(now)
			case <-stop:
				return
			}
		}
	}(gtm.watchdogStop)
}

func (gtm *GameTimeManager) StopWatchdog() {
	if gtm == nil || gtm.watchdogStop == nil {
		return
	}
	close(gtm.watchdogStop)
	gtm.watchdogStop = nil
}

func (gtm *GameTimeManager) GetCurrentTick() int64 {
	return int64(time.Now().Sub(gtm.referenceTime) / gtm.rate)
}
