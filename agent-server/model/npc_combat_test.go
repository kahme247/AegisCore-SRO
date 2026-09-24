package model

import (
	"slices"
	"sync"
	"testing"
	"time"
)

func TestNPCApplyDamageIsAtomicAndAwardsOneDeath(t *testing.T) {
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.ConfigureCombat(100, 77)
	npc.SetLifeState(Alive)

	if damage, killed := npc.ApplyDamage(25); damage != 25 || killed {
		t.Fatalf("first hit = damage %d, killed %t; want 25, false", damage, killed)
	}
	if damage, killed := npc.ApplyDamage(100); damage != 75 || !killed {
		t.Fatalf("lethal hit = damage %d, killed %t; want 75, true", damage, killed)
	}
	if damage, killed := npc.ApplyDamage(1); damage != 0 || killed {
		t.Fatalf("post-death hit = damage %d, killed %t; want 0, false", damage, killed)
	}
	if got, want := npc.ExperienceReward(), uint64(77); got != want {
		t.Fatalf("ExperienceReward() = %d, want %d", got, want)
	}
}

func TestNPCCombatStatusMovesAndRestoresFrostbite(t *testing.T) {
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.SetLifeState(Alive)
	npc.ConfigureCombat(100, 10)
	npc.SetWalkSpeed(8)
	npc.SetRunSpeed(22)
	now := time.Unix(1000, 0)
	if !npc.ApplyCombatStatusAt(CombatStatus{Kind: CombatStatusFrostbite, Token: 1, SkillID: 7972, Power: 50, ExpiresAt: now.Add(time.Second)}, now) {
		t.Fatal("frostbite was not applied")
	}
	if got := npc.GetRunSpeed(); got != 11 {
		t.Fatalf("frostbite speed = %g, want 11", got)
	}
	if _, exists := npc.CombatStatus(CombatStatusFrostbite, now.Add(2*time.Second)); exists {
		t.Fatal("expired frostbite remained active")
	}
	if got := npc.GetRunSpeed(); got != 22 {
		t.Fatalf("restored speed = %g, want 22", got)
	}
}

func TestNPCDullAndFrostbiteComposeMovementSpeed(t *testing.T) {
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.SetLifeState(Alive)
	npc.ConfigureCombat(100, 10)
	npc.SetWalkSpeed(8)
	npc.SetRunSpeed(24)
	now := time.Unix(1000, 0)
	npc.ApplyCombatStatusAt(CombatStatus{Kind: CombatStatusDull, Token: 1, SkillID: 1, ExpiresAt: now.Add(time.Minute)}, now)
	if got := npc.GetRunSpeed(); got != 18 {
		t.Fatalf("Dull run speed = %g, want 18", got)
	}
	npc.ApplyCombatStatusAt(CombatStatus{Kind: CombatStatusFrostbite, Token: 2, SkillID: 2, ExpiresAt: now.Add(time.Minute)}, now)
	if got := npc.GetRunSpeed(); got != 9 {
		t.Fatalf("Dull/Frostbite run speed = %g, want 9", got)
	}
}

func TestNPCStackableCombatStatusesRetainIndependentTokens(t *testing.T) {
	now := time.Unix(1000, 0)
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.ConfigureCombat(1000, 1)
	npc.SetLifeState(Alive)
	first := CombatStatus{Kind: CombatStatusBurn, Token: 11, SourceUID: 101, SkillID: 1001, Level: 3, Payload: 40, ExpiresAt: now.Add(time.Minute), Stackable: true}
	second := CombatStatus{Kind: CombatStatusBurn, Token: 12, SourceUID: 102, SkillID: 1002, Level: 5, Payload: 60, ExpiresAt: now.Add(2 * time.Minute), Stackable: true}
	if !npc.ApplyCombatStatus(first) || !npc.ApplyCombatStatus(second) {
		t.Fatal("stackable statuses were rejected")
	}
	if got, ok := npc.CombatStatusByToken(first.Kind, first.Token, now); !ok || got.SkillID != first.SkillID {
		t.Fatalf("first token = %+v active=%t", got, ok)
	}
	if got, ok := npc.CombatStatusByToken(second.Kind, second.Token, now); !ok || got.SkillID != second.SkillID {
		t.Fatalf("second token = %+v active=%t", got, ok)
	}
	if selected, ok := npc.CombatStatus(CombatStatusBurn, now); !ok || selected.Token != second.Token {
		t.Fatalf("representative status = %+v active=%t, want higher-level token 12", selected, ok)
	}
	if !npc.RemoveCombatStatus(first.Kind, first.Token) {
		t.Fatal("remove first stacked token")
	}
	if _, ok := npc.CombatStatusByToken(first.Kind, first.Token, now); ok {
		t.Fatal("removed stacked token remains active")
	}
	if got, ok := npc.CombatStatusByToken(second.Kind, second.Token, now); !ok || got.Token != second.Token {
		t.Fatalf("sibling token removed with first: %+v active=%t", got, ok)
	}
}

func TestNPCDamageTakenUpDoesNotStackOrReplace(t *testing.T) {
	now := time.Now()
	npc := &NPC{LifeState: Alive}
	npc.combat.hitPoints = 1
	first := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 1, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)}
	second := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 2, SkillID: 8415, Power: 80, ExpiresAt: now.Add(time.Minute)}
	if !npc.ApplyCombatStatus(first) {
		t.Fatal("first tisc status was rejected")
	}
	if npc.ApplyCombatStatus(second) {
		t.Fatal("equal-class tisc status replaced the active record")
	}
	if got, active := npc.CombatStatus(CombatStatusDamageTakenUp, now); !active || got.Token != first.Token || got.Power != first.Power {
		t.Fatalf("active tisc status = %+v, active=%v; want first record", got, active)
	}
}

func TestNPCDamageTakenUpSurvivesDeathUntilExpiry(t *testing.T) {
	now := time.Now()
	npc := &NPC{LifeState: Alive, Mutex: &sync.Mutex{}}
	npc.combat.hitPoints = 10
	status := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 1, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)}
	if !npc.ApplyCombatStatus(status) {
		t.Fatal("failed to apply tisc status")
	}
	if applied, killed := npc.ApplyDamage(10); applied != 10 || !killed {
		t.Fatalf("lethal damage = (%d, %v), want (10, true)", applied, killed)
	}
	if got, active := npc.CombatStatus(CombatStatusDamageTakenUp, now); !active || got.Token != status.Token {
		t.Fatalf("post-death tisc status = %+v, active=%v; want preserved", got, active)
	}
}

func TestNPCDamageTakenUpDispelAndExpiryAllowReapplication(t *testing.T) {
	now := time.Now()
	npc := &NPC{LifeState: Alive}
	npc.combat.hitPoints = 1
	first := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 1, SkillID: 8414, Power: 35, Level: 11, ExpiresAt: now.Add(time.Minute)}
	if !npc.ApplyCombatStatus(first) {
		t.Fatal("failed to apply initial tisc status")
	}
	if removed := npc.CureCombatStatusesWithCurse(0, 0, true, 11, 100, 1, now, func(int) bool { return true }); removed != 1 {
		t.Fatalf("dispelled statuses = %d, want 1", removed)
	}
	second := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 2, SkillID: 8415, Power: 40, Level: 12, ExpiresAt: now.Add(2 * time.Minute)}
	if !npc.ApplyCombatStatus(second) {
		t.Fatal("tisc reapplication after dispel was rejected")
	}
	if _, active := npc.CombatStatus(CombatStatusDamageTakenUp, second.ExpiresAt); active {
		t.Fatal("tisc remained active at its expiry boundary")
	}
	third := CombatStatus{Kind: CombatStatusDamageTakenUp, Token: 3, SkillID: 8416, Power: 45, ExpiresAt: time.Now().Add(time.Minute)}
	if !npc.ApplyCombatStatus(third) {
		t.Fatal("tisc reapplication after expiry was rejected")
	}
}

func TestNPCFreezeStopsAndRestoresMovement(t *testing.T) {
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.SetLifeState(Alive)
	npc.ConfigureCombat(100, 10)
	npc.SetWalkSpeed(8)
	npc.SetRunSpeed(22)
	now := time.Unix(1000, 0)
	frozenSpeed := npc.GetRunSpeed()
	if frozenSpeed != 22 {
		t.Fatalf("arranged speed = %g, want 22", frozenSpeed)
	}
	if !npc.ApplyCombatStatus(CombatStatus{Kind: CombatStatusFreeze, Token: 1, SkillID: 7972, Power: 240, ExpiresAt: now.Add(time.Second)}) {
		t.Fatal("freeze was not applied")
	}
	npc.applyStatusMovementEffect(CombatStatusFreeze, 240, now)
	if npc.WalkSpeed != 0 || npc.RunSpeed != 0 {
		t.Fatalf("freeze speed = %g/%g, want 0/0", npc.WalkSpeed, npc.RunSpeed)
	}
	if _, exists := npc.CombatStatus(CombatStatusFreeze, now.Add(2*time.Second)); exists {
		t.Fatal("expired freeze remained active")
	}
	if got := npc.GetRunSpeed(); got != 22 {
		t.Fatalf("restored speed = %g, want 22", got)
	}
}

func TestNPCCombatBadEffectStateAggregatesAllFamiliesAndLevels(t *testing.T) {
	now := time.Now()
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.ConfigureCombat(1000, 10)
	npc.SetLifeState(Alive)
	for _, status := range []CombatStatus{
		{Kind: CombatStatusFrostbite, SkillID: 1, Power: 240, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusPoison, SkillID: 2, Power: 206, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDarkness, SkillID: 3, Level: 11, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDamageTakenUp, SkillID: 4, Level: 9, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusHidden, SkillID: 5, Level: 7, ExpiresAt: now.Add(time.Minute)},
	} {
		if !npc.ApplyCombatStatus(status) {
			t.Fatalf("failed to apply %+v", status)
		}
	}
	mask, levels := npc.CombatBadEffectState(now)
	wantMask := uint32(1<<1 | 1<<4 | 1<<13 | 1<<20 | 1<<24)
	if mask != wantMask {
		t.Fatalf("mask = %#x, want %#x", mask, wantMask)
	}
	if !slices.Equal(levels, []byte{11, 9, 7}) {
		t.Fatalf("levels = %v, want [11 9 7]", levels)
	}
}

func TestNPCBehavioralDebuffMechanics(t *testing.T) {
	now := time.Unix(100, 0)
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.ConfigureCombat(1000, 10)
	npc.SetLifeState(Alive)
	for token, status := range []CombatStatus{
		{Kind: CombatStatusDarkness, SkillID: 1, Power: 35, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusShortSight, SkillID: 2, Power: 40, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDull, SkillID: 3, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusFear, SkillID: 4, ExpiresAt: now.Add(time.Minute)},
		{Kind: CombatStatusDisease, SkillID: 5, Power: 25, ExpiresAt: now.Add(time.Minute)},
	} {
		status.Token = uint32(token + 1)
		if !npc.ApplyCombatStatusAt(status, now) {
			t.Fatalf("apply status %+v", status)
		}
	}
	if got := npc.CombatHitRate(100, now); got != 65 {
		t.Fatalf("darkness hit rate = %d, want 65", got)
	}
	if got := npc.CombatAttackRange(200, now); got != 120 {
		t.Fatalf("short-sight range = %g, want 120", got)
	}
	if got := npc.CombatActionTimePercent(100, now); got != 125 {
		t.Fatalf("Dull action time = %g, want 125", got)
	}
	if !npc.CombatActionRestricted(now) {
		t.Fatal("Fear did not restrict mob actions")
	}
	if got := npc.CombatStatusChance(60, now); got != 85 {
		t.Fatalf("Disease status chance = %d, want 85", got)
	}
}

func TestNPCDeathQueuesOneReplacement(t *testing.T) {
	area := &SpawnArea{
		Spawn:       Spawn{DelayTimeMin: 1, DelayTimeMax: 1},
		NPCs:        make(map[uint32]*NPC),
		targetCount: 1,
	}
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.SetUniqueID(42)
	npc.ConfigureCombat(1, 10)
	npc.SetLifeState(Alive)
	npc.BindSpawnArea(area)
	area.AddNPC(npc)

	if _, killed := npc.ApplyDamage(1); !killed {
		t.Fatal("lethal damage did not kill NPC")
	}
	if !npc.QueueRespawn() {
		t.Fatal("first QueueRespawn() did not schedule replacement")
	}
	if npc.QueueRespawn() {
		t.Fatal("second QueueRespawn() scheduled a duplicate replacement")
	}
	if got := area.TakeDueRespawns(time.Now().Add(2 * time.Second)); got != 1 {
		t.Fatalf("due replacements = %d, want 1", got)
	}
}

func TestNPCAggroTableRetargetsAfterTauntAndReduction(t *testing.T) {
	npc := &NPC{}
	npc.AddAggro(100, 500)
	npc.AddAggro(200, 300)
	if _, _, _, target := npc.AIProfile(); target != 100 {
		t.Fatalf("initial target = %d, want 100", target)
	}
	npc.AddAggro(200, 250)
	if _, _, _, target := npc.AIProfile(); target != 200 {
		t.Fatalf("taunted target = %d, want 200", target)
	}
	npc.ReduceAggro(200, 100)
	if _, _, _, target := npc.AIProfile(); target != 100 {
		t.Fatalf("reduced target = %d, want 100", target)
	}
	if got := npc.AggroValue(200); got != 450 {
		t.Fatalf("remaining aggro = %d, want 450", got)
	}
}
