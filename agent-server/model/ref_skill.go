package model

import (
	"math"
	"strings"
	"time"
)

type SkillExecutionKind byte

const (
	SkillExecutionUnsupported SkillExecutionKind = iota
	SkillExecutionPassive
	SkillExecutionPhysicalAttack
	SkillExecutionMagicalAttack
	SkillExecutionBuff
	SkillExecutionHealing
	SkillExecutionResurrection
	SkillExecutionStatusAttack
	SkillExecutionBarrier
	SkillExecutionMovement
	SkillExecutionCure
	SkillExecutionAggro
	SkillExecutionTransform
	SkillExecutionSystem
)

type SkillSystemOwner byte

const (
	SkillSystemNone SkillSystemOwner = iota
	SkillSystemMall
	SkillSystemTitle
	SkillSystemFortress
	SkillSystemItem
	SkillSystemEvent
)

const (
	skillParameterAttack                     = 6386804
	skillParameterSystemBuff                 = 1667396966
	skillParameterDuration                   = 1685418593
	skillParameterHeal                       = 1751474540
	skillParameterResurrection               = 1919251317
	skillParameterBlock                      = 25202
	skillParameterParry                      = 25970
	skillParameterHit                        = 26738
	skillParameterDamage                     = 6582901
	skillParameterHP                         = 6844521
	skillParameterMP                         = 7172201
	skillParameterDamageAbsorption           = 1868849522
	skillParameterDefense                    = 1684366960
	skillParameterIntellect                  = 1768846441
	skillParameterStrength                   = 1937011305
	skillParameterGoldDropAmount             = 6775922
	skillParameterShock                      = 25971
	skillParameterTransfer                   = 6645362
	skillParameterBurn                       = 25205
	skillParameterFreeze                     = 26234
	skillParameterFrostbite                  = 26210
	skillParameterPoison                     = 28787
	skillParameterZombie                     = 31330
	skillParameterBleed                      = 25196
	skillParameterConfusion                  = 25441
	skillParameterDarkness                   = 25710
	skillParameterDisease                    = 25715
	skillParameterFear                       = 26213
	skillParameterShortSight                 = 28025
	skillParameterBind                       = 29300
	skillParameterSleep                      = 29541
	skillParameterDull                       = 29548
	skillParameterPanic                      = 1668507760
	skillParameterCombustion                 = 1668509040
	skillParameterHidden                     = 84545280
	skillParameterImpact                     = 1952542324
	skillParameterBarrier                    = 1869506150
	skillParameterTeleport                   = 1952803941
	skillParameterTeleportV1                 = 1952803890
	skillParameterParryDown                  = 1952805476
	skillParameterHitDown                    = 1953002084
	skillParameterPhyDefDown                 = 1668509796
	skillParameterMagDefDown                 = 1668509028
	skillParameterAttackDown                 = 1668510578
	skillParameterDamageTaken                = 1668508020
	skillParameterStun                       = 29556
	skillParameterAggroBlock                 = 1886350433
	skillParameterRecovery                   = 1769105251
	skillParameterRecoveryStatus             = 1919246700 // `real`
	skillParameterAbnormalDurationResistance = 1919246708 // `taer`
	skillParameterRequiredItem               = 1919250793
	skillParameterCureStatus                 = 1668641396
	skillParameterCureLevel                  = 1668641388
	skillParameterCureCurse                  = 25145280
	skillParameterCureCount                  = 1919120754
	skillParameterAggroReduce                = 1685352052
	skillParameterAggroAbsorb                = 1818976615
	skillParameterAggroIncrease              = 1953395762
	skillParameterAbsoluteDamage             = 1885629799
	skillParameterDamageToMP                 = 1684891508
	skillParameterReturnDamage               = 1684891506 // `rgmd`
	skillParameterBloodTrap                  = 29794      // `bt`
	skillParameterTransformMask              = 1835229552
	skillParameterOverlap2                   = 1870031922
	skillParameterDamageType                 = 1685353584
	skillParameterLinkedSame                 = 1818981170
	skillParameterMatch                      = 1836278632
	skillParameterKnockOver                  = 27503
	skillParameterKnockback                  = 27490
	skillParameterLinkedDamage               = 1818977394
	skillParameterDetection                  = 6583412
	skillParameterAttackPower                = 1634754933 // `uapa`
	skillParameterMaxHPReduction             = 1886218352 // `pmhp`
	skillParameterCriticalRate               = 25458      // `rc`
	skillParameterMovementSpeed              = 1752396901 // `etsh`
	skillParameterRangePassive               = 1936028790 // `vtes`
	skillParameterHide                       = 1751737445
	skillParameterCriticalDefense            = 1684238953 // `ircd`
)

type SkillRequiredItem struct {
	TypeID3 int
	TypeID4 int
}

type SkillStatusEffect struct {
	Kind               CombatStatusKind
	Power              int
	Level              int
	Payload            int
	Chance             int
	DurationMS         int
	DurationChoiceMask uint32
	NativeSubtype4     bool
}

// ReturnDamageDescriptor is the four-word native `rgmd` active-effect
// descriptor. It is evaluated by the owning defender when it receives damage.
// BloodTrapDescriptor is the four-word native `bt` descriptor. SR_GameServer
// stores it at RefSkill +0x474, creates abnormal record type 0x18, and copies
// EffectValue to the record's +0x1C subtype-4 value.
type BloodTrapDescriptor struct {
	DurationChoiceMask uint32
	ChancePercent      int
	Level              int
	EffectValue        int
}

type ReturnDamageDescriptor struct {
	// ChancePercent is native `rgmd` word 0. Native requires it to be
	// positive, then uses it with the actor-owned chance accumulator.
	ChancePercent   int
	PhysicalPercent int
	MagicalPercent  int
	MaximumRange    int
}

// RecoveryStatusDescriptor is the actor-owned form of a binary `real` tuple.
// Its mask/value/grade are preserved independently of ordinary cast recovery.
type RecoveryStatusDescriptor struct {
	SkillID uint32
	Mask    uint32
	Value   int
	Grade   int
}

type BuffModifiers struct {
	HP                              int
	HPPercent                       int
	MP                              int
	MPPercent                       int
	Strength                        int
	Intellect                       int
	HitRate                         int
	HitRatePercent                  int
	PhysicalAttack                  int
	MagicalAttack                   int
	CriticalRate                    int
	MovementSpeedPercent            int
	DamagePercent                   int
	DamageAbsorptionPercent         int
	PhysicalDamageAbsorptionPercent int
	MagicalDamageAbsorptionPercent  int
	PhysicalDefense                 int
	MagicalDefense                  int
	BlockRate                       int
	ParryRate                       int
	ParryRatePercent                int
	GoldDropAmountPercent           int
}

// RefSkill contains the server-side timing and reach values needed to execute
// a reference skill without inventing combat constants. The values come from
// the client-matched REF_SKILL catalog and are expressed in vSRO world units
// and milliseconds.
type RefSkill struct {
	ID                  uint32
	CodeName            string
	GroupID             uint32
	BasicGroup          string
	BasicOriginal       uint32
	BasicLevel          byte
	BasicActivity       byte
	BasicChainCode      uint32
	Mastery1            uint32
	Mastery2            uint32
	MasteryLevel1       byte
	MasteryLevel2       byte
	RequiredStr         int
	RequiredInt         int
	RequiredSkill1      uint32
	RequiredSkill2      uint32
	RequiredSkill3      uint32
	RequiredLevel1      byte
	RequiredLevel2      byte
	RequiredLevel3      byte
	RequiredSP          uint32
	RequiredRace        byte
	TargetRequired      bool
	TargetSelf          bool
	TargetAlly          bool
	TargetParty         bool
	TargetEnemyMob      bool
	TargetEnemyPVP      bool
	TargetDeadBody      bool
	RequiredWeapon1     byte
	RequiredWeapon2     byte
	ConsumeHP           int
	ConsumeMP           int
	ConsumeHPRatio      int
	ConsumeMPRatio      int
	ActionPreparingTime int
	ActionCastingTime   int
	ActionDuration      int
	ReuseDelay          int
	CoolTime            int
	// ActionFlyingSpeed and ActionInterruptible are native REF_SKILL
	// controller fields. They are retained even though the common traced
	// execution path does not yet prove their runtime semantics.
	ActionFlyingSpeed            int
	ActionInterruptible          bool
	ActionRange                  int
	ActionOverlap                int
	AttackType                   int
	AttackPercent                int
	AttackMin                    int
	AttackMax                    int
	BuffDuration                 int
	HealHP                       int
	HealHPPercent                int
	HealMP                       int
	HealMPPercent                int
	ResurrectLevel               int
	ShockPower                   int
	ShockChance                  int
	ShockDuration                int
	TransferRange                int
	TransferCount                int
	TransferType                 int
	TransferFalloffPercent       int
	TransferTargetMask           uint32
	StatusEffects                []SkillStatusEffect
	BloodTrap                    BloodTrapDescriptor
	ImpactDamage                 int
	BarrierDrainMS               int
	BarrierMPCost                int
	UpkeepIntervalMS             int
	UpkeepMPCost                 int
	BarrierHitPoints             int
	BarrierAbsorption            int
	BarrierPhysical              bool
	MovementDistance             int
	MovementDurationMS           int
	AggroSuppression             bool
	RecoveryHP                   int
	RecoveryMP                   int
	RecoveryStatusMask           uint32
	RecoveryStatusValue          int
	RecoveryStatusGrade          int
	RequiredItems                []SkillRequiredItem
	CureStatusMask               uint32
	CureStatusMaxLevel           int
	CureCurse                    bool
	CureCurseChance              int
	CureCurseMaxLevel            int
	CureCount                    int
	AggroReduce                  int
	AggroAbsorbPercent           int
	AggroIncrease                int
	AggroIncreasePercent         int
	AbsoluteDamage               int
	DamageToHPPercent            int
	DamageToMPPercent            int
	TransformMaxLevel            int
	TransformItemRefID           uint32
	SecondaryOverlap             int
	DamageTypeFamily             int
	DamageTypePrecedence         int
	LinkedSameGroup              bool
	ReferenceDescriptors         [4][6]int
	ReferenceRadius              int
	MatchMode                    int
	KnockOverKey                 int
	KnockOverChance              int
	KnockbackChance              int
	KnockbackDistance            int
	LinkedDamageMask             uint32
	LinkedDamagePercent          int
	LinkedDamageUseLimit         int
	ReturnDamage                 ReturnDamageDescriptor
	DetectionType                int
	DetectionLevel               int
	HideType                     int
	HideLevel                    int
	HideChance                   int
	PassiveSkillRangeBonus       int
	PassiveHealingPercent        int
	PassiveBlessingPercent       int
	PassiveStatusDurationMask    uint32
	PassiveStatusDurationPercent int
	PassiveCriticalDefense       int
	BuffModifiers                BuffModifiers
	Params                       [50]int
}

func (s RefSkill) IsPassive() bool { return s.BasicActivity == 0 }

// RuntimeCastingDuration is the post-load Action_CastingTime used by the
// vSRO GameServer: authored preparation plus authored casting time.
func (s RefSkill) RuntimeCastingDuration() time.Duration {
	return s.RuntimeCastingDurationAt(100)
}

// RuntimeCastingDurationAt scales the authored active cast phases by the
// native action-time percentage. A value of 100 preserves the catalog timing;
// callers may supply an actor-specific runtime value when one is available.
func (s RefSkill) RuntimeCastingDurationAt(actionTimePercent float64) time.Duration {
	if actionTimePercent < 0 {
		actionTimePercent = 0
	}
	milliseconds := max(0, s.ActionPreparingTime) + max(0, s.ActionCastingTime)
	scaled := int(math.Round(actionTimePercent * float64(milliseconds) / 100))
	return time.Duration(max(0, scaled)) * time.Millisecond
}

// RuntimeImpactDurationAt returns the complete authored attack-animation
// lifecycle. Damage-bearing B071 packets must not be emitted after only the
// preparation/casting phases because physical skills commonly store their
// visible swing or shot animation exclusively in ACTION_DURATION.
func (s RefSkill) RuntimeImpactDurationAt(actionTimePercent float64) time.Duration {
	if actionTimePercent < 0 {
		actionTimePercent = 0
	}
	milliseconds := max(0, s.ActionPreparingTime) + max(0, s.ActionCastingTime) + max(0, s.ActionDuration)
	scaled := int(math.Round(actionTimePercent * float64(milliseconds) / 100))
	return time.Duration(max(0, scaled)) * time.Millisecond
}

// ReuseDuration reproduces SR_GameServer's ordinary activity-2 cooldown
// registration. Cast/action speed scales only the active lifecycle; an
// authored reuse tail beyond that lifecycle remains unscaled.
func (s RefSkill) ReuseDuration(actionTimePercent float64) time.Duration {
	if s.BasicActivity != 2 || s.ActionDuration <= 0 {
		return time.Duration(max(0, s.ReuseDelay)) * time.Millisecond
	}
	if actionTimePercent < 0 {
		actionTimePercent = 0
	}
	lifecycle := max(0, s.ActionPreparingTime) + max(0, s.ActionCastingTime) + max(0, s.ActionDuration)
	tail := max(0, s.ReuseDelay-lifecycle)
	scaled := int(math.Round(actionTimePercent * float64(lifecycle) / 100))
	return time.Duration(max(0, scaled+tail)) * time.Millisecond
}

func (s RefSkill) CoolTimeDuration() time.Duration {
	return time.Duration(max(0, s.CoolTime)) * time.Millisecond
}

// ResourceCosts resolves the REF_SKILL consume tuple. Fixed costs and the
// authored percentage costs are additive and percentage values are based on
// the caster's current derived maximum pool, matching the server's
// pre-engage resource gate. Negative catalog values are treated as absent.
func (s RefSkill) ResourceCosts(maxHP, maxMP int) (hp, mp int) {
	hp = max(0, s.ConsumeHP)
	mp = max(0, s.ConsumeMP)
	if maxHP > 0 && s.ConsumeHPRatio > 0 {
		hp += maxHP * s.ConsumeHPRatio / 100
	}
	if maxMP > 0 && s.ConsumeMPRatio > 0 {
		mp += maxMP * s.ConsumeMPRatio / 100
	}
	return hp, mp
}

func (s RefSkill) HasReferenceDescriptor(discriminator int) bool {
	return discriminator >= 1 && discriminator < len(s.ReferenceDescriptors) && s.ReferenceDescriptors[discriminator][0] == discriminator
}

func (s RefSkill) IsMagicalAttack() bool {
	return s.AttackType == 8 || s.AttackType == 9 || s.AttackType == 10
}

func (s RefSkill) IsPhysicalAttack() bool {
	return s.AttackType == 5 || s.AttackType == 6
}

func (s RefSkill) IsWeaponImbue() bool {
	return s.BasicActivity != 0 && s.BuffDuration > 0 && !s.TargetRequired && s.ActionRange == 0 && s.IsMagicalAttack() && s.AttackPercent > 0
}

func (s RefSkill) ExecutionKind() SkillExecutionKind {
	switch {
	case s.IsPassive():
		return SkillExecutionPassive
	case s.SystemOwner() != SkillSystemNone:
		return SkillExecutionSystem
	case s.IsWeaponImbue():
		// Chinese weapon imbues carry attack parameters but are no-target,
		// short-lived self effects rather than immediate attacks.
		return SkillExecutionBuff
	case s.IsPhysicalAttack():
		return SkillExecutionPhysicalAttack
	case s.IsMagicalAttack():
		return SkillExecutionMagicalAttack
	case s.ResurrectLevel > 0:
		return SkillExecutionResurrection
	case s.HealHP > 0 || s.HealHPPercent > 0 || s.HealMP > 0 || s.HealMPPercent > 0:
		return SkillExecutionHealing
	case s.CureStatusMask != 0 || s.CureCurse || s.CureCount > 0:
		return SkillExecutionCure
	case s.AggroReduce != 0 || s.AggroIncrease != 0:
		return SkillExecutionAggro
	case s.TransformMaxLevel > 0:
		return SkillExecutionTransform
	case s.AbsoluteDamage > 0:
		return SkillExecutionMagicalAttack
	case len(s.StatusEffects) > 0 && (s.TargetEnemyMob || s.TargetRequired || s.TransferCount > 0):
		return SkillExecutionStatusAttack
	case s.BarrierHitPoints > 0:
		return SkillExecutionBarrier
	case s.MovementDistance > 0:
		return SkillExecutionMovement
	case s.BuffDuration > 0 || s.UpkeepIntervalMS > 0 || s.ReturnDamage.ChancePercent > 0:
		return SkillExecutionBuff
	default:
		return SkillExecutionUnsupported
	}
}

// SupportsItemUseBuff reports whether a linked consumable can safely use the
// persisted character-buff path. Mall-owned system skills are admitted only
// for decoded self effects with an implemented runtime consumer.
func (s RefSkill) SupportsItemUseBuff() bool {
	if s.BuffDuration <= 0 {
		return false
	}
	hasRuntimeEffect := s.BuffModifiers != (BuffModifiers{}) ||
		s.RecoveryHP > 0 || s.RecoveryMP > 0 ||
		s.DetectionLevel > 0
	if !hasRuntimeEffect {
		return false
	}
	if s.ExecutionKind() == SkillExecutionBuff {
		return true
	}
	if s.ExecutionKind() != SkillExecutionSystem || s.SystemOwner() != SkillSystemMall {
		return false
	}
	if s.Params[0] != 3 || s.Params[1] != skillParameterSystemBuff ||
		s.Params[2] != skillParameterDuration || s.Params[3] != s.BuffDuration {
		return false
	}
	switch s.Params[4] {
	case skillParameterHP:
		return s.Params[5] > 0 || s.Params[6] > 0
	case skillParameterMP:
		return s.Params[5] > 0 || s.Params[6] > 0
	case skillParameterStrength:
		return s.Params[5] > 0
	case skillParameterIntellect:
		return s.Params[5] > 0
	case skillParameterDamage, skillParameterDamageAbsorption:
		return s.Params[6] > 0
	case skillParameterHit, skillParameterParry:
		return s.Params[5] > 0 || s.Params[6] > 0
	case skillParameterRecovery:
		return s.RecoveryHP > 0 || s.RecoveryMP > 0
	case skillParameterDetection:
		return s.DetectionLevel > 0
	default:
		return false
	}
}

func (s RefSkill) SystemOwner() SkillSystemOwner {
	switch {
	case strings.Contains(s.CodeName, "_NASRUN_"):
		return SkillSystemNone
	case strings.HasPrefix(s.CodeName, "SKILL_MALL_"):
		return SkillSystemMall
	case strings.HasPrefix(s.CodeName, "SKILL_TC_"):
		return SkillSystemTitle
	case strings.HasPrefix(s.CodeName, "SKILL_FORT_"):
		return SkillSystemFortress
	case strings.HasPrefix(s.CodeName, "SKILL_ITEM_"):
		return SkillSystemItem
	case strings.HasPrefix(s.CodeName, "SKILL_EVENT_"):
		return SkillSystemEvent
	default:
		return SkillSystemNone
	}
}

func (s *RefSkill) parseParameters() {
	s.AttackType, s.AttackPercent, s.AttackMin, s.AttackMax = 0, 0, 0, 0
	s.BuffDuration, s.HealHP, s.HealHPPercent, s.HealMP, s.HealMPPercent, s.ResurrectLevel = 0, 0, 0, 0, 0, 0
	s.ShockPower, s.ShockChance, s.ShockDuration, s.TransferRange, s.TransferCount = 0, 0, 0, 0, 0
	s.TransferType, s.TransferFalloffPercent, s.TransferTargetMask = 0, 0, 0
	s.ImpactDamage, s.BarrierDrainMS, s.BarrierMPCost, s.BarrierHitPoints, s.BarrierAbsorption = 0, 0, 0, 0, 0
	s.UpkeepIntervalMS, s.UpkeepMPCost = 0, 0
	s.MovementDistance, s.MovementDurationMS, s.RecoveryHP, s.RecoveryMP = 0, 0, 0, 0
	s.RecoveryStatusMask, s.RecoveryStatusValue, s.RecoveryStatusGrade = 0, 0, 0
	s.AggroSuppression, s.BarrierPhysical = false, false
	s.StatusEffects = nil
	s.BloodTrap = BloodTrapDescriptor{}
	s.RequiredItems = nil
	s.CureStatusMask, s.CureStatusMaxLevel = 0, 0
	s.CureCurse = false
	s.CureCurseChance, s.CureCurseMaxLevel, s.CureCount = 0, 0, 0
	s.AggroReduce, s.AggroAbsorbPercent, s.AggroIncrease, s.AggroIncreasePercent = 0, 0, 0, 0
	s.AbsoluteDamage, s.DamageToHPPercent, s.DamageToMPPercent, s.TransformMaxLevel = 0, 0, 0, 0
	s.TransformItemRefID = 0
	s.SecondaryOverlap, s.DamageTypeFamily, s.DamageTypePrecedence = 0, 0, 0
	s.PassiveSkillRangeBonus = 0
	s.PassiveHealingPercent = 0
	s.PassiveBlessingPercent = 0
	s.PassiveStatusDurationMask, s.PassiveStatusDurationPercent = 0, 0
	s.PassiveCriticalDefense = 0
	s.LinkedSameGroup = false
	s.ReferenceRadius, s.MatchMode = 0, 0
	s.ReferenceDescriptors = [4][6]int{}
	s.KnockOverKey, s.KnockOverChance, s.KnockbackChance, s.KnockbackDistance = 0, 0, 0, 0
	s.LinkedDamageMask, s.LinkedDamagePercent, s.LinkedDamageUseLimit = 0, 0, 0
	s.ReturnDamage = ReturnDamageDescriptor{}
	s.DetectionType, s.DetectionLevel = 0, 0
	s.HideType, s.HideLevel, s.HideChance = 0, 0, 0
	s.BuffModifiers = BuffModifiers{}
	for index, parameter := range s.Params {
		switch parameter {
		case skillParameterAttack:
			if index+5 < len(s.Params) {
				s.AttackType = s.Params[index+1]
				s.AttackPercent = s.Params[index+2]
				s.AttackMin = s.Params[index+3]
				s.AttackMax = s.Params[index+4]
			}
		case skillParameterDuration:
			if index+1 < len(s.Params) {
				s.BuffDuration = s.Params[index+1]
			}
		case skillParameterHeal:
			if index+4 < len(s.Params) {
				s.HealHP, s.HealHPPercent = s.Params[index+1], s.Params[index+2]
				s.HealMP, s.HealMPPercent = s.Params[index+3], s.Params[index+4]
			}
		case skillParameterResurrection:
			if index+2 < len(s.Params) {
				s.ResurrectLevel = s.Params[index+1]
			}
		case skillParameterHP:
			if index+2 < len(s.Params) {
				s.BuffModifiers.HP, s.BuffModifiers.HPPercent = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterMP:
			if index+2 < len(s.Params) {
				s.BuffModifiers.MP, s.BuffModifiers.MPPercent = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterMaxHPReduction:
			if index+4 < len(s.Params) {
				// [INFERENCE] Catalog usage and the authored Life
				// Control/Turnover descriptions identify `pmhp` as a
				// max-HP sacrifice. The native consumer confirms mode 2
				// subtracts payload word 3 through its HP/MP stat channel.
				s.BuffModifiers.HPPercent = -s.Params[index+3]
			}
		case skillParameterStrength:
			if index+2 < len(s.Params) {
				s.BuffModifiers.Strength = s.Params[index+1]
			}
		case skillParameterIntellect:
			if index+2 < len(s.Params) {
				s.BuffModifiers.Intellect = s.Params[index+1]
			}
		case skillParameterHit:
			if index+2 < len(s.Params) {
				s.BuffModifiers.HitRate, s.BuffModifiers.HitRatePercent = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterParry:
			if index+2 < len(s.Params) {
				s.BuffModifiers.ParryRate, s.BuffModifiers.ParryRatePercent = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterDamage:
			if index+2 < len(s.Params) {
				// "urd" stores a reserved value followed by the actual
				// physical/magical attack percentage.
				s.BuffModifiers.DamagePercent = s.Params[index+2]
			}
		case skillParameterAttackPower:
			if index+2 < len(s.Params) {
				// [INFERENCE] Native storage preserves both words. Catalog
				// families corroborate word 1 as Rogue flat physical attack
				// and word 2 as Wizard flat magical attack; the consumer's
				// individual physical/magical channel names remain unrecovered.
				s.BuffModifiers.PhysicalAttack = s.Params[index+1]
				s.BuffModifiers.MagicalAttack = s.Params[index+2]
			}
		case skillParameterCriticalRate:
			if index+1 < len(s.Params) {
				s.BuffModifiers.CriticalRate = s.Params[index+1]
			}
		case skillParameterMovementSpeed:
			if index+1 < len(s.Params) {
				s.BuffModifiers.MovementSpeedPercent = s.Params[index+1]
			}
		case skillParameterRangePassive:
			if index+2 < len(s.Params) {
				switch s.Params[index+1] {
				case 1464422997: // `URIW`, Wizard Magic Bound.
					s.PassiveSkillRangeBonus = s.Params[index+2]
				case 1212961365: // `URLH`, Cleric Praise Heal.
					s.PassiveHealingPercent = s.Params[index+2]
				case 1212958291: // `SFLH`, Cleric Praise Ability.
					s.PassiveBlessingPercent = s.Params[index+2]
				}
			}
		case skillParameterCriticalDefense:
			if index+1 < len(s.Params) {
				s.PassiveCriticalDefense = s.Params[index+1]
			}
		case skillParameterDamageAbsorption:
			if index+2 < len(s.Params) {
				mask, amount := s.Params[index+1], s.Params[index+2]
				if mask == 15 {
					s.BuffModifiers.DamageAbsorptionPercent = amount
				} else {
					if mask&4 != 0 {
						s.BuffModifiers.PhysicalDamageAbsorptionPercent = amount
					}
					if mask&8 != 0 {
						s.BuffModifiers.MagicalDamageAbsorptionPercent = amount
					}
				}
			}
		case skillParameterGoldDropAmount:
			if index+1 < len(s.Params) {
				s.BuffModifiers.GoldDropAmountPercent = s.Params[index+1]
			}
		case skillParameterDefense:
			if index+3 < len(s.Params) {
				s.BuffModifiers.PhysicalDefense = s.Params[index+1]
				s.BuffModifiers.MagicalDefense = s.Params[index+2]
			}
		case skillParameterBlock:
			if index+2 < len(s.Params) {
				s.BuffModifiers.BlockRate = s.Params[index+2]
			}
		case skillParameterShock:
			if index+3 < len(s.Params) {
				s.ShockPower = s.Params[index+3]
				s.ShockChance = s.Params[index+2]
				s.ShockDuration = s.Params[index+1] * 100
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusShock, Power: s.ShockPower, Level: s.abnormalEffectLevel(), Payload: s.ShockPower, Chance: s.ShockChance, DurationMS: s.ShockDuration})
			}
		case skillParameterBurn:
			if index+3 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusBurn, Power: s.Params[index+1], Level: s.abnormalEffectLevel(), Payload: s.Params[index+3], Chance: s.Params[index+2], DurationMS: s.Params[index+1] * 100})
			}
		case skillParameterFreeze:
			if index+2 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusFreeze, Power: s.Params[index+1], Level: s.abnormalEffectLevel(), Chance: s.Params[index+2], DurationMS: s.Params[index+1] * 100})
			}
		case skillParameterFrostbite:
			if index+2 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusFrostbite, Power: s.Params[index+1], Level: s.abnormalEffectLevel(), Chance: s.Params[index+2], DurationMS: s.Params[index+1] * 100})
			}
		case skillParameterPoison:
			if index+3 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusPoison, Power: s.Params[index+1], Level: s.abnormalEffectLevel(), Payload: s.Params[index+3], Chance: s.Params[index+2], DurationMS: s.Params[index+1] * 100})
			}
		case skillParameterZombie:
			if index+2 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusZombie, Power: s.Params[index+1], Level: s.abnormalEffectLevel(), Chance: s.Params[index+2], DurationMS: s.Params[index+1] * 100})
			}
		case skillParameterConfusion, skillParameterFear, skillParameterBind,
			skillParameterSleep, skillParameterDull:
			if index+3 < len(s.Params) {
				kind := curseStatusKind(parameter)
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: kind, DurationMS: s.Params[index+1], Chance: s.Params[index+2], Level: s.Params[index+3]})
			}
		case skillParameterDarkness, skillParameterDisease, skillParameterShortSight:
			if index+4 < len(s.Params) {
				kind := curseStatusKind(parameter)
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: kind, DurationMS: s.Params[index+1], Chance: s.Params[index+2], Level: s.Params[index+3], Power: s.Params[index+4]})
			}
		case skillParameterBleed:
			if index+5 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusBleed, DurationMS: s.Params[index+1], Level: s.Params[index+2], Payload: s.Params[index+3], Power: s.Params[index+4], Chance: s.Params[index+5]})
			}
		case skillParameterPanic, skillParameterCombustion:
			if index+3 < len(s.Params) {
				kind := CombatStatusPanic
				if parameter == skillParameterCombustion {
					kind = CombatStatusCombustion
				}
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: kind, DurationMS: s.Params[index+1], Chance: s.Params[index+2], Level: s.Params[index+3]})
			}
		case skillParameterHidden:
			// `bt` word 0 is a byte-mask. BLOODA's 0x050A0F00 happens to
			// equal the four-byte `hidden` tag and must remain an operand.
			if index > 0 && s.Params[index-1] == skillParameterBloodTrap {
				continue
			}
			if index+3 < len(s.Params) {
				duration := s.BuffDuration
				if duration <= 0 {
					duration = s.ActionDuration
				}
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusHidden, Chance: s.Params[index+1], Level: s.Params[index+2], Payload: s.Params[index+3], DurationMS: duration})
			}
		case skillParameterTransfer:
			var descriptor [6]int
			for payload := range descriptor {
				if index+1+payload < len(s.Params) {
					descriptor[payload] = s.Params[index+1+payload]
				}
			}
			if descriptor[0] >= 1 && descriptor[0] <= 3 {
				s.ReferenceDescriptors[descriptor[0]] = descriptor
			}
			if descriptor[0] == 1 {
				s.TransferType = descriptor[1]
				s.TransferRange, s.TransferCount = descriptor[2], descriptor[3]
				s.TransferFalloffPercent = descriptor[4]
				if descriptor[5] > 0 {
					s.TransferTargetMask = uint32(descriptor[5])
				}
			}
			if descriptor[0] == 2 {
				s.ReferenceRadius = descriptor[2]
			}
		case skillParameterImpact:
			if index+1 < len(s.Params) {
				s.ImpactDamage = s.Params[index+1]
			}
		case skillParameterBarrier:
			if index+2 < len(s.Params) {
				s.UpkeepIntervalMS = s.Params[index+1]
				s.UpkeepMPCost = s.Params[index+2]
			}
			if index+7 < len(s.Params) && s.Params[index+3] == 28791 {
				s.BarrierDrainMS = s.UpkeepIntervalMS
				s.BarrierMPCost = s.UpkeepMPCost
				s.BarrierPhysical = true
				s.BarrierHitPoints = s.Params[index+5]
				s.BarrierAbsorption = s.Params[index+7]
			}
		case skillParameterTeleport, skillParameterTeleportV1:
			if index+2 < len(s.Params) {
				s.MovementDurationMS = s.Params[index+1]
				s.MovementDistance = s.Params[index+2]
			}
		case skillParameterParryDown:
			if index+1 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusParryDown, Power: s.Params[index+1], Chance: 100, DurationMS: s.BuffDuration})
			}
		case skillParameterHitDown:
			if index+1 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusHitDown, Power: s.Params[index+1], Chance: 100, DurationMS: s.BuffDuration})
			}
		case skillParameterPhyDefDown, skillParameterMagDefDown, skillParameterAttackDown, skillParameterDamageTaken:
			if index+4 < len(s.Params) {
				kind := CombatStatusPhysicalDefenseDown
				switch parameter {
				case skillParameterMagDefDown:
					kind = CombatStatusMagicalDefenseDown
				case skillParameterAttackDown:
					kind = CombatStatusAttackDown
				case skillParameterDamageTaken:
					kind = CombatStatusDamageTakenUp
				}
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: kind, Power: s.Params[index+4], Level: s.Params[index+3], Chance: s.Params[index+2], DurationMS: s.Params[index+1]})
			}
		case skillParameterStun:
			if index+3 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusStun, Power: 1, Level: s.Params[index+3], Chance: s.Params[index+2], DurationMS: s.Params[index+1]})
			}
		case skillParameterAbnormalDurationResistance:
			if index+2 < len(s.Params) {
				s.PassiveStatusDurationMask = uint32(s.Params[index+1])
				s.PassiveStatusDurationPercent = s.Params[index+2]
			}
		case skillParameterAggroBlock:
			s.AggroSuppression = true
		case skillParameterRecovery:
			if index+2 < len(s.Params) {
				s.RecoveryHP, s.RecoveryMP = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterRecoveryStatus:
			if index+3 < len(s.Params) {
				s.RecoveryStatusMask = uint32(s.Params[index+1])
				s.RecoveryStatusValue = s.Params[index+2]
				s.RecoveryStatusGrade = s.Params[index+3]
			}
		case skillParameterRequiredItem:
			if index+2 < len(s.Params) && s.Params[index+1] > 0 && s.Params[index+2] > 0 {
				s.RequiredItems = append(s.RequiredItems, SkillRequiredItem{TypeID3: s.Params[index+1], TypeID4: s.Params[index+2]})
			}
		case skillParameterCureStatus:
			if index+2 < len(s.Params) {
				s.CureStatusMask = uint32(s.Params[index+1])
				s.CureStatusMaxLevel = s.Params[index+2]
			}
		case skillParameterCureLevel:
			if index+3 < len(s.Params) && s.Params[index+1] == skillParameterCureCurse {
				s.CureCurse = true
				s.CureCurseChance = s.Params[index+2]
				s.CureCurseMaxLevel = s.Params[index+3]
			}
		case skillParameterCureCount:
			if index+1 < len(s.Params) {
				s.CureCount = s.Params[index+1]
			}
		case skillParameterAggroReduce:
			if index+1 < len(s.Params) {
				s.AggroReduce = s.Params[index+1]
			}
		case skillParameterAggroAbsorb:
			if index+1 < len(s.Params) {
				s.AggroAbsorbPercent = s.Params[index+1]
			}
		case skillParameterAggroIncrease:
			if index+1 < len(s.Params) {
				s.AggroIncrease = s.Params[index+1]
			}
			if index+2 < len(s.Params) {
				s.AggroIncreasePercent = s.Params[index+2]
			}
		case skillParameterAbsoluteDamage:
			if index+1 < len(s.Params) {
				s.AbsoluteDamage = s.Params[index+1]
			}
		case skillParameterReturnDamage:
			if index+4 < len(s.Params) {
				s.ReturnDamage = ReturnDamageDescriptor{
					ChancePercent:   s.Params[index+1],
					PhysicalPercent: s.Params[index+2],
					MagicalPercent:  s.Params[index+3],
					MaximumRange:    s.Params[index+4],
				}
			}
		case skillParameterBloodTrap:
			if index+4 < len(s.Params) {
				s.BloodTrap = BloodTrapDescriptor{
					DurationChoiceMask: uint32(s.Params[index+1]),
					ChancePercent:      s.Params[index+2],
					Level:              s.Params[index+3],
					EffectValue:        s.Params[index+4],
				}
				if s.BloodTrap.DurationChoiceMask != 0 && s.BloodTrap.ChancePercent > 0 && s.BloodTrap.EffectValue > 0 {
					s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{
						Kind:               CombatStatusBloodTrap,
						Level:              s.BloodTrap.Level,
						Payload:            s.BloodTrap.EffectValue,
						Chance:             s.BloodTrap.ChancePercent,
						DurationChoiceMask: s.BloodTrap.DurationChoiceMask,
						NativeSubtype4:     true,
					})
				}
			}
		case skillParameterDamageToMP:
			if index+2 < len(s.Params) {
				s.DamageToHPPercent = s.Params[index+1]
				s.DamageToMPPercent = s.Params[index+2]
			}
		case skillParameterTransformMask:
			if index+2 < len(s.Params) {
				s.TransformMaxLevel = s.Params[index+1]
				s.TransformItemRefID = uint32(s.Params[index+2])
			}
		case skillParameterOverlap2:
			if index+1 < len(s.Params) {
				s.SecondaryOverlap = s.Params[index+1]
			}
		case skillParameterDamageType:
			if index+2 < len(s.Params) {
				s.DamageTypeFamily, s.DamageTypePrecedence = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterLinkedSame:
			s.LinkedSameGroup = true
		case skillParameterMatch:
			if index+1 < len(s.Params) {
				s.MatchMode = s.Params[index+1]
			}
		case skillParameterKnockOver:
			if index+1 < len(s.Params) {
				s.KnockOverKey = s.Params[index+1]
			}
			if index+2 < len(s.Params) {
				s.KnockOverChance = s.Params[index+2]
			}
		case skillParameterKnockback:
			if index+2 < len(s.Params) {
				// Native kb descriptors store probability before distance.
				s.KnockbackChance = s.Params[index+1]
				s.KnockbackDistance = s.Params[index+2]
			}
		case skillParameterLinkedDamage:
			if index+1 < len(s.Params) {
				s.LinkedDamageMask = normalizeLinkedDamageMask(uint32(s.Params[index+1]))
			}
			if index+2 < len(s.Params) {
				s.LinkedDamagePercent = s.Params[index+2]
			}
			if index+3 < len(s.Params) {
				s.LinkedDamageUseLimit = s.Params[index+3]
			}
		case skillParameterDetection:
			if index+2 < len(s.Params) {
				s.DetectionType, s.DetectionLevel = s.Params[index+1], s.Params[index+2]
			}
		case skillParameterHide:
			if index+3 < len(s.Params) {
				s.HideType, s.HideLevel, s.HideChance = s.Params[index+1], s.Params[index+2], s.Params[index+3]
			}
		}
	}
}

func normalizeLinkedDamageMask(mask uint32) uint32 {
	switch mask {
	case 4, 8, 12:
		return mask | 1 | 2
	case 1, 2, 3:
		return mask | 4 | 8
	default:
		return mask
	}
}

func (s RefSkill) LinkedDamageAmount(damage int, magical bool) int {
	if damage <= 0 || s.LinkedDamagePercent <= 0 {
		return 0
	}
	classMask := uint32(8)
	if magical {
		classMask = 4
	}
	if s.LinkedDamageMask&classMask == 0 {
		return 0
	}
	if s.LinkedDamagePercent == 100 {
		return damage
	}
	return min(damage, int(math.Round(float64(damage)*float64(s.LinkedDamagePercent)/100)))
}

// RestorationAmounts decodes the native hste tuple as fixed HP, HP percent,
// fixed MP, and MP percent. Resurrection rows use the same tuple, including
// Chinese percentage-only and European fixed-value variants.
func (s RefSkill) RestorationAmounts(maxHP, maxMP int) (hp, mp int) {
	if maxHP <= 0 || maxMP <= 0 {
		return 0, 0
	}
	hp = s.HealHP + maxHP*s.HealHPPercent/100
	mp = s.HealMP + maxMP*s.HealMPPercent/100
	return min(max(hp, 0), maxHP), min(max(mp, 0), maxMP)
}

func (s RefSkill) abnormalEffectLevel() int {
	return int(max(s.MasteryLevel1, s.MasteryLevel2))
}

func curseStatusKind(parameter int) CombatStatusKind {
	switch parameter {
	case skillParameterConfusion:
		return CombatStatusConfusion
	case skillParameterDarkness:
		return CombatStatusDarkness
	case skillParameterDisease:
		return CombatStatusDisease
	case skillParameterFear:
		return CombatStatusFear
	case skillParameterShortSight:
		return CombatStatusShortSight
	case skillParameterBind:
		return CombatStatusBind
	case skillParameterSleep:
		return CombatStatusSleep
	case skillParameterDull:
		return CombatStatusDull
	default:
		return 0
	}
}

func (s RefSkill) SameLine(other RefSkill) bool {
	if s.ID == 0 || other.ID == 0 {
		return false
	}
	if s.ID == other.ID {
		return true
	}
	if s.BasicOriginal != 0 && other.BasicOriginal != 0 {
		return s.BasicOriginal == other.BasicOriginal
	}
	return s.GroupID == other.GroupID && s.BasicGroup != "" && s.BasicGroup == other.BasicGroup
}
