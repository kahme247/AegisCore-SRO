package model

import (
	"errors"
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
