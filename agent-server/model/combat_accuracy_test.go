package model

import "testing"

func TestCombatHitChanceUsesHitAndParryRates(t *testing.T) {
	tests := []struct{ hit, parry, want int }{
		{0, 0, 100}, {0, 20, 70}, {20, 0, 98}, {20, 20, 90}, {30, 10, 98},
		{10, 30, 80}, {1, 1000, 71}, {1000, 1, 98}, {-10, 20, 70}, {20, -10, 98},
	}
	for _, test := range tests {
		if got := CombatHitChance(test.hit, test.parry); got != test.want {
			t.Fatalf("CombatHitChance(%d, %d) = %d, want %d", test.hit, test.parry, got, test.want)
		}
	}
}
