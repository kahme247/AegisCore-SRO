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
