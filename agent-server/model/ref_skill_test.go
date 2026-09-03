package model

import (
	"testing"
	"time"
)

func TestRefSkillBinaryTimingLifecycle(t *testing.T) {
	skill := RefSkill{BasicActivity: 2, ActionPreparingTime: 200, ActionCastingTime: 600, ActionDuration: 2400, ReuseDelay: 5000, CoolTime: 750}
	if got := skill.RuntimeCastingDuration(); got != 800*time.Millisecond {
		t.Fatalf("runtime casting duration = %s, want 800ms", got)
	}
	if got := skill.RuntimeCastingDurationAt(50); got != 400*time.Millisecond {
		t.Fatalf("runtime casting duration at 50%% = %s, want 400ms", got)
	}
	if got := skill.RuntimeImpactDurationAt(100); got != 3200*time.Millisecond {
		t.Fatalf("runtime impact duration = %s, want 3.2s", got)
	}
	if got := skill.RuntimeImpactDurationAt(50); got != 1600*time.Millisecond {
		t.Fatalf("runtime impact duration at 50%% = %s, want 1.6s", got)
	}
	if got := skill.ReuseDuration(100); got != 5*time.Second {
		t.Fatalf("reuse duration at 100%% = %s, want 5s", got)
	}
	// The 1.8 second authored tail is not scaled; only the 3.2 second active
	// lifecycle is halved.
	if got := skill.ReuseDuration(50); got != 3400*time.Millisecond {
		t.Fatalf("reuse duration at 50%% = %s, want 3.4s", got)
	}
	if got := skill.CoolTimeDuration(); got != 750*time.Millisecond {
		t.Fatalf("cool-time duration = %s, want 750ms", got)
	}
}

func TestRefSkillParsesGenericOnffWithoutInventingBarrier(t *testing.T) {
	skill := RefSkill{Params: [50]int{skillParameterBarrier, 5000, 41}}
	skill.parseParameters()
	if skill.UpkeepIntervalMS != 5000 || skill.UpkeepMPCost != 41 {
		t.Fatalf("upkeep = %d/%d, want 5000/41", skill.UpkeepIntervalMS, skill.UpkeepMPCost)
	}
	if skill.BarrierHitPoints != 0 || skill.BarrierDrainMS != 0 {
		t.Fatalf("non-pw onff classified as barrier: %+v", skill)
	}
}

func TestRefSkillParsesActiveEffectPrecedenceMetadata(t *testing.T) {
	skill := RefSkill{Params: [50]int{
		skillParameterOverlap2, 700,
		skillParameterDamageType, 12, 4,
		skillParameterLinkedSame,
		skillParameterTransfer, 2, 9900, 125, 0, 0, 1,
		skillParameterMatch, 1,
	}}
	skill.parseParameters()
	if skill.SecondaryOverlap != 700 || skill.DamageTypeFamily != 12 || skill.DamageTypePrecedence != 4 || !skill.LinkedSameGroup || skill.ReferenceDescriptors[2] != [6]int{2, 9900, 125, 0, 0, 1} || skill.ReferenceRadius != 125 || skill.MatchMode != 1 {
		t.Fatalf("active-effect metadata = %+v", skill)
	}
}

func TestRefSkillParsesProductionTransferDescriptor(t *testing.T) {
	skill := RefSkill{Params: [50]int{skillParameterTransfer, 1, 2, 20, 3, 35, 24}}
	skill.parseParameters()
	if skill.ReferenceDescriptors[1] != [6]int{1, 2, 20, 3, 35, 24} {
		t.Fatalf("reference descriptor = %v", skill.ReferenceDescriptors[1])
	}
	if skill.TransferType != 2 || skill.TransferRange != 20 || skill.TransferCount != 3 || skill.TransferFalloffPercent != 35 || skill.TransferTargetMask != 24 {
		t.Fatalf("transfer metadata = type:%d range:%d count:%d falloff:%d mask:%d", skill.TransferType, skill.TransferRange, skill.TransferCount, skill.TransferFalloffPercent, skill.TransferTargetMask)
	}
}

func TestRefSkillParsesDetectionActorMetadata(t *testing.T) {
	skill := RefSkill{Params: [50]int{skillParameterDetection, 5, 9, skillParameterTransfer, 2, 1, 150, 0, 0, 10}}
	skill.parseParameters()
	if skill.DetectionType != 5 || skill.DetectionLevel != 9 || skill.ReferenceRadius != 150 || !skill.HasReferenceDescriptor(2) {
		t.Fatalf("detection metadata = type:%d level:%d radius:%d descriptor:%v", skill.DetectionType, skill.DetectionLevel, skill.ReferenceRadius, skill.ReferenceDescriptors[2])
	}
}

func TestRefSkillParsesRealRecoveryDescriptor(t *testing.T) {
	skill := RefSkill{Params: [50]int{3, skillParameterRecoveryStatus, 0x400, 50, 11}}
	skill.parseParameters()
	if skill.RecoveryStatusMask != 0x400 || skill.RecoveryStatusValue != 50 || skill.RecoveryStatusGrade != 11 {
		t.Fatalf("real descriptor = mask:%X value:%d grade:%d", skill.RecoveryStatusMask, skill.RecoveryStatusValue, skill.RecoveryStatusGrade)
	}
}

func TestRefSkillParsesHideBodyStateMetadata(t *testing.T) {
	skill := RefSkill{Params: [50]int{skillParameterHide, 2, 9, 50}}
	skill.parseParameters()
	if skill.HideType != 2 || skill.HideLevel != 9 || skill.HideChance != 50 {
		t.Fatalf("hide metadata = type:%d level:%d chance:%d", skill.HideType, skill.HideLevel, skill.HideChance)
	}
}

func TestRefSkillParsesKnockOverAndKnockbackDescriptors(t *testing.T) {
	knockOver := RefSkill{Params: [50]int{skillParameterKnockOver, 17, 50}}
	knockOver.parseParameters()
	if knockOver.KnockOverKey != 17 || knockOver.KnockOverChance != 50 || knockOver.KnockbackDistance != 0 {
		t.Fatalf("ko metadata = %+v", knockOver)
	}
	knockback := RefSkill{Params: [50]int{skillParameterKnockback, 30, 20}}
	knockback.parseParameters()
	if knockback.KnockbackChance != 30 || knockback.KnockbackDistance != 20 || knockback.KnockOverKey != 0 {
		t.Fatalf("kb metadata = %+v", knockback)
	}
}

func TestRefSkillNormalizesLinkedDamageDescriptor(t *testing.T) {
	magical := RefSkill{Params: [50]int{skillParameterLinkedDamage, 4, 35, 3}}
	magical.parseParameters()
	if magical.LinkedDamageMask != 7 || magical.LinkedDamagePercent != 35 || magical.LinkedDamageUseLimit != 3 {
		t.Fatalf("magical lkdr = mask:%d percent:%d limit:%d", magical.LinkedDamageMask, magical.LinkedDamagePercent, magical.LinkedDamageUseLimit)
	}
	if got := magical.LinkedDamageAmount(200, true); got != 70 {
		t.Fatalf("magical linked amount = %d, want 70", got)
	}
	if got := magical.LinkedDamageAmount(200, false); got != 0 {
		t.Fatalf("physical amount through magical mask = %d, want 0", got)
	}
	physicalAndMagical := RefSkill{Params: [50]int{skillParameterLinkedDamage, 3, 100, 0}}
	physicalAndMagical.parseParameters()
	if physicalAndMagical.LinkedDamageMask != 15 || physicalAndMagical.LinkedDamageAmount(200, false) != 200 || physicalAndMagical.LinkedDamageAmount(200, true) != 200 {
		t.Fatalf("combined lkdr = %+v", physicalAndMagical)
	}
}

func TestRefSkillResourceCostsCombineFixedAndPercentagePools(t *testing.T) {
	skill := RefSkill{ConsumeHP: 25, ConsumeMP: 10, ConsumeHPRatio: 5, ConsumeMPRatio: 10}
	hp, mp := skill.ResourceCosts(1000, 500)
	if hp != 75 || mp != 60 {
		t.Fatalf("resource costs = hp:%d mp:%d, want hp:75 mp:60", hp, mp)
	}
}

func TestNASRUNMallSkillExecutesAsBuff(t *testing.T) {
	skill := RefSkill{CodeName: "SKILL_MALL_NASRUN_ECCENTRICDEMON_01", BasicActivity: 2, BuffDuration: 600000}
	if skill.SystemOwner() != SkillSystemNone || skill.ExecutionKind() != SkillExecutionBuff {
		t.Fatalf("NASRUN classification = owner %d execution %d", skill.SystemOwner(), skill.ExecutionKind())
	}
}

func TestRefSkillParsesRecoveredAggroIncreaseFlatAndPercent(t *testing.T) {
	skill := RefSkill{}
	copy(skill.Params[:], []int{skillParameterAggroIncrease, 320, 45})
	skill.parseParameters()
	if skill.AggroIncrease != 320 || skill.AggroIncreasePercent != 45 {
		t.Fatalf("aggro increase = flat:%d percent:%d, want 320 and 45", skill.AggroIncrease, skill.AggroIncreasePercent)
	}
}

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
	healing.Params[4] = 10
	healing.Params[5] = 12
	healing.Params[6] = 5
	healing.parseParameters()
	if healing.ExecutionKind() != SkillExecutionHealing || healing.HealHP != 369 || healing.HealHPPercent != 10 || healing.HealMP != 12 || healing.HealMPPercent != 5 {
		t.Fatalf("healing skill = %+v", healing)
	}

	resurrection := RefSkill{BasicActivity: 2}
	copy(resurrection.Params[:], []int{0, skillParameterHeal, 0, 10, 0, 10, skillParameterResurrection, 30, 2})
	resurrection.parseParameters()
	if resurrection.ExecutionKind() != SkillExecutionResurrection || resurrection.ResurrectLevel != 30 || resurrection.HealHPPercent != 10 || resurrection.HealMPPercent != 10 {
		t.Fatalf("resurrection skill = %+v", resurrection)
	}
	if hp, mp := resurrection.RestorationAmounts(1234, 987); hp != 123 || mp != 98 {
		t.Fatalf("percentage resurrection = %d/%d, want 123/98", hp, mp)
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
	if skill.ShockPower != 240 || skill.ShockChance != 25 || skill.ShockDuration != 24000 {
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
	if len(fire.StatusEffects) != 1 || fire.StatusEffects[0] != (SkillStatusEffect{Kind: CombatStatusBurn, Power: 240, Payload: 108, Chance: 25, DurationMS: 24000}) {
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
		{Kind: CombatStatusFreeze, Power: 240, Chance: 5, DurationMS: 24000},
		{Kind: CombatStatusFrostbite, Power: 240, Chance: 25, DurationMS: 24000},
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
		skillParameterDamage, 0, 4,
		skillParameterDefense, 37, 41, 0,
		skillParameterBlock, 15, 3,
	})
	skill.parseParameters()

	want := BuffModifiers{HP: 102, HPPercent: 5, MP: 80, MPPercent: 4, Strength: 12, Intellect: 13, HitRate: 9, HitRatePercent: 2, DamagePercent: 4, PhysicalDefense: 37, MagicalDefense: 41, BlockRate: 3, ParryRate: 11, ParryRatePercent: 3}
	if skill.BuffModifiers != want {
		t.Fatalf("buff modifiers = %+v, want %+v", skill.BuffModifiers, want)
	}
}

func TestRefSkillParsesPiercingForceDamageAfterReservedValue(t *testing.T) {
	skill := RefSkill{BasicActivity: 2}
	copy(skill.Params[:], []int{
		3, skillParameterDuration, 809244,
		skillParameterDamage, 0, 19,
	})
	skill.parseParameters()
	if skill.ExecutionKind() != SkillExecutionBuff || skill.BuffDuration != 809244 || skill.BuffModifiers.DamagePercent != 19 {
		t.Fatalf("piercing force = %+v", skill)
	}
}

func TestRefSkillClassifiesStatusOnlyEnemySkills(t *testing.T) {
	skill := RefSkill{BasicActivity: 2, TargetEnemyMob: true}
	skill.Params[0] = skillParameterFrostbite
	skill.Params[1] = 238
	skill.Params[2] = 100
	skill.parseParameters()
	if skill.ExecutionKind() != SkillExecutionStatusAttack {
		t.Fatalf("status-only execution kind = %d", skill.ExecutionKind())
	}
}

func TestRefSkillParsesDedicatedChineseSkillMechanics(t *testing.T) {
	frostNova := RefSkill{BasicActivity: 2, TargetEnemyMob: true}
	copy(frostNova.Params[:], []int{0, skillParameterFreeze, 240, 50, skillParameterFrostbite, 240, 100, skillParameterTransfer, 1, 1, 50, 5, 0, 24, skillParameterImpact, 5065})
	frostNova.parseParameters()
	if frostNova.ExecutionKind() != SkillExecutionStatusAttack || frostNova.ImpactDamage != 5065 || frostNova.TransferRange != 50 || frostNova.TransferCount != 5 {
		t.Fatalf("frost nova = %+v", frostNova)
	}

	barrier := RefSkill{BasicActivity: 2}
	copy(barrier.Params[:], []int{3, skillParameterBarrier, 5000, 1067, 28791, 7, 21334, 0, 98})
	barrier.parseParameters()
	if barrier.ExecutionKind() != SkillExecutionBarrier || barrier.BarrierDrainMS != 5000 || barrier.BarrierMPCost != 1067 || barrier.BarrierHitPoints != 21334 || barrier.BarrierAbsorption != 98 || !barrier.BarrierPhysical {
		t.Fatalf("barrier = %+v", barrier)
	}
	requiredShield := RefSkill{BasicActivity: 2}
	copy(requiredShield.Params[:], []int{3, skillParameterDuration, 637815, skillParameterRequiredItem, 4, 1})
	requiredShield.parseParameters()
	if len(requiredShield.RequiredItems) != 1 || requiredShield.RequiredItems[0] != (SkillRequiredItem{TypeID3: 4, TypeID4: 1}) {
		t.Fatalf("required shield = %+v", requiredShield.RequiredItems)
	}

	movement := RefSkill{BasicActivity: 2}
	copy(movement.Params[:], []int{0, skillParameterTeleport, 500, 245})
	movement.parseParameters()
	if movement.ExecutionKind() != SkillExecutionMovement || movement.MovementDurationMS != 500 || movement.MovementDistance != 245 {
		t.Fatalf("movement = %+v", movement)
	}

	therapy := RefSkill{BasicActivity: 2}
	copy(therapy.Params[:], []int{3, skillParameterDuration, 300000, skillParameterAggroBlock, 3, 113, skillParameterRecovery, 285, 285})
	therapy.parseParameters()
	if therapy.ExecutionKind() != SkillExecutionBuff || !therapy.AggroSuppression || therapy.RecoveryHP != 285 || therapy.RecoveryMP != 285 {
		t.Fatalf("source therapy = %+v", therapy)
	}

	vital := RefSkill{BasicActivity: 2, TargetEnemyMob: true}
	copy(vital.Params[:], []int{0, skillParameterPhyDefDown, 30000, 80, 11, 712})
	vital.parseParameters()
	want := SkillStatusEffect{Kind: CombatStatusPhysicalDefenseDown, Power: 712, Level: 11, Chance: 80, DurationMS: 30000}
	if vital.ExecutionKind() != SkillExecutionStatusAttack || len(vital.StatusEffects) != 1 || vital.StatusEffects[0] != want {
		t.Fatalf("vital spot = %+v", vital)
	}
}

func TestRefSkillParsesCatalogWideAbnormalStatesAndTeleportVariants(t *testing.T) {
	tests := []struct {
		name   string
		params []int
		want   SkillStatusEffect
	}{
		{"poison", []int{skillParameterPoison, 240, 25, 91}, SkillStatusEffect{Kind: CombatStatusPoison, Power: 240, Payload: 91, Chance: 25, DurationMS: 24000}},
		{"zombie", []int{skillParameterZombie, 206, 70}, SkillStatusEffect{Kind: CombatStatusZombie, Power: 206, Chance: 70, DurationMS: 20600}},
		{"sleep", []int{skillParameterSleep, 5000, 80, 11}, SkillStatusEffect{Kind: CombatStatusSleep, Chance: 80, Level: 11, DurationMS: 5000}},
		{"darkness", []int{skillParameterDarkness, 30000, 75, 10, 122}, SkillStatusEffect{Kind: CombatStatusDarkness, Power: 122, Chance: 75, Level: 10, DurationMS: 30000}},
		{"bleed", []int{skillParameterBleed, 12000, 0, 9, 72, 65}, SkillStatusEffect{Kind: CombatStatusBleed, Power: 72, Chance: 65, Level: 9, DurationMS: 12000}},
		{"division", []int{skillParameterDamageTaken, 30000, 80, 11, 35}, SkillStatusEffect{Kind: CombatStatusDamageTakenUp, Power: 35, Chance: 80, Level: 11, DurationMS: 30000}},
		{"panic", []int{skillParameterPanic, 8000, 60, 7}, SkillStatusEffect{Kind: CombatStatusPanic, Chance: 60, Level: 7, DurationMS: 8000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var skill RefSkill
			copy(skill.Params[:], test.params)
			skill.TargetEnemyMob = true
			skill.BasicActivity = 2
			skill.parseParameters()
			if len(skill.StatusEffects) != 1 || skill.StatusEffects[0] != test.want {
				t.Fatalf("effects = %+v, want %+v", skill.StatusEffects, test.want)
			}
			if skill.ExecutionKind() != SkillExecutionStatusAttack {
				t.Fatalf("execution kind = %d, want status attack", skill.ExecutionKind())
			}
		})
	}

	movement := RefSkill{BasicActivity: 2}
	copy(movement.Params[:], []int{skillParameterTeleportV1, 500, 245})
	movement.parseParameters()
	if movement.ExecutionKind() != SkillExecutionMovement || movement.MovementDurationMS != 500 || movement.MovementDistance != 245 {
		t.Fatalf("v1 teleport = %+v", movement)
	}
}

func TestRefSkillParsesUtilitySkillFamiliesFromOfficialArgumentCounts(t *testing.T) {
	cure := RefSkill{BasicActivity: 2}
	copy(cure.Params[:], []int{
		0, skillParameterCureStatus, 63, 36,
		skillParameterCureLevel, skillParameterCureCurse, 100, 1,
		skillParameterCureCount, 2,
	})
	cure.parseParameters()
	if cure.ExecutionKind() != SkillExecutionCure || cure.CureStatusMask != 63 || cure.CureStatusMaxLevel != 36 || !cure.CureCurse || cure.CureCurseChance != 100 || cure.CureCurseMaxLevel != 1 || cure.CureCount != 2 {
		t.Fatalf("water cure = %+v", cure)
	}

	absorb := RefSkill{BasicActivity: 2, TargetRequired: true, TargetEnemyMob: true}
	copy(absorb.Params[:], []int{0, skillParameterAbsoluteDamage, 130, skillParameterDamageToMP, 0, 100})
	absorb.parseParameters()
	if absorb.ExecutionKind() != SkillExecutionMagicalAttack || absorb.AbsoluteDamage != 130 || absorb.DamageToHPPercent != 0 || absorb.DamageToMPPercent != 100 {
		t.Fatalf("MP absorb = %+v", absorb)
	}

	transform := RefSkill{BasicActivity: 2, TargetRequired: true}
	copy(transform.Params[:], []int{0, skillParameterTransformMask, 20, 10371})
	transform.parseParameters()
	if transform.ExecutionKind() != SkillExecutionTransform || transform.TransformMaxLevel != 20 || transform.TransformItemRefID != 10371 {
		t.Fatalf("monster mask = %+v", transform)
	}
}
