package model

import "testing"

func TestGuildSiegeAuthorityLimitsMatchGameServer(t *testing.T) {
	tests := []struct {
		authority byte
		limit     int
		valid     bool
	}{
		{GuildSiegeAuthorityCommander, 4, true},
		{GuildSiegeAuthorityDefense, 1, true},
		{GuildSiegeAuthorityOffense, 1, true},
		{GuildSiegeAuthorityProduction, 1, true},
		{GuildSiegeAuthorityCombatant, 10, true},
		{GuildSiegeAuthorityNone, 0, false},
		{GuildSiegeAuthorityMaster, 0, false},
		{3, 0, false},
	}
	for _, test := range tests {
		limit, valid := guildSiegeAuthorityLimit(test.authority)
		if limit != test.limit || valid != test.valid {
			t.Fatalf("authority %#x = (%d,%v), want (%d,%v)", test.authority, limit, valid, test.limit, test.valid)
		}
	}
}
