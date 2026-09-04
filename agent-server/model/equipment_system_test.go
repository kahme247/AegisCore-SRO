package model

import "testing"

func equipmentReference(itemClass, rarity int) RefItem {
	return RefItem{RefObject: RefObject{Rarity: rarity, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6}}, ItemClass: itemClass}
}

func TestEquipmentDegreesOneThroughElevenAndRejectsOutsideScope(t *testing.T) {
	for degree := 1; degree <= 11; degree++ {
		for tier := 0; tier < 3; tier++ {
			if got := equipmentReference((degree-1)*3+tier+1, 0).Degree(); got != degree {
				t.Fatalf("degree %d tier %d = %d", degree, tier, got)
			}
		}
	}
	for _, class := range []int{0, 34, 36} {
		if got := equipmentReference(class, 0).Degree(); got != 0 {
			t.Fatalf("unsupported item class %d degree = %d", class, got)
		}
	}
}

func TestEquipmentSealGrades(t *testing.T) {
	for rarity, want := range map[int]SealGrade{0: SealNone, 1: SealStar, 2: SealMoon, 3: SealSun, 4: SealNone} {
		if got := equipmentReference(31, rarity).Seal(); got != want {
			t.Fatalf("rarity %d seal = %d, want %d", rarity, got, want)
		}
	}
}

func TestProtectionRulesAndEnhancementFailure(t *testing.T) {
	protection := EquipmentProtection{}
	if protection.Add(ProtectionAstral) {
		t.Fatal("astral was added without immortal")
	}
	if !protection.Add(ProtectionImmortal) || !protection.Add(ProtectionAstral) || !protection.Add(ProtectionSteady) {
		t.Fatal("valid protections were rejected")
	}
	level, remaining, destroyed, durabilityLost := ApplyEnhancementFailure(8, protection)
	if level != 4 || destroyed || durabilityLost || remaining != (EquipmentProtection{Steady: 1}) {
		t.Fatalf("protected failure = level %d protection %+v destroyed=%v durability=%v", level, remaining, destroyed, durabilityLost)
	}
	level, _, destroyed, durabilityLost = ApplyEnhancementFailure(8, EquipmentProtection{})
	if level != 8 || !destroyed || durabilityLost {
		t.Fatalf("unprotected high-level failure = level %d destroyed=%v durability=%v", level, destroyed, durabilityLost)
	}
}

func TestAdvancedElixirAndMaximumPlus(t *testing.T) {
	if got, ok := ApplyAdvancedElixir(10, 2); !ok || got != 12 {
		t.Fatalf("+10 with B elixir = %d/%v", got, ok)
	}
	if got, ok := ApplyAdvancedElixir(12, 1); ok || got != 12 {
		t.Fatalf("advanced elixir exceeded +12: %d/%v", got, ok)
	}
}

func TestAvatarAndDevilSpiritSlots(t *testing.T) {
	for type4, want := range map[int]byte{1: 0, 2: 1, 3: 2, 4: 3} {
		ref := RefItem{RefObject: RefObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 13, TypeID4: type4}}}
		if got, ok := AvatarSlotFor(ref); !ok || got != want {
			t.Fatalf("avatar type %d slot = %d/%v", type4, got, ok)
		}
	}
	devil := RefItem{RefObject: RefObject{CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1}}}
	if got, ok := AvatarSlotFor(devil); !ok || got != 4 || !devil.IsDevilSpirit() {
		t.Fatalf("Devil Spirit slot = %d/%v", got, ok)
	}
}
