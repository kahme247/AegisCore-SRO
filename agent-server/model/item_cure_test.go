package model

import (
	"database/sql/driver"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestItemCureFromReferenceDecodesStockFamilies(t *testing.T) {
	random := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 1},
	}, Param1: 25_145_280, Param2: 9, Param3: 100}
	effect, ok := ItemCureFromReference(random)
	if !ok || effect.Kind != ItemCureRandomStatus || effect.StatusMask != 0x017FAFC0 ||
		effect.StatusGrade != 9 || effect.Chance != 100 || effect.Count != 1 {
		t.Fatalf("random cure = %+v/%t", effect, ok)
	}

	all := RefItem{RefObject: RefObject{
		IsUseable: true,
		TypeInfo:  TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 6},
	}, Param1: 36, Param2: 37, Param3: 38, Param4: 39, Param5: 40, Param6: 41}
	effect, ok = ItemCureFromReference(all)
	if !ok || effect.Kind != ItemCureShortenElemental || effect.ElementalPower != ([6]uint16{36, 37, 38, 39, 40, 41}) {
		t.Fatalf("all cure = %+v/%t", effect, ok)
	}

	for _, invalid := range []RefItem{
		{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 7}}, Param1: 36},
		{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 8}}, Param1: 36},
		{RefObject: random.RefObject, Param1: random.Param1, Param2: random.Param2},
		{RefObject: all.RefObject, Param1: all.Param1, Param2: all.Param2, Param3: all.Param3, Param4: all.Param4, Param5: all.Param5},
	} {
		if effect, ok = ItemCureFromReference(invalid); ok {
			t.Fatalf("invalid reference decoded as %+v", effect)
		}
	}
}

func TestStockDirectCureCatalogShapesDecode(t *testing.T) {
	rows := []struct {
		id      uint32
		typeID4 int
		value   int
	}{
		{id: 55, typeID4: 6, value: 36},
		{id: 56, typeID4: 6, value: 68},
		{id: 57, typeID4: 6, value: 108},
		{id: 58, typeID4: 6, value: 172},
		{id: 59, typeID4: 6, value: 228},
		{id: 10374, typeID4: 1, value: 3},
		{id: 10375, typeID4: 1, value: 6},
		{id: 10376, typeID4: 1, value: 9},
		{id: 10377, typeID4: 1, value: 12},
		{id: 10537, typeID4: 1, value: 9},
	}
	for _, row := range rows {
		ref := RefItem{RefObject: RefObject{
			ID: row.id, IsUseable: true,
			TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: row.typeID4},
		}}
		if row.typeID4 == 1 {
			ref.Param1, ref.Param2, ref.Param3 = 25_145_280, row.value, 100
		} else {
			ref.Param1, ref.Param2, ref.Param3 = row.value, row.value, row.value
			ref.Param4, ref.Param5, ref.Param6 = row.value, row.value, row.value
		}
		if effect, ok := ItemCureFromReference(ref); !ok || effect.Kind == 0 {
			t.Fatalf("catalog cure %d did not decode: %+v/%t", row.id, effect, ok)
		}
	}
}

func TestSQLItemUseRepositoryConsumesCureWithoutStatusPreflight(t *testing.T) {
	previousRefs := RefItems
	t.Cleanup(func() { RefItems = previousRefs })
	ref := RefItem{RefObject: RefObject{
		ID: 10376, IsUseable: true,
		TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 1},
	}, Param1: 25_145_280, Param2: 9, Param3: 100}
	RefItems = map[uint32]RefItem{ref.ID: ref}

	state, database := newCharacterDeletionTestDatabase(t,
		deletionQuery("SELECT `id`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z`", []driver.Value{int64(7), int64(25000), float64(1), float64(2), float64(3)}),
		deletionQuery("SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`", []driver.Value{
			int64(500), int64(ref.ID), int64(2), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
		}),
		deletionExec("UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1", 1),
		deletionQuery("FROM `SRO_SHARD`.`INVENTORY` inv"),
	)
	repository, err := NewItemUseRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.UseItem(t.Context(), 7, 14, InventoryItemType(ref), ItemUseVitals{
		HP: 600, MP: 250, MaxHP: 600, MaxMP: 250, Level: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemCure == nil || result.ItemCure.Kind != ItemCureRandomStatus || result.NewAmount != 1 {
		t.Fatalf("committed cure result = %+v", result)
	}
	state.assert(t, 1, 0)
}

func TestPlayerItemCureShortensNativeElementalDurations(t *testing.T) {
	now := time.Unix(1_000, 0)
	player := &Player{LifeState: Alive}
	freeze := CombatStatus{Kind: CombatStatusFreeze, Token: 1, Level: 3, ExpiresAt: now.Add(10 * time.Second)}
	shock := CombatStatus{Kind: CombatStatusShock, Token: 2, Level: 3, ExpiresAt: now.Add(20 * time.Second)}
	darkness := CombatStatus{Kind: CombatStatusDarkness, Token: 3, Level: 3, ExpiresAt: now.Add(time.Minute)}
	for _, status := range []CombatStatus{freeze, shock, darkness} {
		if !player.ApplyCombatStatusAt(status, now) {
			t.Fatalf("apply status %+v", status)
		}
	}

	application := player.ApplyItemCure(ItemCureEffect{
		Kind: ItemCureShortenElemental,
		ElementalPower: [6]uint16{
			36, 36, 36, 36, 36, 36,
		},
	}, now, nil)
	wantFreezeExpiry := freeze.ExpiresAt.Add(-3_492 * time.Millisecond)
	if !application.Changed || application.Removed != 1 || !reflect.DeepEqual(application.RefreshAt, []time.Time{wantFreezeExpiry}) {
		t.Fatalf("application = %+v, want one removal and refresh at %v", application, wantFreezeExpiry)
	}
	if current, active := player.CombatStatus(CombatStatusFreeze, now); !active || !current.ExpiresAt.Equal(wantFreezeExpiry) {
		t.Fatalf("freeze = %+v/%t, want expiry %v", current, active, wantFreezeExpiry)
	}
	if _, active := player.CombatStatus(CombatStatusShock, now); active {
		t.Fatal("27-second shock reduction did not expire a 20-second status")
	}
	if _, active := player.CombatStatus(CombatStatusDarkness, now); !active {
		t.Fatal("high-bit curse was changed by elemental-duration cure")
	}
}

func TestPlayerItemCureRandomizesOneMaskedStatusAndScalesChanceByGrade(t *testing.T) {
	now := time.Unix(2_000, 0)
	player := &Player{LifeState: Alive}
	sleep := CombatStatus{Kind: CombatStatusSleep, Token: 1, Level: 3, ExpiresAt: now.Add(time.Minute)}
	darkness := CombatStatus{Kind: CombatStatusDarkness, Token: 2, Level: 5, ExpiresAt: now.Add(time.Minute)}
	for _, status := range []CombatStatus{sleep, darkness} {
		if !player.ApplyCombatStatusAt(status, now) {
			t.Fatalf("apply status %+v", status)
		}
	}
	effect := ItemCureEffect{
		Kind: ItemCureRandomStatus, StatusMask: CombatStatusBadEffectBit(CombatStatusSleep) | CombatStatusBadEffectBit(CombatStatusDarkness),
		StatusGrade: 3, Chance: 100, Count: 1,
	}

	rolls := []int{0, 51} // Shuffle darkness first, then fail its scaled 50% chance.
	randomIntn := func(limit int) int {
		value := rolls[0]
		rolls = rolls[1:]
		if value < 0 || value >= limit {
			t.Fatalf("scripted random value %d outside [0,%d)", value, limit)
		}
		return value
	}
	if application := player.ApplyItemCure(effect, now, randomIntn); application.Changed || application.Removed != 0 {
		t.Fatalf("failed cure application = %+v", application)
	}
	if _, active := player.CombatStatus(CombatStatusSleep, now); !active {
		t.Fatal("unselected sleep status was removed")
	}
	if _, active := player.CombatStatus(CombatStatusDarkness, now); !active {
		t.Fatal("failed 50% roll removed darkness")
	}

	rolls = []int{0, 50}
	if application := player.ApplyItemCure(effect, now, randomIntn); !application.Changed || application.Removed != 1 {
		t.Fatalf("successful cure application = %+v", application)
	}
	if _, active := player.CombatStatus(CombatStatusSleep, now); !active {
		t.Fatal("count-one cure removed the unselected sleep status")
	}
	if _, active := player.CombatStatus(CombatStatusDarkness, now); active {
		t.Fatal("successful scaled roll did not remove darkness")
	}
}

func TestPlayerItemCureWithoutEligibleStatusHasNoRuntimeChange(t *testing.T) {
	player := &Player{LifeState: Alive}
	application := player.ApplyItemCure(ItemCureEffect{
		Kind: ItemCureRandomStatus, StatusMask: 0x017FAFC0, StatusGrade: 9, Chance: 100, Count: 1,
	}, time.Unix(3_000, 0), func(int) int {
		t.Fatal("random source called without an eligible status")
		return 0
	})
	if application.Changed || application.Removed != 0 || len(application.RefreshAt) != 0 {
		t.Fatalf("no-effect application = %+v", application)
	}
}

func TestCOSItemCureShortensNPCElementalStatuses(t *testing.T) {
	now := time.Unix(4_000, 0)
	npc := &NPC{Mutex: &sync.Mutex{}}
	npc.ConfigureCombat(100, 1)
	npc.SetLifeState(Alive)
	for _, status := range []CombatStatus{
		{Kind: CombatStatusFrostbite, Token: 1, SkillID: 10, ExpiresAt: now.Add(10 * time.Second)},
		{Kind: CombatStatusShock, Token: 2, SkillID: 11, ExpiresAt: now.Add(20 * time.Second)},
		{Kind: CombatStatusDarkness, Token: 3, SkillID: 12, ExpiresAt: now.Add(time.Minute)},
	} {
		if !npc.ApplyCombatStatusAt(status, now) {
			t.Fatalf("apply NPC status %+v", status)
		}
	}
	ref := RefItem{RefObject: RefObject{IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 7}}}
	ref.Param1, ref.Param2, ref.Param3 = 36, 36, 36
	ref.Param4, ref.Param5, ref.Param6 = 36, 36, 36
	effect, ok := COSItemCureFromReference(ref)
	if !ok {
		t.Fatal("COS cure reference did not decode")
	}
	application := npc.ApplyItemCure(effect, now)
	wantFrostbiteExpiry := now.Add(time.Second)
	if !application.Changed || application.Removed != 1 || !reflect.DeepEqual(application.RefreshAt, []time.Time{wantFrostbiteExpiry}) {
		t.Fatalf("COS cure application = %+v", application)
	}
	if status, active := npc.CombatStatus(CombatStatusFrostbite, now); !active || !status.ExpiresAt.Equal(wantFrostbiteExpiry) {
		t.Fatalf("frostbite = %+v/%t", status, active)
	}
	if _, active := npc.CombatStatus(CombatStatusShock, now); active {
		t.Fatal("COS cure did not remove shock")
	}
	if _, active := npc.CombatStatus(CombatStatusDarkness, now); !active {
		t.Fatal("COS cure changed high abnormal state")
	}
}
