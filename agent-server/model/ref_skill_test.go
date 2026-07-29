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
