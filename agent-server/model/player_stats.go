package model

// DerivedCharacterStats is the authoritative runtime view of the values sent
// in the character stat recalculation packet and used by combat. Persisted
// base attributes are deliberately kept separate from temporary skill/item
// modifiers and equipment, then combined here.
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
	MaxHP        int
	MaxMP        int
}

type equipmentStats struct {
	AttackStats
	DefenseStats
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

		switch slot {
		case SlotPrimaryWeapon:
			if ref.TypeID3 != TypeID3Weapon {
				continue
			}
			variance := WeaponStatsFromVariance(item.Variance)
			result.PhyAttackMin += itemStat(ref.PhyAttackMinLower, ref.PhyAttackMinUpper, ref.PhyAttackIncrease, variance.PhyAttack, item.OptLevel)
			result.PhyAttackMax += itemStat(ref.PhyAttackMaxLower, ref.PhyAttackMaxUpper, ref.PhyAttackIncrease, variance.PhyAttack, item.OptLevel)
			result.MagAttackMin += itemStat(ref.MagAttackMinLower, ref.MagAttackMinUpper, ref.MagAttackIncrease, variance.MagAttack, item.OptLevel)
			result.MagAttackMax += itemStat(ref.MagAttackMaxLower, ref.MagAttackMaxUpper, ref.MagAttackIncrease, variance.MagAttack, item.OptLevel)
			result.HitRate += itemStat(ref.HitRateLower, ref.HitRateUpper, ref.HitRateIncrease, variance.HitRate, item.OptLevel)
			result.CriticalRate += itemStat(ref.CritRateLower, ref.CritRateUpper, 0, variance.CriticalRate, 0)
		case SlotSecondaryWeapon:
			variance := ShieldStatsFromVariance(item.Variance)
			result.PhyDef += itemStat(ref.PhyDefLower, ref.PhyDefUpper, ref.PhyDefIncrease, variance.PhyDefense, item.OptLevel)
			result.MagDef += itemStat(ref.MagDefLower, ref.MagDefUpper, ref.MagDefIncrease, variance.MagDefense, item.OptLevel)
			result.BlockRate += itemStat(ref.BlockingRateLower, ref.BlockingRateUpper, 0, variance.BlockRate, 0)
		case SlotHelmet, SlotChest, SlotShoulder, SlotGlove, SlotPants, SlotBoots:
			variance := ArmorStatsFromVariance(item.Variance)
			result.PhyDef += itemStat(ref.PhyDefLower, ref.PhyDefUpper, ref.PhyDefIncrease, variance.PhyDefense, item.OptLevel)
			result.MagDef += itemStat(ref.MagDefLower, ref.MagDefUpper, ref.MagDefIncrease, variance.MagDefense, item.OptLevel)
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
