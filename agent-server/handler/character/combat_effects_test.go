package character

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
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

func TestNASRUNSkillOwnershipComesFromActiveDevilSpirit(t *testing.T) {
	original := model.RefItems
	model.RefItems = map[uint32]model.RefItem{24405: {
		RefObject: model.RefObject{ID: 24405, CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1}},
	}}
	t.Cleanup(func() { model.RefItems = original })

	devil := model.Item{SRObject: model.SRObject{RefObjectID: 24405}, Data: 2_419_200, StackSize: 1}
	devil.SetTypeInfo(model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1})
	devil.RentInfo.PeriodEndTime = uint32(time.Now().Add(28 * 24 * time.Hour).Unix())
	player := &model.Player{Inventory: model.NewInventory(nil), AvatarInventory: model.NewInventory(map[byte]model.Item{4: devil})}
	skill := model.RefSkill{
		ID:            31142,
		CodeName:      "SKILL_MALL_NASRUN_ECCENTRICDEMON_01",
		RequiredItems: []model.SkillRequiredItem{{TypeID3: 14, TypeID4: 1}},
	}

	if !playerOwnsSkill(player, skill.ID, skill) {
		t.Fatal("active Devil Spirit did not grant its NASRUN skill")
	}
	devil.RentInfo.PeriodEndTime = uint32(time.Now().Add(-time.Second).Unix())
	player.AvatarInventory = model.NewInventory(map[byte]model.Item{4: devil})
	if playerOwnsSkill(player, skill.ID, skill) {
		t.Fatal("expired Devil Spirit still granted its NASRUN skill")
	}
}

func TestCombatCooldownTracksReuseAndCoolTimeSeparately(t *testing.T) {
	now := time.Unix(100, 0)
	handler := &CombatHandler{}
	key := skillCooldownKey{playerUID: 7, skillID: 99}
	skill := model.RefSkill{
		BasicActivity:       2,
		ActionPreparingTime: 200,
		ActionCastingTime:   600,
		ActionDuration:      2400,
		ReuseDelay:          5000,
		CoolTime:            750,
	}
	handler.registerSkillCooldown(key, skill, now)
	if got := handler.nextSkill[key]; !got.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("per-skill reuse = %s, want %s", got, now.Add(5*time.Second))
	}
	if got := handler.nextCoolTime[key.playerUID]; !got.Equal(now.Add(750 * time.Millisecond)) {
		t.Fatalf("cool time = %s, want %s", got, now.Add(750*time.Millisecond))
	}
	if handler.skillAvailable(skillCooldownKey{playerUID: key.playerUID, skillID: 100}, now.Add(500*time.Millisecond)) {
		t.Fatal("different skill bypassed actor cool time")
	}
	if !handler.skillAvailable(skillCooldownKey{playerUID: key.playerUID, skillID: 100}, now.Add(time.Second)) {
		t.Fatal("expired actor cool time still blocked a different skill")
	}
	if handler.skillAvailable(key, now.Add(4*time.Second)) {
		t.Fatal("per-skill reuse expired too early")
	}
}

func TestActionTimeProviderScalesCastAndReuseLifecycle(t *testing.T) {
	now := time.Unix(100, 0)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 7}}
	handler := &CombatHandler{}
	handler.ConfigureActionTimeProvider(func(*model.Player, model.RefSkill) float64 { return 50 })
	skill := model.RefSkill{BasicActivity: 2, ActionPreparingTime: 200, ActionCastingTime: 600, ActionDuration: 2400, ReuseDelay: 5000}
	if got := skill.RuntimeCastingDurationAt(handler.skillActionTimePercent(player, skill)); got != 400*time.Millisecond {
		t.Fatalf("scaled cast duration = %s, want 400ms", got)
	}
	key := skillCooldownKey{playerUID: player.GetUniqueID(), skillID: skill.ID}
	handler.registerSkillCooldownForPlayer(player, key, skill, now)
	if got, want := handler.nextSkill[key], now.Add(3400*time.Millisecond); !got.Equal(want) {
		t.Fatalf("scaled reuse = %s, want %s", got, want)
	}
}

func TestFrostbiteScalesPlayerAndEachComboPhaseActionTime(t *testing.T) {
	now := time.Unix(100, 0)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 7}}
	player.SetLifeState(model.Alive)
	handler := &CombatHandler{now: func() time.Time { return now }}
	if !player.ApplyCombatStatusAt(model.CombatStatus{Kind: model.CombatStatusFrostbite, Token: 1, SkillID: 7972, ExpiresAt: now.Add(time.Second)}, now) {
		t.Fatal("apply player frostbite")
	}
	phase2 := model.RefSkill{ID: 2, ActionCastingTime: 300, ActionDuration: 700}
	handler.referenceData = model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{2: phase2}})
	phase1 := model.RefSkill{ID: 1, BasicChainCode: 2, ActionCastingTime: 200, ActionDuration: 800}
	if got := handler.skillActionTimePercent(player, phase1); got != 200 {
		t.Fatalf("frostbite action time = %g%%, want 200%%", got)
	}
	if got := handler.skillComboImpactDuration(player, phase1); got != 4*time.Second {
		t.Fatalf("frostbite combo lifecycle = %s, want 4s", got)
	}
}

func TestDullUsesStockMovementAndActionFactors(t *testing.T) {
	now := time.Now()
	player := &model.Player{SRObject: model.SRObject{UniqueID: 7}, WalkSpeed: 10, RunSpeed: 20}
	player.SetLifeState(model.Alive)
	if !player.ApplyCombatStatusAt(model.CombatStatus{Kind: model.CombatStatusDull, Token: 1, SkillID: 1, ExpiresAt: now.Add(time.Minute)}, now) {
		t.Fatal("apply player Dull")
	}
	handler := &CombatHandler{now: func() time.Time { return now }}
	if got := handler.skillActionTimePercent(player, model.RefSkill{}); got != 125 {
		t.Fatalf("Dull action time = %g%%, want 125%%", got)
	}
	walk, run := player.MovementSpeeds()
	if walk != 7.5 || run != 15 {
		t.Fatalf("Dull movement = %g/%g, want 7.5/15", walk, run)
	}
}

func TestLongCooldownSinkReceivesNativeCoordinationBoundary(t *testing.T) {
	now := time.Unix(100, 0)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 8}}
	var received time.Time
	var receivedSkill uint32
	handler := &CombatHandler{}
	handler.ConfigureLongCooldownSink(func(_ *model.Player, skill model.RefSkill, expiry time.Time) {
		receivedSkill, received = skill.ID, expiry
	})
	skill := model.RefSkill{ID: 9016, BasicActivity: 2, ActionDuration: 300000, ReuseDelay: 300000}
	key := skillCooldownKey{playerUID: player.GetUniqueID(), skillID: skill.ID}
	handler.registerSkillCooldownForPlayer(player, key, skill, now)
	if receivedSkill != skill.ID || !received.Equal(now.Add(300*time.Second)) {
		t.Fatalf("long cooldown sink = skill:%d expiry:%s, want skill:%d expiry:%s", receivedSkill, received, skill.ID, now.Add(300*time.Second))
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
	target.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusShock, Token: 2, SkillID: 3406, Power: 50, Payload: 50, ExpiresAt: now.Add(time.Minute)})
	if !handler.playerAttackLands(player, target, now) {
		t.Fatal("shocked hit rejected")
	}
	if len(chances) != 3 || chances[0] != 75 || chances[1] != 92 || chances[2] != 98 {
		t.Fatalf("hit chances = %v, want [75 92 98]", chances)
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
	percentSkill := combatEffectNPC(207, 9000, 1000)
	if result := handler.resolvePlayerDamageWithAggro(player, percentSkill, 200, false, false, now, 0, 0, 200); result.appliedDamage != 200 {
		t.Fatalf("post-mitigation 200%% skill damage = %d, want 200", result.appliedDamage)
	}
	if _, ok := player.CommitActiveBuff(10, 20, time.Minute, model.BuffModifiers{DamagePercent: 25}); !ok {
		t.Fatal("failed to arrange urd damage buff")
	}
	buffed := combatEffectNPC(206, 9000, 1000)
	if applied, _ := handler.applyPlayerDamage(player, buffed, 200); applied != 125 {
		t.Fatalf("post-mitigation urd damage = %d, want 125", applied)
	}
	player.RemoveActiveBuff(20)
	physicalDown := combatEffectNPC(202, 9000, 1000)
	physicalDown.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusPhysicalDefenseDown, Token: 1, SkillID: 8399, Power: 80, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, physicalDown, 200); applied != 170 {
		t.Fatalf("physical-defense-down damage = %d, want stock 30%% defense floor result 170", applied)
	}
	magicalDown := combatEffectNPC(203, 9000, 1000)
	magicalDown.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusMagicalDefenseDown, Token: 2, SkillID: 8405, Power: 100, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, magicalDown, 200, true); applied != 164 {
		t.Fatalf("magical-defense-down damage = %d, want stock 30%% defense floor result 164", applied)
	}
	damageUp := combatEffectNPC(204, 9000, 1000)
	damageUp.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusDamageTakenUp, Token: 3, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, damageUp, 200); applied != 135 {
		t.Fatalf("damage-taken-up damage = %d, want 135", applied)
	}
	magicalDamageUp := combatEffectNPC(208, 9000, 1000)
	magicalDamageUp.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusDamageTakenUp, Token: 5, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, magicalDamageUp, 200, true); applied != 108 {
		t.Fatalf("magical damage-taken-up damage = %d, want 108", applied)
	}
	absolute := combatEffectNPC(205, 9000, 1000)
	absolute.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusDamageTakenUp, Token: 4, SkillID: 8414, Power: 35, ExpiresAt: now.Add(time.Minute)})
	if applied, _ := handler.applyPlayerDamage(player, absolute, 130, true, true); applied != 130 {
		t.Fatalf("absolute damage = %d, want 130", applied)
	}
}

func TestNPCDamageUsesReferenceAbsorptionBeforeDefense(t *testing.T) {
	player := &model.Player{}
	target := combatEffectNPC(209, 9001, 1000)
	handler := &CombatHandler{world: &combatWorldFake{player: player}, referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
		9001: {RefObject: model.RefObject{ID: 9001}, PhyDef: 100, PhyAbsorbRate: 50},
	}})}
	result := handler.applyPlayerDamageResult(player, target, 200)
	if result.appliedDamage != 33 {
		t.Fatalf("reference absorption damage = %d, want 33", result.appliedDamage)
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

	calculated := (&CombatHandler{}).skillStrikeDamage(player, nil, model.RefSkill{AbsoluteDamage: 20_000_000})
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

func TestPlayerMagicalDamageDoesNotBecomeCritical(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(210, 9000, 1000)
	handler := &CombatHandler{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
		9000: {RefObject: model.RefObject{ID: 9000}},
	}})}

	result := handler.applyPlayerDamageResult(player, target, 500, true, false, true)
	if result.wireDamage != 500 || result.appliedDamage != 500 || result.damageState != model.DamageStateNormal || result.killed {
		t.Fatalf("magical critical-flag result = %+v, want normal 500 damage", result)
	}
	if hp := target.CurrentHitPoints(); hp != 500 {
		t.Fatalf("target HP = %d, want 500", hp)
	}
}

func TestMSCHModeOneProducesStateOnlySourceResult(t *testing.T) {
	now := time.Now()
	const skillID = uint32(7127)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	if _, ok := player.CommitActiveBuff(skillID, 77, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("arrange msch source singleton")
	}
	target := combatEffectNPC(210, 9000, 1000)
	handler := &CombatHandler{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		skillID: {ID: skillID, CodeName: "SKILL_ETC_TRANS_MONSTER_01", MatchMode: 1},
	}})}
	result := handler.resolvePlayerDamage(player, target, 500, false, false, now)
	if result.attackState != model.DamageResultSourceState || result.wireDamage != 0 || result.appliedDamage != 0 || result.killed {
		t.Fatalf("msch(1) result = %+v", result)
	}
	if hp := target.CurrentHitPoints(); hp != 1000 {
		t.Fatalf("msch(1) mutated target HP to %d", hp)
	}
	if handler.sourceUsesStateOnlyResult(player, now.Add(2*time.Minute)) {
		t.Fatal("expired msch(1) singleton remained active")
	}
}

func TestSiegeTargetsProduceStateOnlyTargetResult(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	handler := &CombatHandler{}
	for _, test := range []struct {
		name     string
		typeInfo model.TypeInfo
		want     bool
	}{
		{name: "siege object subtype two", typeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 4, TypeID4: 2}, want: true},
		{name: "other siege object subtype", typeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 4, TypeID4: 1}},
		{name: "siege structure", typeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 5, TypeID4: 7}, want: true},
		{name: "ordinary mob", typeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := combatEffectNPC(211, 9000, 1000)
			target.TypeInfo = test.typeInfo
			result := handler.resolvePlayerDamage(player, target, 500, false, false, time.Now())
			if got := result.attackState == model.DamageResultTargetState; got != test.want {
				t.Fatalf("target-state result = %t, want %t (%+v)", got, test.want, result)
			}
			if test.want {
				if result.wireDamage != 0 || result.appliedDamage != 0 || result.killed {
					t.Fatalf("state-only result mutated damage: %+v", result)
				}
				if hp := target.CurrentHitPoints(); hp != 1000 {
					t.Fatalf("state-only target HP = %d, want 1000", hp)
				}
			}
		})
	}
}

func TestSiegeStructureDamageUsesDurableAuthority(t *testing.T) {
	player := &model.Player{ID: 77, SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(211, 9000, 1000)
	target.TypeInfo = model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 5, TypeID4: 7}
	var gotUniqueID uint32
	var gotActor int
	var gotDamage uint32
	handler := &CombatHandler{}
	handler.ConfigureFortressDamage(func(_ context.Context, uniqueID uint32, actor int, damage uint32, _ time.Time) (model.FortressDamageResult, error) {
		gotUniqueID, gotActor, gotDamage = uniqueID, actor, damage
		return model.FortressDamageResult{Structure: model.FortressStructure{FortressID: 1, UniqueID: uniqueID, RefEventID: 2, State: 1}}, nil
	})
	result := handler.resolvePlayerDamage(player, target, 500, false, false, time.Now())
	if result.attackState != model.DamageResultTargetState || gotUniqueID != target.GetUniqueID() || gotActor != player.ID || gotDamage != 500 {
		t.Fatalf("siege damage result=%+v callback=(%d,%d,%d)", result, gotUniqueID, gotActor, gotDamage)
	}
	if hp := target.CurrentHitPoints(); hp != 1000 {
		t.Fatalf("generic NPC HP mutated to %d", hp)
	}
}

func TestPlayerTargetSkillAppliesMitigatedDamage(t *testing.T) {
	attacker := &model.Player{
		SRObject:        model.SRObject{UniqueID: 100},
		BaseStats:       model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20},
		Skills:          map[uint32]byte{9001: 1},
		Level:           10,
		LifeState:       model.Alive,
		BaseAttackStats: model.AttackStats{PhyAttackMin: 100, PhyAttackMax: 100, HitRate: 100},
	}
	target := &model.Player{
		SRObject:         model.SRObject{UniqueID: 200},
		BaseStats:        model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20},
		Level:            10,
		LifeState:        model.Alive,
		BaseDefenseStats: model.DefenseStats{PhyDef: 25},
	}
	packets := 0
	handler := &CombatHandler{
		send: func(_ *server.Session, packet network.Packet) error {
			packets++
			return nil
		},
		blockRoll:  func(int) bool { return false },
		damageRoll: func() int { return 0 },
		now:        time.Now,
	}
	skill := model.RefSkill{ID: 9001, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("player-target skill failed: %v", err)
	}
	if got := target.CurrentHP(); got >= 1000 {
		t.Fatalf("target HP = %d, want damage applied", got)
	}
	if packets < 2 {
		t.Fatalf("sent packets = %d, want action-state and attack packets", packets)
	}
}

func TestPlayerTargetMagicalSkillDoesNotRollCritical(t *testing.T) {
	attacker := &model.Player{
		SRObject:        model.SRObject{UniqueID: 100},
		BaseStats:       model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20},
		Level:           10,
		LifeState:       model.Alive,
		BaseAttackStats: model.AttackStats{MagAttackMin: 100, MagAttackMax: 100, HitRate: 100, CriticalRate: 100},
	}
	target := &model.Player{
		SRObject:  model.SRObject{UniqueID: 200},
		BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20},
		Level:     10,
		LifeState: model.Alive,
	}
	criticalRolls := 0
	handler := &CombatHandler{
		send:         func(*server.Session, network.Packet) error { return nil },
		blockRoll:    func(int) bool { return false },
		damageRoll:   func() int { return 0 },
		criticalRoll: func(int) bool { criticalRolls++; return true },
	}
	skill := model.RefSkill{ID: 9002, TargetEnemyPVP: true, AttackType: 8, AttackMin: 100, AttackMax: 100}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("player-target magical skill failed: %v", err)
	}
	if hp := target.CurrentHP(); hp != 796 {
		t.Fatalf("target HP = %d, want 796 from non-critical magical damage", hp)
	}
	if criticalRolls != 0 {
		t.Fatalf("magical critical rolls = %d, want 0", criticalRolls)
	}
}

func TestShortSightRejectsPVPAttackBeyondReducedRange(t *testing.T) {
	now := time.Unix(100, 0)
	attacker := &model.Player{
		ID: 1, SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 0, 0, 0)},
		BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10,
		BaseAttackStats: model.AttackStats{PhyAttackMin: 100, PhyAttackMax: 100, HitRate: 100},
	}
	target := &model.Player{
		ID: 2, SRObject: model.SRObject{UniqueID: 200, RtNavmeshPosition: combatTestPosition(24231, 75, 0, 0)},
		BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10,
	}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	attacker.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusShortSight, Token: 1, SkillID: 1, Power: 50, ExpiresAt: now.Add(time.Minute),
	}, now)
	handler := &CombatHandler{
		now: func() time.Time { return now }, send: func(*server.Session, network.Packet) error { return nil },
		blockRoll: func(int) bool { return false }, damageRoll: func() int { return 0 },
	}
	skill := model.RefSkill{ID: 1, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, ActionRange: 100}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatal(err)
	}
	if got := target.CurrentHP(); got != 1000 {
		t.Fatalf("Short Sight out-of-range attack changed HP to %d", got)
	}
}

func TestDarknessReducesPVPVarianceDamage(t *testing.T) {
	now := time.Unix(100, 0)
	attacker := &model.Player{
		BaseStats: model.BaseStats{HP: 1000, Str: 20}, Level: 10,
		BaseAttackStats: model.AttackStats{PhyAttackMin: 100, PhyAttackMax: 200, HitRate: 100},
	}
	target := &model.Player{BaseStats: model.BaseStats{HP: 1000}, Level: 10, BaseDefenseStats: model.DefenseStats{ParryRate: 100}}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	handler := &CombatHandler{now: func() time.Time { return now }, damageRoll: func() int { return 0 }}
	skill := model.RefSkill{AttackType: 5}
	baseline := handler.playerSkillDamageAgainstPlayerAt(attacker, target, skill, now)
	attacker.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusDarkness, Token: 1, SkillID: 1, Power: 80, ExpiresAt: now.Add(time.Minute),
	}, now)
	darkened := handler.playerSkillDamageAgainstPlayerAt(attacker, target, skill, now)
	if darkened >= baseline {
		t.Fatalf("Darkness variance damage = %d, want below baseline %d", darkened, baseline)
	}
}

func TestShockAndParryDownReducePlayerPVPParry(t *testing.T) {
	now := time.Unix(100, 0)
	attacker := &model.Player{
		BaseStats: model.BaseStats{HP: 1000, Str: 20}, Level: 10,
		BaseAttackStats: model.AttackStats{PhyAttackMin: 100, PhyAttackMax: 200, HitRate: 100},
	}
	target := &model.Player{
		BaseStats: model.BaseStats{HP: 1000}, Level: 10,
		BaseDefenseStats: model.DefenseStats{ParryRate: 100},
	}
	handler := &CombatHandler{damageRoll: func() int { return 0 }}
	skill := model.RefSkill{AttackType: 5}
	baseline := handler.playerSkillDamageAgainstPlayerAt(attacker, target, skill, now)
	target.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusParryDown, Token: 1, Power: 40, ExpiresAt: now.Add(time.Minute),
	}, now)
	target.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusShock, Token: 2, Power: 50, Payload: 50, ExpiresAt: now.Add(time.Minute),
	}, now)
	reducedParry := handler.playerSkillDamageAgainstPlayerAt(attacker, target, skill, now)
	if reducedParry <= baseline {
		t.Fatalf("Shock/Parry Down variance damage = %d, want above baseline %d", reducedParry, baseline)
	}
}

func TestPlayerTargetDamageTakenUpUsesFinalFactor(t *testing.T) {
	now := time.Unix(2000, 0)
	target := &model.Player{BaseStats: model.BaseStats{HP: 1000}, BaseDefenseStats: model.DefenseStats{PhyDef: 25}}
	target.SetLifeState(model.Alive)
	if !target.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusDamageTakenUp, Token: 1, Power: 35, ExpiresAt: now.Add(time.Minute)}) {
		t.Fatal("failed to install damage-taken-up status")
	}
	handler := &CombatHandler{}
	if got := handler.mitigatePlayerSkillDamage(target, 100, false, false, now, 100); got != 101 {
		t.Fatalf("player final damage = %d, want 101", got)
	}
}

func TestPlayerTargetAttackDownReducesPvPDamage(t *testing.T) {
	now := time.Unix(2100, 0)
	attacker := &model.Player{BaseStats: model.BaseStats{HP: 1000}, BaseAttackStats: model.AttackStats{PhyAttackMin: 100, PhyAttackMax: 100, HitRate: 100}, Level: 10}
	target := &model.Player{BaseStats: model.BaseStats{HP: 1000}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	handler := &CombatHandler{damageRoll: func() int { return 0 }}
	base := handler.playerSkillDamageAgainstPlayerAt(attacker, target, model.RefSkill{AttackType: 5}, now)
	if !attacker.ApplyCombatStatus(model.CombatStatus{Kind: model.CombatStatusAttackDown, Token: 2, Power: 50, ExpiresAt: now.Add(time.Minute)}) {
		t.Fatal("failed to install attack-down status")
	}
	reduced := handler.playerSkillDamageAgainstPlayerAt(attacker, target, model.RefSkill{AttackType: 5}, now)
	if base <= reduced || reduced != model.CombatFinalDamageFactor(base, -50) {
		t.Fatalf("attack-down damage = base:%d reduced:%d, want %d", base, reduced, model.CombatFinalDamageFactor(base, -50))
	}
}

func TestPlayerPVPUsesSourceDamagePercentAfterMitigation(t *testing.T) {
	now := time.Unix(2200, 0)
	attacker := &model.Player{CombatDamagePercent: 20}
	target := &model.Player{BaseDefenseStats: model.DefenseStats{PhyDef: 25}}
	handler := &CombatHandler{}
	damage := handler.mitigatePlayerSkillDamage(target, 100, false, false, now, 100)
	damage += damage * attacker.ActiveCombatDamagePercent() / 100
	if damage != 90 {
		t.Fatalf("source damage percent result = %d, want 90", damage)
	}
}

func TestPlayerPVPLinkedDamageInterceptsPrimaryAndConsumesLimit(t *testing.T) {
	source := &model.Player{SRObject: model.SRObject{UniqueID: 300}, BaseStats: model.BaseStats{HP: 1000, MP: 100}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 301}, BaseStats: model.BaseStats{HP: 1000, MP: 100}}
	linked := &model.Player{SRObject: model.SRObject{UniqueID: 302}, BaseStats: model.BaseStats{HP: 1000, MP: 100}}
	for _, player := range []*model.Player{source, target, linked} {
		player.SetLifeState(model.Alive)
	}
	source.KnownObjectList = model.NewKnownObjectList(source)
	source.KnownObjectList.AddObject(target)
	source.KnownObjectList.AddObject(linked)
	if _, ok := target.CommitLinkedActiveBuff(9100, 55, linked.GetUniqueID(), 0, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("failed to install linked damage buff")
	}
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		9100: {ID: 9100, LinkedDamageMask: 15, LinkedDamagePercent: 50, LinkedDamageUseLimit: 1},
	}})
	handler := &CombatHandler{referenceData: catalog, now: time.Now}
	remaining, redirected, application, token := handler.interceptPlayerLinkedDamage(source, target, 200, false)
	if remaining != 100 || redirected != linked || application.AppliedDamage != 100 || token != 55 {
		t.Fatalf("linked interception = remaining:%d redirected:%v applied:%d removed:%d", remaining, redirected, application.AppliedDamage, token)
	}
	if linked.CurrentHP() != 900 || len(target.ActiveBuffSnapshot()) != 0 {
		t.Fatalf("linked target HP=%d active buffs=%v, want HP 900 and consumed buff", linked.CurrentHP(), target.ActiveBuffSnapshot())
	}
}

func TestPVPAdmissionProviderCanRejectTargetedSkill(t *testing.T) {
	attacker := &model.Player{SRObject: model.SRObject{UniqueID: 330}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 331}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	handler := &CombatHandler{send: func(*server.Session, network.Packet) error { return nil }}
	handler.ConfigurePVPAdmissionProvider(func(*model.Player, *model.Player, model.RefSkill) bool { return false })
	skill := model.RefSkill{ID: 9015, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("rejected PVP skill returned error: %v", err)
	}
	if target.CurrentHP() != 1000 {
		t.Fatalf("rejected PVP skill changed target HP=%d", target.CurrentHP())
	}
}

func TestNestedLinkedBuffTeardownRemovesEntireOwnerGraph(t *testing.T) {
	parent := &model.Player{SRObject: model.SRObject{UniqueID: 401}}
	child := &model.Player{SRObject: model.SRObject{UniqueID: 402}}
	grandchild := &model.Player{SRObject: model.SRObject{UniqueID: 403}}
	if _, ok := child.CommitLinkedActiveBuff(9200, 200, parent.GetUniqueID(), 100, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("failed to install child linked buff")
	}
	if _, ok := grandchild.CommitLinkedActiveBuff(9201, 300, child.GetUniqueID(), 200, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("failed to install grandchild linked buff")
	}
	handler := &CombatHandler{}
	handler.registerLinkedBuff(100, 200, child)
	handler.registerLinkedBuff(200, 300, grandchild)
	// A malformed cycle must remain bounded while still removing both levels.
	handler.registerLinkedBuff(300, 100, parent)
	handler.removeLinkedBuffs(parent, 100)
	if len(child.ActiveBuffSnapshot()) != 0 || len(grandchild.ActiveBuffSnapshot()) != 0 {
		t.Fatalf("nested linked buffs remain: child=%v grandchild=%v", child.ActiveBuffSnapshot(), grandchild.ActiveBuffSnapshot())
	}
	if len(handler.linkedBuffs) != 0 || len(handler.linkedParents) != 0 {
		t.Fatalf("linked graph remains: buffs=%v parents=%v", handler.linkedBuffs, handler.linkedParents)
	}
}

func TestNormalSkillConsumesAuthoredHP(t *testing.T) {
	player := &model.Player{ID: 310, SRObject: model.SRObject{UniqueID: 310}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{}
	handler := &CombatHandler{vitals: vitals, send: func(*server.Session, network.Packet) error { return nil }}
	if err := handler.spendSkillResources(context.Background(), &server.Session{}, player, model.RefSkill{ConsumeHP: 100}); err != nil {
		t.Fatalf("HP resource debit failed: %v", err)
	}
	if got := player.CurrentHP(); got != 900 {
		t.Fatalf("HP after skill debit = %d, want 900", got)
	}
}

func TestNormalSkillResourceDebitRollsBackOnSecondPoolFailure(t *testing.T) {
	player := &model.Player{ID: 311, SRObject: model.SRObject{UniqueID: 311}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{}
	handler := &CombatHandler{vitals: vitals, send: func(*server.Session, network.Packet) error { return nil }}
	_ = handler.spendSkillResources(context.Background(), &server.Session{}, player, model.RefSkill{ConsumeHP: 100, ConsumeMP: 10})
	if hp, mp := player.CurrentVitals(); hp != 1000 || mp != 100 {
		t.Fatalf("resource rollback vitals = hp:%d mp:%d, want hp:1000 mp:100", hp, mp)
	}
}

func TestPlayerTargetSkillRestoresDamageToCasterVitals(t *testing.T) {
	attacker := &model.Player{ID: 301, SRObject: model.SRObject{UniqueID: 104}, BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 204}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{}
	handler := &CombatHandler{vitals: vitals, send: func(*server.Session, network.Packet) error { return nil }, blockRoll: func(int) bool { return false }, damageRoll: func() int { return 0 }}
	skill := model.RefSkill{ID: 9007, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, DamageToHPPercent: 50}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("player-target recovery skill failed: %v", err)
	}
	if attacker.CurrentHP() <= 100 || vitals.saveCalls == 0 {
		t.Fatalf("caster recovery HP=%d saveCalls=%d, want restored and persisted", attacker.CurrentHP(), vitals.saveCalls)
	}
}

func TestZombieInvertsDamageToHPRecovery(t *testing.T) {
	now := time.Unix(100, 0)
	attacker := &model.Player{ID: 301, SRObject: model.SRObject{UniqueID: 104}, BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{ID: 302, SRObject: model.SRObject{UniqueID: 204}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	attacker.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusZombie, Token: 1, SkillID: 1, ExpiresAt: now.Add(time.Minute),
	}, now)
	vitals := &combatVitalsFake{}
	handler := &CombatHandler{
		now: func() time.Time { return now }, vitals: vitals,
		send:      func(*server.Session, network.Packet) error { return nil },
		blockRoll: func(int) bool { return false }, damageRoll: func() int { return 0 },
	}
	skill := model.RefSkill{ID: 9007, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, DamageToHPPercent: 50}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatal(err)
	}
	if attacker.CurrentHP() >= 100 {
		t.Fatalf("Zombie damage-to-HP recovery raised HP to %d", attacker.CurrentHP())
	}
}

func TestHealingSkillAppliesOnlyAtAuthoredImpact(t *testing.T) {
	caster := &model.Player{
		ID: 302, SRObject: model.SRObject{UniqueID: 106},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	caster.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	cooldowns := &combatCooldownFake{}
	var scheduled func()
	handler := &CombatHandler{
		vitals: vitals, cooldowns: cooldowns,
		schedule: func(_ time.Duration, fn func()) { scheduled = fn },
		send:     func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{
		ID: 9017, BasicActivity: 2, TargetSelf: true,
		HealHP: 50, ActionCastingTime: 25, ConsumeMP: 10, ReuseDelay: 1000,
	}
	if err := handler.executeHealing(context.Background(), &server.Session{}, caster, caster, skill); err != nil {
		t.Fatalf("delayed healing failed: %v", err)
	}
	if scheduled == nil {
		t.Fatal("healing did not schedule impact")
	}
	if cooldowns.checkCalls != 1 || cooldowns.claimCalls != 1 {
		t.Fatalf("cooldown calls = check:%d claim:%d, want check:1 claim:1", cooldowns.checkCalls, cooldowns.claimCalls)
	}
	if got := caster.CurrentHP(); got != 100 {
		t.Fatalf("healing applied before impact: hp=%d", got)
	}
	scheduled()
	if got := caster.CurrentHP(); got != 150 {
		t.Fatalf("healing at impact = %d, want 150", got)
	}
}

func TestHealingSkillDurableCooldownRejectsBeforeResourceSpend(t *testing.T) {
	caster := &model.Player{
		ID: 303, SRObject: model.SRObject{UniqueID: 107},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	caster.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	cooldowns := &combatCooldownFake{checkErr: model.ErrCharacterCooldownActive}
	handler := &CombatHandler{
		vitals: vitals, cooldowns: cooldowns,
		send: func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{ID: 9018, BasicActivity: 2, TargetSelf: true, HealHP: 50, ConsumeMP: 10, ReuseDelay: 1000}
	if err := handler.executeHealing(context.Background(), &server.Session{}, caster, caster, skill); err != nil {
		t.Fatalf("active durable cooldown returned error: %v", err)
	}
	if cooldowns.checkCalls != 1 || cooldowns.claimCalls != 0 {
		t.Fatalf("cooldown calls = check:%d claim:%d, want check:1 claim:0", cooldowns.checkCalls, cooldowns.claimCalls)
	}
	if vitals.spendCalls != 0 {
		t.Fatalf("active durable cooldown spent resources: calls=%d", vitals.spendCalls)
	}
}

func TestZombieInvertsHealingIntoHPDamage(t *testing.T) {
	now := time.Unix(100, 0)
	target := &model.Player{
		ID: 304, SRObject: model.SRObject{UniqueID: 108},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	target.SetLifeState(model.Alive)
	target.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusZombie, Token: 1, ExpiresAt: now.Add(time.Minute),
	}, now)
	vitals := &combatVitalsFake{spent: true}
	handler := &CombatHandler{
		now:    func() time.Time { return now },
		vitals: vitals,
		send:   func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{ID: 9019, BasicActivity: 2, TargetSelf: true, HealHP: 40}
	if err := handler.executeHealing(context.Background(), &server.Session{}, target, target, skill); err != nil {
		t.Fatalf("zombie healing cast failed: %v", err)
	}
	if got := target.CurrentHP(); got != 60 {
		t.Fatalf("zombie healing HP = %d, want 60", got)
	}
	if vitals.savedHP != 60 {
		t.Fatalf("persisted zombie HP = %d, want 60", vitals.savedHP)
	}
}

func TestZombieInvertsPeriodicRecoveryBuffTick(t *testing.T) {
	now := time.Unix(100, 0)
	player := &model.Player{ID: 305, SRObject: model.SRObject{UniqueID: 109}, BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20}}
	player.SetLifeState(model.Alive)
	player.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusZombie, Token: 1, SkillID: 1, ExpiresAt: now.Add(time.Minute),
	}, now)
	const token = uint32(77)
	if _, ok := player.CommitActiveBuffAt(2, token, player.GetUniqueID(), time.Minute, model.BuffModifiers{}, now); !ok {
		t.Fatal("commit recovery buff")
	}
	var scheduled func()
	handler := &CombatHandler{
		now: func() time.Time { return now }, vitals: &combatVitalsFake{},
		schedule: func(_ time.Duration, fn func()) { scheduled = fn },
		send:     func(*server.Session, network.Packet) error { return nil },
	}
	handler.scheduleRecoveryTick(player, model.RefSkill{RecoveryHP: 40}, token, time.Second)
	if scheduled == nil {
		t.Fatal("recovery tick was not scheduled")
	}
	scheduled()
	if got := player.CurrentHP(); got != 60 {
		t.Fatalf("Zombie periodic recovery HP = %d, want 60", got)
	}
}

func TestCancelledDelayedHealingDoesNotApply(t *testing.T) {
	caster := &model.Player{
		ID: 303, SRObject: model.SRObject{UniqueID: 107},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	caster.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	var scheduled func()
	handler := &CombatHandler{
		vitals:   vitals,
		schedule: func(_ time.Duration, fn func()) { scheduled = fn },
		send:     func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{ID: 9018, BasicActivity: 2, TargetSelf: true, HealHP: 50, ActionCastingTime: 25, ConsumeMP: 10}
	if err := handler.executeHealing(context.Background(), &server.Session{}, caster, caster, skill); err != nil {
		t.Fatalf("delayed healing failed: %v", err)
	}
	handler.cancelDelayedCast(caster.GetUniqueID())
	scheduled()
	if got := caster.CurrentHP(); got != 100 {
		t.Fatalf("cancelled healing changed hp=%d", got)
	}
}

func TestReplacingDelayedCastCompletesSupersededAction(t *testing.T) {
	handler := &CombatHandler{}
	playerUID := uint32(107)
	firstCancelled := 0
	secondCancelled := 0

	firstToken := handler.beginDelayedCast(playerUID, func() { firstCancelled++ })
	secondToken := handler.beginDelayedCast(playerUID, func() { secondCancelled++ })

	if firstCancelled != 1 || secondCancelled != 0 {
		t.Fatalf("replacement cleanup calls = first:%d second:%d, want first:1 second:0", firstCancelled, secondCancelled)
	}
	if handler.consumeDelayedCast(playerUID, firstToken) {
		t.Fatal("superseded cast token remained consumable")
	}
	if !handler.consumeDelayedCast(playerUID, secondToken) {
		t.Fatal("replacement cast token was not consumable")
	}
}

func TestBuffSkillAppliesOnlyAtAuthoredImpact(t *testing.T) {
	player := &model.Player{
		ID: 304, SRObject: model.SRObject{UniqueID: 108},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	player.SetLifeState(model.Alive)
	callbacks := make([]func(), 0, 2)
	handler := &CombatHandler{
		schedule: func(_ time.Duration, fn func()) { callbacks = append(callbacks, fn) },
		send:     func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{
		ID: 9019, BasicActivity: 2, BuffDuration: 1000,
		ActionCastingTime: 25, BuffModifiers: model.BuffModifiers{Strength: 7},
	}
	if err := handler.executeBuff(context.Background(), &server.Session{}, player, player, skill); err != nil {
		t.Fatalf("delayed buff failed: %v", err)
	}
	if len(callbacks) != 1 || len(player.ActiveBuffSnapshot()) != 0 || player.BonusStats.Str != 0 {
		t.Fatalf("buff applied before impact: callbacks=%d active=%d strength=%d", len(callbacks), len(player.ActiveBuffSnapshot()), player.BonusStats.Str)
	}
	callbacks[0]()
	if len(player.ActiveBuffSnapshot()) != 1 || player.BonusStats.Str != 7 {
		t.Fatalf("buff at impact: active=%d strength=%d", len(player.ActiveBuffSnapshot()), player.BonusStats.Str)
	}
}

func TestLifeTurnoverScalesCurrentHPAndPublishesVitals(t *testing.T) {
	player := &model.Player{
		ID: 305, SRObject: model.SRObject{UniqueID: 109},
		Level: 100, BaseStats: model.BaseStats{Str: 100, Int: 100, MP: 100},
	}
	player.SetLifeState(model.Alive)
	maxHP, _ := player.MaxVitals()
	player.BaseStats.HP = maxHP
	var sent []network.Packet
	vitals := &combatVitalsFake{}
	handler := &CombatHandler{
		vitals: vitals,
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}
	skill := model.RefSkill{
		ID: 9974, BasicActivity: 2, BuffDuration: 15 * 60 * 1000,
		BuffModifiers: model.BuffModifiers{HPPercent: -50, DamagePercent: 25, MagicalAttack: 2011},
	}
	if err := handler.executeBuff(context.Background(), &server.Session{}, player, player, skill); err != nil {
		t.Fatalf("Life Turnover cast: %v", err)
	}
	if hp, _ := player.CurrentVitals(); hp != maxHP/2 {
		t.Fatalf("Life Turnover current HP = %d, want %d", hp, maxHP/2)
	}
	if vitals.saveCalls != 1 || vitals.savedID != player.ID || vitals.savedHP != maxHP/2 || vitals.savedMP != 100 {
		t.Fatalf("Life Turnover persistence = %+v", vitals)
	}
	if len(sent) == 0 || sent[len(sent)-1].MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("Life Turnover packets do not end with vitals update: %+v", sent)
	}
}

func TestCancelledDelayedBuffDoesNotApply(t *testing.T) {
	player := &model.Player{
		ID: 305, SRObject: model.SRObject{UniqueID: 109},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	player.SetLifeState(model.Alive)
	var callback func()
	handler := &CombatHandler{
		schedule: func(_ time.Duration, fn func()) { callback = fn },
		send:     func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{ID: 9020, BasicActivity: 2, BuffDuration: 1000, ActionCastingTime: 25, BuffModifiers: model.BuffModifiers{Strength: 9}}
	if err := handler.executeBuff(context.Background(), &server.Session{}, player, player, skill); err != nil {
		t.Fatalf("delayed buff failed: %v", err)
	}
	handler.cancelDelayedCast(player.GetUniqueID())
	callback()
	if len(player.ActiveBuffSnapshot()) != 0 || player.BonusStats.Str != 0 {
		t.Fatalf("cancelled buff changed state: active=%d strength=%d", len(player.ActiveBuffSnapshot()), player.BonusStats.Str)
	}
}

func TestResurrectionTransactionRunsOnlyAtAuthoredImpact(t *testing.T) {
	caster := &model.Player{
		ID: 306, SRObject: model.SRObject{UniqueID: 110},
		BaseStats: model.BaseStats{HP: 200, MP: 150, Str: 20, Int: 20},
	}
	target := &model.Player{
		ID: 307, SRObject: model.SRObject{UniqueID: 210},
		BaseStats: model.BaseStats{HP: 0, MP: 0, Str: 20, Int: 20},
	}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Dead)
	party := &model.Party{Number: 11}
	if err := caster.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	if err := target.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	vitals := &combatVitalsFake{resurrection: true}
	var scheduled func()
	handler := &CombatHandler{
		vitals:    vitals,
		schedule:  func(_ time.Duration, fn func()) { scheduled = fn },
		send:      func(*server.Session, network.Packet) error { return nil },
		broadcast: func(*model.Player, *network.Packet) {},
	}
	skill := model.RefSkill{
		ID: 9021, BasicActivity: 2, TargetRequired: true, TargetAlly: true,
		ResurrectLevel: 30, HealHPPercent: 10, HealMPPercent: 10,
		ConsumeMP: 30, ActionCastingTime: 25,
	}
	if err := handler.executeResurrection(context.Background(), &server.Session{}, caster, target, skill); err != nil {
		t.Fatalf("delayed resurrection failed: %v", err)
	}
	if scheduled == nil || vitals.resurrectionCalls != 0 || target.GetLifeState() != model.Dead {
		t.Fatalf("resurrection ran before impact: scheduled=%v calls=%d life=%d", scheduled != nil, vitals.resurrectionCalls, target.GetLifeState())
	}
	scheduled()
	if vitals.resurrectionCalls != 1 || target.GetLifeState() != model.Alive {
		t.Fatalf("resurrection at impact: calls=%d life=%d", vitals.resurrectionCalls, target.GetLifeState())
	}
}

func TestBarrierSkillInstallsOnlyAtAuthoredImpact(t *testing.T) {
	player := &model.Player{
		ID: 308, SRObject: model.SRObject{UniqueID: 111},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	player.SetLifeState(model.Alive)
	var scheduled func()
	handler := &CombatHandler{
		world:    &combatWorldFake{player: player},
		schedule: func(_ time.Duration, fn func()) { scheduled = fn },
		send:     func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{
		ID: 9022, BasicActivity: 2, BarrierHitPoints: 500,
		BarrierAbsorption: 90, BarrierPhysical: true, ActionCastingTime: 25,
	}
	if err := handler.executeBarrierSkill(context.Background(), &server.Session{}, player, skill); err != nil {
		t.Fatalf("delayed barrier failed: %v", err)
	}
	if scheduled == nil || player.BarrierSnapshot().Token != 0 || player.IsBarrierRooted() {
		t.Fatalf("barrier installed before impact: scheduled=%v barrier=%+v rooted=%v", scheduled != nil, player.BarrierSnapshot(), player.IsBarrierRooted())
	}
	scheduled()
	barrier := player.BarrierSnapshot()
	if barrier.SkillID != skill.ID || barrier.HitPoints != 500 || !player.IsBarrierRooted() {
		t.Fatalf("barrier at impact = %+v rooted=%v", barrier, player.IsBarrierRooted())
	}
}

func TestInvalidMovementPathDoesNotConsumeSkillResources(t *testing.T) {
	player := &model.Player{
		ID: 309, SRObject: model.SRObject{UniqueID: 112, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	handler := &CombatHandler{
		world:  &combatWorldFake{player: player, blocked: true},
		vitals: vitals,
		send:   func(*server.Session, network.Packet) error { return nil },
	}
	skill := model.RefSkill{ID: 9023, BasicActivity: 2, MovementDistance: 100, ConsumeMP: 10}
	if err := handler.executeMovementSkill(context.Background(), &server.Session{}, player, skill); err != nil {
		t.Fatalf("invalid movement returned error: %v", err)
	}
	if vitals.spendCalls != 0 {
		t.Fatalf("invalid movement spent resources: calls=%d", vitals.spendCalls)
	}
}

func TestBuffTargetOutsideAuthoredRangeIsRejected(t *testing.T) {
	caster := &model.Player{
		ID: 310, SRObject: model.SRObject{UniqueID: 113, RtNavmeshPosition: combatTestPosition(24231, 0, 0, 100)},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	target := &model.Player{
		ID: 311, SRObject: model.SRObject{UniqueID: 114, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	handler := &CombatHandler{send: func(*server.Session, network.Packet) error { return nil }}
	skill := model.RefSkill{ID: 9024, BasicActivity: 2, BuffDuration: 1000, ActionRange: 10, BuffModifiers: model.BuffModifiers{Strength: 5}}
	if err := handler.executeBuff(context.Background(), &server.Session{}, caster, target, skill); err != nil {
		t.Fatalf("out-of-range buff returned error: %v", err)
	}
	if len(target.ActiveBuffSnapshot()) != 0 || target.BonusStats.Str != 0 {
		t.Fatalf("out-of-range buff mutated target: active=%d strength=%d", len(target.ActiveBuffSnapshot()), target.BonusStats.Str)
	}
}

func TestHealingTargetOutsideAuthoredRangeIsRejected(t *testing.T) {
	caster := &model.Player{
		ID: 312, SRObject: model.SRObject{UniqueID: 115, RtNavmeshPosition: combatTestPosition(24231, 0, 0, 100)},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	target := &model.Player{
		ID: 313, SRObject: model.SRObject{UniqueID: 116, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
	}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	handler := &CombatHandler{vitals: vitals, send: func(*server.Session, network.Packet) error { return nil }}
	skill := model.RefSkill{ID: 9025, BasicActivity: 2, TargetAlly: true, HealHP: 50, ActionRange: 10, ConsumeMP: 10}
	if err := handler.executeHealing(context.Background(), &server.Session{}, caster, target, skill); err != nil {
		t.Fatalf("out-of-range healing returned error: %v", err)
	}
	if vitals.spendCalls != 0 || target.CurrentHP() != 100 {
		t.Fatalf("out-of-range healing mutated state: spends=%d hp=%d", vitals.spendCalls, target.CurrentHP())
	}
}

func TestPlayerTargetSkillDisplacementPublishesEndpoint(t *testing.T) {
	source := &model.Player{SRObject: model.SRObject{UniqueID: 105, RtNavmeshPosition: combatTestPosition(24231, 0, 0, 100)}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 205, RtNavmeshPosition: combatTestPosition(24231, 10, 0, 100)}}
	world := &combatWorldFake{player: source}
	record := model.CombatDamageRecord{AttackState: model.DamageAttackStateNormal}
	handler := &CombatHandler{world: world}
	if !handler.applyPlayerSkillDisplacement(source, target, model.RefSkill{KnockbackDistance: 5}, &record) {
		t.Fatal("player displacement was not applied")
	}
	if record.AttackState != model.DamageResultKnockback || target.GetNavmeshPosition().Offset.X != 15 || record.EndpointX != 15 {
		t.Fatalf("displacement state=%d target=%v endpoint=%v, want knockback at x=15", record.AttackState, target.GetNavmeshPosition(), record.EndpointX)
	}
}

func TestPlayerTargetSkillDisplacementInterruptsCommittedCombat(t *testing.T) {
	source := &model.Player{SRObject: model.SRObject{UniqueID: 105, RtNavmeshPosition: combatTestPosition(24231, 0, 0, 100)}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 205, RtNavmeshPosition: combatTestPosition(24231, 10, 0, 100)}}
	cancelled := false
	handler := &CombatHandler{
		world:       &combatWorldFake{player: source},
		rollPercent: func(int) bool { return true },
		send:        func(*server.Session, network.Packet) error { return nil },
		pending:     map[uint32]*pendingAttack{target.GetUniqueID(): {}},
		repeating:   map[uint32]*pendingAttack{target.GetUniqueID(): {}},
		delayedCasts: map[uint32]delayedCast{target.GetUniqueID(): {
			token: 1, cancel: func() { cancelled = true },
		}},
	}
	record := model.CombatDamageRecord{AttackState: model.DamageAttackStateNormal}
	if !handler.applyPlayerSkillDisplacement(source, target, model.RefSkill{KnockbackChance: 100, KnockbackDistance: 5}, &record) {
		t.Fatal("player displacement was not applied")
	}
	if !cancelled || handler.pending[target.GetUniqueID()] != nil || handler.repeating[target.GetUniqueID()] != nil {
		t.Fatalf("displaced player combat survived: cancelled=%t pending=%t repeating=%t", cancelled, handler.pending[target.GetUniqueID()] != nil, handler.repeating[target.GetUniqueID()] != nil)
	}
}

func TestPlayerTargetSkillResolvesAfterAuthoredCastTime(t *testing.T) {
	attacker := &model.Player{SRObject: model.SRObject{UniqueID: 101}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 201}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	var scheduled func()
	handler := &CombatHandler{
		send:       func(*server.Session, network.Packet) error { return nil },
		schedule:   func(_ time.Duration, fn func()) { scheduled = fn },
		blockRoll:  func(int) bool { return false },
		damageRoll: func() int { return 0 },
	}
	skill := model.RefSkill{ID: 9004, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, ActionCastingTime: 25}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("delayed player-target skill failed: %v", err)
	}
	if scheduled == nil {
		t.Fatal("cast did not schedule impact")
	}
	if target.CurrentHP() != 1000 {
		t.Fatalf("target HP before impact = %d, want 1000", target.CurrentHP())
	}
	scheduled()
	if target.CurrentHP() >= 1000 {
		t.Fatalf("target HP after impact = %d, want damage", target.CurrentHP())
	}
}

func TestCancelledDelayedPlayerSkillDoesNotApplyAtImpact(t *testing.T) {
	attacker := &model.Player{SRObject: model.SRObject{UniqueID: 111}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 211}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	var scheduled func()
	var broadcasts []network.Packet
	handler := &CombatHandler{
		send:     func(*server.Session, network.Packet) error { return nil },
		schedule: func(_ time.Duration, fn func()) { scheduled = fn },
		broadcast: func(_ *model.Player, packet *network.Packet) {
			if packet != nil {
				broadcasts = append(broadcasts, *packet)
			}
		},
		blockRoll:  func(int) bool { return false },
		damageRoll: func() int { return 0 },
	}
	skill := model.RefSkill{ID: 9014, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, ActionCastingTime: 25}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("delayed player-target skill failed: %v", err)
	}
	if scheduled == nil {
		t.Fatal("cast did not schedule impact")
	}
	handler.CancelPlayerAttack(nil, attacker.GetUniqueID())
	scheduled()
	if target.CurrentHP() != 1000 {
		t.Fatalf("cancelled cast damaged target: HP=%d, want 1000", target.CurrentHP())
	}
	foundEnd := false
	for _, packet := range broadcasts {
		if packet.MessageID == opcode.EntitySkillCastEnd {
			foundEnd = true
			break
		}
	}
	if !foundEnd {
		t.Fatal("cancelled cast did not publish B071 completion/interruption")
	}
}

func TestDelayedPlayerSkillEndsWhenTargetDiesBeforeImpact(t *testing.T) {
	attacker := &model.Player{SRObject: model.SRObject{UniqueID: 121}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 221}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	var scheduled func()
	var broadcasts []network.Packet
	handler := &CombatHandler{
		send:     func(*server.Session, network.Packet) error { return nil },
		schedule: func(_ time.Duration, fn func()) { scheduled = fn },
		broadcast: func(_ *model.Player, packet *network.Packet) {
			if packet != nil {
				broadcasts = append(broadcasts, *packet)
			}
		},
		blockRoll:  func(int) bool { return false },
		damageRoll: func() int { return 0 },
	}
	skill := model.RefSkill{ID: 9024, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, ActionCastingTime: 25}
	if err := handler.executePlayerSkillAttack(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("delayed player-target skill failed: %v", err)
	}
	if scheduled == nil {
		t.Fatal("cast did not schedule impact")
	}
	target.SetLifeState(model.Dead)
	scheduled()
	if target.CurrentHP() != 1000 {
		t.Fatalf("dead target was damaged at delayed impact: HP=%d", target.CurrentHP())
	}
	foundEnd := false
	for _, packet := range broadcasts {
		if packet.MessageID == opcode.EntitySkillCastEnd {
			foundEnd = true
			break
		}
	}
	if !foundEnd {
		t.Fatal("dead-target delayed cast did not publish B071 completion")
	}
}

func TestPlayerTargetStatusSkillPublishesAndAppliesEffect(t *testing.T) {
	attacker := &model.Player{SRObject: model.SRObject{UniqueID: 102}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 202}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	attacker.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	skill := model.RefSkill{ID: 9005, TargetEnemyPVP: true, TargetRequired: true, BasicActivity: 1, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusStun, Chance: 100, DurationMS: 1000, Level: 1}}}
	world := &combatWorldFake{player: attacker}
	handler := &CombatHandler{world: world, send: func(*server.Session, network.Packet) error { return nil }, rollPercent: func(int) bool { return true }}
	if err := handler.executeTargetedPlayerStatusSkill(context.Background(), &server.Session{}, attacker, target, skill); err != nil {
		t.Fatalf("player-target status skill failed: %v", err)
	}
	if _, active := target.CombatStatus(model.CombatStatusStun, time.Now()); !active {
		t.Fatal("status skill did not install stun")
	}
	if world.stops != 1 {
		t.Fatalf("status application stops = %d, want 1", world.stops)
	}
}
func TestPlayerBadEffectPacketsIncludeLocalStatusSnapshot(t *testing.T) {
	now := time.Unix(100, 0)
	target := &model.Player{SRObject: model.SRObject{UniqueID: 202}}
	target.SetLifeState(model.Alive)
	if !target.ApplyCombatStatusAt(model.CombatStatus{
		Kind:      model.CombatStatusStun,
		Level:     7,
		AppliedAt: now,
		ExpiresAt: now.Add(10 * time.Second),
	}, now) {
		t.Fatal("could not install stun status")
	}

	update, snapshot := buildPlayerBadEffectPackets(target, now)
	if update.MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("visible status opcode = %04X, want %04X", update.MessageID, opcode.EntityUpdateStatus)
	}
	if snapshot.MessageID != opcode.ActionUnknownUpdate2 {
		t.Fatalf("local status opcode = %04X, want %04X", snapshot.MessageID, opcode.ActionUnknownUpdate2)
	}
	if got, want := snapshot.Payload(), []byte{0, 0x40, 0, 0, 100, 0, 0, 0, 0, 0, 7, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("local status snapshot = % X, want % X", got, want)
	}
}

func TestFearImmediatelyInterruptsCommittedPlayerAction(t *testing.T) {
	now := time.Unix(100, 0)
	source := &model.Player{SRObject: model.SRObject{UniqueID: 102}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 202}}
	target.SetLifeState(model.Alive)
	cancelled := false
	actionStates := 0
	handler := &CombatHandler{
		delayedCasts: map[uint32]delayedCast{
			target.GetUniqueID(): {token: 1, cancel: func() { cancelled = true }},
		},
		send: func(*server.Session, network.Packet) error {
			actionStates++
			return nil
		},
		rollPercent: func(int) bool { return true },
	}
	skill := model.RefSkill{ID: 9006, StatusEffects: []model.SkillStatusEffect{{
		Kind: model.CombatStatusFear, Chance: 100, DurationMS: 1000, Level: 1,
	}}}
	handler.applyPlayerSkillStatuses(target, source, skill, now)
	if !cancelled {
		t.Fatal("Fear did not cancel the committed player action")
	}
	if _, exists := handler.delayedCasts[target.GetUniqueID()]; exists {
		t.Fatal("Fear left the committed player action registered")
	}
	if actionStates != 1 {
		t.Fatalf("Fear action-state failures = %d, want 1", actionStates)
	}
}

func TestAreaPlayerStatusSkillTargetsEnemyPlayers(t *testing.T) {
	caster := &model.Player{SRObject: model.SRObject{UniqueID: 103, RtNavmeshPosition: combatTestPosition(24231, 0, 0, 100)}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 203, RtNavmeshPosition: combatTestPosition(24231, 10, 0, 100)}, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20}, Level: 10}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	caster.KnownObjectList = model.NewKnownObjectList(caster)
	caster.KnownObjectList.AddObject(target)
	handler := &CombatHandler{send: func(*server.Session, network.Packet) error { return nil }, rollPercent: func(int) bool { return true }}
	skill := model.RefSkill{ID: 9006, TargetEnemyPVP: true, BasicActivity: 1, TransferRange: 20, TransferCount: 1, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusStun, Chance: 100, DurationMS: 1000, Level: 1}}}
	if err := handler.executeAreaStatusSkill(context.Background(), &server.Session{}, caster, skill); err != nil {
		t.Fatalf("area player status skill failed: %v", err)
	}
	if _, active := target.CombatStatus(model.CombatStatusStun, time.Now()); !active {
		t.Fatal("area status skill did not install stun on enemy player")
	}
}

func TestPlayerAbnormalDamageTicksAfterPvPStatus(t *testing.T) {
	now := time.Unix(1000, 0)
	source := &model.Player{SRObject: model.SRObject{UniqueID: 320}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 321}, BaseStats: model.BaseStats{HP: 1000, MP: 100}}
	linked := &model.Player{SRObject: model.SRObject{UniqueID: 322}, BaseStats: model.BaseStats{HP: 1000, MP: 100}}
	source.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	linked.SetLifeState(model.Alive)
	source.KnownObjectList = model.NewKnownObjectList(source)
	source.KnownObjectList.AddObject(linked)
	status := model.CombatStatus{Kind: model.CombatStatusBurn, Token: 77, SkillID: 9101, SourceUID: source.GetUniqueID(), Payload: 120, ExpiresAt: now.Add(11 * time.Second)}
	if !target.ApplyCombatStatus(status) {
		t.Fatal("failed to install PvP abnormal status")
	}
	if _, ok := target.CommitLinkedActiveBuff(9100, 78, linked.GetUniqueID(), 0, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("failed to install linked damage buff for abnormal tick")
	}
	var tick func()
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		9100: {ID: 9100, LinkedDamageMask: 15, LinkedDamagePercent: 100, LinkedDamageUseLimit: 1},
	}})
	handler := &CombatHandler{referenceData: catalog, now: func() time.Time { return now }, schedule: func(_ time.Duration, fn func()) { tick = fn }}
	handler.schedulePlayerStatusDamageTick(source, target, status, now.Add(5*time.Second))
	if tick == nil {
		t.Fatal("abnormal damage tick was not scheduled")
	}
	tick()
	if target.CurrentHP() != 1000 || linked.CurrentHP() != 880 {
		t.Fatalf("abnormal tick HP = target:%d linked:%d, want target:1000 linked:880", target.CurrentHP(), linked.CurrentHP())
	}
}

func TestBloodTrapNaturalExpiryAppliesSubtype4WithSourceAndTokenGuard(t *testing.T) {
	now := time.Unix(1000, 0)
	source := &model.Player{SRObject: model.SRObject{UniqueID: 320}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 321}, BaseStats: model.BaseStats{HP: 1000, MP: 100}}
	source.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	status := model.CombatStatus{Kind: model.CombatStatusBloodTrap, Token: 77, SkillID: 9101, SourceUID: source.GetUniqueID(), Payload: 120, ExpiresAt: now.Add(time.Second)}
	if !target.ApplyCombatStatusAt(status, now) {
		t.Fatal("failed to install blood-trap status")
	}
	var expiry func()
	var sent []network.Packet
	handler := &CombatHandler{
		world:     &combatWorldFake{player: source},
		now:       func() time.Time { return now },
		schedule:  func(_ time.Duration, fn func()) { expiry = fn },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	handler.schedulePlayerStatusExpiration(target, status, false)
	if expiry == nil {
		t.Fatal("blood-trap expiry was not scheduled")
	}
	now = status.ExpiresAt
	expiry()
	if got := target.CurrentHP(); got != 880 {
		t.Fatalf("blood-trap HP = %d, want 880", got)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.EntitySkillApplicationResponse {
		t.Fatalf("subtype-4 packets = %+v", sent)
	}
	if got, want := sent[0].Payload(), []byte{4, 0, 0x41, 0x01, 0, 0, 120, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subtype-4 payload = % X, want % X", got, want)
	}

	replacement := model.CombatStatus{Kind: model.CombatStatusBloodTrap, Token: 78, SkillID: 9101, SourceUID: source.GetUniqueID(), Payload: 300, ExpiresAt: now.Add(time.Second)}
	if !target.ApplyCombatStatusAt(replacement, now) {
		t.Fatal("failed to install blood-trap replacement")
	}
	sent = nil
	handler.schedulePlayerStatusExpiration(target, status, false)
	expiry()
	if got := target.CurrentHP(); got != 880 || len(sent) != 0 {
		t.Fatalf("old token detonated replacement: HP=%d packets=%d", got, len(sent))
	}
}

func TestNPCAbnormalDamageTickDoesNotReplayPlayerAttackAnimation(t *testing.T) {
	now := time.Unix(1000, 0)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 320}}
	target := combatEffectNPC(321, 9000, 1000)
	status := model.CombatStatus{Kind: model.CombatStatusBurn, Token: 77, SkillID: 8214, SourceUID: player.GetUniqueID(), Payload: 10, ExpiresAt: now.Add(11 * time.Second)}
	if !target.ApplyCombatStatus(status) {
		t.Fatal("failed to install NPC abnormal status")
	}
	var tick func()
	var sent network.Packet
	handler := &CombatHandler{
		now:      func() time.Time { return now },
		schedule: func(_ time.Duration, fn func()) { tick = fn },
		broadcast: func(_ *model.Player, packet *network.Packet) {
			sent = *packet
		},
	}
	handler.scheduleStatusDamageTick(&server.Session{}, player, target, status, now.Add(5*time.Second))
	if tick == nil {
		t.Fatal("NPC abnormal damage tick was not scheduled")
	}
	tick()
	decoded, err := vsro188.DecodeCombatAttackBeginResponse(sent)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SourceUID != target.GetUniqueID() {
		t.Fatalf("periodic damage source UID = %d, want afflicted target %d", decoded.SourceUID, target.GetUniqueID())
	}
}

func TestPlayerAttackRatingDoesNotCreateSyntheticMisses(t *testing.T) {
	now := time.Now()
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(210, 9000, 1000)
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Characters: map[uint32]model.RefChar{
		9000: {RefObject: model.RefObject{ID: 9000}, EvasionRate: 100},
	}})
	handler := &CombatHandler{
		referenceData:  catalog,
		hitRollPercent: func(int) bool { return false },
	}

	result := handler.resolvePlayerDamage(player, target, 500, false, false, now)
	if result.attackState != model.DamageAttackStateNormal || result.wireDamage != 500 || result.appliedDamage != 500 || result.killed {
		t.Fatalf("damage result = %+v, want landed 500 damage", result)
	}
	if hp := target.CurrentHitPoints(); hp != 500 {
		t.Fatalf("target HP = %d, want 500", hp)
	}
}

func TestRecoveredAggroIncreaseScalesDamageThreatThenAddsFlat(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(210, 9000, 1000)
	handler := &CombatHandler{}
	result := handler.applyPlayerDamageResultWithAggro(player, target, 200, 35, 25)
	if result.appliedDamage != 200 {
		t.Fatalf("applied damage = %d, want 200", result.appliedDamage)
	}
	if got := target.AggroValue(player.GetUniqueID()); got != 285 {
		t.Fatalf("threat = %d, want round(200 * 1.25) + 35 = 285", got)
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

func TestDiseaseRaisesPlayerStatusChanceAgainstMob(t *testing.T) {
	now := time.Now()
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	target := combatEffectNPC(205, 9000, 1000)
	if !target.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusDisease, Token: 1, SkillID: 1, Power: 25, ExpiresAt: now.Add(time.Minute),
	}, now) {
		t.Fatal("apply Disease")
	}
	var chance int
	handler := &CombatHandler{
		rollPercent: func(value int) bool { chance = value; return true },
		broadcast:   func(*model.Player, *network.Packet) {},
	}
	skill := model.RefSkill{ID: 8416, StatusEffects: []model.SkillStatusEffect{{
		Kind: model.CombatStatusStun, Chance: 60, DurationMS: 5000,
	}}}
	handler.applySkillStatuses(nil, player, target, skill, now)
	if chance != 85 {
		t.Fatalf("Disease-adjusted chance = %d, want 85", chance)
	}
	if _, active := target.CombatStatus(model.CombatStatusStun, now); !active {
		t.Fatal("Disease-adjusted status was not applied")
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

func TestGenericBuffUpkeepDebitsMPAndCancelsWhenDebitFails(t *testing.T) {
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7,
		BaseStats: model.BaseStats{HP: 100, MP: 100},
	}
	player.SetLifeState(model.Alive)
	const token = uint32(55)
	if _, ok := player.CommitActiveBuff(900, token, 0, model.BuffModifiers{Strength: 3}); !ok {
		t.Fatal("failed to arrange indefinite keep-up buff")
	}
	vitals := &combatVitalsFake{spent: true}
	var callbacks []func()
	var sent []network.Packet
	handler := &CombatHandler{
		vitals: vitals,
		schedule: func(delay time.Duration, callback func()) {
			if delay != 5*time.Second {
				t.Fatalf("upkeep delay = %s, want 5s", delay)
			}
			callbacks = append(callbacks, callback)
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	skill := model.RefSkill{ID: 900, UpkeepIntervalMS: 5000, UpkeepMPCost: 41}
	handler.scheduleBuffUpkeep(player, skill, token)
	callbacks[0]()
	if vitals.spendCalls != 1 || vitals.expectedMP != 100 || vitals.cost != 41 {
		t.Fatalf("first upkeep debit = calls:%d expected:%d cost:%d", vitals.spendCalls, vitals.expectedMP, vitals.cost)
	}
	if _, mp := player.CurrentVitals(); mp != 59 {
		t.Fatalf("MP after upkeep = %d, want 59", mp)
	}
	if len(player.ActiveBuffSnapshot()) != 1 || len(callbacks) != 2 {
		t.Fatalf("active buff/callback count = %d/%d, want 1/2", len(player.ActiveBuffSnapshot()), len(callbacks))
	}

	vitals.spent = false
	callbacks[1]()
	if len(player.ActiveBuffSnapshot()) != 0 || player.BonusStats.Str != 0 {
		t.Fatalf("failed debit did not remove buff: buffs=%+v strength=%d", player.ActiveBuffSnapshot(), player.BonusStats.Str)
	}
	if len(sent) < 3 || sent[len(sent)-2].MessageID != opcode.EntityBuffRemove || sent[len(sent)-1].MessageID != opcode.EntityUpdateStats {
		t.Fatalf("teardown packets = %04X", packetOpcodesForCombat(sent))
	}
}

func TestActiveBuffAdmissionUsesRecoveredOverlapAndRankRules(t *testing.T) {
	const oldToken = uint32(77)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	if _, ok := player.CommitActiveBuff(10, oldToken, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("failed to arrange active buff")
	}
	handler := &CombatHandler{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		10: {ID: 10, GroupID: 5, BasicLevel: 3, ActionOverlap: 900, SecondaryOverlap: 901, DamageTypeFamily: 22, DamageTypePrecedence: 4},
	}})}

	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 11, MatchMode: 1, ActionOverlap: 901}); ok || len(replacements) != 0 {
		t.Fatalf("ordinary overlap admitted: replacements=%v admitted=%t", replacements, ok)
	}
	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 12, DamageTypeFamily: 22, DamageTypePrecedence: 3}); ok || len(replacements) != 0 {
		t.Fatalf("lower dttp rank admitted: replacements=%v admitted=%t", replacements, ok)
	}
	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 13, DamageTypeFamily: 22, DamageTypePrecedence: 4}); !ok || !reflect.DeepEqual(replacements, []uint32{oldToken}) {
		t.Fatalf("equal dttp rank = replacements:%v admitted:%t", replacements, ok)
	}
	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 14, GroupID: 5, BasicLevel: 2}); ok || len(replacements) != 0 {
		t.Fatalf("lower same-group level admitted: replacements=%v admitted=%t", replacements, ok)
	}
	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 15, GroupID: 5, BasicLevel: 4}); !ok || !reflect.DeepEqual(replacements, []uint32{oldToken}) {
		t.Fatalf("higher same-group level = replacements:%v admitted:%t", replacements, ok)
	}
	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 16, GroupID: 5, BasicLevel: 4, ReferenceDescriptors: [4][6]int{2: {2, 1, 100}}}); !ok || !reflect.DeepEqual(replacements, []uint32{oldToken}) {
		t.Fatalf("unresolved refr-2 fallback = replacements:%v admitted:%t", replacements, ok)
	}
	if replacements, ok := handler.activeBuffAdmission(player, model.RefSkill{ID: 17, GroupID: 5, BasicLevel: 4, LinkedSameGroup: true}); !ok || len(replacements) != 0 {
		t.Fatalf("lks2 suppression = replacements:%v admitted:%t", replacements, ok)
	}
}

func TestSkillDisplacementUsesAuthoredKBAndFixedKOEndpoints(t *testing.T) {
	region := navmeshv2.NewRegionFromInt16(25000)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(0, 2, 0)})
	target := combatEffectNPC(200, 9000, 1000)
	target.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(10, 2, 0)})
	world := &combatWorldFake{player: player}
	interruptedUID := uint32(0)
	handler := &CombatHandler{world: world, interruptMob: func(npc *model.NPC) {
		interruptedUID = npc.GetUniqueID()
	}}
	// Skill impact runs under the combat mutex. Displacement must not attempt
	// to acquire that same mutex recursively before it can emit B071.
	handler.mu.Lock()
	defer handler.mu.Unlock()

	kb := model.CombatDamageRecord{}
	if !handler.applySkillDisplacement(player, target, model.RefSkill{KnockbackDistance: 35}, &kb) {
		t.Fatal("authored knockback was not applied")
	}
	if kb.AttackState != model.DamageResultKnockback || kb.EndpointRegion != uint16(region.ID) || kb.EndpointX != 45 || kb.EndpointY != 2 || kb.EndpointZ != 0 {
		t.Fatalf("knockback result = %+v", kb)
	}
	if interruptedUID != target.GetUniqueID() {
		t.Fatalf("interrupted NPC UID = %d, want %d", interruptedUID, target.GetUniqueID())
	}

	ko := model.CombatDamageRecord{}
	if !handler.applySkillDisplacement(player, target, model.RefSkill{KnockOverKey: 1, KnockbackDistance: 99}, &ko) {
		t.Fatal("knock-over was not applied")
	}
	if ko.AttackState != model.DamageResultKnockOver || ko.EndpointX != 65 {
		t.Fatalf("fixed 20-unit knock-over result = %+v", ko)
	}
}

func TestSkillKnockbackUsesAuthoredChanceAndDistance(t *testing.T) {
	region := navmeshv2.NewRegionFromInt16(25000)
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}}
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(0, 2, 0)})
	target := combatEffectNPC(200, 9000, 1000)
	target.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(10, 2, 0)})
	world := &combatWorldFake{player: player}
	rolls := 0
	handler := &CombatHandler{world: world, rollPercent: func(chance int) bool {
		rolls++
		return chance == 30
	}}

	record := model.CombatDamageRecord{}
	if !handler.applySkillDisplacement(player, target, model.RefSkill{KnockbackChance: 30, KnockbackDistance: 20}, &record) {
		t.Fatal("authored knockback roll did not displace target")
	}
	if rolls != 1 || record.EndpointX != 30 {
		t.Fatalf("knockback rolls=%d endpoint=%v, want 1/30", rolls, record.EndpointX)
	}

	target.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(10, 2, 0)})
	handler.rollPercent = func(chance int) bool { return false }
	if handler.applySkillDisplacement(player, target, model.RefSkill{KnockbackChance: 30, KnockbackDistance: 20}, &record) {
		t.Fatal("failed authored knockback roll displaced target")
	}
	if target.GetNavmeshPosition().Offset.X != 10 {
		t.Fatalf("failed knockback moved target to x=%v", target.GetNavmeshPosition().Offset.X)
	}
}

func TestReferenceDiscriminatorTwoPropagatesBuffWithinAuthoredRadius(t *testing.T) {
	region := navmeshv2.NewRegionFromInt16(25000)
	newPlayer := func(uid uint32, x float32) *model.Player {
		player := &model.Player{SRObject: model.NewSRObject(), BaseStats: model.BaseStats{HP: 100, MP: 100}}
		player.SetUniqueID(uid)
		player.SetLifeState(model.Alive)
		player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(x, 0, 0)})
		player.CharKnownObjectList = model.NewCharKnownObjectList(player)
		player.KnownObjectList = player.CharKnownObjectList
		return player
	}
	caster, nearby, far := newPlayer(100, 0), newPlayer(101, 75), newPlayer(102, 175)
	caster.KnownObjectList.AddObject(nearby)
	caster.KnownObjectList.AddObject(far)
	var scheduled []time.Duration
	var packets []network.Packet
	handler := &CombatHandler{
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{}),
		schedule:      func(delay time.Duration, _ func()) { scheduled = append(scheduled, delay) },
		send:          func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil },
		broadcast:     func(_ *model.Player, packet *network.Packet) { packets = append(packets, *packet) },
	}
	skill := model.RefSkill{ID: 900, ReferenceDescriptors: [4][6]int{2: {2, 0, 100}}, ReferenceRadius: 100, BuffDuration: 15000, BuffModifiers: model.BuffModifiers{Strength: 4}}
	if err := handler.propagateReferenceBuff(caster, caster, skill, 700, 15*time.Second); err != nil {
		t.Fatalf("propagateReferenceBuff: %v", err)
	}
	nearbyBuffs := nearby.ActiveBuffSnapshot()
	if len(nearbyBuffs) != 1 || nearbyBuffs[0].SkillID != skill.ID || nearbyBuffs[0].SourceUID != caster.GetUniqueID() || nearbyBuffs[0].ParentToken != 700 || nearby.BonusStats.Str != 4 {
		t.Fatalf("nearby propagated buff = %+v strength=%d", nearbyBuffs, nearby.BonusStats.Str)
	}
	if len(far.ActiveBuffSnapshot()) != 0 || far.BonusStats.Str != 0 {
		t.Fatalf("out-of-radius player received buff: %+v", far.ActiveBuffSnapshot())
	}
	if !reflect.DeepEqual(scheduled, []time.Duration{15 * time.Second}) {
		t.Fatalf("propagated schedules = %v", scheduled)
	}
	if got := packetOpcodesForCombat(packets); !reflect.DeepEqual(got, []uint16{opcode.EntityBuffAdd, opcode.EntityUpdateStats}) {
		t.Fatalf("propagated packets = %04X", got)
	}
	// Parent ownership survives spatial changes; teardown must not depend on
	// discovering the child in a fresh radius search.
	nearby.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(500, 0, 0)})
	handler.removeBuff(caster, 700)
	if len(nearby.ActiveBuffSnapshot()) != 0 || nearby.BonusStats.Str != 0 || len(handler.linkedBuffs) != 0 || len(handler.linkedParents) != 0 {
		t.Fatalf("linked teardown left child=%+v strength=%d links=%v/%v", nearby.ActiveBuffSnapshot(), nearby.BonusStats.Str, handler.linkedBuffs, handler.linkedParents)
	}
	if got := packetOpcodesForCombat(packets); !reflect.DeepEqual(got, []uint16{opcode.EntityBuffAdd, opcode.EntityUpdateStats, opcode.EntityBuffRemove, opcode.EntityUpdateStats}) {
		t.Fatalf("linked teardown packets = %04X", got)
	}
}

func TestDetectionDescriptorDoesNotPropagateCasterBuff(t *testing.T) {
	region := navmeshv2.NewRegionFromInt16(25000)
	caster := &model.Player{SRObject: model.NewSRObject(), BaseStats: model.BaseStats{HP: 100, MP: 100}}
	nearby := &model.Player{SRObject: model.NewSRObject(), BaseStats: model.BaseStats{HP: 100, MP: 100}}
	for uid, player := range map[uint32]*model.Player{100: caster, 101: nearby} {
		player.SetUniqueID(uid)
		player.SetLifeState(model.Alive)
		player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(0, 0, 0)})
		player.KnownObjectList = model.NewKnownObjectList(player)
	}
	caster.KnownObjectList.AddObject(nearby)
	handler := &CombatHandler{}
	skill := model.RefSkill{ID: 901, DetectionType: 5, DetectionLevel: 9, ReferenceDescriptors: [4][6]int{2: {2, 1, 150}}, ReferenceRadius: 150, BuffDuration: 60000}
	if err := handler.propagateReferenceBuff(caster, caster, skill, 701, time.Minute); err != nil {
		t.Fatalf("detect propagation: %v", err)
	}
	if buffs := nearby.ActiveBuffSnapshot(); len(buffs) != 0 {
		t.Fatalf("nearby player received caster detection actor: %+v", buffs)
	}
}

func TestSyncHiddenBodyStateMapsNativeHideTypesAndRemoval(t *testing.T) {
	now := time.Now()
	player := &model.Player{LifeState: model.Alive, BodyState: model.Normal}
	player.SetUniqueID(100)
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		901: {ID: 901, HideType: 1, HideLevel: 6},
	}})
	handler := &CombatHandler{referenceData: catalog, now: func() time.Time { return now }}
	if _, ok := player.CommitActiveBuff(901, 701, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("commit hide buff")
	}
	handler.syncHiddenBodyState(player)
	if player.GetBodyState() != model.Stealth {
		t.Fatalf("hide type 1 body state = %d, want stealth", player.GetBodyState())
	}
	if !player.RemoveActiveBuff(701) {
		t.Fatal("remove hide buff")
	}
	handler.syncHiddenBodyState(player)
	if player.GetBodyState() != model.Normal {
		t.Fatalf("removed hide body state = %d, want normal", player.GetBodyState())
	}
}

func TestClericPraiseIncreasesHealingSkillOutput(t *testing.T) {
	now := time.Unix(100, 0)
	caster := &model.Player{
		ID: 401, SRObject: model.SRObject{UniqueID: 401},
		BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20},
		Skills:    map[uint32]byte{11665: 1},
	}
	caster.SetLifeState(model.Alive)
	caster.RecalculatePassiveSkillModifiers(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Skills: map[uint32]model.RefSkill{
			11665: {ID: 11665, BasicActivity: 0, PassiveHealingPercent: 20},
		},
	}))
	target := &model.Player{ID: 402, SRObject: model.SRObject{UniqueID: 402}, BaseStats: model.BaseStats{HP: 100, MP: 100, Str: 20, Int: 20}}
	target.SetLifeState(model.Alive)
	target.ApplyCombatDamage(50, false)
	vitals := &combatVitalsFake{spent: true}
	handler := &CombatHandler{now: func() time.Time { return now }, vitals: vitals, send: func(*server.Session, network.Packet) error { return nil }}
	skill := model.RefSkill{ID: 9026, BasicActivity: 2, TargetAlly: true, HealHP: 40}
	if err := handler.executeHealing(context.Background(), &server.Session{}, caster, target, skill); err != nil {
		t.Fatalf("Cleric healing cast failed: %v", err)
	}
	if got := target.CurrentHP(); got != 98 {
		t.Fatalf("Cleric Praise healing HP = %d, want 98", got)
	}
}

func TestClericPraiseAmplifiesForceAndMentalBlessings(t *testing.T) {
	caster := &model.Player{Skills: map[uint32]byte{11848: 1}}
	caster.RecalculatePassiveSkillModifiers(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Skills: map[uint32]model.RefSkill{
			11848: {ID: 11848, BasicActivity: 0, PassiveBlessingPercent: 15},
		},
	}))
	for _, test := range []struct {
		name  string
		skill model.RefSkill
		want  model.BuffModifiers
	}{
		{"force", model.RefSkill{CodeName: "SKILL_EU_CLERIC_BLESSA_STR_A_01", BuffModifiers: model.BuffModifiers{Strength: 20}}, model.BuffModifiers{Strength: 23}},
		{"mental", model.RefSkill{CodeName: "SKILL_EU_CLERIC_BLESSA_INT_B_01", BuffModifiers: model.BuffModifiers{Intellect: 20}}, model.BuffModifiers{Intellect: 23}},
		{"body", model.RefSkill{CodeName: "SKILL_EU_CLERIC_BLESSA_PHYSICAL_A_01", BuffModifiers: model.BuffModifiers{PhysicalDefense: 20}}, model.BuffModifiers{PhysicalDefense: 20}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := clericBlessingModifiers(caster, test.skill); got != test.want {
				t.Fatalf("modifiers = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPlayerStatusDurationResistanceScalesAndRejectsStatuses(t *testing.T) {
	now := time.Unix(100, 0)
	source := &model.Player{SRObject: model.SRObject{UniqueID: 1}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 2}, Skills: map[uint32]byte{10: 1}}
	target.SetLifeState(model.Alive)
	target.RecalculatePassiveSkillModifiers(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Skills: map[uint32]model.RefSkill{
			10: {ID: 10, BasicActivity: 0, PassiveStatusDurationMask: 1, PassiveStatusDurationPercent: 40},
		},
	}))
	handler := &CombatHandler{rollPercent: func(chance int) bool { return chance == 100 }}
	handler.applyPlayerSkillStatuses(target, source, model.RefSkill{ID: 20, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusFreeze, Chance: 100, DurationMS: 1000}}}, now)
	status, active := target.CombatStatus(model.CombatStatusFreeze, now)
	if !active || !status.ExpiresAt.Equal(now.Add(600*time.Millisecond)) {
		t.Fatalf("Freeze status expiry = %v, active = %v; want %v", status.ExpiresAt, active, now.Add(600*time.Millisecond))
	}
	rejected := &model.Player{SRObject: model.SRObject{UniqueID: 3}, Skills: map[uint32]byte{11: 1}}
	rejected.SetLifeState(model.Alive)
	rejected.RecalculatePassiveSkillModifiers(model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Skills: map[uint32]model.RefSkill{
			11: {ID: 11, BasicActivity: 0, PassiveStatusDurationMask: 1, PassiveStatusDurationPercent: 100},
		},
	}))
	handler.applyPlayerSkillStatuses(rejected, source, model.RefSkill{ID: 21, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusFreeze, Chance: 100, DurationMS: 1000}}}, now)
	if _, active := rejected.CombatStatus(model.CombatStatusFreeze, now); active {
		t.Fatal("100% duration resistance installed Freeze")
	}
}
