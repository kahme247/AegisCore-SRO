package character

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestNASRUNRequiresActiveEquippedDevilSpirit(t *testing.T) {
	original := model.RefItems
	model.RefItems = map[uint32]model.RefItem{24405: {
		RefObject: model.RefObject{ID: 24405, CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1}},
	}}
	t.Cleanup(func() { model.RefItems = original })

	devil := model.Item{SRObject: model.SRObject{RefObjectID: 24405}, Data: 180000, StackSize: 1}
	devil.SetTypeInfo(model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1})
	skill := model.RefSkill{RequiredItems: []model.SkillRequiredItem{{TypeID3: 14, TypeID4: 1}}}
	player := &model.Player{Inventory: model.NewInventory(nil), AvatarInventory: model.NewInventory(map[byte]model.Item{4: devil})}
	if playerHasRequiredSkillItem(player, skill) {
		t.Fatal("inactive Devil Spirit satisfied NASRUN requirement")
	}
	devil.RentInfo.PeriodEndTime = uint32(time.Now().Add(time.Minute).Unix())
	player.AvatarInventory = model.NewInventory(map[byte]model.Item{4: devil})
	if !playerHasRequiredSkillItem(player, skill) {
		t.Fatal("active equipped Devil Spirit did not satisfy NASRUN requirement")
	}
}

func combatEffectNPC(uid uint32, refID uint32, hp int) *model.NPC {
	npc := &model.NPC{SRObject: model.SRObject{
		UniqueID: uid, RefObjectID: refID,
		TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
	}, Mutex: &sync.Mutex{}}
	npc.ConfigureCombat(hp, 1)
	npc.SetLifeState(model.Alive)
	return npc
}

func TestVitalSpotParryDownAndShockChangeAuthoritativeHitChance(t *testing.T) {
	now := time.Now()
	player := &model.Player{BaseStats: model.BaseStats{Str: 20, Int: 20}}
	target := combatEffectNPC(200, 9000, 1000)
	var chances []int
	handler := &CombatHandler{
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
			9000: {RefObject: model.RefObject{ID: 9000}, EvasionRate: 89},
		}}),
		hitRollPercent: func(chance int) bool { chances = append(chances, chance); return true },
	}
	if !handler.playerAttackLands(player, target, now) {
		t.Fatal("baseline hit rejected")
	}
	target.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusParryDown, Token: 1, SkillID: 1609, Power: 80, ExpiresAt: now.Add(time.Minute)})
	if !handler.playerAttackLands(player, target, now) {
		t.Fatal("parry-down hit rejected")
	}
	target.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusShock, Token: 2, SkillID: 3406, Power: 206, ExpiresAt: now.Add(time.Minute)})
	if !handler.playerAttackLands(player, target, now) {
		t.Fatal("shocked hit rejected")
	}
	if len(chances) != 3 || chances[0] != 11 || chances[1] != 55 || chances[2] != 73 {
		t.Fatalf("hit chances = %v, want [11 55 73]", chances)
	}
}

func TestVitalSpotDefenseAndDamageTakenEffectsUseMonsterDefense(t *testing.T) {
	now := time.Now()
	player := &model.Player{}
	handler := &CombatHandler{
		world: &combatWorldFake{player: player}, now: func() time.Time { return now },
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
			9000: {RefObject: model.RefObject{ID: 9000}, PhyDef: 100, MagDef: 120},
		}}),
	}
	plain := combatEffectNPC(201, 9000, 1000)
	if applied, _ := handler.applyPlayerDamage(player, plain, 200); applied != 100 {
		t.Fatalf("plain physical damage = %d, want 100", applied)
	}
	physicalDown := combatEffectNPC(202, 9000, 1000)
	physicalDown.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusPhysicalDefenseDown, Token: 1, SkillID: 8399, Power: 80, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, physicalDown, 200); applied != 180 {
		t.Fatalf("physical-defense-down damage = %d, want 180", applied)
	}
	magicalDown := combatEffectNPC(203, 9000, 1000)
	magicalDown.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusMagicalDefenseDown, Token: 2, SkillID: 8405, Power: 100, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, magicalDown, 200, true); applied != 180 {
		t.Fatalf("magical-defense-down damage = %d, want 180", applied)
	}
	damageUp := combatEffectNPC(204, 9000, 1000)
	damageUp.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusDamageTakenUp, Token: 3, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, damageUp, 200); applied != 135 {
		t.Fatalf("damage-taken-up damage = %d, want 135", applied)
	}
	absolute := combatEffectNPC(205, 9000, 1000)
	absolute.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusDamageTakenUp, Token: 4, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, absolute, 130, true, true); applied != 130 {
		t.Fatalf("absolute damage = %d, want 130", applied)
	}
}

func TestPlayerOverkillReportsCalculatedDamageAndAppliesRemainingHP(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(206, 9000, 50)
	handler := &CombatHandler{
		world:    &combatWorldFake{player: player},
		schedule: func(time.Duration, func()) {},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
			9000: {RefObject: model.RefObject{ID: 9000}, PhyDef: 100},
		}}),
	}
	result := handler.applyPlayerDamageResult(player, target, 500)
	if result.wireDamage != 400 || result.appliedDamage != 50 || !result.killed {
		t.Fatalf("overkill result = %+v, want wire 400, applied 50, killed", result)
	}
	if hp := target.CurrentHitPoints(); hp != 0 {
		t.Fatalf("target HP = %d, want 0", hp)
	}

	largeTarget := combatEffectNPC(207, 9000, 30_000_000)
	largeResult := handler.applyPlayerDamageResult(player, largeTarget, 20_000_000, false, true)
	if largeResult.wireDamage != maximumWireDamage || largeResult.appliedDamage != 20_000_000 || largeResult.killed {
		t.Fatalf("large hit result = %+v, want wire %d, applied 20000000, alive", largeResult, maximumWireDamage)
	}
}

func TestSkillDamageIsCappedOnlyWhenWrittenToTheClient(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(210, 9000, 30_000_000)
	handler := &CombatHandler{world: &combatWorldFake{player: player}}

	calculated := skillStrikeDamage(player, model.RefSkill{AbsoluteDamage: 20_000_000})
	if calculated != 20_000_000 {
		t.Fatalf("authoritative skill damage = %d, want 20000000 before wire encoding", calculated)
	}
	result := handler.applyPlayerDamageResult(player, target, calculated, false, true)
	if result.wireDamage != maximumWireDamage || result.appliedDamage != 20_000_000 || result.killed {
		t.Fatalf("high skill result = %+v, want wire %d and applied 20000000", result, maximumWireDamage)
	}
	if hp := target.CurrentHitPoints(); hp != 10_000_000 {
		t.Fatalf("target HP = %d, want 10000000", hp)
	}
}

func TestPlayerPhysicalCriticalDoublesDamageAndMonsterBlockPreventsIt(t *testing.T) {
	now := time.Now()
	player := &model.Player{
		SRObject:        model.SRObject{UniqueID: 100},
		BaseAttackStats: model.AttackStats{CriticalRate: 100},
	}
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
		9000: {RefObject: model.RefObject{ID: 9000}, PhyDef: 100},
		9001: {RefObject: model.RefObject{ID: 9001}, BlockRate: 100},
	}})
	handler := &CombatHandler{
		referenceData:  catalog,
		hitRollPercent: func(int) bool { return true },
		criticalRoll:   func(int) bool { return true },
		blockRoll:      func(int) bool { return true },
	}

	target := combatEffectNPC(208, 9000, 1000)
	critical := handler.resolvePlayerDamage(player, target, 500, false, false, now)
	if critical.wireDamage != 800 || critical.appliedDamage != 800 || critical.damageState != model.DamageStateCritical || critical.killed {
		t.Fatalf("critical result = %+v, want 800 critical damage", critical)
	}

	blockedTarget := combatEffectNPC(209, 9001, 1000)
	blocked := handler.resolvePlayerDamage(player, blockedTarget, 500, false, false, now)
	if blocked.attackState != model.DamageAttackStateBlock || blocked.wireDamage != 0 || blocked.appliedDamage != 0 || blocked.killed {
		t.Fatalf("blocked result = %+v, want block without damage", blocked)
	}
	if hp := blockedTarget.CurrentHitPoints(); hp != 1000 {
		t.Fatalf("blocked target HP = %d, want 1000", hp)
	}
}

func TestVitalSpotFaintUsesAuthoredProbabilityAndStopsOnSuccess(t *testing.T) {
	now := time.Now()
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(205, 9000, 1000)
	world := &combatWorldFake{player: player}
	rollResult := false
	var chances []int
	handler := &CombatHandler{
		world: world, now: func() time.Time { return now },
		rollPercent: func(chance int) bool { chances = append(chances, chance); return rollResult },
		broadcast:   func(*model.Player, *network.Packet) {},
	}
	skill := model.RefSkill{ID: 8416, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusStun, Power: 1, Chance: 80, DurationMS: 5000}}}
	handler.applySkillStatuses(nil, player, target, skill, now)
	if _, active := target.CombatStatus(model.CombatStatusStun, now); active || world.stops != 0 {
		t.Fatal("failed 80% roll applied stun")
	}
	rollResult = true
	handler.applySkillStatuses(nil, player, target, skill, now)
	status, active := target.CombatStatus(model.CombatStatusStun, now)
	if !active || status.ExpiresAt.Sub(now) != 5*time.Second || world.stops != 1 {
		t.Fatalf("successful faint = active:%t duration:%s stops:%d", active, status.ExpiresAt.Sub(now), world.stops)
	}
	if len(chances) != 2 || chances[0] != 80 || chances[1] != 80 {
		t.Fatalf("faint rolls = %v, want [80 80]", chances)
	}
}

func TestForceSeriesUseAuthoredStatusProbabilities(t *testing.T) {
	now := time.Now()
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(206, 9000, 1000)
	var chances []int
	handler := &CombatHandler{
		world: &combatWorldFake{player: player}, now: func() time.Time { return now },
		rollPercent: func(chance int) bool { chances = append(chances, chance); return true },
		broadcast:   func(*model.Player, *network.Packet) {},
	}
	force := model.RefSkill{ID: 900, StatusEffects: []model.SkillStatusEffect{
		{Kind: model.CombatStatusFreeze, Power: 206, Chance: 5, DurationMS: 5000},
		{Kind: model.CombatStatusFrostbite, Power: 206, Chance: 25, DurationMS: 5000},
		{Kind: model.CombatStatusShock, Power: 206, Chance: 20, DurationMS: 5000},
		{Kind: model.CombatStatusBurn, Power: 206, Chance: 25, DurationMS: 9100},
	}}
	handler.applySkillStatuses(nil, player, target, force, now)
	if got, want := chances, []int{5, 25, 20, 25}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Force-series rolls = %v, want %v", got, want)
	}
}

func TestAbsoluteDamageConversionRestoresMPFromAppliedDamage(t *testing.T) {
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7, Level: 20,
		BaseStats:  model.BaseStats{HP: 100, MP: 1, Str: 20, Int: 20},
		BonusStats: model.BonusBaseStats{BaseStats: model.BaseStats{MP: 1000}},
	}
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{}
	handler := &CombatHandler{
		vitals: vitals,
		send:   func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{AbsoluteDamage: 130, DamageToMPPercent: 100}
	if err := handler.restoreDamageVitals(context.Background(), nil, player, skill, 130); err != nil {
		t.Fatalf("restoreDamageVitals: %v", err)
	}
	if vitals.saveCalls != 1 || vitals.savedHP != 100 || vitals.savedMP != 131 {
		t.Fatalf("saved vitals = calls:%d hp:%d mp:%d, want 1/100/131", vitals.saveCalls, vitals.savedHP, vitals.savedMP)
	}
	if hp, mp := player.CurrentVitals(); hp != 100 || mp != 131 {
		t.Fatalf("player vitals = %d/%d, want 100/131", hp, mp)
	}
}
