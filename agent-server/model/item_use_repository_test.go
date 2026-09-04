package model

import (
	"database/sql"
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

func TestRecoveryValuesAppliesReferenceFlatAndPercentValuesWithCaps(t *testing.T) {
	ref := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 3},
	}}
	ref.Param1, ref.Param2, ref.Param3, ref.Param4 = 120, 25, 50, 25
	hp, mp, changed := recoveryValues(ref, ItemUseVitals{HP: 10, MP: 20, MaxHP: 200, MaxMP: 200})
	if !changed || hp != 180 || mp != 120 {
		t.Fatalf("recoveryValues() = (%d, %d, %v), want (180, 120, true)", hp, mp, changed)
	}
	if hp, mp, changed = recoveryValues(ref, ItemUseVitals{HP: 200, MP: 200, MaxHP: 200, MaxMP: 200}); changed || hp != 200 || mp != 200 {
		t.Fatalf("full recoveryValues() = (%d, %d, %v), want no effect", hp, mp, changed)
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
	if IsReturnScroll(specialDestination) {
		t.Fatal("special-destination scroll was treated as a bound-city scroll")
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
