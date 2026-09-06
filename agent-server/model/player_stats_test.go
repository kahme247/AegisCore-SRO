package model

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/utils"
)

func TestDerivedStatsCombinesEquipmentEnhancementAndRuntimeEffects(t *testing.T) {
	original := RefItems
	RefItems = map[uint32]RefItem{
		1001: {
			RefObject:         RefObject{ID: 1001, TypeInfo: TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: TypeID3Weapon}},
			PhyAttackMinLower: 100, PhyAttackMinUpper: 200,
			PhyAttackMaxLower: 200, PhyAttackMaxUpper: 300,
			PhyAttackIncrease:          10,
			PhyAttackReinforceMinLower: 20, PhyAttackReinforceMinUpper: 20,
			PhyAttackReinforceMaxLower: 30, PhyAttackReinforceMaxUpper: 30,
			MagAttackMinLower: 50, MagAttackMinUpper: 70,
			MagAttackMaxLower: 80, MagAttackMaxUpper: 100,
			MagAttackIncrease:          5,
			MagAttackReinforceMinLower: 10, MagAttackReinforceMinUpper: 10,
			MagAttackReinforceMaxLower: 15, MagAttackReinforceMaxUpper: 15,
			HitRateLower: 20, HitRateUpper: 30, HitRateIncrease: 2,
			CritRateLower: 5, CritRateUpper: 10,
		},
		1002: {
			RefObject:   RefObject{ID: 1002},
			PhyDefLower: 20, PhyDefUpper: 40, PhyDefIncrease: 2,
			MagDefLower: 10, MagDefUpper: 30, MagDefIncrease: 1,
			PhyDefReinforceLower: 5, PhyDefReinforceUpper: 5,
			MagDefReinforceLower: 6, MagDefReinforceUpper: 6,
			EvasionRateLower: 5, EvasionRateUpper: 15, EvasionRateIncrease: 1,
		},
		1003: {
			RefObject:   RefObject{ID: 1003},
			PhyDefLower: 10, PhyDefUpper: 20, PhyDefIncrease: 1,
			MagDefLower: 20, MagDefUpper: 40, MagDefIncrease: 2,
			PhyDefReinforceLower: 4, PhyDefReinforceUpper: 4,
			MagDefReinforceLower: 8, MagDefReinforceUpper: 8,
			BlockingRateLower: 5, BlockingRateUpper: 10,
		},
	}
	t.Cleanup(func() { RefItems = original })

	weaponVariance := WeaponVariance{PhyAttack: 31, MagAttack: 31, HitRate: 31, CriticalRate: 31}.ToVariance()
	armorVariance := ArmorVariance{PhyDefense: 31, MagDefense: 31, ParryRate: 31}.ToVariance()
	shieldVariance := ShieldVariance{PhyDefense: 31, MagDefense: 31, BlockRate: 31}.ToVariance()
	player := &Player{
		Level:             10,
		BaseStats:         BaseStats{Str: 100, Int: 80},
		BaseAttackStats:   AttackStats{PhyAttackMin: 7, PhyAttackMax: 9, MagAttackMin: 3, MagAttackMax: 4, HitRate: 2},
		BaseDefenseStats:  DefenseStats{PhyDef: 3, MagDef: 4, ParryRate: 2},
		BonusStats:        BonusBaseStats{BaseStats: BaseStats{Str: 10, Int: 20, HP: 50, MP: 25}, HpPercent: 10},
		BonusAttackStats:  BonusAttackStats{AttackStats: AttackStats{PhyAttackMin: 5, PhyAttackMax: 6, MagAttackMin: 7, MagAttackMax: 8, HitRate: 4}, PhyAttackPercent: 10, MagAttackPercent: 20, HitRatePercent: 10},
		BonusDefenseStats: BonusDefenseStats{DefenseStats: DefenseStats{PhyDef: 5, MagDef: 6, ParryRate: 4}, PhyDefPercent: 10, MagDefPercent: 20, ParryRatePercent: 10},
		Inventory: NewInventory(map[byte]Item{
			SlotPrimaryWeapon:   {SRObject: SRObject{RefObjectID: 1001}, Variance: weaponVariance, OptLevel: 2},
			SlotHelmet:          {SRObject: SRObject{RefObjectID: 1002}, Variance: armorVariance, OptLevel: 3},
			SlotSecondaryWeapon: {SRObject: SRObject{RefObjectID: 1003}, Variance: shieldVariance, OptLevel: 1},
		}),
	}

	got := player.DerivedStats()
	if got.Strength != 110 || got.Intellect != 100 {
		t.Fatalf("effective attributes = %d/%d, want 110/100", got.Strength, got.Intellect)
	}
	// Physical minimum: (base attribute 49 + intrinsic 7 + weapon 220 + effect 5) * 110%.
	if got.PhyAttackMin != 331 || got.PhyAttackMax != 479 {
		t.Fatalf("physical attack = %d-%d, want 331-479", got.PhyAttackMin, got.PhyAttackMax)
	}
	// Magical minimum: (base attribute 45 + intrinsic 3 + weapon 80 + effect 7) * 120%.
	if got.MagAttackMin != 174 || got.MagAttackMax != 242 {
		t.Fatalf("magical attack = %d-%d, want 174-242", got.MagAttackMin, got.MagAttackMax)
	}
	// Equipment contributes armor plus shield, including both items' enhancement levels.
	if got.PhyDef != 140 || got.MagDef != 166 {
		t.Fatalf("defense = %d/%d, want 140/166", got.PhyDef, got.MagDef)
	}
	if got.HitRate != 56 || got.ParryRate != 38 {
		t.Fatalf("hit/parry = %d/%d, want 56/38", got.HitRate, got.ParryRate)
	}
	if got.MaxHP <= 0 || got.MaxMP <= 0 {
		t.Fatalf("max vitals = %d/%d", got.MaxHP, got.MaxMP)
	}
	if average := player.AverageAttackPower(false); average != (got.PhyAttackMin+got.PhyAttackMax)/2 {
		t.Fatalf("combat physical average = %d", average)
	}
}

func TestEquippedItemStatsIgnoresBackpackEquipment(t *testing.T) {
	original := RefItems
	RefItems = map[uint32]RefItem{1001: {
		RefObject:         RefObject{ID: 1001, TypeInfo: TypeInfo{TypeID3: TypeID3Weapon}},
		PhyAttackMinLower: 100, PhyAttackMinUpper: 100,
		PhyAttackMaxLower: 200, PhyAttackMaxUpper: 200,
	}}
	t.Cleanup(func() { RefItems = original })

	player := &Player{BaseStats: BaseStats{Str: 20, Int: 20}, Inventory: NewInventory(map[byte]Item{
		20: {SRObject: SRObject{RefObjectID: 1001}},
	})}
	got := player.DerivedStats()
	if got.PhyAttackMin != 9 || got.PhyAttackMax != 13 {
		t.Fatalf("backpack item changed attack to %d-%d", got.PhyAttackMin, got.PhyAttackMax)
	}
}

func TestEquippedMagicOptionsAffectDerivedStats(t *testing.T) {
	player := &Player{
		Level:     10,
		BaseStats: BaseStats{Str: 20, Int: 20},
		Inventory: NewInventory(map[byte]Item{
			SlotHelmet: {MagicOptions: []MagicOption{
				{ParamType: magicParamStrength, Value: 2},
				{ParamType: magicParamIntellect, Value: 3},
				{ParamType: magicParamHP, Value: 150},
				{ParamType: magicParamMP, Value: 100},
				{ParamType: magicParamHitRate, Value: 4},
				{ParamType: magicParamEvasionRate, Value: 5},
				{ParamType: magicParamCriticalRate, Value: 1},
			}},
			20: {MagicOptions: []MagicOption{{ParamType: magicParamStrength, Value: 99}}},
		}),
		AvatarInventory: NewInventory(map[byte]Item{
			4: {MagicOptions: []MagicOption{{ParamType: magicParamStrength, Value: 1}}},
		}),
	}

	got := player.DerivedStats()
	if got.Strength != 23 || got.Intellect != 23 {
		t.Fatalf("attributes = %d/%d, want 23/23", got.Strength, got.Intellect)
	}
	if got.HitRate != 15 || got.ParryRate != 16 || got.CriticalRate != 1 {
		t.Fatalf("rates = hit %d parry %d critical %d", got.HitRate, got.ParryRate, got.CriticalRate)
	}
	wantHP := utils.BaseHPOrMP(10, 23) + 150
	wantMP := utils.BaseHPOrMP(10, 23) + 100
	if got.MaxHP != wantHP || got.MaxMP != wantMP {
		t.Fatalf("max vitals = %d/%d, want %d/%d", got.MaxHP, got.MaxMP, wantHP, wantMP)
	}
}

func TestDerivedStatsIncludesEquippedAccessoryAbsorption(t *testing.T) {
	original := RefItems
	RefItems = map[uint32]RefItem{1004: {
		RefObject:       RefObject{ID: 1004, TypeInfo: TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: 5}},
		PhyAbsRateLower: 10, PhyAbsRateUpper: 20,
		MagAbsRateLower: 20, MagAbsRateUpper: 40,
	}}
	t.Cleanup(func() { RefItems = original })
	player := &Player{Inventory: NewInventory(map[byte]Item{
		SlotLeftRing: {SRObject: SRObject{RefObjectID: 1004}, Variance: AccessoryVariance{PhyAbsorb: 31, MagAbsorb: 31}.ToVariance()},
	})}
	got := player.DerivedStats()
	if got.PhyAbsorb != 20 || got.MagAbsorb != 40 {
		t.Fatalf("accessory absorption = %d/%d, want 20/40", got.PhyAbsorb, got.MagAbsorb)
	}
}
