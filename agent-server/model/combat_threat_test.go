package model

import "testing"

func TestCombatThreatMatchesRecovered2tntArithmetic(t *testing.T) {
	tests := []struct {
		base, flat, percent, want int
	}{
		{200, 35, 25, 285},
		{101, 0, 50, 152}, // x87 nearest-even rounding of 151.5
		{200, -500, 0, 0},
	}
	for _, test := range tests {
		if got := CombatThreat(test.base, test.flat, test.percent); got != test.want {
			t.Fatalf("CombatThreat(%d, %d, %d) = %d, want %d", test.base, test.flat, test.percent, got, test.want)
		}
	}
}
