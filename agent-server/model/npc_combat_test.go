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
