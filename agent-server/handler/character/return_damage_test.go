package character

import (
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/g3n/engine/math32"
)

func returnDamagePlayer(uid uint32, x float32) *model.Player {
	player := &model.Player{SRObject: model.SRObject{UniqueID: uid}}
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Offset: math32.NewVector3(x, 0, 0)})
	return player
}

func TestRGMDRangeGatePrecedesKeyedAdmissionAndQueuesPhysicalMagicalReturn(t *testing.T) {
	now := time.Unix(100, 0)
	const skillID = uint32(7001)
	attacker := returnDamagePlayer(100, 0)
	defender := returnDamagePlayer(200, 20)
	if _, ok := defender.CommitActiveBuffAt(skillID, 1, defender.GetUniqueID(), time.Minute, model.BuffModifiers{}, now); !ok {
		t.Fatal("arrange rgmd actor")
	}
	calls := 0
	handler := &CombatHandler{
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, ReturnDamage: model.ReturnDamageDescriptor{ChancePercent: 50, PhysicalPercent: 25, MagicalPercent: 50, MaximumRange: 10}},
		}}),
		returnDamageRoll: func(owner *model.Player, chancePercent int) bool {
			calls++
			return owner == defender && chancePercent == 50
		},
	}
	handler.admitReturnDamage(attacker, defender, 100, 100, now)
	if calls != 0 {
		t.Fatalf("out-of-range keyed admissions = %d, want 0", calls)
	}
	if pending := attacker.DrainReturnDamage(); len(pending) != 0 {
		t.Fatalf("out-of-range queued records = %+v, want none", pending)
	}
	defender.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Offset: math32.NewVector3(9, 0, 0)})
	handler.admitReturnDamage(attacker, defender, 100, 100, now)
	queued := attacker.DrainReturnDamage()
	if calls != 1 || len(queued) != 1 {
		t.Fatalf("in-range admission calls=%d queue=%+v", calls, queued)
	}
	if queued[0].Amount != 75 || queued[0].SourceUID != defender.GetUniqueID() || queued[0].SkillID != skillID {
		t.Fatalf("queued rgmd = %+v, want skill=%d source=%d amount=75", queued[0], skillID, defender.GetUniqueID())
	}
}

func TestRGMDBarrierOnlyHitStillQueuesReturnDamage(t *testing.T) {
	now := time.Unix(100, 0)
	const skillID = uint32(7003)
	attacker := returnDamagePlayer(100, 0)
	attacker.BaseStats.HP = 100
	attacker.SetLifeState(model.Alive)
	defender := returnDamagePlayer(200, 1)
	defender.BaseStats.HP = 100
	defender.SetLifeState(model.Alive)
	if _, ok := defender.CommitActiveBuffAt(skillID, 3, defender.GetUniqueID(), time.Minute, model.BuffModifiers{}, now); !ok {
		t.Fatal("arrange rgmd actor")
	}
	if !defender.CommitBarrier(model.ActiveBarrier{SkillID: 8001, Token: 4, HitPoints: 100, Absorption: 100, Physical: true}) {
		t.Fatal("arrange full physical barrier")
	}
	handler := &CombatHandler{
		referenceData:    model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: {ID: skillID, ReturnDamage: model.ReturnDamageDescriptor{ChancePercent: 100, PhysicalPercent: 50, MaximumRange: 10}}}}),
		returnDamageRoll: func(*model.Player, int) bool { return true },
	}
	application := handler.applyPlayerDamageWithReturn(attacker, defender, 40, false, 40, 0, now)
	if application.AppliedDamage != 0 || application.BarrierDamage != 40 {
		t.Fatalf("barrier application = %+v, want full barrier absorption", application)
	}
	queued := attacker.DrainReturnDamage()
	if len(queued) != 1 || queued[0].Amount != 20 {
		t.Fatalf("barrier-only rgmd queue = %+v, want 20 return", queued)
	}
}

func TestRGMDFullyRedirectedPlayerHitStillQueuesReturn(t *testing.T) {
	now := time.Unix(100, 0)
	const skillID = uint32(7006)
	attacker := returnDamagePlayer(100, 0)
	defender := returnDamagePlayer(200, 1)
	if _, ok := defender.CommitActiveBuffAt(skillID, 4, defender.GetUniqueID(), time.Minute, model.BuffModifiers{}, now); !ok {
		t.Fatal("arrange rgmd actor")
	}
	handler := &CombatHandler{
		referenceData:    model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: {ID: skillID, ReturnDamage: model.ReturnDamageDescriptor{ChancePercent: 100, PhysicalPercent: 50, MaximumRange: 10}}}}),
		returnDamageRoll: func(*model.Player, int) bool { return true },
	}
	application := handler.applyPlayerDamageWithReturn(attacker, defender, 0, false, 40, 0, now)
	if application.AppliedDamage != 0 || application.BarrierDamage != 0 {
		t.Fatalf("redirected direct application = %+v, want no direct damage", application)
	}
	queued := attacker.DrainReturnDamage()
	if len(queued) != 1 || queued[0].Amount != 20 {
		t.Fatalf("fully redirected rgmd queue = %+v, want amount 20", queued)
	}
}

func TestQueuedRGMDExecutesWithDeadDefenderAsSource(t *testing.T) {
	attacker := returnDamagePlayer(100, 0)
	attacker.BaseStats.HP = 20
	attacker.SetLifeState(model.Alive)
	defender := returnDamagePlayer(200, 1)
	defender.SetLifeState(model.Dead)
	if accepted, _ := attacker.QueueReturnDamage(model.PendingReturnDamage{SkillID: 7004, SourceUID: defender.GetUniqueID(), Amount: 25}); !accepted {
		t.Fatal("queue rgmd return")
	}
	var packets []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: defender},
		broadcast: func(_ *model.Player, packet *network.Packet) {
			packets = append(packets, *packet)
		},
	}
	handler.processQueuedReturnDamage(attacker)
	if hp := attacker.CurrentHP(); hp != 0 || attacker.GetLifeState() != model.Dead {
		t.Fatalf("attacker return state = HP:%d life:%v, want dead at 0", hp, attacker.GetLifeState())
	}
	if len(packets) != 2 || packets[0].MessageID != opcode.EntitySkillApplicationResponse {
		t.Fatalf("dead-defender packets = %#v, want B0BC plus life-state update", packets)
	}
	payload := packets[0].Payload()
	if len(payload) != 11 || payload[0] != 1 || payload[1] != 0x5C || payload[5] != 0x80 || payload[10] != 0 {
		t.Fatalf("lethal dead-defender B0BC payload = % X", payload)
	}
}

func TestBloodTrapDepletionPropagatesSubtype4Flag(t *testing.T) {
	source := returnDamagePlayer(320, 0)
	target := returnDamagePlayer(321, 1)
	target.BaseStats.HP = 100
	target.SetLifeState(model.Alive)
	var packets []network.Packet
	handler := &CombatHandler{
		world:     &combatWorldFake{player: source},
		broadcast: func(_ *model.Player, packet *network.Packet) { packets = append(packets, *packet) },
	}
	handler.executeBloodTrapExpiry(target, model.CombatStatus{SourceUID: source.GetUniqueID(), Payload: 120})
	if target.GetLifeState() != model.Dead || len(packets) != 2 {
		t.Fatalf("blood-trap depletion = life:%v packets:%d, want dead and two packets", target.GetLifeState(), len(packets))
	}
	if got, want := packets[0].Payload(), []byte{4, 0x80, 0x41, 0x01, 0, 0, 120, 0}; string(got) != string(want) {
		t.Fatalf("lethal subtype-4 payload = % X, want % X", got, want)
	}
}

func TestRGMDUsesSinglePerAttackerScheduledDrain(t *testing.T) {
	now := time.Unix(100, 0)
	const skillID = uint32(7005)
	attacker := returnDamagePlayer(100, 0)
	defender := returnDamagePlayer(200, 1)
	if _, ok := defender.CommitActiveBuffAt(skillID, 5, defender.GetUniqueID(), time.Minute, model.BuffModifiers{}, now); !ok {
		t.Fatal("arrange rgmd actor")
	}
	var callbacks []func()
	handler := &CombatHandler{
		referenceData:    model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: {ID: skillID, ReturnDamage: model.ReturnDamageDescriptor{ChancePercent: 100, PhysicalPercent: 100, MaximumRange: 10}}}}),
		returnDamageRoll: func(*model.Player, int) bool { return true },
		schedule:         func(_ time.Duration, callback func()) { callbacks = append(callbacks, callback) },
	}
	handler.admitReturnDamage(attacker, defender, 10, 0, now)
	handler.admitReturnDamage(attacker, defender, 10, 0, now)
	if len(callbacks) != 1 {
		t.Fatalf("scheduled drains = %d, want 1", len(callbacks))
	}
	callbacks[0]()
	handler.admitReturnDamage(attacker, defender, 10, 0, now)
	if len(callbacks) != 2 {
		t.Fatalf("scheduled drains after completion = %d, want 2", len(callbacks))
	}
}

func TestRGMDActiveBuffTeardownStopsAdmission(t *testing.T) {
	now := time.Unix(100, 0)
	const skillID = uint32(7002)
	attacker := returnDamagePlayer(100, 0)
	defender := returnDamagePlayer(200, 1)
	if _, ok := defender.CommitActiveBuffAt(skillID, 2, defender.GetUniqueID(), time.Minute, model.BuffModifiers{}, now); !ok {
		t.Fatal("arrange rgmd actor")
	}
	handler := &CombatHandler{
		referenceData:    model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: {ID: skillID, ReturnDamage: model.ReturnDamageDescriptor{ChancePercent: 100, PhysicalPercent: 100, MaximumRange: 10}}}}),
		returnDamageRoll: func(*model.Player, int) bool { return true },
	}
	if !defender.RemoveActiveBuff(2) {
		t.Fatal("remove rgmd actor")
	}
	handler.admitReturnDamage(attacker, defender, 100, 0, now)
	if queued := attacker.DrainReturnDamage(); len(queued) != 0 {
		t.Fatalf("removed rgmd actor queued %+v", queued)
	}
}
