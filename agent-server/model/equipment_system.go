package model

import "strings"

const (
	MinEquipmentDegree = 1
	MaxEquipmentDegree = 11
	MaxEquipmentPlus   = 12
	MaxProtectionCount = 6
)

type SealGrade byte

const (
	SealNone SealGrade = iota
	SealStar
	SealMoon
	SealSun
)

type ProtectionKind byte

const (
	ProtectionAstral ProtectionKind = iota + 1
	ProtectionImmortal
	ProtectionSteady
)

func (r RefItem) Degree() int {
	if r.ItemClass < 1 {
		return 0
	}
	degree := (r.ItemClass-1)/3 + 1
	if degree < MinEquipmentDegree || degree > MaxEquipmentDegree {
		return 0
	}
	return degree
}

func (r RefItem) Seal() SealGrade {
	if !r.IsEquipment() || r.Degree() == 0 {
		return SealNone
	}
	switch r.Rarity {
	case 1:
		return SealStar
	case 2:
		return SealMoon
	case 3:
		return SealSun
	default:
		return SealNone
	}
}

func (r RefItem) IsAvatar() bool {
	return r.TypeID1 == TypeID1Item && r.TypeID2 == TypeID2Wearable && r.TypeID3 == 13
}

func (r RefItem) IsDevilSpirit() bool {
	return r.TypeID1 == TypeID1Item && r.TypeID2 == TypeID2Wearable &&
		r.TypeID3 == 14 && r.TypeID4 == 1 && strings.Contains(strings.ToUpper(r.CodeName), "NASRUN")
}

func AvatarSlotFor(ref RefItem) (byte, bool) {
	if ref.IsDevilSpirit() {
		return 4, true
	}
	if !ref.IsAvatar() {
		return 0, false
	}
	switch ref.TypeID4 {
	case 1:
		return 0, true
	case 2:
		return 1, true
	case 3:
		return 2, true
	case 4:
		return 3, true
	default:
		return 0, false
	}
}

type EquipmentProtection struct {
	Astral   byte
	Immortal byte
	Steady   byte
}

func (p EquipmentProtection) Valid() bool {
	return p.Astral <= MaxProtectionCount && p.Immortal <= MaxProtectionCount &&
		p.Steady <= MaxProtectionCount && p.Astral <= p.Immortal
}

func (p *EquipmentProtection) Add(kind ProtectionKind) bool {
	if p == nil {
		return false
	}
	next := *p
	switch kind {
	case ProtectionAstral:
		if next.Astral >= next.Immortal {
			return false
		}
		next.Astral++
	case ProtectionImmortal:
		if next.Immortal >= MaxProtectionCount {
			return false
		}
		next.Immortal++
	case ProtectionSteady:
		if next.Steady >= MaxProtectionCount {
			return false
		}
		next.Steady++
	default:
		return false
	}
	if !next.Valid() {
		return false
	}
	*p = next
	return true
}

func ApplyEnhancementFailure(level byte, protection EquipmentProtection) (byte, EquipmentProtection, bool, bool) {
	destroyed := level >= 6
	durabilityLost := true
	if protection.Immortal > 0 && destroyed {
		protection.Immortal--
		destroyed = false
	}
	if protection.Astral > 0 && level >= 4 {
		protection.Astral--
		level = 4
	} else if !destroyed {
		level = 0
	}
	if protection.Steady > 0 {
		protection.Steady--
		durabilityLost = false
	}
	return level, protection, destroyed, durabilityLost
}

func ApplyAdvancedElixir(level, increase byte) (byte, bool) {
	if increase == 0 || increase > 2 || level > MaxEquipmentPlus || level+increase > MaxEquipmentPlus {
		return level, false
	}
	return level + increase, true
}
