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

func TestPlayerCombatDamageReportsPWBarrierResultData(t *testing.T) {
	player := &Player{BaseStats: BaseStats{HP: 1000}}
	player.SetLifeState(Alive)
	if !player.CommitBarrier(ActiveBarrier{SkillID: 8010, Token: 1, HitPoints: 150, Absorption: 100, Physical: true}) {
		t.Fatal("commit barrier failed")
	}
	first := player.ApplyCombatDamage(100, false)
	if first.WireDamage != 0 || first.AppliedDamage != 0 || first.BarrierDamage != 100 || first.BarrierRemaining != 50 || first.BarrierMaximum != 150 || first.BarrierExhausted {
		t.Fatalf("first barrier application = %+v", first)
	}
	second := player.ApplyCombatDamage(100, false)
	if second.WireDamage != 50 || second.AppliedDamage != 50 || second.BarrierDamage != 50 || second.BarrierRemaining != 0 || second.BarrierMaximum != 150 || !second.BarrierExhausted {
		t.Fatalf("second barrier application = %+v", second)
	}
}

func TestActiveBuffUseLimitRemovesSubordinateEffectAtExactCount(t *testing.T) {
	player := &Player{}
	if _, ok := player.CommitActiveBuff(900, 55, 0, BuffModifiers{Strength: 3}); !ok {
		t.Fatal("failed to arrange linked buff")
	}
	if removed, ok := player.CommitActiveBuffUse(55, 2); !ok || removed {
		t.Fatalf("first use = removed:%t ok:%t", removed, ok)
	}
	if got := player.ActiveBuffSnapshot()[0].UseCount; got != 1 {
		t.Fatalf("use count = %d, want 1", got)
	}
	if removed, ok := player.CommitActiveBuffUse(55, 2); !ok || !removed {
		t.Fatalf("second use = removed:%t ok:%t", removed, ok)
	}
	if len(player.ActiveBuffSnapshot()) != 0 || player.BonusStats.Str != 0 {
		t.Fatalf("limited subordinate remained active: buffs=%+v strength=%d", player.ActiveBuffSnapshot(), player.BonusStats.Str)
	}
}
