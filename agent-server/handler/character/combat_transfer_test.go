package character

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/server"
)

func transferTestActors(t *testing.T, playerX, primaryX, primaryZ float32, positions ...[2]float32) (*CombatHandler, *model.Player, *model.NPC, []*model.NPC) {
	t.Helper()
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, playerX, 0, 100)}}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	newMob := func(uid uint32, x, z float32) *model.NPC {
		mob := &model.NPC{SRObject: model.SRObject{
			UniqueID: uid, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, x, 0, 100+z),
		}, Type: "NPC", Mutex: &sync.Mutex{}}
		mob.ConfigureCombat(1000, 1)
		mob.SetLifeState(model.Alive)
		player.KnownObjectList.AddObject(mob)
		return mob
	}
	primary := newMob(200, primaryX, primaryZ)
	candidates := make([]*model.NPC, 0, len(positions))
	for index, position := range positions {
		candidates = append(candidates, newMob(uint32(201+index), position[0], position[1]))
	}
	return &CombatHandler{}, player, primary, candidates
}

func TestTransferTargetsUseAuthoredCenters(t *testing.T) {
	handler, player, primary, candidates := transferTestActors(t, 0, 100, 0, [2]float32{10, 0}, [2]float32{105, 0})
	if got := handler.transferTargets(player, primary, model.RefSkill{TransferType: 1, TransferRange: 20}, 2); len(got) != 1 || got[0] != candidates[0] {
		t.Fatalf("type 1 targets = %v, want caster-near mob", transferTargetIDs(got))
	}
	if got := handler.transferTargets(player, primary, model.RefSkill{TransferType: 2, TransferRange: 20}, 2); len(got) != 1 || got[0] != candidates[1] {
		t.Fatalf("type 2 targets = %v, want primary-near mob", transferTargetIDs(got))
	}
}

func TestTransferTargetsChainProgressively(t *testing.T) {
	handler, player, primary, candidates := transferTestActors(t, -20, 0, 0, [2]float32{9, 0}, [2]float32{18, 0}, [2]float32{27, 0})
	got := handler.transferTargets(player, primary, model.RefSkill{TransferType: 6, TransferRange: 10}, 3)
	if len(got) != 3 || got[0] != candidates[0] || got[1] != candidates[1] || got[2] != candidates[2] {
		t.Fatalf("type 6 targets = %v, want progressive chain", transferTargetIDs(got))
	}
}

func TestTransferTargetsDirectionalAndOwnedFamilies(t *testing.T) {
	handler, player, primary, candidates := transferTestActors(t, 0, 20, 0, [2]float32{15, 2}, [2]float32{10, 10})
	for _, transferType := range []int{3, 4} {
		got := handler.transferTargets(player, primary, model.RefSkill{TransferType: transferType, TransferRange: 5}, 2)
		if len(got) != 1 || got[0] != candidates[0] {
			t.Fatalf("type %d targets = %v, want only on-axis mob", transferType, transferTargetIDs(got))
		}
	}
	for _, transferType := range []int{5, 7} {
		if got := handler.transferTargets(player, primary, model.RefSkill{TransferType: transferType, TransferRange: 100}, 2); len(got) != 0 {
			t.Fatalf("type %d used arbitrary visible mobs: %v", transferType, transferTargetIDs(got))
		}
	}
	provided := candidates[:1]
	handler.ConfigureSkillOwnerTargetProvider(func(*model.NPC, model.RefSkill, int) []*model.NPC { return append(provided, candidates[0]) })
	for _, transferType := range []int{5, 7} {
		got := handler.transferTargets(player, primary, model.RefSkill{TransferType: transferType}, 2)
		if len(got) != 1 || got[0] != provided[0] {
			t.Fatalf("type %d provider targets = %v, want provider-owned target", transferType, transferTargetIDs(got))
		}
	}
}

func TestTransferFalloffAccountsForPrimaryHit(t *testing.T) {
	factor := 65 // The primary has already consumed the first 35% step.
	factor = nextTransferFactor(factor, 35)
	if factor != 42 {
		t.Fatalf("first secondary factor = %d, want 42", factor)
	}
	factor = nextTransferFactor(factor, 35)
	if factor != 27 {
		t.Fatalf("second secondary factor = %d, want 27", factor)
	}
	if got := scaleTransferAmount(1000, 27); got != 270 {
		t.Fatalf("scaled transfer amount = %d, want 270", got)
	}
}

func TestHealingTransferTargetsChainAcrossEligiblePartyMembers(t *testing.T) {
	party := &model.Party{Number: 1}
	newPlayer := func(uid uint32, x float32, member bool) *model.Player {
		player := &model.Player{SRObject: model.SRObject{UniqueID: uid, RtNavmeshPosition: combatTestPosition(24231, x, 0, 100)}}
		player.SetLifeState(model.Alive)
		player.KnownObjectList = model.NewKnownObjectList(player)
		if member {
			if err := player.AddToParty(party); err != nil {
				t.Fatalf("add player %d to party: %v", uid, err)
			}
		}
		return player
	}
	caster := newPlayer(100, -5, true)
	primary := newPlayer(200, 9, true)
	second := newPlayer(201, 18, true)
	third := newPlayer(202, 27, true)
	fourth := newPlayer(204, 36, true)
	outsider := newPlayer(203, 10, false)
	for _, player := range []*model.Player{primary, second, third, fourth, outsider} {
		caster.KnownObjectList.AddObject(player)
	}
	skill := model.RefSkill{TargetAlly: true, TargetParty: true, TransferType: 6, TransferRange: 10}
	got := (&CombatHandler{}).healingTransferTargets(caster, primary, skill, 3)
	if len(got) != 3 || got[0] != second || got[1] != third || got[2] != fourth {
		t.Fatalf("chain-heal targets = %v, want progressive party chain", healingTargetIDs(got))
	}
}

func TestHealingTransferTargetMaskControlsCasterAdmission(t *testing.T) {
	party := &model.Party{Number: 2}
	newPlayer := func(uid uint32, x float32) *model.Player {
		player := &model.Player{SRObject: model.SRObject{UniqueID: uid, RtNavmeshPosition: combatTestPosition(24231, x, 0, 100)}}
		player.SetLifeState(model.Alive)
		player.KnownObjectList = model.NewKnownObjectList(player)
		if err := player.AddToParty(party); err != nil {
			t.Fatalf("add player %d to party: %v", uid, err)
		}
		return player
	}
	caster := newPlayer(100, 0)
	primary := newPlayer(200, 8)
	near := newPlayer(201, 12)
	caster.KnownObjectList.AddObject(primary)
	caster.KnownObjectList.AddObject(near)
	base := model.RefSkill{TargetAlly: true, TargetParty: true, TransferType: 2, TransferRange: 20}
	withoutCaster := (&CombatHandler{}).healingTransferTargets(caster, primary, base, 2)
	if len(withoutCaster) != 1 || withoutCaster[0] != near {
		t.Fatalf("mask-zero targets = %v, want only propagated member", healingTargetIDs(withoutCaster))
	}
	base.TransferTargetMask = 1
	withCaster := (&CombatHandler{}).healingTransferTargets(caster, primary, base, 2)
	if len(withCaster) != 2 || withCaster[0] != near || withCaster[1] != caster {
		t.Fatalf("mask-one targets = %v, want nearest propagated member then caster", healingTargetIDs(withCaster))
	}
}

func TestPlayerPVPTransfersUseProgressiveGeometry(t *testing.T) {
	newPlayer := func(uid uint32, x float32) *model.Player {
		player := &model.Player{
			SRObject:  model.SRObject{UniqueID: uid, RtNavmeshPosition: combatTestPosition(24231, x, 0, 100)},
			BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10,
		}
		player.SetLifeState(model.Alive)
		player.KnownObjectList = model.NewKnownObjectList(player)
		return player
	}
	source := newPlayer(100, -20)
	primary := newPlayer(200, 0)
	first := newPlayer(201, 9)
	second := newPlayer(202, 18)
	for _, player := range []*model.Player{primary, first, second} {
		source.KnownObjectList.AddObject(player)
	}
	handler := &CombatHandler{damageRoll: func() int { return 0 }, blockRoll: func(int) bool { return false }}
	skill := model.RefSkill{ID: 9002, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, TransferType: 6, TransferRange: 10, TransferCount: 3}
	if err := handler.applyPlayerPVPTransfers(context.Background(), &server.Session{}, source, primary, skill, time.Now()); err != nil {
		t.Fatalf("pvp transfers failed: %v", err)
	}
	if first.CurrentHP() >= 1000 || second.CurrentHP() >= 1000 {
		t.Fatalf("progressive targets HP = %d,%d, want both damaged", first.CurrentHP(), second.CurrentHP())
	}
}

func TestPlayerPVPTransfersUseDirectionalGeometry(t *testing.T) {
	newPlayer := func(uid uint32, x, z float32) *model.Player {
		player := &model.Player{SRObject: model.SRObject{UniqueID: uid, RtNavmeshPosition: combatTestPosition(24231, x, 0, z)}, BaseStats: model.BaseStats{HP: 1000, Str: 20, Int: 20}, Level: 10}
		player.SetLifeState(model.Alive)
		player.KnownObjectList = model.NewKnownObjectList(player)
		return player
	}
	source := newPlayer(100, 0, 100)
	primary := newPlayer(200, 20, 100)
	onAxis := newPlayer(201, 18, 102)
	offAxis := newPlayer(202, 18, 112)
	for _, player := range []*model.Player{primary, onAxis, offAxis} {
		source.KnownObjectList.AddObject(player)
	}
	handler := &CombatHandler{damageRoll: func() int { return 0 }, blockRoll: func(int) bool { return false }}
	skill := model.RefSkill{ID: 9003, TargetEnemyPVP: true, AttackType: 5, AttackMin: 100, AttackMax: 100, TransferType: 3, TransferRange: 5, TransferCount: 3}
	if err := handler.applyPlayerPVPTransfers(context.Background(), &server.Session{}, source, primary, skill, time.Now()); err != nil {
		t.Fatalf("directional pvp transfers failed: %v", err)
	}
	if onAxis.CurrentHP() >= 1000 || offAxis.CurrentHP() != 1000 {
		t.Fatalf("directional targets HP = %d,%d, want on-axis damage only", onAxis.CurrentHP(), offAxis.CurrentHP())
	}
}

func transferTargetIDs(targets []*model.NPC) []uint32 {
	ids := make([]uint32, len(targets))
	for index, target := range targets {
		ids[index] = target.GetUniqueID()
	}
	return ids
}

func healingTargetIDs(targets []*model.Player) []uint32 {
	ids := make([]uint32, len(targets))
	for index, target := range targets {
		ids[index] = target.GetUniqueID()
	}
	return ids
}
