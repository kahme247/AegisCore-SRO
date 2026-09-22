package model

import (
	"testing"
	"time"
)

func TestRecalculatePassiveSkillModifiersReplacesLevelsAndPreservesActiveBuffs(t *testing.T) {
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		69:  {ID: 69, BasicActivity: 0, BuffModifiers: BuffModifiers{HP: 102}},
		934: {ID: 934, BasicActivity: 0, BuffModifiers: BuffModifiers{HP: 197}},
		77:  {ID: 77, BasicActivity: 2, BuffModifiers: BuffModifiers{HP: 50}},
	}})
	player := &Player{Skills: map[uint32]byte{69: 1}}
	if _, ok := player.CommitActiveBuff(77, 1, time.Minute, BuffModifiers{HP: 50}); !ok {
		t.Fatal("failed to apply active buff")
	}

	player.RecalculatePassiveSkillModifiers(catalog)
	if player.BonusStats.HP != 152 || player.PassiveSkillModifiers.HP != 102 {
		t.Fatalf("level 1 passive HP = bonus:%d passive:%d, want 152/102", player.BonusStats.HP, player.PassiveSkillModifiers.HP)
	}
	player.SetSkills([]CharacterSkill{{ID: 934, Enabled: 1}})
	player.RecalculatePassiveSkillModifiers(catalog)
	if player.BonusStats.HP != 247 || player.PassiveSkillModifiers.HP != 197 {
		t.Fatalf("level 2 passive HP = bonus:%d passive:%d, want 247/197", player.BonusStats.HP, player.PassiveSkillModifiers.HP)
	}
}

func TestPassiveCriticalModifierReachesDerivedStats(t *testing.T) {
	const skillID = uint32(9003)
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		skillID: {ID: skillID, BasicActivity: 0, BuffModifiers: BuffModifiers{CriticalRate: 2}},
	}})
	player := &Player{
		Skills:          map[uint32]byte{skillID: 1},
		BaseAttackStats: AttackStats{CriticalRate: 30},
	}
	player.RecalculatePassiveSkillModifiers(catalog)
	if got := player.DerivedStats().CriticalRate; got != 32 {
		t.Fatalf("passive critical rate = %d, want 32", got)
	}
}

func TestRecalculatePassiveSkillModifiersRequiresAuthoredEquippedItem(t *testing.T) {
	const skillID = uint32(39)
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		skillID: {
			ID: skillID, BasicActivity: 0, BuffModifiers: BuffModifiers{BlockRate: 2},
			RequiredItems: []SkillRequiredItem{{TypeID3: 4, TypeID4: 1}},
		},
	}})
	player := &Player{Skills: map[uint32]byte{skillID: 1}, Inventory: NewInventory(nil), AvatarInventory: NewInventory(nil)}
	player.RecalculatePassiveSkillModifiers(catalog)
	if player.BonusDefenseStats.BlockRate != 0 {
		t.Fatalf("shield passive without shield = %d, want 0", player.BonusDefenseStats.BlockRate)
	}
	player.Inventory.Items[SlotSecondaryWeapon] = Item{SRObject: SRObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 4, TypeID4: 1}}}
	player.RecalculatePassiveSkillModifiers(catalog)
	if player.BonusDefenseStats.BlockRate != 2 {
		t.Fatalf("shield passive with shield = %d, want 2", player.BonusDefenseStats.BlockRate)
	}
	delete(player.Inventory.Items, SlotSecondaryWeapon)
	player.RecalculatePassiveSkillModifiers(catalog)
	if player.BonusDefenseStats.BlockRate != 0 {
		t.Fatalf("shield passive after unequip = %d, want 0", player.BonusDefenseStats.BlockRate)
	}
}

func TestRecalculatePassiveSkillsRegistersRealRecoveryDescriptors(t *testing.T) {
	player := &Player{Skills: map[uint32]byte{9001: 1, 9002: 0}}
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		9001: {ID: 9001, BasicActivity: 0, RecoveryStatusMask: 0x400, RecoveryStatusValue: 50, RecoveryStatusGrade: 11},
		9002: {ID: 9002, BasicActivity: 0, RecoveryStatusMask: 0x800, RecoveryStatusValue: 80, RecoveryStatusGrade: 12},
	}})
	player.RecalculatePassiveSkillModifiers(catalog)
	got := player.PassiveRecoveryStatusSnapshot()
	if len(got) != 1 || got[0] != (RecoveryStatusDescriptor{SkillID: 9001, Mask: 0x400, Value: 50, Grade: 11}) {
		t.Fatalf("passive real registry = %+v", got)
	}
}

func TestPassiveCriticalDefenseRequiresEnabledSkill(t *testing.T) {
	const skillID = uint32(8661)
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		skillID: {ID: skillID, BasicActivity: 0, PassiveCriticalDefense: 14},
	}})
	player := &Player{Skills: map[uint32]byte{skillID: 1}}
	player.RecalculatePassiveSkillModifiers(catalog)
	if got := player.PassiveCriticalDefense(); got != 14 {
		t.Fatalf("passive critical defense = %d, want 14", got)
	}
	player.Skills[skillID] = 0
	player.RecalculatePassiveSkillModifiers(catalog)
	if got := player.PassiveCriticalDefense(); got != 0 {
		t.Fatalf("disabled passive critical defense = %d, want 0", got)
	}
}

func TestPassiveClericPraiseHealingRequiresEnabledSkill(t *testing.T) {
	const skillID = uint32(11665)
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		skillID: {ID: skillID, BasicActivity: 0, PassiveHealingPercent: 20},
	}})
	player := &Player{Skills: map[uint32]byte{skillID: 1}}
	player.RecalculatePassiveSkillModifiers(catalog)
	if got := player.PassiveHealingPercent(); got != 20 {
		t.Fatalf("passive healing percent = %d, want 20", got)
	}
}

func TestPassiveClericPraiseBlessingRequiresEnabledSkill(t *testing.T) {
	const skillID = uint32(11848)
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		skillID: {ID: skillID, BasicActivity: 0, PassiveBlessingPercent: 15},
	}})
	player := &Player{Skills: map[uint32]byte{skillID: 1}}
	player.RecalculatePassiveSkillModifiers(catalog)
	if got := player.PassiveBlessingPercent(); got != 15 {
		t.Fatalf("passive blessing percent = %d, want 15", got)
	}
}

func TestPassiveAbnormalDurationResistanceUsesIndependentChannels(t *testing.T) {
	catalog := NewReferenceDataCatalog(ReferenceDataSnapshot{Skills: map[uint32]RefSkill{
		1: {ID: 1, BasicActivity: 0, PassiveStatusDurationMask: 1, PassiveStatusDurationPercent: 10},
		2: {ID: 2, BasicActivity: 0, PassiveStatusDurationMask: 8, PassiveStatusDurationPercent: 20},
		3: {ID: 3, BasicActivity: 0, PassiveStatusDurationMask: 2, PassiveStatusDurationPercent: 150},
		4: {ID: 4, BasicActivity: 0, PassiveStatusDurationMask: 4 | 16 | 32, PassiveStatusDurationPercent: 30},
	}})
	player := &Player{Skills: map[uint32]byte{1: 1, 2: 1, 3: 1, 4: 1}}
	player.RecalculatePassiveSkillModifiers(catalog)
	for _, test := range []struct {
		kind CombatStatusKind
		want int
	}{
		{CombatStatusFreeze, 900},
		{CombatStatusBurn, 800},
		{CombatStatusFrostbite, 0},
		{CombatStatusShock, 700},
		{CombatStatusPoison, 700},
		{CombatStatusZombie, 700},
	} {
		if got := player.CombatStatusDurationMS(test.kind, 1000); got != test.want {
			t.Fatalf("status %d duration = %d, want %d", test.kind, got, test.want)
		}
	}
}
