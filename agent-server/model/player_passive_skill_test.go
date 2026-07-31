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
