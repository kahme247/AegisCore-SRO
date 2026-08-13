package model

import (
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
	if !npc.ApplyCombatStatus(CombatStatus{Kind: CombatStatusFrostbite, Token: 1, SkillID: 7972, Power: 50, ExpiresAt: now.Add(time.Second)}) {
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
