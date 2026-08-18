package model

import "testing"

func TestCombatHitChanceUsesHitAndParryRates(t *testing.T) {
	tests := []struct{ hit, parry, want int }{
		{0, 0, 100}, {0, 20, 5}, {20, 0, 95}, {20, 20, 50}, {30, 10, 75},
		{1, 1000, 5}, {1000, 1, 95},
	}
	for _, test := range tests {
		if got := CombatHitChance(test.hit, test.parry); got != test.want {
			t.Fatalf("CombatHitChance(%d, %d) = %d, want %d", test.hit, test.parry, got, test.want)
		}
	}
}
