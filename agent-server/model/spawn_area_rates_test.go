package model

import (
	"testing"
	"time"
)

func TestSpawnAreaRatesScaleCountAndFixedDatabaseDelay(t *testing.T) {
	area := InitSpawnAreaFromSpawnNestWithRates(Spawn{MaxTotalCount: 3, DelayTimeMin: 10, DelayTimeMax: 10}, 200, 200)
	if got := len(area.RespawnTimes); got != 6 {
		t.Fatalf("initial spawn count = %d, want 6", got)
	}
	if got := area.TakeDueRespawns(time.Now()); got != 6 {
		t.Fatalf("immediately due initial population = %d, want 6", got)
	}
	delay := area.calculateRespawnTime().Sub(time.Now())
	if delay < 4900*time.Millisecond || delay > 5100*time.Millisecond {
		t.Fatalf("scaled fixed respawn delay = %v, want about 5s", delay)
	}
}

func TestSpawnAreaLiveCountRatesReconcilePendingPopulation(t *testing.T) {
	area := InitSpawnAreaFromSpawnNestWithRates(Spawn{MaxTotalCount: 4, DelayTimeMin: 10, DelayTimeMax: 10}, 100, 100)
	area.ApplyGameplayRates(200, 200)
	if got := len(area.RespawnTimes); got != 8 {
		t.Fatalf("increased pending population = %d, want 8", got)
	}
	area.ApplyGameplayRates(50, 200)
	if got := len(area.RespawnTimes); got != 2 {
		t.Fatalf("decreased pending population = %d, want 2", got)
	}
	delay := area.calculateRespawnTime().Sub(time.Now())
	if delay < 4900*time.Millisecond || delay > 5100*time.Millisecond {
		t.Fatalf("live respawn delay = %v, want about 5s", delay)
	}
}
