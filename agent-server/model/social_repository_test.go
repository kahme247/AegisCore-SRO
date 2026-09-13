package model

import (
	"errors"
	"math"
	"testing"
)

func TestGuildCapacityByLevel(t *testing.T) {
	tests := []struct {
		level byte
		want  int
	}{{1, 15}, {2, 20}, {3, 25}, {4, 35}, {5, 50}, {9, 50}}
	for _, test := range tests {
		if got := guildCapacity(test.level); got != test.want {
			t.Fatalf("guildCapacity(%d) = %d, want %d", test.level, got, test.want)
		}
	}
}

func TestWeeklyJobRewardAllocationsMatchStockProportionalFloorAndCap(t *testing.T) {
	members := []weeklyJobRewardMember{
		{characterID: 1, contribution: 1, pending: 10},
		{characterID: 2, contribution: 2, pending: 20},
		{characterID: 3, contribution: 0, pending: uint64(math.MaxInt32)},
	}
	got := weeklyJobRewardAllocations(100, members)
	if got[1] != 43 || got[2] != 86 || got[3] != math.MaxInt32 {
		t.Fatalf("allocations = %#v", got)
	}
	large := weeklyJobRewardAllocations(math.MaxUint64, []weeklyJobRewardMember{{characterID: 4, contribution: math.MaxUint64}})
	if large[4] != math.MaxInt32 {
		t.Fatalf("capped allocation = %d", large[4])
	}
}

func TestCombatJobExperienceMatchesLevelFactorClampAndTraderPenalty(t *testing.T) {
	levels, err := newLevelTable([]LevelDataRow{{Level: 1, JobActivityFactor: 24}, {Level: 2, JobActivityFactor: 47}})
	if err != nil {
		t.Fatal(err)
	}
	if got := CombatJobExperience(levels, 1, 2, JobThief); got != 87 {
		t.Fatalf("thief combat job EXP = %d, want 87", got)
	}
	if got := CombatJobExperience(levels, 1, 2, JobTrader); got != 43 {
		t.Fatalf("trader combat job EXP = %d, want 43", got)
	}
	if got := CombatJobExperience(levels, 2, 1, JobHunter); got != 15 {
		t.Fatalf("lower-target combat job EXP = %d, want 15", got)
	}
}

func TestPartyCombatJobExperienceAppliesLevelShareAndFivePercentBonus(t *testing.T) {
	if got := PartyCombatJobExperience(87, 20, 50, 2); got != 36 {
		t.Fatalf("party job EXP = %d, want 36", got)
	}
	if got := PartyCombatJobExperience(87, 30, 50, 2); got != 54 {
		t.Fatalf("party job EXP = %d, want 54", got)
	}
}

func TestValidateSocialTextRejectsProtocolUnsafeIdentity(t *testing.T) {
	for _, value := range []string{"", " padded", "padded ", "bad\x00name", "this-guild-name-is-far-too-long"} {
		if err := validateSocialText("guild name", value, 20, false); !errors.Is(err, ErrSocialInvalidRequest) {
			t.Fatalf("validateSocialText(%q) error = %v, want ErrSocialInvalidRequest", value, err)
		}
	}
	if err := validateSocialText("guild name", "Aegis", 20, false); err != nil {
		t.Fatalf("valid guild name rejected: %v", err)
	}
}

func TestSocialPermissionMaskIsCompleteAndStable(t *testing.T) {
	if GuildPermissionAll != 31 {
		t.Fatalf("GuildPermissionAll = %d, want 31", GuildPermissionAll)
	}
	if GuildMemberMaster != 0 || GuildMemberVice != 5 || GuildMemberNormal != 10 {
		t.Fatalf("guild member classes changed")
	}
}
