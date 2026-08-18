package model

import "testing"

func TestPlayerPhysicalBarrierAbsorbsDamageUntilItsHitPointsAreSpent(t *testing.T) {
	player := &Player{BaseStats: BaseStats{HP: 1000}}
	player.SetLifeState(Alive)
	if !player.CommitBarrier(ActiveBarrier{SkillID: 8010, Token: 1, HitPoints: 150, Absorption: 100, Physical: true}) {
		t.Fatal("commit barrier failed")
	}
	if applied, killed := player.ApplyDamage(100); applied != 0 || killed {
		t.Fatalf("first hit = applied:%d killed:%t", applied, killed)
	}
	if applied, killed := player.ApplyDamage(100); applied != 50 || killed {
		t.Fatalf("second hit = applied:%d killed:%t", applied, killed)
	}
	if got := player.CurrentHP(); got != 950 {
		t.Fatalf("HP = %d, want 950", got)
	}
}

func TestPlayerMagicalBarrierOnlyAbsorbsMagicalDamage(t *testing.T) {
	player := &Player{BaseStats: BaseStats{HP: 1000}}
	player.SetLifeState(Alive)
	if !player.CommitBarrier(ActiveBarrier{SkillID: 3431, Token: 2, HitPoints: 200, Absorption: 100}) {
		t.Fatal("commit magical barrier failed")
	}
	if applied, _ := player.ApplyDamage(100); applied != 100 {
		t.Fatalf("physical damage through fire wall = %d, want 100", applied)
	}
	if applied, _ := player.ApplyMagicalDamage(100); applied != 0 {
		t.Fatalf("magical damage through fire wall = %d, want 0", applied)
	}
}
