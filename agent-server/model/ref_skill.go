package model

type SkillExecutionKind byte

const (
	SkillExecutionUnsupported SkillExecutionKind = iota
	SkillExecutionPassive
	SkillExecutionPhysicalAttack
	SkillExecutionMagicalAttack
	SkillExecutionBuff
	SkillExecutionHealing
	SkillExecutionResurrection
)

const (
	skillParameterAttack       = 6386804
	skillParameterDuration     = 1685418593
	skillParameterHeal         = 1751474540
	skillParameterResurrection = 1919251317
	skillParameterBlock        = 25202
	skillParameterParry        = 25970
	skillParameterHit          = 26738
	skillParameterDamage       = 6582901
	skillParameterHP           = 6844521
	skillParameterMP           = 7172201
	skillParameterDefense      = 1684366960
	skillParameterIntellect    = 1768846441
	skillParameterStrength     = 1937011305
	skillParameterShock        = 25971
	skillParameterTransfer     = 6645362
	skillParameterBurn         = 25205
	skillParameterFreeze       = 26234
	skillParameterFrostbite    = 26210
)

type SkillStatusEffect struct {
	Kind       CombatStatusKind
	Power      int
	Chance     int
	DurationMS int
}

type BuffModifiers struct {
	HP               int
	HPPercent        int
	MP               int
	MPPercent        int
	Strength         int
	Intellect        int
	HitRate          int
	HitRatePercent   int
	DamagePercent    int
	PhysicalDefense  int
	MagicalDefense   int
	BlockRate        int
	ParryRate        int
	ParryRatePercent int
}

// RefSkill contains the server-side timing and reach values needed to execute
// a reference skill without inventing combat constants. The values come from
// the client-matched REF_SKILL catalog and are expressed in vSRO world units
// and milliseconds.
type RefSkill struct {
	ID                  uint32
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
	ActionRange         int
	AttackType          int
	AttackPercent       int
	AttackMin           int
	AttackMax           int
	BuffDuration        int
	HealHP              int
	HealMP              int
	ResurrectLevel      int
	ShockPower          int
	ShockChance         int
	ShockDuration       int
	TransferRange       int
	TransferCount       int
	StatusEffects       []SkillStatusEffect
	BuffModifiers       BuffModifiers
	Params              [50]int
}

func (s RefSkill) IsPassive() bool { return s.BasicActivity == 0 }

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
	case s.IsWeaponImbue():
		// Chinese weapon imbues carry attack parameters but are no-target,
		// short-lived self effects rather than immediate attacks.
		return SkillExecutionBuff
	case s.IsPhysicalAttack():
		return SkillExecutionPhysicalAttack
	case s.IsMagicalAttack():
		return SkillExecutionMagicalAttack
	case s.HealHP > 0 || s.HealMP > 0:
		return SkillExecutionHealing
	case s.ResurrectLevel > 0:
		return SkillExecutionResurrection
	case s.BuffDuration > 0:
		return SkillExecutionBuff
	default:
		return SkillExecutionUnsupported
	}
}

func (s *RefSkill) parseParameters() {
	s.AttackType, s.AttackPercent, s.AttackMin, s.AttackMax = 0, 0, 0, 0
	s.BuffDuration, s.HealHP, s.HealMP, s.ResurrectLevel = 0, 0, 0, 0
	s.ShockPower, s.ShockChance, s.ShockDuration, s.TransferRange, s.TransferCount = 0, 0, 0, 0, 0
	s.StatusEffects = nil
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
				s.HealHP = s.Params[index+1]
				s.HealMP = s.Params[index+3]
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
			if index+1 < len(s.Params) {
				s.BuffModifiers.DamagePercent = s.Params[index+1]
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
				s.ShockPower = s.Params[index+1]
				s.ShockChance = s.Params[index+2]
				s.ShockDuration = s.Params[index+3] * 100
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusShock, Power: s.ShockPower, Chance: s.ShockChance, DurationMS: s.ShockDuration})
			}
		case skillParameterBurn:
			if index+3 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusBurn, Power: s.Params[index+1], Chance: s.Params[index+2], DurationMS: s.Params[index+3] * 100})
			}
		case skillParameterFreeze:
			if index+2 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusFreeze, Power: s.Params[index+1], Chance: s.Params[index+2], DurationMS: 5000})
			}
		case skillParameterFrostbite:
			if index+2 < len(s.Params) {
				s.StatusEffects = append(s.StatusEffects, SkillStatusEffect{Kind: CombatStatusFrostbite, Power: s.Params[index+1], Chance: s.Params[index+2], DurationMS: 5000})
			}
		case skillParameterTransfer:
			if index+4 < len(s.Params) {
				s.TransferRange = s.Params[index+3]
				s.TransferCount = s.Params[index+4]
			}
		}
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
