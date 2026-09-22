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

func TestPlayerDamageTakenUpDoesNotStackOrReplace(t *testing.T) {
	now := time.Now()
	player := &Player{LifeState: Alive}
	first := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 1, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)}
	second := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 2, SkillID: 8415, Power: 80, ExpiresAt: now.Add(time.Minute)}
	if !player.ApplyCombatStatus(first) {
		t.Fatal("first tisc status was rejected")
	}
	if player.ApplyCombatStatus(second) {
		t.Fatal("equal-class tisc status replaced the active record")
	}
	if got, active := player.CombatStatus(CombatStatusDamageTakenUp, now); !active || got.Token != first.Token || got.Power != first.Power {
		t.Fatalf("active tisc status = %+v, active=%v; want first record", got, active)
	}
}

func TestPlayerDamageTakenUpAdmissionUsesSuppliedClock(t *testing.T) {
	now := time.Unix(1000, 0)
	player := &Player{LifeState: Alive}
	first := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 1, Power: 35, ExpiresAt: now.Add(time.Minute)}
	second := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 2, Power: 80, ExpiresAt: now.Add(2 * time.Minute)}
	if !player.ApplyCombatStatusAt(first, now) {
		t.Fatal("first timestamped tisc status was rejected")
	}
	if player.ApplyCombatStatusAt(second, now) {
		t.Fatal("active timestamped tisc status was replaced")
	}
	if !player.ApplyCombatStatusAt(second, now.Add(2*time.Minute)) {
		t.Fatal("expired timestamped tisc status was not replaceable")
	}
}

func TestPlayerDamageTakenUpSurvivesDeathUntilExpiry(t *testing.T) {
	now := time.Now()
	player := &Player{LifeState: Alive, BaseStats: BaseStats{HP: 10}}
	status := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 1, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)}
	if !player.ApplyCombatStatus(status) {
		t.Fatal("failed to apply tisc status")
	}
	if result := player.ApplyCombatDamage(10, false); result.AppliedDamage != 10 || !result.Killed {
		t.Fatalf("lethal damage = %+v, want applied 10 and killed", result)
	}
	if got, active := player.CombatStatus(CombatStatusDamageTakenUp, now); !active || got.Token != status.Token {
		t.Fatalf("post-death tisc status = %+v, active=%v; want preserved", got, active)
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

func TestPlayerCombatMovementRestrictedByControlStatuses(t *testing.T) {
	now := time.Unix(100, 0)
	player := &Player{}
	if player.CombatMovementRestricted(now) {
		t.Fatal("player without control status is movement restricted")
	}
	player.ApplyCombatStatus(CombatStatus{Kind: CombatStatusFreeze, Token: 1, ExpiresAt: now.Add(time.Second)})
	if !player.CombatMovementRestricted(now) {
		t.Fatal("active freeze did not restrict movement")
	}
	if player.CombatMovementRestricted(now.Add(2 * time.Second)) {
		t.Fatal("expired freeze still restricts movement")
	}
	player.ApplyCombatStatus(CombatStatus{Kind: CombatStatusBind, Token: 2, ExpiresAt: now.Add(time.Minute)})
	if !player.CombatMovementRestricted(now) {
		t.Fatal("active bind did not restrict movement")
	}
}

func TestPlayerCombatActionRestrictedByEveryControlStatus(t *testing.T) {
	now := time.Unix(100, 0)
	for token, kind := range []CombatStatusKind{
		CombatStatusFreeze,
		CombatStatusStun,
		CombatStatusSleep,
		CombatStatusBind,
		CombatStatusPetrify,
		CombatStatusFear,
	} {
		player := &Player{}
		player.ApplyCombatStatus(CombatStatus{
			Kind:      kind,
			Token:     uint32(token + 1),
			ExpiresAt: now.Add(time.Second),
		})
		if !player.CombatActionRestricted(now) {
			t.Fatalf("%v did not restrict offensive actions", kind)
		}
		if player.CombatActionRestricted(now.Add(2 * time.Second)) {
			t.Fatalf("expired %v still restricted offensive actions", kind)
		}
	}
}

func TestPlayerBehavioralAbnormalMechanics(t *testing.T) {
	now := time.Unix(100, 0)
	player := &Player{}
	for token, status := range []CombatStatus{
		{Kind: CombatStatusFear, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDarkness, Power: 35, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusShortSight, Power: 40, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDisease, Power: 25, ExpiresAt: now.Add(time.Minute)},
	} {
		status.Token = uint32(token + 1)
		if !player.ApplyCombatStatusAt(status, now) {
			t.Fatalf("apply status %+v", status)
		}
	}
	if !player.CombatActionRestricted(now) {
		t.Fatal("fear did not restrict offensive actions")
	}
	if got := player.CombatHitRate(100, now); got != 65 {
		t.Fatalf("darkness hit rate = %d, want 65", got)
	}
	if got := player.CombatAttackRange(200, now); got != 120 {
		t.Fatalf("short-sight range = %g, want 120", got)
	}
	if got := player.CombatStatusChance(60, now); got != 85 {
		t.Fatalf("disease status chance = %d, want 85", got)
	}
}

func TestPlayerHitAndParryDebuffsComposeAcrossAttackers(t *testing.T) {
	now := time.Unix(100, 0)
	player := &Player{}
	for token, status := range []CombatStatus{
		{Kind: CombatStatusHitDown, Power: 20, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDarkness, Power: 35, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusParryDown, Power: 30, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusShock, Power: 50, Payload: 50, ExpiresAt: now.Add(time.Minute)},
	} {
		status.Token = uint32(token + 1)
		if !player.ApplyCombatStatusAt(status, now) {
			t.Fatalf("apply status %+v", status)
		}
	}
	if got := player.CombatHitRate(100, now); got != 45 {
		t.Fatalf("combined hit rate = %d, want 45", got)
	}
	if got := player.CombatParryRate(100, now); got != 35 {
		t.Fatalf("combined parry rate = %d, want 35", got)
	}
}

func TestPlayerTimedActionAdmissionAndRelease(t *testing.T) {
	player := &Player{}
	if !player.BeginTimedAction(100) {
		t.Fatal("first timed action was rejected")
	}
	if !player.TimedActionLocked() {
		t.Fatal("active timed action did not lock player actions")
	}
	if player.BeginTimedAction(200) {
		t.Fatal("second timed action replaced the active cast")
	}
	if player.EndTimedAction(200) {
		t.Fatal("wrong token ended active timed action")
	}
	if !player.EndTimedAction(100) || player.TimedActionLocked() {
		t.Fatal("matching token did not release timed action")
	}
}
