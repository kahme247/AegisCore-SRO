package character

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
)

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
