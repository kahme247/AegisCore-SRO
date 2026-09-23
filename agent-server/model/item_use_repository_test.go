package model

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

func TestInventoryItemTypeMatchesVSRO188Formula(t *testing.T) {
	ref := RefItem{RefObject: RefObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 1}}}
	if got := InventoryItemType(ref); got != 0x08EC {
		t.Fatalf("InventoryItemType() = %#04x, want 0x08EC for HP recovery", got)
	}
	ref.IsCashItem = true
	if got := InventoryItemType(ref); got != 0x08ED {
		t.Fatalf("cash InventoryItemType() = %#04x, want 0x08ED", got)
	}
}

func TestRecoveryValuesUsesStockFlatScalingAndIgnoresPercentWhenFlatValuesExist(t *testing.T) {
	ref := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 3},
	}}
	ref.Param1, ref.Param2, ref.Param3, ref.Param4 = 120, 25, 50, 25
	vitals := ItemUseVitals{HP: 10, MP: 20, MaxHP: 200, MaxMP: 200, Level: 10, Strength: 100, Intelligence: 80}
	hp, mp, changed := recoveryValues(ref, vitals)
	if !changed || hp != 187 || mp != 91 {
		t.Fatalf("recoveryValues() = (%d, %d, %v), want stock-scaled (187, 91, true)", hp, mp, changed)
	}
	if hp, mp, changed = recoveryValues(ref, ItemUseVitals{HP: 200, MP: 200, MaxHP: 200, MaxMP: 200, Level: 10, Strength: 100, Intelligence: 80}); changed || hp != 200 || mp != 200 {
		t.Fatalf("full recoveryValues() = (%d, %d, %v), want no effect", hp, mp, changed)
	}
}

func TestRecoveryValuesZombieInvertsPotionHPAndKeepsMPPositive(t *testing.T) {
	ref := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 3},
	}}
	ref.Param1, ref.Param3 = 50, 40
	hp, mp, changed := recoveryValues(ref, ItemUseVitals{
		HP: 60, MP: 20, MaxHP: 200, MaxMP: 200, Level: 1, InvertHPRecovery: true,
	})
	if !changed || hp != 10 || mp != 60 {
		t.Fatalf("zombie potion recovery = (%d, %d, %v), want (10, 60, true)", hp, mp, changed)
	}
	hp, mp, changed = recoveryValues(ref, ItemUseVitals{
		HP: 30, MP: 20, MaxHP: 200, MaxMP: 200, Level: 1, InvertHPRecovery: true,
	})
	if !changed || hp != 0 || mp != 60 {
		t.Fatalf("lethal zombie potion recovery = (%d, %d, %v), want (0, 60, true)", hp, mp, changed)
	}
}

func TestRecoveryValuesUsesMaximumPercentOnlyWithoutFlatRecovery(t *testing.T) {
	ref := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 3}}}
	ref.Param2, ref.Param4 = 25, 10
	hp, mp, changed := recoveryValues(ref, ItemUseVitals{HP: 10, MP: 20, MaxHP: 200, MaxMP: 300, Level: 50, Strength: 300, Intelligence: 300})
	if !changed || hp != 60 || mp != 50 {
		t.Fatalf("percent recoveryValues() = (%d, %d, %v), want (60, 50, true)", hp, mp, changed)
	}
}

func TestRecoveryValuesRejectsNonRecoveryExpendables(t *testing.T) {
	ref := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 1}}}
	if hp, mp, ok := recoveryValues(ref, ItemUseVitals{HP: 50, MP: 50, MaxHP: 200, MaxMP: 200}); ok || hp != 50 || mp != 50 {
		t.Fatalf("scroll recoveryValues() = (%d, %d, %v), want unchanged unsupported item", hp, mp, ok)
	}
}

func TestIsReturnScrollRecognizesBoundCityFamilyOnly(t *testing.T) {
	ordinary := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 1},
	}}
	ordinary.Param1 = 30_000
	ordinary.Param3Desc = " resurrect "
	if !IsReturnScroll(ordinary) {
		t.Fatal("ordinary return-city scroll was not recognized")
	}

	reverse := ordinary
	reverse.TypeID4 = 3
	if IsReturnScroll(reverse) {
		t.Fatal("reverse-return scroll was treated as a bound-city scroll")
	}
	if !IsReverseReturnScroll(reverse) || !IsTeleportScroll(reverse) {
		t.Fatal("reverse-return scroll was not recognized as a teleport scroll")
	}
	specialDestination := ordinary
	specialDestination.Param3Desc = "THIEFDEN"
	specialDestination.Param4Desc = "STORE_TD_GATE"
	if IsReturnScroll(specialDestination) || !IsSpecialReturnScroll(specialDestination) || !IsTeleportScroll(specialDestination) {
		t.Fatal("special-destination scroll classification is incorrect")
	}
	ordinary.Param1 = 0
	if IsReturnScroll(ordinary) {
		t.Fatal("return scroll without a reference-data cast duration was accepted")
	}
}

func TestIsCOSScrollRecognizesLiveGrowthAndAbilityPetItems(t *testing.T) {
	for _, test := range []struct {
		name string
		ref  RefItem
	}{
		{name: "growth flute", ref: RefItem{RefObject: RefObject{CodeName: "ITEM_COS_P_FLUTE_WHITE", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 1}}, Param1Desc: "COS_P_WOLF_WHITE_001"}},
		{name: "ability scroll", ref: RefItem{RefObject: RefObject{CodeName: "ITEM_COS_P_RACCOONDOG_SCROLL", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 2}}, Param1Desc: "COS_P_RACCOONDOG"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !IsCOSScroll(test.ref) {
				t.Fatalf("IsCOSScroll(%s) = false", test.ref.CodeName)
			}
		})
	}
	invalid := RefItem{RefObject: RefObject{CodeName: "ITEM_COS_P_FLUTE_WHITE", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 1}}}
	if IsCOSScroll(invalid) {
		t.Fatal("non-usable COS item was accepted")
	}
}

func TestIsCOSTransportScrollSeparatesTemporaryRideItemsFromPetInstruments(t *testing.T) {
	for _, transport := range []RefItem{
		{RefObject: RefObject{CodeName: "ITEM_COS_T_WHITEELEPHANT_SCROLL", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 2}}, Param1Desc: "COS_T_WHITEELEPHANT"},
		// The live catalog's red horse is a C-family ride, not a T-family job transport.
		{RefObject: RefObject{CodeName: "ITEM_COS_C_HORSE1", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 2}}, Param1Desc: "COS_C_HORSE1"},
	} {
		if !IsCOSTransportScroll(transport) {
			t.Fatalf("stock TypeID 3/3/3/2 ride scroll %s was not recognized", transport.CodeName)
		}
		if IsCOSScroll(transport) {
			t.Fatalf("temporary ride scroll %s was classified as a persistent pet instrument", transport.CodeName)
		}
	}
	transport := RefItem{RefObject: RefObject{CodeName: "ITEM_COS_T_WHITEELEPHANT_SCROLL", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 2}}, Param1Desc: "COS_T_WHITEELEPHANT"}
	eventTransport := transport
	eventTransport.CodeName = "ITEM_EVENT_COS_T_WHITEELEPHANT_SCROLL"
	if !IsCOSTransportScroll(eventTransport) {
		t.Fatal("event ride scroll was not recognized")
	}
	petInstrument := RefItem{RefObject: RefObject{CodeName: "ITEM_COS_P_FLUTE", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1, TypeID4: 1}}}
	if IsCOSTransportScroll(petInstrument) {
		t.Fatal("persistent pet instrument was classified as a temporary ride scroll")
	}
}

func TestCOSResourcePotionClassifiersUseConfirmedTypeIDs(t *testing.T) {
	hp := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 4}}, Param1: 360}
	if !IsCOSHealthPotion(hp) || IsCOSHGPotion(hp) {
		t.Fatal("COS HP potion classification mismatch")
	}
	hgp := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 9}}, Param1: 10}
	if !IsCOSHGPotion(hgp) || IsCOSHealthPotion(hgp) {
		t.Fatal("COS HGP potion classification mismatch")
	}
	if IsCOSHealthPotion(RefItem{RefObject: RefObject{TypeInfo: hp.TypeInfo}}) {
		t.Fatal("non-usable COS HP reference was accepted")
	}
}

func TestCOSPetTimedServiceFieldsResolveCatalogSkillAndCategory(t *testing.T) {
	old := RefSkills
	defer func() { RefSkills = old }()
	RefSkills = map[uint32]RefSkill{7001: {ID: 7001, CodeName: "SKILL_MALL_PET_GROWTH_POTION_01"}}
	ref := RefItem{RefObject: RefObject{ID: 22712, IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 15}}, Param1: 1800}
	category, jobID, data1, ok := COSPetTimedServiceFields(ref, COSGrowthPet)
	if !ok || category != 0 || jobID != 7001 || data1 != 0 {
		t.Fatalf("growth service fields = %d,%d,%d,%v", category, jobID, data1, ok)
	}
	watch := ref
	watch.ID = 22710
	RefSkills[7002] = RefSkill{ID: 7002, CodeName: "SKILL_MALL_PET_WATCH_CHULHYEN_01"}
	category, jobID, data1, ok = COSPetTimedServiceFields(watch, COSAbilityPet)
	if !ok || category != 5 || jobID != 7002 || data1 != 28 {
		t.Fatalf("watch service fields = %d,%d,%d,%v", category, jobID, data1, ok)
	}
	if _, _, _, ok = COSPetTimedServiceFields(watch, COSGrowthPet); ok {
		t.Fatal("watch service accepted a growth pet")
	}
}

func TestIsCOSExtensionUsesConfirmedTypeAndMinuteDuration(t *testing.T) {
	oneDay := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 12}}, Param1: 1440}
	if !IsCOSExtension(oneDay) || time.Duration(oneDay.Param1)*time.Minute != 24*time.Hour {
		t.Fatal("one-day COS extension reference mismatch")
	}
	if IsCOSExtension(RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 15}}}) {
		t.Fatal("pet service item was classified as rental extension")
	}
}

func TestIsCOSRevivalRequiresPetRevivalCatalogName(t *testing.T) {
	ref := RefItem{RefObject: RefObject{CodeName: "ITEM_COS_REVIVAL", IsUseable: true}}
	if !IsCOSRevival(ref) {
		t.Fatal("COS revival item was not recognized")
	}
	character := ref
	character.CodeName = "ITEM_RESURRECTION"
	if IsCOSRevival(character) {
		t.Fatal("character resurrection item was classified as COS revival")
	}
	grassOfLife := RefItem{RefObject: RefObject{CodeName: "ITEM_COS_P_CURE_ALL_01", IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 7}}}
	if IsCOSRevival(grassOfLife) {
		t.Fatal("unresolved Grass of Life cure parameters were guessed as revival")
	}
}

func TestValidateCOSItemRentalRejectsExpiredAbilityPetOnly(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if err := validateCOSItemRental(COSAbilityPet, sql.NullTime{Valid: true, Time: now.Add(-time.Second)}, now); !errors.Is(err, ErrItemUseExpired) {
		t.Fatalf("expired ability rental error = %v, want ErrItemUseExpired", err)
	}
	if err := validateCOSItemRental(COSAbilityPet, sql.NullTime{}, now); !errors.Is(err, ErrItemUseExpired) {
		t.Fatalf("missing ability rental error = %v, want ErrItemUseExpired", err)
	}
	if err := validateCOSItemRental(COSAbilityPet, sql.NullTime{Valid: true, Time: now.Add(time.Second)}, now); err != nil {
		t.Fatalf("future ability rental rejected: %v", err)
	}
	if err := validateCOSItemRental(COSGrowthPet, sql.NullTime{}, now); err != nil {
		t.Fatalf("permanent growth pet rejected: %v", err)
	}
}

func TestMovementSpeedEffectDoesNotStackAndOldExpiryCannotClearReplacement(t *testing.T) {
	player := &Player{WalkSpeed: 15, RunSpeed: 30, MotionState: Running}
	first, replaced := player.ApplyMovementSpeedEffect(3975, 50, time.Now().Add(30*time.Minute), 101)
	if replaced != 0 {
		t.Fatalf("first speed effect replaced token %d", replaced)
	}
	if got := player.GetMovementSpeed(); got != 45 {
		t.Fatalf("50%% movement speed = %f, want 45", got)
	}
	second, replaced := player.ApplyMovementSpeedEffect(3977, 100, time.Now().Add(time.Hour), 202)
	if replaced != 101 {
		t.Fatalf("replacement token = %d, want 101", replaced)
	}
	if _, cleared := player.ClearMovementSpeedEffect(first); cleared {
		t.Fatal("old speed effect expiry cleared its replacement")
	}
	if got := player.GetMovementSpeed(); got != 60 {
		t.Fatalf("replacement movement speed = %f, want 60", got)
	}
	expiredToken, cleared := player.ClearMovementSpeedEffect(second)
	if !cleared || expiredToken != 202 || player.GetMovementSpeed() != 30 {
		t.Fatal("current speed effect did not expire back to base speed")
	}
}

func TestExtendedSpeedEffectExpirationAccumulatesAndCapsAtTwentyFourHours(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	expiresAt, extended := extendedSpeedEffectExpiration(now, time.Time{}, 30*time.Minute)
	if !extended || !expiresAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("first expiration = %v/%v", expiresAt, extended)
	}
	expiresAt, extended = extendedSpeedEffectExpiration(now, expiresAt, 30*time.Minute)
	if !extended || !expiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("second expiration = %v/%v", expiresAt, extended)
	}
	expiresAt, extended = extendedSpeedEffectExpiration(now, now.Add(23*time.Hour+45*time.Minute), 30*time.Minute)
	if !extended || !expiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("capped expiration = %v/%v", expiresAt, extended)
	}
	if _, extended = extendedSpeedEffectExpiration(now, expiresAt, 30*time.Minute); extended {
		t.Fatal("effect extended beyond 24-hour cap")
	}
}

func TestConfiguredSkillPointExpendableMutatesBalanceWithConsumption(t *testing.T) {
	previousRefs := RefItems
	t.Cleanup(func() { RefItems = previousRefs })
	ref := RefItem{RefObject: RefObject{
		ID: 36771, IsUseable: true,
		TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 12},
	}}
	RefItems = map[uint32]RefItem{ref.ID: ref}
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `id`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z`", []driver.Value{int64(7), int64(25000), float64(1), float64(2), float64(3)}),
		deletionQuery("SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`", []driver.Value{
			int64(500), int64(ref.ID), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
		}),
		deletionQuery("FROM `EXPENDABLE_EFFECT_CONFIG`", []driver.Value{"skill_points", int64(1), int64(0), int64(0)}),
		deletionQuery("SELECT `SKILL_POINTS`", []driver.Value{int64(99)}),
		deletionExec("UPDATE `CHAR` SET `SKILL_POINTS`", 1),
		deletionExec("DELETE FROM `INVENTORY`", 1),
		deletionExec("DELETE FROM `ITEM`", 1),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY` inv"),
	)
	repository, err := NewItemUseRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.UseItem(t.Context(), 7, 14, InventoryItemType(ref), ItemUseVitals{HP: 10, MP: 10, MaxHP: 100, MaxMP: 100, Level: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.SkillPoints != 100 || result.NewAmount != 0 {
		t.Fatalf("skill-point result = %+v", result)
	}
	state.assert(t, 1, 0)
}


func TestConfiguredTimedProgressionPersistsReinforcementService(t *testing.T) {
	previousRefs := RefItems
	t.Cleanup(func() { RefItems = previousRefs })
	ref := RefItem{RefObject: RefObject{
		ID: 39365, IsUseable: true,
		TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 11},
	}, Param1: 1800}
	RefItems = map[uint32]RefItem{ref.ID: ref}
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `id`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z`", []driver.Value{int64(7), int64(25000), float64(1), float64(2), float64(3)}),
		deletionQuery("SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`", []driver.Value{
			int64(500), int64(ref.ID), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
		}),
		deletionQuery("FROM `EXPENDABLE_EFFECT_CONFIG`", []driver.Value{"opt_level", int64(2), int64(15), int64(0), int64(0), true}),
		deletionQuery("SELECT r.`TypeID_3`,it.`OPT_LEVEL`", []driver.Value{int64(TypeID3Weapon), int64(4)}),
		deletionQuery("SELECT `ACTIVATED_AT`,`EXPIRES_AT` FROM `CHAR_ITEM_SERVICE`"),
		deletionExec("INSERT INTO `CHAR_ITEM_SERVICE`", 1),
		deletionExec("DELETE FROM `INVENTORY`", 1),
		deletionExec("DELETE FROM `ITEM`", 1),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY` inv"),
	)
	repository, err := NewItemUseRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.UseTargetedItem(t.Context(), 7, 14, InventoryItemType(ref), ItemUseVitals{HP: 10, MP: 10, MaxHP: 100, MaxMP: 100, Level: 1}, COSItemTarget{ObjectID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemService == nil || result.ItemService.Kind != ItemServiceReinforceWeapon || result.ItemService.EffectValue != 2 ||
		!result.ItemService.TargetSlotSet || result.ItemService.TargetSlot != 6 ||
		result.ItemService.TargetBaseOptLevel != 4 || result.ItemService.TargetProjectedOptLevel != 6 {
		t.Fatalf("temporary option-level reinforcement service = %+v", result.ItemService)
	}
	if lifetime := result.ItemService.ExpiresAt.Sub(result.ItemService.ActivatedAt); lifetime != 30*time.Minute {
		t.Fatalf("reinforcement lifetime = %v, want 30m", lifetime)
	}
	state.assert(t, 1, 0)
}

func TestBoothExpendableSelectsStallAvatarWithConsumption(t *testing.T) {
	previousRefs := RefItems
	t.Cleanup(func() { RefItems = previousRefs })
	ref := RefItem{RefObject: RefObject{
		ID: 3847, IsUseable: true,
		TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 4},
	}}
	RefItems = map[uint32]RefItem{ref.ID: ref}
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `id`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z`", []driver.Value{int64(7), int64(25000), float64(1), float64(2), float64(3)}),
		deletionQuery("SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`", []driver.Value{
			int64(500), int64(ref.ID), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
		}),
		deletionExec("DELETE FROM `INVENTORY`", 1),
		deletionExec("DELETE FROM `ITEM`", 1),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY` inv"),
	)
	repository, err := NewItemUseRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.UseItem(t.Context(), 7, 14, InventoryItemType(ref), ItemUseVitals{HP: 10, MP: 10, MaxHP: 100, MaxMP: 100, Level: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.StallAvatarRefID != ref.ID || result.NewAmount != 0 {
		t.Fatalf("booth result = %+v", result)
	}
	state.assert(t, 1, 0)
}

func TestPartyMonsterSummonScrollLoadsLevelBandedCandidatesBeforeConsumption(t *testing.T) {
	previousRefs := RefItems
	t.Cleanup(func() { RefItems = previousRefs })
	ref := RefItem{RefObject: RefObject{
		ID: 9318, CodeName: "ITEM_ETC_E060517_SUMMON_PARTY_SCROLL", IsUseable: true,
		TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 9},
	}}
	RefItems = map[uint32]RefItem{ref.ID: ref}
	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `id`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z`", []driver.Value{int64(7), int64(25000), float64(1), float64(2), float64(3)}),
		deletionQuery("SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`", []driver.Value{
			int64(500), int64(ref.ID), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
		}),
		deletionQuery("FROM `REF_ITEM_SUMMON_MONSTER`",
			[]driver.Value{"MOB_CH_STRONG_TIGER", int64(1), int64(MonsterNormal), int64(100), int64(300)},
			[]driver.Value{"MOB_WC_DEVILBUG", int64(1), int64(MonsterGiant), int64(100), int64(300)},
		),
		deletionExec("DELETE FROM `INVENTORY`", 1),
		deletionExec("DELETE FROM `ITEM`", 1),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY` inv"),
	)
	repository, err := NewItemUseRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.UseItem(t.Context(), 7, 14, InventoryItemType(ref), ItemUseVitals{HP: 10, MP: 10, MaxHP: 100, MaxMP: 100, Level: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.SummonMonster == nil || len(result.SummonMonster.Candidates) != 2 ||
		result.SummonMonster.Candidates[0].CodeName != "MOB_CH_STRONG_TIGER" ||
		result.SummonMonster.Candidates[1].Rarity != MonsterGiant ||
		result.SummonMonster.Radius != 100 || result.SummonMonster.Lifetime != 5*time.Minute {
		t.Fatalf("summon-scroll result = %+v", result.SummonMonster)
	}
	state.assert(t, 1, 0)
}
