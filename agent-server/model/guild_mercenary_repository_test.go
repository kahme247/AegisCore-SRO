package model

import "testing"

func TestGuildMercenaryAttributeCost(t *testing.T) {
	tests := []struct {
		current, requested byte
		gold               uint64
		valid              bool
	}{
		{0, 1, GuildMercenaryAddGold, true},
		{1, 2, GuildMercenaryAddGold, true},
		{3, 4, 0, false},
		{1, 1, 0, false},
		{3, 0, GuildMercenaryClearGold, true},
		{0, 0, GuildMercenaryClearGold, false},
		{0, 3, 0, false},
		{0, 0x20, 0, false},
	}
	for _, test := range tests {
		gold, valid := guildMercenaryAttributeCost(test.current, test.requested)
		if gold != test.gold || valid != test.valid {
			t.Fatalf("cost(%#x,%#x) = (%d,%v), want (%d,%v)", test.current, test.requested, gold, valid, test.gold, test.valid)
		}
	}
}
