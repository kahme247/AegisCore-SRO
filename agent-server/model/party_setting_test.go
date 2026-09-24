package model

import "testing"

func TestPartySettingMaxMembersUsesExperienceSharingBit(t *testing.T) {
	for setting, want := range map[PartySetting]int{
		ExpDistItemDistNoInvite:   4,
		ExpShareItemDistNoInvite:  8,
		ExpDistItemShareNoInvite:  4,
		ExpShareItemShareNoInvite: 8,
		ExpDistItemDist:           4,
		ExpShareItemDist:          8,
		ExpDistItemShare:          4,
		ExpShareItemShare:         8,
	} {
		if got := setting.MaxMembers(); got != want {
			t.Fatalf("setting %03b MaxMembers() = %d, want %d", setting, got, want)
		}
	}
}
