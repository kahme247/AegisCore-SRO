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
