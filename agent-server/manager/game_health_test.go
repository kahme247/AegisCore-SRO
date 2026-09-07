package manager

import (
	"testing"
	"time"
)

func TestGameHealthTracksUpdatesAndDetectsStalls(t *testing.T) {
	gameHealth = gameHealthState{}
	now := time.Unix(1000, 0)
	recordGameUpdate(now, 25*time.Millisecond, 7)
	snapshot := GameHealth(now.Add(time.Second))
	if snapshot.Stalled || snapshot.UpdateCount != 1 || snapshot.MovementObjects != 7 || snapshot.LastDuration != 25*time.Millisecond {
		t.Fatalf("fresh health = %+v", snapshot)
	}
	if !checkGameUpdateWatchdog(now.Add(DefaultGameUpdateStallThreshold + time.Second)) {
		t.Fatal("watchdog did not detect stale game update")
	}
	if snapshot = GameHealth(now.Add(DefaultGameUpdateStallThreshold + time.Second)); snapshot.StallCount != 1 {
		t.Fatalf("stalled health = %+v", snapshot)
	}
}
