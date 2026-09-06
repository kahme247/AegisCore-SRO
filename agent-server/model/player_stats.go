package model

// DerivedCharacterStats is the authoritative runtime view of the values sent
// in the character stat recalculation packet and used by combat. Persisted
// base attributes are deliberately kept separate from temporary skill/item
// modifiers and equipment, then combined here. Absorption values are runtime
// combat fields and are not serialized in the compact stat-update packet.
type DerivedCharacterStats struct {
	Strength     int
	Intellect    int
	PhyAttackMin int
	PhyAttackMax int
	MagAttackMin int
	MagAttackMax int
	PhyDef       int
	MagDef       int
	HitRate      int
	ParryRate    int
	CriticalRate int
	BlockRate    int
	PhyAbsorb    int
	MagAbsorb    int
	MaxHP        int
	MaxMP        int
}

type magicOptionStats struct {
	Strength     int
	Intellect    int
	HP           int
	MP           int
	HitRate      int
	ParryRate    int
	CriticalRate int
}

const (
	magicParamStrength     = uint32(0x00737472)
	magicParamIntellect    = uint32(0x00696e74)
	magicParamHP           = uint32(0x00006870)
	magicParamMP           = uint32(0x00006d70)
	magicParamHitRate      = uint32(0x00006872)
	magicParamEvasionRate  = uint32(0x00006572)
	magicParamCriticalRate = uint32(0x00637269)
)

func equippedMagicOptionStats(inventory, avatar Inventory) magicOptionStats {
	var result magicOptionStats
	apply := func(item Item) {
		for _, option := range item.MagicOptions {
			value := int(option.Value)
			switch option.ParamType {
			case magicParamStrength:
				result.Strength += value
			case magicParamIntellect:
				result.Intellect += value
			case magicParamHP:
				result.HP += value
			case magicParamMP:
				result.MP += value
			case magicParamHitRate:
				result.HitRate += value
			case magicParamEvasionRate:
				result.ParryRate += value
			case magicParamCriticalRate:
				result.CriticalRate += value
			}
		}
	}
	for slot, item := range inventory.Items {
		if slot <= SlotRightRing && slot != SlotExtra {
			apply(item)
		}
	}
	for slot, item := range avatar.Items {
		if slot < AvatarInventorySize {
			apply(item)
		}
	}
	return result
}

type equipmentStats struct {
	AttackStats
	DefenseStats
	PhyAbsorb int
	MagAbsorb int
}

func equippedItemStats(inventory Inventory) equipmentStats {
	var result equipmentStats
	for slot, item := range inventory.Items {
		if slot > SlotRightRing || slot == SlotExtra {
			continue
		}
		ref, exists := RefItems[item.RefObjectID]
		if !exists {
			continue
		}
		if ref.TypeID3 == 5 || ref.TypeID3 == 12 {
			variance := AccessoryStatsFromVariance(item.Variance)
			result.PhyAbsorb += itemStat(ref.PhyAbsRateLower, ref.PhyAbsRateUpper, ref.PhyAbsRateIncrease, variance.PhyAbsorb, item.OptLevel)
			result.MagAbsorb += itemStat(ref.MagAbsRateLower, ref.MagAbsRateUpper, ref.MagAbsRateIncrease, variance.MagAbsorb, item.OptLevel)
			continue
		}

		switch slot {
		case SlotPrimaryWeapon:
			if ref.TypeID3 != TypeID3Weapon {
				continue
			}
			variance := WeaponStatsFromVariance(item.Variance)
			result.PhyAttackMin += itemStat(ref.PhyAttackMinLower, ref.PhyAttackMinUpper, ref.PhyAttackIncrease, variance.PhyAttack, item.OptLevel)
			result.PhyAttackMin += itemStat(ref.PhyAttackReinforceMinLower, ref.PhyAttackReinforceMinUpper, 0, variance.PhyReinforce, 0)
			result.PhyAttackMax += itemStat(ref.PhyAttackMaxLower, ref.PhyAttackMaxUpper, ref.PhyAttackIncrease, variance.PhyAttack, item.OptLevel)
			result.PhyAttackMax += itemStat(ref.PhyAttackReinforceMaxLower, ref.PhyAttackReinforceMaxUpper, 0, variance.PhyReinforce, 0)
			result.MagAttackMin += itemStat(ref.MagAttackMinLower, ref.MagAttackMinUpper, ref.MagAttackIncrease, variance.MagAttack, item.OptLevel)
			result.MagAttackMin += itemStat(ref.MagAttackReinforceMinLower, ref.MagAttackReinforceMinUpper, 0, variance.MagReinforce, 0)
			result.MagAttackMax += itemStat(ref.MagAttackMaxLower, ref.MagAttackMaxUpper, ref.MagAttackIncrease, variance.MagAttack, item.OptLevel)
			result.MagAttackMax += itemStat(ref.MagAttackReinforceMaxLower, ref.MagAttackReinforceMaxUpper, 0, variance.MagReinforce, 0)
			result.HitRate += itemStat(ref.HitRateLower, ref.HitRateUpper, ref.HitRateIncrease, variance.HitRate, item.OptLevel)
			result.CriticalRate += itemStat(ref.CritRateLower, ref.CritRateUpper, 0, variance.CriticalRate, 0)
		case SlotSecondaryWeapon:
			variance := ShieldStatsFromVariance(item.Variance)
			result.PhyDef += itemStat(ref.PhyDefLower, ref.PhyDefUpper, ref.PhyDefIncrease, variance.PhyDefense, item.OptLevel)
			result.PhyDef += itemStat(ref.PhyDefReinforceLower, ref.PhyDefReinforceUpper, 0, variance.PhyReinforce, 0)
			result.MagDef += itemStat(ref.MagDefLower, ref.MagDefUpper, ref.MagDefIncrease, variance.MagDefense, item.OptLevel)
			result.MagDef += itemStat(ref.MagDefReinforceLower, ref.MagDefReinforceUpper, 0, variance.MagReinforce, 0)
			result.BlockRate += itemStat(ref.BlockingRateLower, ref.BlockingRateUpper, 0, variance.BlockRate, 0)
		case SlotHelmet, SlotChest, SlotShoulder, SlotGlove, SlotPants, SlotBoots:
			variance := ArmorStatsFromVariance(item.Variance)
			result.PhyDef += itemStat(ref.PhyDefLower, ref.PhyDefUpper, ref.PhyDefIncrease, variance.PhyDefense, item.OptLevel)
			result.PhyDef += itemStat(ref.PhyDefReinforceLower, ref.PhyDefReinforceUpper, 0, variance.PhyReinforce, 0)
			result.MagDef += itemStat(ref.MagDefLower, ref.MagDefUpper, ref.MagDefIncrease, variance.MagDefense, item.OptLevel)
			result.MagDef += itemStat(ref.MagDefReinforceLower, ref.MagDefReinforceUpper, 0, variance.MagReinforce, 0)
			result.ParryRate += itemStat(ref.EvasionRateLower, ref.EvasionRateUpper, ref.EvasionRateIncrease, variance.ParryRate, item.OptLevel)
		}
	}
	return result
}

// itemStat decodes the stock five-bit item variance and applies the reference
// catalog's per-enhancement increase. Silkroad truncates fractional display
// stats, so conversion to int is intentional.
func itemStat(lower, upper, optIncrease float64, variance int, optLevel byte) int {
	if lower <= 0 || upper < lower {
		return 0
	}
	if variance < 0 {
		variance = 0
	} else if variance > int(VarianceStatMask) {
		variance = int(VarianceStatMask)
	}
	value := lower + (upper-lower)*float64(variance)/float64(VarianceStatMask)
	value += optIncrease * float64(optLevel)
	return int(value)
}
