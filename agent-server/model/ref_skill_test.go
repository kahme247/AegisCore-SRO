package model

import "testing"

func TestRefSkillParsesTaggedParametersAtArbitraryOffsets(t *testing.T) {
	var skill RefSkill
	skill.Params[6] = skillParameterAttack
	skill.Params[7] = 9
	skill.Params[8] = 325
	skill.Params[9] = 32
	skill.Params[10] = 39
	skill.Params[20] = skillParameterDuration
	skill.Params[21] = 15000
	skill.parseParameters()

	if !skill.IsMagicalAttack() || skill.AttackPercent != 325 || skill.AttackMin != 32 || skill.AttackMax != 39 {
		t.Fatalf("attack parameters = %+v", skill)
	}
	if skill.BuffDuration != 15000 {
		t.Fatalf("buff duration = %d, want 15000", skill.BuffDuration)
	}
}

func TestRefSkillClassifiesHealingAndResurrection(t *testing.T) {
	healing := RefSkill{BasicActivity: 2}
	healing.Params[2] = skillParameterHeal
	healing.Params[3] = 369
	healing.Params[5] = 12
	healing.parseParameters()
	if healing.ExecutionKind() != SkillExecutionHealing || healing.HealHP != 369 || healing.HealMP != 12 {
		t.Fatalf("healing skill = %+v", healing)
	}

	resurrection := RefSkill{BasicActivity: 2}
	resurrection.Params[4] = skillParameterResurrection
	resurrection.Params[5] = 30
	resurrection.parseParameters()
	if resurrection.ExecutionKind() != SkillExecutionResurrection || resurrection.ResurrectLevel != 30 {
		t.Fatalf("resurrection skill = %+v", resurrection)
	}
}

func TestRefSkillParsesLightningImbueTransferAndShock(t *testing.T) {
	skill := RefSkill{BasicActivity: 2}
	skill.Params = [50]int{
		3, skillParameterDuration, 8000,
		skillParameterAttack, 8, 100, 964, 1791, 100,
		skillParameterShock, 240, 25, 50,
		skillParameterTransfer, 1, 6, 50, 2, 80, 24,
	}
	skill.parseParameters()
	if !skill.IsWeaponImbue() || skill.ExecutionKind() != SkillExecutionBuff {
		t.Fatalf("lightning skill was not classified as an imbue: %+v", skill)
	}
	if skill.ShockPower != 240 || skill.ShockChance != 25 || skill.ShockDuration != 5000 {
		t.Fatalf("shock = power:%d chance:%d duration:%d", skill.ShockPower, skill.ShockChance, skill.ShockDuration)
	}
	if skill.TransferRange != 50 || skill.TransferCount != 2 {
		t.Fatalf("transfer = range:%d count:%d", skill.TransferRange, skill.TransferCount)
	}
}

func TestRefSkillParsesFireAndColdImbueStatuses(t *testing.T) {
	fire := RefSkill{BasicActivity: 2}
	copy(fire.Params[:], []int{
		3, skillParameterDuration, 8000,
		skillParameterAttack, 8, 100, 1216, 2026, 100,
		skillParameterBurn, 240, 25, 108,
	})
	fire.parseParameters()
	if len(fire.StatusEffects) != 1 || fire.StatusEffects[0] != (SkillStatusEffect{Kind: CombatStatusBurn, Power: 240, Chance: 25, DurationMS: 10800}) {
		t.Fatalf("fire statuses = %+v", fire.StatusEffects)
	}

	cold := RefSkill{BasicActivity: 2}
	copy(cold.Params[:], []int{
		3, skillParameterDuration, 8000,
		skillParameterAttack, 8, 100, 993, 1489, 100,
		skillParameterFreeze, 240, 5,
		skillParameterFrostbite, 240, 25,
	})
	cold.parseParameters()
	want := []SkillStatusEffect{
		{Kind: CombatStatusFreeze, Power: 240, Chance: 5, DurationMS: 5000},
		{Kind: CombatStatusFrostbite, Power: 240, Chance: 25, DurationMS: 5000},
	}
	if len(cold.StatusEffects) != len(want) || cold.StatusEffects[0] != want[0] || cold.StatusEffects[1] != want[1] {
		t.Fatalf("cold statuses = %+v, want %+v", cold.StatusEffects, want)
	}
}

func TestRefSkillParsesVerifiedBuffModifiers(t *testing.T) {
	var skill RefSkill
	copy(skill.Params[:], []int{
		skillParameterHP, 102, 5,
		skillParameterMP, 80, 4,
		skillParameterStrength, 12, 0,
		skillParameterIntellect, 13, 0,
		skillParameterHit, 9, 2,
		skillParameterParry, 11, 3,
		skillParameterDamage, 4,
		skillParameterDefense, 37, 41, 0,
		skillParameterBlock, 15, 3,
	})
	skill.parseParameters()

	want := BuffModifiers{HP: 102, HPPercent: 5, MP: 80, MPPercent: 4, Strength: 12, Intellect: 13, HitRate: 9, HitRatePercent: 2, DamagePercent: 4, PhysicalDefense: 37, MagicalDefense: 41, BlockRate: 3, ParryRate: 11, ParryRatePercent: 3}
	if skill.BuffModifiers != want {
		t.Fatalf("buff modifiers = %+v, want %+v", skill.BuffModifiers, want)
	}
}
