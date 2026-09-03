package model

import "testing"

func TestCombatVarianceDamageMatchesRecoveredGameServerSequence(t *testing.T) {
	tests := []struct {
		name                                    string
		minimum, maximum, attack, parry, al, tl int
		rolls                                   [4]int
		want                                    int
	}{
		{"equal rates centered at half", 100, 200, 100, 100, 50, 50, [4]int{0, 32767, 32767, 1}, 150},
		{"minimum of three rolls is deviation", 100, 200, 100, 100, 50, 50, [4]int{16383, 8191, 24575, 1}, 174},
		{"even sign subtracts deviation", 100, 200, 100, 100, 50, 50, [4]int{16383, 8191, 24575, 0}, 125},
		{"level advantage caps at thirty percent", 100, 200, 100, 100, 100, 50, [4]int{0, 0, 0, 1}, 180},
		{"center clamps to ten percent", 100, 200, 1, 1000, 1, 100, [4]int{0, 0, 0, 1}, 110},
		{"zero parry clamps center to ninety percent", 100, 200, 1, 0, 1, 1, [4]int{0, 0, 0, 1}, 190},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CombatVarianceDamage(test.minimum, test.maximum, test.attack, test.parry, test.al, test.tl, test.rolls)
			if got != test.want {
				t.Fatalf("CombatVarianceDamage() = %d, want %d", got, test.want)
			}
		})
	}
}
