package model

import (
	"testing"
	"time"
)

func TestCommitActiveBuffAtUsesExecutionTimestamp(t *testing.T) {
	now := time.Unix(1000, 0)
	player := &Player{}
	if _, ok := player.CommitActiveBuffAt(9001, 77, 1, 5*time.Second, BuffModifiers{}, now); !ok {
		t.Fatal("timestamped buff commit failed")
	}
	buffs := player.ActiveBuffSnapshot()
	if len(buffs) != 1 {
		t.Fatalf("active buff count = %d, want 1", len(buffs))
	}
	if got, want := buffs[0].ExpiresAt, now.Add(5*time.Second); !got.Equal(want) {
		t.Fatalf("buff expiry = %s, want %s", got, want)
	}
}

func TestActiveRealRecoveryDescriptorFollowsBuffTeardown(t *testing.T) {
	player := &Player{}
	if _, ok := player.CommitActiveBuff(9002, 88, time.Minute, BuffModifiers{}); !ok {
		t.Fatal("active buff commit failed")
	}
	descriptor := RecoveryStatusDescriptor{SkillID: 9002, Mask: 0x800, Value: 80, Grade: 12}
	player.RegisterActiveRecoveryStatus(88, descriptor)
	if got := player.RecoveryStatusSnapshot(); len(got) != 1 || got[0] != descriptor {
		t.Fatalf("active recovery snapshot = %+v, want %+v", got, descriptor)
	}
	if !player.RemoveActiveBuff(88) {
		t.Fatal("active buff removal failed")
	}
	if got := player.RecoveryStatusSnapshot(); len(got) != 0 {
		t.Fatalf("active recovery survived buff teardown: %+v", got)
	}
}

func TestRecoveryStatusForMaskUsesHighestGrade(t *testing.T) {
	player := &Player{}
	player.Skills = map[uint32]byte{9003: 1, 9004: 1}
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		9003: {ID: 9003, BasicActivity: 0, RecoveryStatusMask: 0x400, RecoveryStatusValue: 30, RecoveryStatusGrade: 4},
		9004: {ID: 9004, BasicActivity: 0, RecoveryStatusMask: 0x400, RecoveryStatusValue: 50, RecoveryStatusGrade: 8},
	}})
	player.RecalculatePassiveSkillModifiers(catalog)
	snapshot := player.RecoveryStatusSnapshot()
	if len(snapshot) != 2 || snapshot[0].Grade != 4 || snapshot[1].Grade != 8 {
		t.Fatalf("recovery snapshot ordering = %+v, want grades [4 8]", snapshot)
	}
	got, ok := player.RecoveryStatusForMask(0x400)
	if !ok || got.SkillID != 9004 || got.Grade != 8 {
		t.Fatalf("mask lookup = %+v, found=%t; want skill 9004 grade 8", got, ok)
	}
}
