package model

import (
	"testing"
	"time"
)

func TestPlayerCureSeparatesElementalMaskPowerFromCurseLevel(t *testing.T) {
	now := time.Now()
	player := &Player{}
	player.ApplyCombatStatus(CombatStatus{Kind: CombatStatusFreeze, Token: 1, SkillID: 10, Level: 35, ExpiresAt: now.Add(time.Minute)})
	player.ApplyCombatStatus(CombatStatus{Kind: CombatStatusShock, Token: 2, SkillID: 11, Level: 40, ExpiresAt: now.Add(time.Minute)})
	player.ApplyCombatStatus(CombatStatus{Kind: CombatStatusStun, Token: 3, SkillID: 12, Level: 2, ExpiresAt: now.Add(time.Minute)})

	removed := player.CureCombatStatusesWithCurse(63, 36, true, 1, 100, 2, now, func(int) bool { return true })
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, active := player.CombatStatus(CombatStatusFreeze, now); active {
		t.Fatal("eligible freeze was not cured")
	}
	if _, active := player.CombatStatus(CombatStatusShock, now); !active {
		t.Fatal("over-level shock was cured")
	}
	if _, active := player.CombatStatus(CombatStatusStun, now); !active {
		t.Fatal("over-level curse was cured")
	}
}

func TestPlayerCurseCureUsesAuthoredProbability(t *testing.T) {
	now := time.Now()
	player := &Player{}
	player.ApplyCombatStatus(CombatStatus{Kind: CombatStatusStun, Token: 1, SkillID: 12, Level: 1, ExpiresAt: now.Add(time.Minute)})
	if removed := player.CureCombatStatusesWithCurse(0, 0, true, 1, 80, 1, now, func(chance int) bool { return chance == 100 }); removed != 0 {
		t.Fatalf("failed 80%% roll removed %d effects", removed)
	}
	if removed := player.CureCombatStatusesWithCurse(0, 0, true, 1, 80, 1, now, func(chance int) bool { return chance == 80 }); removed != 1 {
		t.Fatalf("successful 80%% roll removed %d effects, want 1", removed)
	}
}
