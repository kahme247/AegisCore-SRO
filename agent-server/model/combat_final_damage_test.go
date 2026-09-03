package model

import "testing"

func TestCombatMitigatedDamageUsesDivisionBeforeDefense(t *testing.T) {
	if got := CombatMitigatedDamage(1000, 100, 25); got != 700 {
		t.Fatalf("mitigated damage = %v, want 700", got)
	}
	if got := CombatMitigatedDamage(100, 200, 0); got != 0 {
		t.Fatalf("negative mitigated damage = %v, want 0", got)
	}
}

func TestCombatLowDamageCorrection(t *testing.T) {
	if got := CombatLowDamageCorrection(40, 1000, 32767); got != 4 {
		t.Fatalf("upper correction = %v, want 4", got)
	}
	if got := CombatLowDamageCorrection(40, 1000, 0); got != 1 {
		t.Fatalf("lower correction = %v, want 1", got)
	}
	if got := CombatLowDamageCorrection(50, 1000, 0); got != 50 {
		t.Fatalf("threshold damage = %v, want unchanged 50", got)
	}
	if got := CombatLowDamageCorrection(0, 1000, 32767); got != 1 {
		t.Fatalf("zero correction = %v, want forced 1", got)
	}
}

func TestCombatFinalDamageFactorUsesTargetSlotPercent(t *testing.T) {
	for _, test := range []struct {
		damage int
		factor int
		want   int
	}{
		{100, 35, 135},
		{80, 35, 108},
		{101, 35, 136},
		{100, 0, 100},
		{100, -100, 0},
	} {
		if got := CombatFinalDamageFactor(test.damage, test.factor); got != test.want {
			t.Fatalf("CombatFinalDamageFactor(%d, %d) = %d, want %d", test.damage, test.factor, got, test.want)
		}
	}
}

func TestCombatDamageClampAndTruncate(t *testing.T) {
	for _, test := range []struct {
		damage float32
		want   int
	}{
		{-1, 0},
		{12.99, 12},
		{MaximumSRDamage - 1, MaximumSRDamage - 1},
		{MaximumSRDamage, MaximumSRDamage},
	} {
		if got := CombatDamageClamp(test.damage); got != test.want {
			t.Fatalf("CombatDamageClamp(%v) = %d, want %d", test.damage, got, test.want)
		}
	}
}
