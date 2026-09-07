package manager

import (
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	DefaultGameUpdateStallThreshold = 30 * time.Second
	EscalatedGameUpdateThreshold    = 10 * time.Second
)

type GameHealthSnapshot struct {
	LastUpdate      time.Time
	LastDuration    time.Duration
	MaxDuration     time.Duration
	UpdateCount     uint64
	MovementObjects uint64
	Stalled         bool
	StallCount      uint64
}

type gameHealthState struct {
	lastUpdateUnix atomic.Int64
	lastDuration   atomic.Int64
	maxDuration    atomic.Int64
	updates        atomic.Uint64
	moving         atomic.Uint64
	stalls         atomic.Uint64
}

var gameHealth gameHealthState

func recordGameUpdate(at time.Time, duration time.Duration, moving int) {
	gameHealth.lastUpdateUnix.Store(at.UnixNano())
	gameHealth.lastDuration.Store(int64(duration))
	gameHealth.updates.Add(1)
	if moving < 0 {
		moving = 0
	}
	gameHealth.moving.Store(uint64(moving))
	for {
		current := gameHealth.maxDuration.Load()
		if int64(duration) <= current || gameHealth.maxDuration.CompareAndSwap(current, int64(duration)) {
			break
		}
	}
}

func GameHealth(now time.Time) GameHealthSnapshot {
	lastNanos := gameHealth.lastUpdateUnix.Load()
	last := time.Time{}
	if lastNanos != 0 {
		last = time.Unix(0, lastNanos)
	}
	threshold := DefaultGameUpdateStallThreshold
	if gameHealth.stalls.Load() != 0 {
		threshold = EscalatedGameUpdateThreshold
	}
	stalled := !last.IsZero() && now.Sub(last) > threshold
	return GameHealthSnapshot{
		LastUpdate: last, LastDuration: time.Duration(gameHealth.lastDuration.Load()),
		MaxDuration: time.Duration(gameHealth.maxDuration.Load()), UpdateCount: gameHealth.updates.Load(),
		MovementObjects: gameHealth.moving.Load(), Stalled: stalled, StallCount: gameHealth.stalls.Load(),
	}
}

func checkGameUpdateWatchdog(now time.Time) bool {
	snapshot := GameHealth(now)
	if !snapshot.Stalled {
		return false
	}
	count := gameHealth.stalls.Add(1)
	logrus.Errorf("game update watchdog stalled last_update=%s age=%s movement_objects=%d detection=%d", snapshot.LastUpdate.UTC().Format(time.RFC3339Nano), now.Sub(snapshot.LastUpdate), snapshot.MovementObjects, count)
	return true
}
