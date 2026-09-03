package character

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

type combatWorldFake struct {
	player   *model.Player
	objects  map[uint32]model.ISRObject
	distance float32
	stops    int
	blocked  bool
}

func TestBuildServerInventoryItemAddedMatchesStockB034Layout(t *testing.T) {
	packet := buildServerInventoryItemAdded(0x0D, model.Item{})
	if got, want := packet.Payload(), []byte{1, 0x0E, 0x0D, 0x3C}; !bytes.Equal(got, want) {
		t.Fatalf("server inventory item-added payload = % X, want % X", got, want)
	}
}

type combatVitalsFake struct {
	spendCalls        int
	expectedMP        int
	cost              int
	spent             bool
	saveCalls         int
	savedID           int
	savedHP           int
	savedMP           int
	resurrectionCalls int
	resurrection      bool
	resurrectionArgs  [6]int
}

type growthPetCombatFake struct {
	engagedPlayer *model.Player
	engagedTarget *model.NPC
	engagements   int
	awards        int
}

type rangedAmmoCombatFake struct {
	remaining []uint16
	calls     [][2]int64
}

func (f *rangedAmmoCombatFake) ConsumeEquippedRangedAmmo(_ context.Context, characterID, itemID int64) (uint16, error) {
	f.calls = append(f.calls, [2]int64{characterID, itemID})
	if len(f.remaining) == 0 {
		return 0, model.ErrRangedAmmoUnavailable
	}
	remaining := f.remaining[0]
	f.remaining = f.remaining[1:]
	return remaining, nil
}

func (f *growthPetCombatFake) EngageOwnerTarget(player *model.Player, target *model.NPC) error {
	f.engagedPlayer, f.engagedTarget = player, target
	f.engagements++
	return nil
}

func (f *growthPetCombatFake) AwardOwnerKillExperience(context.Context, *model.Player, *model.NPC) error {
	f.awards++
	return nil
}

func (f *combatVitalsFake) SaveVitals(_ context.Context, id, hp, mp int) error {
	f.saveCalls++
	f.savedID, f.savedHP, f.savedMP = id, hp, mp
	return nil
}
func (*combatVitalsFake) Resurrect(context.Context, int, int, int) (bool, error) {
	return false, nil
}
func (f *combatVitalsFake) SpendMP(_ context.Context, _ int, expectedMP, cost int) (bool, error) {
	f.spendCalls++
	f.expectedMP = expectedMP
	f.cost = cost
	return f.spent, nil
}
func (*combatVitalsFake) SpendHP(context.Context, int, int, int) (bool, error) {
	return true, nil
}
func (f *combatVitalsFake) CastResurrection(_ context.Context, casterID, targetID, expectedMP, cost, hp, mp int) (bool, error) {
	f.resurrectionCalls++
	f.resurrectionArgs = [6]int{casterID, targetID, expectedMP, cost, hp, mp}
	return f.resurrection, nil
}

func (f *combatWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}

func (f *combatWorldFake) GetObjectByUniqueId(uid uint32) (model.ISRObject, error) {
	object, exists := f.objects[uid]
	if !exists {
		return nil, fmt.Errorf("object %d not found", uid)
	}
	return object, nil
}

func (f *combatWorldFake) DistanceTo(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) float32 {
	return f.distance
}

func (*combatWorldFake) RemoveVisibleObject(model.ISRObject) {}

func (f *combatWorldFake) StopMovingCharacter(character model.ICharacter, position navmeshv2.RtNavmeshPosition, reason string, _ bool) bool {
	f.stops++
	movement := character.GetMovementData()
	if movement != nil {
		character.FinalizeMovement(movement.Generation, position, reason)
	}
	return true
}

func (f *combatWorldFake) NewPosFromGlobalCoordinates(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	if global == nil || f.player == nil {
		return navmeshv2.RtNavmeshPosition{}, fmt.Errorf("global position unavailable")
	}
	position := f.player.GetNavmeshPosition()
	position.Offset = math32.NewVector3(
		global.X-float32(position.Region.X)*navmeshv2.RegionWidth,
		global.Y,
		global.Z-float32(position.Region.Y)*navmeshv2.RegionHeight,
	)
	return position, nil
}

func (f *combatWorldFake) CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool {
	return !f.blocked
}

func TestCombatCancelAcceptsSinglePhaseByte(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(2)
	if err := (&CombatHandler{}).handlePacket(context.Background(), nil, packet); err != nil {
		t.Fatalf("single-byte cancel returned error: %v", err)
	}
}

func TestCombatBaseAttackReachIncludesCombatantRadii(t *testing.T) {
	previous := model.RefItems
	t.Cleanup(func() { model.RefItems = previous })
	model.RefItems = map[uint32]model.RefItem{
		179: {RefObject: model.RefObject{ID: 179}, Range: 18},
		180: {RefObject: model.RefObject{ID: 180}, Range: 180},
	}
	player := &model.Player{
		SRObject:  model.SRObject{RefObjectID: 1907},
		Inventory: model.NewInventory(nil),
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{
		RefObjectID: 179,
		TypeInfo:    model.TypeInfo{TypeID1: model.TypeID1Item, TypeID2: model.TypeID2Wearable, TypeID3: model.TypeID3Weapon, TypeID4: 5},
	}}
	target := &model.NPC{SRObject: model.SRObject{RefObjectID: 1933}}
	handler := &CombatHandler{referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{
			1907: {RefObject: model.RefObject{ID: 1907, BCRadius: 4}},
			1933: {RefObject: model.RefObject{ID: 1933, BCRadius: 6}},
		},
	})}

	if got, want := handler.baseAttackReach(player, target), float32(33); got != want {
		t.Fatalf("glaive attack reach = %g, want %g", got, want)
	}
	if got, want := handler.skillAttackReach(player, target, model.RefSkill{ActionRange: 150}), float32(183); got != want {
		t.Fatalf("skill reach with authored addition = %g, want %g", got, want)
	}
	if got, want := handler.skillAttackReach(player, target, model.RefSkill{TransferRange: 100}), float32(133); got != want {
		t.Fatalf("transfer-authored skill reach = %g, want %g", got, want)
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{
		RefObjectID: 180,
		TypeInfo:    model.TypeInfo{TypeID1: model.TypeID1Item, TypeID2: model.TypeID2Wearable, TypeID3: model.TypeID3Weapon, TypeID4: 6},
	}}
	if got, want := handler.skillAttackReach(player, target, model.RefSkill{ActionRange: 10}), float32(205); got != want {
		t.Fatalf("bow skill reach with small authored addition = %g, want %g", got, want)
	}
}

func TestCombatGhostWalkMovesForwardByReferenceDistance(t *testing.T) {
	const skillID = uint32(8142)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{SRObject: model.SRObject{
		UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100),
	}, ID: 42, Session: session, Skills: map[uint32]byte{skillID: 1}}
	player.SetLifeState(model.Alive)
	world := &combatWorldFake{player: player}
	var sent []network.Packet
	handler := &CombatHandler{
		world: world, nextSkill: make(map[skillCooldownKey]time.Time),
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, MovementDistance: 245},
		}}),
		send:       func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast:  func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
		damageRoll: midpointDamageRoll(),
	}
	// The stock client sends Ghost Walk as the seven-byte no-target form even
	// though REF_SKILL marks the row as target-required.
	if err := handler.handlePacket(context.Background(), session, combatSelfSkillPacket(skillID)); err != nil {
		t.Fatal(err)
	}
	if got, want := player.GetNavmeshPosition().Offset.X, float32(345); got != want {
		t.Fatalf("ghost walk X = %g, want %g", got, want)
	}
	if world.stops != 1 {
		t.Fatalf("ghost walk stops = %d, want 1", world.stops)
	}
	for _, packet := range sent {
		if packet.MessageID == opcode.EntityMovementResponse {
			t.Fatalf("ghost walk emitted ordinary movement packet: % X", packet.Payload())
		}
	}
	var completion *network.Packet
	for index := range sent {
		if sent[index].MessageID == opcode.EntitySkillCastEnd {
			completion = &sent[index]
			break
		}
	}
	if completion == nil || completion.DataSize != 10 || completion.Payload()[9] != 0 {
		t.Fatalf("ghost walk completion = %#v, want 10-byte B071 completion", completion)
	}
}

func TestCombatGhostWalkAcceptsStockAreaTargetPacket(t *testing.T) {
	const skillID = uint32(8142)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{SRObject: model.SRObject{
		UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100),
	}, ID: 42, Session: session, Skills: map[uint32]byte{skillID: 1}}
	player.SetLifeState(model.Alive)
	var completion func()
	var completionDelay time.Duration
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player}, nextSkill: make(map[skillCooldownKey]time.Time),
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, MovementDurationMS: 500, MovementDistance: 245},
		}}),
		schedule:  func(delay time.Duration, callback func()) { completionDelay, completion = delay, callback },
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	packet := network.EmptyClientPacket()
	packet.WriteByte(actionPhaseStart)
	packet.WriteByte(actionTypeAttack)
	packet.WriteUInt32(skillID)
	packet.WriteByte(2)
	packet.WriteUInt16(24231)
	packet.WriteUInt32(200)
	packet.WriteUInt32(0)
	packet.WriteUInt32(100)
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("stock area-target Ghost Walk disconnected session: %v", err)
	}
	if got, want := player.GetNavmeshPosition().Offset.X, float32(200); got != want {
		t.Fatalf("Ghost Walk endpoint X = %g, want %g", got, want)
	}
	if completion != nil || completionDelay != 0 {
		t.Fatalf("Ghost Walk scheduled a delayed endpoint: callback:%t delay:%s", completion != nil, completionDelay)
	}
}

func TestCombatGhostWalkCancellationDoesNotCommitEndpoint(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{
		UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100),
	}, ID: 42}
	player.SetLifeState(model.Alive)
	var sent []network.Packet
	handler := &CombatHandler{
		world:     &combatWorldFake{player: player},
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	skill := model.RefSkill{ID: 8142, BasicActivity: 2, MovementDurationMS: 500, MovementDistance: 245}
	session := &server.Session{UserContext: server.UserContext{UniqueID: player.GetUniqueID()}}
	if err := handler.executeMovementSkill(context.Background(), session, player, skill); err != nil {
		t.Fatalf("ghost walk failed: %v", err)
	}
	handler.CancelPlayerAttack(nil, player.GetUniqueID())
	if got := player.GetNavmeshPosition().Offset.X; got != 345 {
		t.Fatalf("instant Ghost Walk endpoint = %g, want 345", got)
	}
	foundEnd := false
	for _, packet := range sent {
		if packet.MessageID == opcode.EntitySkillCastEnd {
			foundEnd = true
			break
		}
	}
	if !foundEnd {
		t.Fatalf("cancelled ghost walk packets = %v, want B071 interruption", packetOpcodesForCombat(sent))
	}
}

func TestCombatIcebergWallCreatesPhysicalBarrier(t *testing.T) {
	const skillID = uint32(8010)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 42, Session: session, Skills: map[uint32]byte{skillID: 1}}
	player.SetLifeState(model.Alive)
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player}, nextSkill: make(map[skillCooldownKey]time.Time),
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, BarrierHitPoints: 21334, BarrierAbsorption: 98, BarrierPhysical: true},
		}}),
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), session, combatSelfSkillPacket(skillID)); err != nil {
		t.Fatal(err)
	}
	barrier := player.BarrierSnapshot()
	if barrier.SkillID != skillID || barrier.HitPoints != 21334 || !barrier.Physical {
		t.Fatalf("iceberg barrier = %+v", barrier)
	}
	if !player.IsBarrierRooted() {
		t.Fatal("iceberg wall did not root its caster")
	}
}

func TestCombatResolvedHitBroadcastsDamageThenEndsAction(t *testing.T) {
	var sequence []network.Packet
	handler := &CombatHandler{
		broadcast: func(_ *model.Player, packet *network.Packet) {
			sequence = append(sequence, *packet)
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sequence = append(sequence, packet)
			return nil
		},
	}
	attack := buildAttackBegin(10, 20, 30, 4, false)
	if err := handler.emitAttackAndEnd(&server.Session{}, &model.Player{}, &attack); err != nil {
		t.Fatal(err)
	}
	if len(sequence) != 2 || sequence[0].MessageID != opcode.EntitySkillCastBegin || sequence[1].MessageID != opcode.EntityActionResponse {
		t.Fatalf("attack completion sequence = %#v", sequence)
	}
	if got := sequence[1].Payload(); !bytes.Equal(got, []byte{2, 0}) {
		t.Fatalf("action end payload = % X, want 02 00", got)
	}
}

func TestCombatNormalAttackAppliesToAllNearbyFieldMobReferences(t *testing.T) {
	for _, test := range []struct {
		refObjID uint32
		name     string
	}{
		{refObjID: 1933, name: "MOB_CH_MANGNYANG"},
		{refObjID: 1934, name: "MOB_CH_BIGEYEGHOST_CLON"},
		{refObjID: 1935, name: "MOB_CH_BIGEYEGHOST"},
		{refObjID: 1936, name: "MOB_CH_GYO_CLON"},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
			player := &model.Player{
				SRObject: model.SRObject{
					UniqueID:          100,
					RtNavmeshPosition: combatTestPosition(24231, 100, 500, 100),
				},
				ID:        42,
				Session:   session,
				BaseStats: model.BaseStats{Str: 20, Int: 20},
			}
			player.SetLifeState(model.Alive)
			player.KnownObjectList = model.NewKnownObjectList(player)
			mob := &model.NPC{
				SRObject: model.SRObject{
					UniqueID:          200,
					RefObjectID:       test.refObjID,
					Name:              test.name,
					TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
					RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
				},
				Type:  "NPC",
				Mutex: &sync.Mutex{},
			}
			mob.ConfigureCombat(100, 10)
			mob.SetLifeState(model.Alive)
			mob.KnownObjectList = model.NewKnownObjectList(mob)
			player.GetKnownObjectList().AddObject(mob)

			var sent []network.Packet
			handler := &CombatHandler{
				world: &combatWorldFake{
					player:   player,
					objects:  map[uint32]model.ISRObject{mob.GetUniqueID(): mob},
					distance: 4,
				},
				send: func(_ *server.Session, packet network.Packet) error {
					sent = append(sent, packet)
					return nil
				},
				broadcast: func(_ *model.Player, packet *network.Packet) {
					sent = append(sent, *packet)
				},
			}
			packet := network.EmptyClientPacket()
			packet.WriteByte(actionPhaseStart)
			packet.WriteByte(actionTypeAttack)
			packet.WriteUInt32(mob.GetUniqueID())
			packet.WriteByte(0)

			if err := handler.handlePacket(context.Background(), session, packet); err != nil {
				t.Fatalf("normal attack failed: %v", err)
			}
			if len(sent) != 4 || sent[0].MessageID != opcode.EntityMovementTurnResponse || sent[1].MessageID != opcode.EntityActionResponse || sent[2].MessageID != opcode.EntitySkillCastBegin || sent[3].MessageID != opcode.EntityActionResponse {
				opcodes := make([]uint16, len(sent))
				for i := range sent {
					opcodes[i] = sent[i].MessageID
				}
				t.Fatalf("normal attack packet sequence = %04X", opcodes)
			}
			if mob.CurrentHitPoints() >= 100 {
				t.Fatal("normal attack did not damage mob")
			}
			if !mob.GetKnownObjectList().Knows(player) {
				t.Fatal("successful hit did not make the mob aware of its attacker")
			}
			_, _, _, targetUID := mob.AIProfile()
			if targetUID != player.GetUniqueID() {
				t.Fatalf("mob aggro target = %d, want player %d", targetUID, player.GetUniqueID())
			}
		})
	}
}

func TestCombatLightningImbueAddsDamageTransfersAndAppliesShock(t *testing.T) {
	const imbueID = uint32(8129)
	now := time.Unix(1000, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Skills: map[uint32]byte{imbueID: 1},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	newMob := func(uid uint32, x float32) *model.NPC {
		mob := &model.NPC{SRObject: model.SRObject{
			UniqueID: uid, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, x, 0, 100),
		}, Type: "NPC", Mutex: &sync.Mutex{}}
		mob.ConfigureCombat(10000, 10)
		mob.SetLifeState(model.Alive)
		mob.KnownObjectList = model.NewKnownObjectList(mob)
		player.GetKnownObjectList().AddObject(mob)
		return mob
	}
	primary := newMob(200, 104)
	secondary1 := newMob(201, 106)
	secondary2 := newMob(202, 108)
	outside := newMob(203, 110)
	imbue := model.RefSkill{
		ID: imbueID, BasicActivity: 2, BuffDuration: 8000, AttackType: 8, AttackPercent: 100, AttackMin: 964, AttackMax: 1791,
		ShockPower: 240, ShockChance: 25, ShockDuration: 5000, TransferRange: 50, TransferCount: 2,
	}
	if _, ok := player.CommitActiveBuff(imbueID, 900, 8*time.Second, model.BuffModifiers{}); !ok {
		t.Fatal("failed to arrange lightning imbue")
	}
	var sent []network.Packet
	handler := &CombatHandler{
		world:         &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{primary.GetUniqueID(): primary}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{imbueID: imbue}}),
		nextAttack:    make(map[uint32]time.Time), now: func() time.Time { return now }, rollPercent: func(chance int) bool { return chance == 25 },
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(primary.GetUniqueID())); err != nil {
		t.Fatalf("imbued attack: %v", err)
	}
	if got, want := primary.CurrentHitPoints(), 8601; got != want {
		t.Fatalf("primary HP = %d, want %d", got, want)
	}
	for _, mob := range []*model.NPC{secondary1} {
		if got, want := mob.CurrentHitPoints(), 8612; got != want {
			t.Fatalf("secondary %d HP = %d, want %d", mob.GetUniqueID(), got, want)
		}
		status, exists := mob.CombatStatus(model.CombatStatusShock, now)
		if !exists || status.Power != 240 || !status.ExpiresAt.Equal(now.Add(5*time.Second)) {
			t.Fatalf("secondary %d shock = %+v exists=%t", mob.GetUniqueID(), status, exists)
		}
	}
	if secondary2.CurrentHitPoints() != 10000 {
		t.Fatalf("third simultaneous target HP = %d, want untouched", secondary2.CurrentHitPoints())
	}
	if outside.CurrentHitPoints() != 10000 {
		t.Fatalf("outside target HP = %d, want untouched", outside.CurrentHitPoints())
	}
	if status, exists := primary.CombatStatus(model.CombatStatusShock, now); !exists || status.Power != 240 {
		t.Fatalf("primary shock = %+v exists=%t", status, exists)
	}
}

func TestCombatElementalSkillAppliesStatusesAndSimultaneousTargets(t *testing.T) {
	const skillID = uint32(8304)
	now := time.Unix(1000, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Skills: map[uint32]byte{skillID: 1},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	newMob := func(uid uint32, x float32) *model.NPC {
		mob := &model.NPC{SRObject: model.SRObject{
			UniqueID: uid, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, x, 0, 100),
		}, Type: "NPC", Mutex: &sync.Mutex{}}
		mob.ConfigureCombat(100000, 10)
		mob.SetLifeState(model.Alive)
		mob.KnownObjectList = model.NewKnownObjectList(mob)
		player.GetKnownObjectList().AddObject(mob)
		return mob
	}
	primary := newMob(200, 104)
	secondary := []*model.NPC{
		newMob(201, 105), newMob(202, 106), newMob(203, 107), newMob(204, 108), newMob(205, 109),
	}
	skill := model.RefSkill{
		ID: skillID, BasicActivity: 2, AttackType: 10, AttackPercent: 330, AttackMin: 1899, AttackMax: 3166,
		TransferRange: 70, TransferCount: 5,
		StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusBurn, Power: 240, Chance: 25, DurationMS: 11000}},
	}
	var sent []network.Packet
	handler := &CombatHandler{
		world:         &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{primary.GetUniqueID(): primary}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: skill}}),
		nextAttack:    make(map[uint32]time.Time), now: func() time.Time { return now }, rollPercent: func(chance int) bool { return chance == 25 },
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, primary.GetUniqueID())); err != nil {
		t.Fatalf("elemental skill: %v", err)
	}
	for _, mob := range append([]*model.NPC{primary}, secondary[:4]...) {
		if mob.CurrentHitPoints() >= 100000 {
			t.Fatalf("simultaneous target %d was not damaged", mob.GetUniqueID())
		}
		status, exists := mob.CombatStatus(model.CombatStatusBurn, now)
		if !exists || status.SkillID != skillID || status.Power != 240 {
			t.Fatalf("target %d burn = %+v exists=%t", mob.GetUniqueID(), status, exists)
		}
	}
	if got := secondary[4].CurrentHitPoints(); got != 100000 {
		t.Fatalf("sixth target HP = %d, want untouched", got)
	}
}

func TestCombatFireImbueBurnTicksAuthoritativeDamage(t *testing.T) {
	const imbueID = uint32(8214)
	now := time.Unix(1000, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 42, Session: session}
	player.SetLifeState(model.Alive)
	target := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
	}, Mutex: &sync.Mutex{}}
	target.ConfigureCombat(1000, 10)
	target.SetLifeState(model.Alive)
	target.KnownObjectList = model.NewKnownObjectList(target)
	var scheduled func()
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player}, now: func() time.Time { return now }, rollPercent: func(int) bool { return true },
		schedule:  func(_ time.Duration, callback func()) { scheduled = callback },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	imbue := model.RefSkill{ID: imbueID, StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusBurn, Power: 240, Chance: 25, DurationMS: 10800}}}
	handler.applyImbueStatuses(session, player, target, imbue, now)
	if scheduled == nil {
		t.Fatal("burn did not schedule its first tick")
	}
	scheduled()
	if got, want := target.CurrentHitPoints(), 760; got != want {
		t.Fatalf("burned HP = %d, want %d", got, want)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.EntityUpdateStatus || sent[1].MessageID != opcode.EntitySkillCastBegin {
		t.Fatalf("burn packets = %04X", packetOpcodesForCombat(sent))
	}
}

func TestCombatTargetedLearnedSkillCastUsesElevenByteStockLayout(t *testing.T) {
	const skillID = uint32(315)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject:  model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:        42,
		Session:   session,
		BaseStats: model.BaseStats{Str: 26, Int: 23},
		Skills:    map[uint32]byte{skillID: 1},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(1000, 10)
	mob.SetLifeState(model.Alive)
	mob.KnownObjectList = model.NewKnownObjectList(mob)
	player.GetKnownObjectList().AddObject(mob)

	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, BasicChainCode: 323, ActionDuration: 428, ReuseDelay: 8000, AttackType: 5, AttackPercent: 30, AttackMin: 21, AttackMax: 26, TargetEnemyMob: true, TargetEnemyPVP: true},
			323:     {ID: 323, BasicActivity: 2, BasicChainCode: 331, ActionDuration: 612, AttackType: 5, AttackPercent: 53, AttackMin: 21, AttackMax: 26},
			331:     {ID: 331, BasicActivity: 2, ActionDuration: 993, AttackType: 5, AttackPercent: 120, AttackMin: 21, AttackMax: 26},
		}}),
		nextAttack: make(map[uint32]time.Time),
		now:        func() time.Time { return time.Unix(100, 0) },
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatalf("targeted skill cast failed: %v", err)
	}
	if mob.CurrentHitPoints() >= 1000 {
		t.Fatal("targeted skill cast did not damage mob")
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntityActionResponse}) {
		t.Fatalf("skill cast packets = %04X, want turn B074 B070 B074", got)
	}
	cast := sent[2]
	if _, err := cast.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if _, err := cast.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if _, err := cast.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if got, _ := cast.ReadUInt32(); got != skillID {
		t.Fatalf("B070 skill ID = %d, want %d", got, skillID)
	}
	for range 2 {
		if _, err := cast.ReadUInt32(); err != nil {
			t.Fatal(err)
		}
	}
	if delegated, err := cast.ReadUInt32(); err != nil || delegated != mob.GetUniqueID() {
		t.Fatalf("B070 target UID = %d, want %d: %v", delegated, mob.GetUniqueID(), err)
	}
	if hasDamage, _ := cast.ReadBool(); !hasDamage {
		t.Fatal("combo B070 has no damage block")
	}
	if hitCount, _ := cast.ReadByte(); hitCount != 3 {
		t.Fatalf("combo B070 hit count = %d, want 3", hitCount)
	}
	key := skillCooldownKey{playerUID: player.GetUniqueID(), skillID: skillID}
	if next := handler.nextSkill[key]; !next.Equal(time.Unix(108, 0)) {
		t.Fatalf("skill cooldown ends at %v, want %v", next, time.Unix(108, 0))
	}
}

func TestCombatSkillDamageConnectsEquipmentBuffsDefenseAndWireResult(t *testing.T) {
	const (
		physicalSkillID = uint32(7001)
		magicalSkillID  = uint32(7002)
		blockedSkillID  = uint32(7003)
		criticalParam   = uint32(0x00637269)
	)
	previous := model.RefItems
	t.Cleanup(func() { model.RefItems = previous })
	model.RefItems = map[uint32]model.RefItem{5001: {
		RefObject: model.RefObject{ID: 5001, TypeInfo: model.TypeInfo{
			TypeID1: model.TypeID1Item, TypeID2: model.TypeID2Wearable, TypeID3: model.TypeID3Weapon,
		}},
		PhyAttackMinLower: 100, PhyAttackMinUpper: 131,
		PhyAttackMaxLower: 200, PhyAttackMaxUpper: 231,
		PhyAttackIncrease: 10,
		MagAttackMinLower: 50, MagAttackMinUpper: 81,
		MagAttackMaxLower: 100, MagAttackMaxUpper: 131,
		MagAttackIncrease: 5,
		CritRateLower:     3, CritRateUpper: 3,
	}}

	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{HP: 1000, MP: 100, Str: 20, Int: 20},
		Skills: map[uint32]byte{physicalSkillID: 1, magicalSkillID: 1, blockedSkillID: 1},
		Inventory: model.NewInventory(map[byte]model.Item{
			model.SlotPrimaryWeapon: {
				SRObject: model.SRObject{RefObjectID: 5001},
				Variance: model.WeaponVariance{
					PhyAttack: 31, MagAttack: 31, CriticalRate: 31,
				}.ToVariance(),
				OptLevel:     2,
				MagicOptions: []model.MagicOption{{ParamType: criticalParam, Value: 7}},
			},
		}),
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	if _, ok := player.CommitActiveBuff(900, 901, time.Minute, model.BuffModifiers{DamagePercent: 25}); !ok {
		t.Fatal("failed to arrange the authored damage buff")
	}
	stats := player.DerivedStats()
	if stats.PhyAttackMin != 160 || stats.PhyAttackMax != 264 || stats.MagAttackMin != 100 || stats.MagAttackMax != 154 || stats.CriticalRate != 10 || player.ActiveCombatDamagePercent() != 25 {
		t.Fatalf("equipment/buff stats = %+v damage=%d, want physical 160-264 magical 100-154 damage 25 critical 10", stats, player.ActiveCombatDamagePercent())
	}

	newMob := func(uid, refID uint32) *model.NPC {
		mob := &model.NPC{SRObject: model.SRObject{
			UniqueID: uid, RefObjectID: refID,
			TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
		}, Type: "NPC", Mutex: &sync.Mutex{}}
		mob.ConfigureCombat(5000, 10)
		mob.SetLifeState(model.Alive)
		mob.KnownObjectList = model.NewKnownObjectList(mob)
		player.GetKnownObjectList().AddObject(mob)
		return mob
	}
	physicalTarget := newMob(201, 9001)
	magicalTarget := newMob(202, 9002)
	blockedTarget := newMob(203, 9003)
	world := &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{
		201: physicalTarget, 202: magicalTarget, 203: blockedTarget,
	}}
	physicalSkill := model.RefSkill{
		ID: physicalSkillID, BasicActivity: 2, AttackType: 5, AttackPercent: 200, AttackMin: 40, AttackMax: 60,
		StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusBurn, Power: 25, Chance: 100, DurationMS: 5000}},
	}
	magicalSkill := model.RefSkill{ID: magicalSkillID, BasicActivity: 2, AttackType: 10, AttackPercent: 150, AttackMin: 10, AttackMax: 30}
	blockedSkill := physicalSkill
	blockedSkill.ID, blockedSkill.ConsumeMP, blockedSkill.ReuseDelay = blockedSkillID, 20, 3000
	vitals := &combatVitalsFake{spent: true}
	var sent []network.Packet
	criticalRolls := 0
	handler := &CombatHandler{
		world: world, vitals: vitals, now: func() time.Time { return time.Unix(100, 0) },
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
			Characters: map[uint32]model.RefChar{
				9001: {RefObject: model.RefObject{ID: 9001}, PhyDef: 130},
				9002: {RefObject: model.RefObject{ID: 9002}, MagDef: 67},
				9003: {RefObject: model.RefObject{ID: 9003}, PhyDef: 130, BlockRate: 100},
			},
			Skills: map[uint32]model.RefSkill{physicalSkillID: physicalSkill, magicalSkillID: magicalSkill, blockedSkillID: blockedSkill},
		}),
		hitRollPercent: func(int) bool { return true },
		criticalRoll: func(rate int) bool {
			criticalRolls++
			if rate != 10 {
				t.Fatalf("critical roll rate = %d, want equipment-derived 10", rate)
			}
			return true
		},
		blockRoll:   func(rate int) bool { return rate == 100 },
		rollPercent: func(int) bool { return true },
		damageRoll:  midpointDamageRoll(),
		send:        func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast:   func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(physicalSkillID, physicalTarget.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got := physicalTarget.CurrentHitPoints(); got != 4340 {
		t.Fatalf("physical critical target HP = %d, want 4340", got)
	}
	physicalRecord := combatBeginDamageRecord(t, sent, physicalSkillID)
	if physicalRecord.attackState != model.DamageAttackStateNormal || physicalRecord.damageState != model.DamageStateCritical|model.DamageStateStatus || physicalRecord.damage != 660 || physicalRecord.effectMask == 0 {
		t.Fatalf("physical critical B070 = %+v, want critical/status damage 660", physicalRecord)
	}
	if _, active := physicalTarget.CombatStatus(model.CombatStatusBurn, time.Unix(100, 0)); !active {
		t.Fatal("landed physical skill did not connect its authored status")
	}

	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(magicalSkillID, magicalTarget.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got := magicalTarget.CurrentHitPoints(); got != 4850 {
		t.Fatalf("magical target HP = %d, want 4850", got)
	}
	magicalRecord := combatBeginDamageRecord(t, sent, magicalSkillID)
	if magicalRecord.attackState != model.DamageAttackStateNormal || magicalRecord.damageState != model.DamageStateNormal || magicalRecord.damage != 150 || magicalRecord.effectMask != 0 {
		t.Fatalf("magical B070 = %+v, want normal magical damage 150", magicalRecord)
	}
	if criticalRolls != 1 {
		t.Fatalf("physical/magical critical rolls = %d, want only the physical skill to roll", criticalRolls)
	}

	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(blockedSkillID, blockedTarget.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got := blockedTarget.CurrentHitPoints(); got != 5000 {
		t.Fatalf("blocked target HP = %d, want 5000", got)
	}
	blockedRecord := combatBeginDamageRecord(t, sent, blockedSkillID)
	if blockedRecord.attackState != model.DamageAttackStateBlock || blockedRecord.damageState != 0 || blockedRecord.damage != 0 || blockedRecord.effectMask != 0 {
		t.Fatalf("blocked B070 = %+v, want block-only record", blockedRecord)
	}
	if _, active := blockedTarget.CombatStatus(model.CombatStatusBurn, time.Unix(100, 0)); active {
		t.Fatal("blocked skill applied its status")
	}
	if _, _, _, targetUID := blockedTarget.AIProfile(); targetUID != 0 {
		t.Fatalf("blocked skill created aggro target %d", targetUID)
	}
	if _, mp := player.CurrentVitals(); mp != 80 || vitals.spendCalls != 1 || vitals.cost != 20 {
		t.Fatalf("blocked skill MP = %d, spend calls/cost = %d/%d; want 80 and 1/20", mp, vitals.spendCalls, vitals.cost)
	}
	wantCooldown := time.Unix(103, 0)
	if got := handler.nextSkill[skillCooldownKey{playerUID: 100, skillID: blockedSkillID}]; !got.Equal(wantCooldown) {
		t.Fatalf("blocked skill cooldown = %v, want %v", got, wantCooldown)
	}
}

func midpointDamageRoll() func() int {
	rolls := [...]int{13106, 32767, 32767, 0}
	index := 0
	return func() int {
		roll := rolls[index%len(rolls)]
		index++
		return roll
	}
}

type combatTestDamageRecord struct {
	attackState byte
	damageState byte
	damage      uint32
	effectMask  uint32
}

func combatBeginDamageRecord(t *testing.T, packets []network.Packet, skillID uint32) combatTestDamageRecord {
	t.Helper()
	for index := len(packets) - 1; index >= 0; index-- {
		payload := packets[index].Payload()
		if packets[index].MessageID != opcode.EntitySkillCastBegin || len(payload) < 27 || binary.LittleEndian.Uint32(payload[3:7]) != skillID {
			continue
		}
		record := combatTestDamageRecord{attackState: payload[26]}
		if record.attackState&model.DamageAttackStateBlock != 0 {
			return record
		}
		if len(payload) < 35 {
			t.Fatalf("skill %d B070 damage record is truncated: % X", skillID, payload)
		}
		record.damageState = payload[27]
		record.damage = uint32(payload[28]) | uint32(payload[29])<<8 | uint32(payload[30])<<16
		record.effectMask = binary.LittleEndian.Uint32(payload[31:35])
		return record
	}
	t.Fatalf("skill %d B070 damage record not found", skillID)
	return combatTestDamageRecord{}
}

func TestCombatCastTimeSkillAppliesDamageAtAuthoredImpact(t *testing.T) {
	const skillID = uint32(8084)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Int: 100}, Skills: map[uint32]byte{skillID: 1},
	}
	playerPosition := player.GetNavmeshPosition()
	playerPosition.Heading = 32768
	player.SetNavmeshPosition(playerPosition)
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(100000, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)
	var callback func()
	var delay time.Duration
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, AttackType: 10, AttackPercent: 300, AttackMin: 810, AttackMax: 1216, ActionPreparingTime: 1000, ActionCastingTime: 1000},
		}}),
		nextSkill: make(map[skillCooldownKey]time.Time),
		schedule:  func(got time.Duration, fn func()) { delay, callback = got, fn },
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if mob.CurrentHitPoints() != 100000 || callback == nil || delay != 2*time.Second {
		t.Fatalf("pre-impact state: hp=%d callback=%t delay=%s", mob.CurrentHitPoints(), callback != nil, delay)
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin}) {
		t.Fatalf("cast start packets = %04X", got)
	}
	if heading := player.GetNavmeshPosition().Heading; heading != 0 {
		t.Fatalf("cast heading = %g, want target-facing east heading 0", heading)
	}
	callback()
	if mob.CurrentHitPoints() >= 100000 {
		t.Fatal("cast-time impact did not damage mob")
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd, opcode.EntityActionResponse}) {
		t.Fatalf("completed cast packets = %04X", got)
	}
}

func TestCombatPhysicalSkillWaitsForActionAnimationBeforeDamage(t *testing.T) {
	const skillID = uint32(7899)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 42, Session: session,
		BaseStats: model.BaseStats{Str: 100}, Skills: map[uint32]byte{skillID: 1},
	}
	player.SetLifeState(model.Alive)
	mob := &model.NPC{SRObject: model.SRObject{UniqueID: 200}, Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(10000, 10)
	mob.SetLifeState(model.Alive)
	var callback func()
	var delay time.Duration
	handler := &CombatHandler{
		nextSkill: make(map[skillCooldownKey]time.Time),
		schedule:  func(got time.Duration, fn func()) { delay, callback = got, fn },
		send:      func(*server.Session, network.Packet) error { return nil },
		broadcast: func(*model.Player, *network.Packet) {},
	}
	skill := model.RefSkill{ID: skillID, BasicActivity: 2, AttackType: 5, AttackMin: 100, AttackMax: 100, ActionDuration: 600}
	if err := handler.executeSkillAttack(context.Background(), session, player, mob, skill); err != nil {
		t.Fatal(err)
	}
	if callback == nil || delay != 600*time.Millisecond {
		t.Fatalf("physical-skill impact schedule = callback:%t delay:%s, want 600ms", callback != nil, delay)
	}
	if got := mob.CurrentHitPoints(); got != 10000 {
		t.Fatalf("mob HP before animation impact = %d, want 10000", got)
	}
	callback()
	if got := mob.CurrentHitPoints(); got >= 10000 {
		t.Fatalf("mob HP after animation impact = %d, want damage", got)
	}
}

func TestCombatCastTimeSkillEndsWhenTargetDiesBeforeImpact(t *testing.T) {
	const skillID = uint32(8084)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Int: 100}, Skills: map[uint32]byte{skillID: 1},
	}
	player.SetLifeState(model.Alive)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(1000, 10)
	mob.SetLifeState(model.Alive)
	var callback func()
	var sent []network.Packet
	handler := &CombatHandler{
		schedule:  func(_ time.Duration, fn func()) { callback = fn },
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
		nextSkill: make(map[skillCooldownKey]time.Time),
	}
	skill := model.RefSkill{ID: skillID, BasicActivity: 2, AttackType: 10, AttackMin: 100, AttackMax: 100, ActionCastingTime: 25}
	if err := handler.executeSkillAttack(context.Background(), session, player, mob, skill); err != nil {
		t.Fatal(err)
	}
	if callback == nil {
		t.Fatal("cast did not schedule its delayed impact")
	}
	mob.SetLifeState(model.Dead)
	callback()
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd, opcode.EntityActionResponse}) {
		t.Fatalf("dead-target completion packets = %04X", got)
	}
}

func TestCombatStatusOnlyColdWaveAppliesFrostbite(t *testing.T) {
	const skillID = uint32(7997)
	now := time.Unix(1000, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, Skills: map[uint32]byte{skillID: 1},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(1000, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)
	var sent []network.Packet
	skill := model.RefSkill{
		ID: skillID, BasicActivity: 2, TargetRequired: true, TargetEnemyMob: true, ActionRange: 150,
		StatusEffects: []model.SkillStatusEffect{{Kind: model.CombatStatusFrostbite, Power: 238, Chance: 100, DurationMS: 5000}},
	}
	handler := &CombatHandler{
		world:         &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: skill}}),
		nextSkill:     make(map[skillCooldownKey]time.Time), now: func() time.Time { return now }, rollPercent: func(int) bool { return true },
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	status, exists := mob.CombatStatus(model.CombatStatusFrostbite, now)
	if !exists || status.Power != 238 {
		t.Fatalf("cold wave frostbite = %+v exists=%t", status, exists)
	}
	if mob.CurrentHitPoints() != 1000 {
		t.Fatalf("status-only skill dealt direct damage: hp=%d", mob.CurrentHitPoints())
	}
	var statusPacket, castEnd *network.Packet
	for index := range sent {
		switch sent[index].MessageID {
		case opcode.EntityUpdateStatus:
			statusPacket = &sent[index]
		case opcode.EntitySkillCastEnd:
			castEnd = &sent[index]
		case opcode.EntityBuffAdd:
			t.Fatalf("Cold Wave published its casting skill as B0BD: % X", sent[index].Payload())
		}
	}
	if statusPacket == nil || len(statusPacket.Payload()) < 11 || statusPacket.Payload()[6] != 4 || binary.LittleEndian.Uint32(statusPacket.Payload()[7:11]) != 2 {
		t.Fatalf("Cold Wave status packet = %#v, want Frostbite abnormal-state bit 2", statusPacket)
	}
	if castEnd == nil || len(castEnd.Payload()) < 25 || binary.LittleEndian.Uint32(castEnd.Payload()[21:25]) != 2 {
		t.Fatalf("Cold Wave cast end = %#v, want Frostbite effect record", castEnd)
	}
}

func TestCombatFrostNovaAppliesAreaColdStatusesWithoutDirectDamage(t *testing.T) {
	const skillID = uint32(8024)
	now := time.Unix(1000, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, Skills: map[uint32]byte{skillID: 1},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(10000, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)
	skill := model.RefSkill{
		ID: skillID, BasicActivity: 2, TargetEnemyMob: true, TransferRange: 50, TransferCount: 5, ImpactDamage: 5065,
		StatusEffects: []model.SkillStatusEffect{
			{Kind: model.CombatStatusFreeze, Power: 240, Chance: 50, DurationMS: 5000},
			{Kind: model.CombatStatusFrostbite, Power: 240, Chance: 100, DurationMS: 5000},
		},
	}
	handler := &CombatHandler{
		world:         &combatWorldFake{player: player},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: skill}}),
		nextSkill:     make(map[skillCooldownKey]time.Time), now: func() time.Time { return now }, rollPercent: func(int) bool { return true },
		send: func(*server.Session, network.Packet) error { return nil }, broadcast: func(*model.Player, *network.Packet) {},
	}
	if err := handler.handlePacket(context.Background(), session, combatSelfSkillPacket(skillID)); err != nil {
		t.Fatal(err)
	}
	if got, want := mob.CurrentHitPoints(), 10000; got != want {
		t.Fatalf("frost nova HP = %d, want %d", got, want)
	}
	if _, exists := mob.CombatStatus(model.CombatStatusFreeze, now); !exists {
		t.Fatal("frost nova did not freeze target")
	}
}

func TestCombatRejectsTargetedSkillNotOwnedByPlayer(t *testing.T) {
	const skillID = uint32(292)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, Skills: map[uint32]byte{},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)

	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID},
		}}),
		send: func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
	}
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatalf("unowned skill returned transport error: %v", err)
	}
	if mob.CurrentHitPoints() != 100 || len(sent) != 1 || !bytes.Equal(sent[0].Payload(), []byte{2, 0}) {
		t.Fatalf("unowned skill changed combat state: hp=%d packets=%#v", mob.CurrentHitPoints(), sent)
	}
}

func TestCombatMagicalSkillRequiresWeaponAndSpendsMP(t *testing.T) {
	const skillID = uint32(10005)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject:  model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:        42,
		Session:   session,
		BaseStats: model.BaseStats{HP: 200, MP: 100, Str: 10, Int: 30},
		Skills:    map[uint32]byte{skillID: 1},
		Inventory: model.NewInventory(nil),
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 11}}}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(1000, 10)
	mob.SetLifeState(model.Alive)
	mob.KnownObjectList = model.NewKnownObjectList(mob)
	player.GetKnownObjectList().AddObject(mob)
	vitals := &combatVitalsFake{spent: true}
	var sent []network.Packet
	handler := &CombatHandler{
		world:  &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		vitals: vitals,
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, AttackType: 9, AttackPercent: 325, AttackMin: 32, AttackMax: 39, RequiredInt: 30, RequiredWeapon1: 11, RequiredWeapon2: 255, ConsumeMP: 50},
		}}),
		nextAttack: make(map[uint32]time.Time),
		send:       func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast:  func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatalf("magical cast failed: %v", err)
	}
	if vitals.spendCalls != 1 || vitals.expectedMP != 100 || vitals.cost != 50 {
		t.Fatalf("MP spend = calls:%d expected:%d cost:%d", vitals.spendCalls, vitals.expectedMP, vitals.cost)
	}
	if _, mp := player.CurrentVitals(); mp != 50 {
		t.Fatalf("player MP = %d, want 50", mp)
	}
	if len(sent) != 5 || sent[4].MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("magical cast packets = %04X, want turn, action, damage, end, vitals", packetOpcodesForCombat(sent))
	}

	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2}}}
	before := mob.CurrentHitPoints()
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatalf("wrong-weapon cast returned transport error: %v", err)
	}
	if vitals.spendCalls != 1 || mob.CurrentHitPoints() != before {
		t.Fatalf("wrong-weapon cast spent MP or dealt damage: calls=%d hp=%d/%d", vitals.spendCalls, mob.CurrentHitPoints(), before)
	}
	assertSkillCastError(t, sent[len(sent)-1], skillCastErrorInvalidEquipment)

	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 11}}}
	player.BaseStats.Int = 29
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatalf("low-int cast returned transport error: %v", err)
	}
	if vitals.spendCalls != 1 || mob.CurrentHitPoints() != before {
		t.Fatalf("low-int cast spent MP or dealt damage: calls=%d hp=%d/%d", vitals.spendCalls, mob.CurrentHitPoints(), before)
	}
	assertSkillCastError(t, sent[len(sent)-1], skillCastErrorInvalidSkill)

	player.BaseStats.Int = 30
	if !player.CommitMP(50, 1) {
		t.Fatal("failed to prepare low-MP player state")
	}
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, mob.GetUniqueID())); err != nil {
		t.Fatalf("low-MP cast returned transport error: %v", err)
	}
	if vitals.spendCalls != 1 || mob.CurrentHitPoints() != before {
		t.Fatalf("low-MP cast spent MP or dealt damage: calls=%d hp=%d/%d", vitals.spendCalls, mob.CurrentHitPoints(), before)
	}
	assertSkillCastError(t, sent[len(sent)-1], skillCastErrorInvalidMP)
}

func assertSkillCastError(t *testing.T, packet network.Packet, reason byte) {
	t.Helper()
	if packet.MessageID != opcode.EntitySkillCastBegin || !bytes.Equal(packet.Payload(), []byte{2, reason, skillCastErrorCategory}) {
		t.Fatalf("skill cast error = opcode %04X payload % X, want B070 02 %02X 30", packet.MessageID, packet.Payload(), reason)
	}
}

func TestCombatSelfBuffPublishesLifecycleAndExpires(t *testing.T) {
	const skillID = uint32(77)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject:  model.SRObject{UniqueID: 100},
		ID:        42,
		Session:   session,
		BaseStats: model.BaseStats{HP: 200, MP: 100, Str: 20, Int: 20},
		Skills:    map[uint32]byte{skillID: 1},
		Inventory: model.NewInventory(nil),
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 6}}}
	player.Inventory.Items[model.SlotSecondaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 4, TypeID4: 1}}, StackSize: 20}
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	var sent []network.Packet
	var expire func()
	handler := &CombatHandler{
		world:  &combatWorldFake{player: player},
		vitals: vitals,
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, BuffDuration: 15000, UpkeepIntervalMS: 5000, UpkeepMPCost: 41, ReuseDelay: 30000, RequiredWeapon1: 6, RequiredWeapon2: 255, ConsumeMP: 10, BuffModifiers: model.BuffModifiers{HP: 50, Strength: 5, PhysicalDefense: 12}},
		}}),
		nextSkill: make(map[skillCooldownKey]time.Time),
		schedule: func(delay time.Duration, callback func()) {
			if delay != 15*time.Second {
				t.Fatalf("buff expiration delay = %v, want 15s", delay)
			}
			expire = callback
		},
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatSelfSkillPacket(skillID)); err != nil {
		t.Fatalf("self buff cast failed: %v", err)
	}
	want := []uint16{opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd, opcode.EntityBuffAdd, opcode.EntityActionResponse, opcode.EntityUpdateStats, opcode.EntityUpdateStatus}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, want) {
		t.Fatalf("buff packets = %04X, want %04X", got, want)
	}
	if len(player.ActiveBuffs) != 1 || expire == nil {
		t.Fatalf("active buffs/callback = %#v/%v", player.ActiveBuffs, expire != nil)
	}
	if player.BonusStats.HP != 50 || player.BonusStats.Str != 5 || player.BonusDefenseStats.PhyDef != 12 {
		t.Fatalf("applied modifiers = base=%+v defense=%+v", player.BonusStats, player.BonusDefenseStats)
	}
	expire()
	if len(player.ActiveBuffs) != 0 || sent[len(sent)-2].MessageID != opcode.EntityBuffRemove || sent[len(sent)-1].MessageID != opcode.EntityUpdateStats {
		t.Fatalf("expired buff state = %#v packets=%04X", player.ActiveBuffs, packetOpcodesForCombat(sent))
	}
	if player.BonusStats.HP != 0 || player.BonusStats.Str != 0 || player.BonusDefenseStats.PhyDef != 0 {
		t.Fatalf("rolled-back modifiers = base=%+v defense=%+v", player.BonusStats, player.BonusDefenseStats)
	}
}

func TestCombatSelfHealingPersistsVitalsAndPublishesCast(t *testing.T) {
	const skillID = uint32(149)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject:  model.SRObject{UniqueID: 100},
		ID:        42,
		Level:     10,
		Session:   session,
		BaseStats: model.BaseStats{HP: 10, MP: 20, Str: 20, Int: 20},
		Skills:    map[uint32]byte{skillID: 1},
		Inventory: model.NewInventory(nil),
	}
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	var sent []network.Packet
	handler := &CombatHandler{
		world:  &combatWorldFake{player: player},
		vitals: vitals,
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, TargetRequired: false, TargetSelf: false, HealHP: 50, HealMP: 20, ConsumeMP: 10, ReuseDelay: 1000},
		}}),
		nextSkill: make(map[skillCooldownKey]time.Time),
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatSelfSkillPacket(skillID)); err != nil {
		t.Fatalf("self healing cast failed: %v", err)
	}
	if hp, mp := player.CurrentVitals(); hp != 60 || mp != 30 {
		t.Fatalf("healed vitals = %d/%d, want 60/30", hp, mp)
	}
	if vitals.spendCalls != 1 || vitals.expectedMP != 20 || vitals.cost != 10 {
		t.Fatalf("MP spend = calls:%d expected:%d cost:%d", vitals.spendCalls, vitals.expectedMP, vitals.cost)
	}
	if vitals.saveCalls != 1 || vitals.savedID != 42 || vitals.savedHP != 60 || vitals.savedMP != 30 {
		t.Fatalf("saved healing = calls:%d id:%d hp:%d mp:%d", vitals.saveCalls, vitals.savedID, vitals.savedHP, vitals.savedMP)
	}
	want := []uint16{opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd, opcode.EntityActionResponse, opcode.EntityUpdateStatus}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, want) {
		t.Fatalf("healing packets = %04X, want %04X", got, want)
	}
}

func TestCombatTargetedResurrectionCommitsMPAndRevivalAtomically(t *testing.T) {
	const skillID = uint32(152)
	casterSession := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	targetSession := &server.Session{UserContext: server.UserContext{UniqueID: 200, CharacterID: 84}}
	caster := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(25000, 100, 0, 100)}, ID: 42, Level: 20, Session: casterSession,
		BaseStats: model.BaseStats{HP: 200, MP: 150, Str: 20, Int: 20}, Skills: map[uint32]byte{skillID: 1}, Inventory: model.NewInventory(nil),
	}
	target := &model.Player{
		SRObject: model.SRObject{UniqueID: 200, RtNavmeshPosition: combatTestPosition(25000, 101, 0, 100)}, ID: 84, Level: 20, Session: targetSession,
		BaseStats: model.BaseStats{HP: 0, MP: 0, Str: 20, Int: 20}, Inventory: model.NewInventory(nil),
	}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Dead)
	party := &model.Party{Number: 9}
	if err := caster.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	if err := target.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	vitals := &combatVitalsFake{resurrection: true}
	var sent []network.Packet
	skill := model.RefSkill{
		ID: skillID, BasicActivity: 2, TargetRequired: true, TargetAlly: true, TargetParty: true, TargetDeadBody: true,
		HealHPPercent: 10, HealMPPercent: 10, ResurrectLevel: 27, ConsumeMP: 30, ReuseDelay: 1000, ActionRange: 150,
	}
	handler := &CombatHandler{
		world:  &combatWorldFake{player: caster, objects: map[uint32]model.ISRObject{target.GetUniqueID(): target}},
		vitals: vitals, referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{skillID: skill}}),
		nextSkill: make(map[skillCooldownKey]time.Time),
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), casterSession, combatSkillPacket(skillID, target.GetUniqueID())); err != nil {
		t.Fatalf("resurrection cast failed: %v", err)
	}
	maxHP, maxMP := target.MaxVitals()
	wantHP, wantMP := skill.RestorationAmounts(maxHP, maxMP)
	if hp, mp := target.CurrentVitals(); hp != wantHP || mp != wantMP || target.GetLifeState() != model.Alive {
		t.Fatalf("target resurrection = life:%d vitals:%d/%d, want alive %d/%d", target.GetLifeState(), hp, mp, wantHP, wantMP)
	}
	if _, mp := caster.CurrentVitals(); mp != 120 {
		t.Fatalf("caster MP = %d, want 120", mp)
	}
	wantArgs := [6]int{42, 84, 150, 30, wantHP, wantMP}
	if vitals.resurrectionCalls != 1 || vitals.resurrectionArgs != wantArgs {
		t.Fatalf("persistent resurrection = calls:%d args:%v, want 1 %v", vitals.resurrectionCalls, vitals.resurrectionArgs, wantArgs)
	}
	wantOpcodes := []uint16{
		opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd,
		opcode.EntityActionResponse, opcode.EntityUpdateStatus, opcode.EntityUpdateMovementState, opcode.EntityUpdateStatus,
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, wantOpcodes) {
		t.Fatalf("resurrection packets = %04X, want %04X", got, wantOpcodes)
	}
}

func TestCombatResurrectionRejectsTargetAboveAuthoredLevel(t *testing.T) {
	const skillID = uint32(152)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	caster := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 42, Level: 20, Session: session, BaseStats: model.BaseStats{HP: 200, MP: 150}, Skills: map[uint32]byte{skillID: 1}}
	target := &model.Player{SRObject: model.SRObject{UniqueID: 200}, ID: 84, Level: 28, BaseStats: model.BaseStats{HP: 0}}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Dead)
	party := &model.Party{Number: 9}
	_ = caster.AddToParty(party)
	_ = target.AddToParty(party)
	vitals := &combatVitalsFake{resurrection: true}
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: caster, objects: map[uint32]model.ISRObject{200: target}}, vitals: vitals,
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, TargetRequired: true, TargetAlly: true, TargetParty: true, TargetDeadBody: true, HealHPPercent: 10, ResurrectLevel: 27},
		}}),
		send: func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
	}
	if err := handler.handlePacket(context.Background(), session, combatSkillPacket(skillID, 200)); err != nil {
		t.Fatal(err)
	}
	if vitals.resurrectionCalls != 0 || target.GetLifeState() != model.Dead {
		t.Fatalf("over-level resurrection mutated target: calls=%d life=%d", vitals.resurrectionCalls, target.GetLifeState())
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityActionResponse}) {
		t.Fatalf("over-level packets = %04X", got)
	}
}

func TestCombatRightClickCancelsOwnedActiveBuff(t *testing.T) {
	const skillID = uint32(1377)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 42, Session: session, Skills: map[uint32]byte{skillID: 1}}
	player.SetLifeState(model.Alive)
	if _, ok := player.CommitActiveBuff(skillID, 900, time.Minute, model.BuffModifiers{DamagePercent: 5}); !ok {
		t.Fatal("failed to arrange active buff")
	}
	var sent []network.Packet
	handler := &CombatHandler{
		world:     &combatWorldFake{player: player},
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	if err := handler.handlePacket(context.Background(), session, combatCancelBuffPacket(skillID)); err != nil {
		t.Fatalf("cancel buff: %v", err)
	}
	if len(player.ActiveBuffs) != 0 || player.ActiveCombatDamagePercent() != 0 {
		t.Fatalf("active buff was not rolled back: %#v", player.ActiveBuffs)
	}
	if got, want := packetOpcodesForCombat(sent), []uint16{opcode.EntityBuffRemove, opcode.EntityUpdateStats}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cancel packets = %04X, want %04X", got, want)
	}
}

func TestValidHealingTargetRequiresSharedParty(t *testing.T) {
	caster, target := &model.Player{}, &model.Player{}
	caster.SetLifeState(model.Alive)
	target.SetLifeState(model.Alive)
	skill := model.RefSkill{TargetAlly: true}
	if validHealingTarget(caster, target, skill) {
		t.Fatal("unaffiliated player accepted as healing target")
	}
	party := &model.Party{Number: 9}
	if err := caster.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	if err := target.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	if !validHealingTarget(caster, target, skill) {
		t.Fatal("shared party player rejected as healing target")
	}
}

func TestPlayerMeetsSkillCastRequirements(t *testing.T) {
	player := &model.Player{
		BaseStats: model.BaseStats{Str: 30, Int: 20},
		Inventory: model.NewInventory(nil),
	}
	equipWeapon := func(typeID4 int) {
		delete(player.Inventory.Items, model.SlotSecondaryWeapon)
		player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{
			TypeID1: 3,
			TypeID2: 1,
			TypeID3: 6,
			TypeID4: typeID4,
		}}}
		if typeID4 == 6 {
			player.Inventory.Items[model.SlotSecondaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 4, TypeID4: 1}}, StackSize: 20}
		}
	}

	tests := []struct {
		name       string
		skill      model.RefSkill
		weaponType int
		want       bool
	}{
		{name: "bow with bow", skill: model.RefSkill{RequiredWeapon1: 6, RequiredWeapon2: 255}, weaponType: 6, want: true},
		{name: "bow with sword", skill: model.RefSkill{RequiredWeapon1: 6, RequiredWeapon2: 255}, weaponType: 2, want: false},
		{name: "bow with glaive", skill: model.RefSkill{RequiredWeapon1: 6, RequiredWeapon2: 255}, weaponType: 5, want: false},
		{name: "either sword or blade", skill: model.RefSkill{RequiredWeapon1: 2, RequiredWeapon2: 3}, weaponType: 3, want: true},
		{name: "second weapon after sentinel", skill: model.RefSkill{RequiredWeapon1: 255, RequiredWeapon2: 6}, weaponType: 2, want: false},
		{name: "enough str and int", skill: model.RefSkill{RequiredStr: 30, RequiredInt: 20}, weaponType: 2, want: true},
		{name: "not enough str", skill: model.RefSkill{RequiredStr: 31}, weaponType: 2, want: false},
		{name: "not enough int", skill: model.RefSkill{RequiredInt: 21}, weaponType: 2, want: false},
		{name: "unrestricted", skill: model.RefSkill{RequiredWeapon1: 255, RequiredWeapon2: 255}, weaponType: 5, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			equipWeapon(test.weaponType)
			if got := playerMeetsSkillCastRequirements(player, test.skill); got != test.want {
				t.Fatalf("playerMeetsSkillCastRequirements() = %t, want %t", got, test.want)
			}
		})
	}

	delete(player.Inventory.Items, model.SlotPrimaryWeapon)
	if playerMeetsSkillCastRequirements(player, model.RefSkill{RequiredWeapon1: 6, RequiredWeapon2: 255}) {
		t.Fatal("bow skill without an equipped bow was allowed")
	}
	equipWeapon(6)
	delete(player.Inventory.Items, model.SlotSecondaryWeapon)
	if playerMeetsSkillCastRequirements(player, model.RefSkill{RequiredWeapon1: 6, RequiredWeapon2: 255}) {
		t.Fatal("bow skill without equipped arrows was allowed")
	}
	if reason, rejected := skillCastRejection(player, model.RefSkill{RequiredWeapon1: 6, RequiredWeapon2: 255}); !rejected || reason != skillCastErrorInvalidAmmo {
		t.Fatalf("bow skill without arrows rejection = (%02X, %t), want ammunition error", reason, rejected)
	}
	shieldSkill := model.RefSkill{RequiredWeapon1: 255, RequiredWeapon2: 255, RequiredItems: []model.SkillRequiredItem{{TypeID3: 4, TypeID4: 1}}}
	if playerMeetsSkillCastRequirements(player, shieldSkill) {
		t.Fatal("shield-required skill without an equipped shield was allowed")
	}
	player.Inventory.Items[model.SlotSecondaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 4, TypeID4: 1}}}
	if !playerMeetsSkillCastRequirements(player, shieldSkill) {
		t.Fatal("shield-required skill rejected an equipped Chinese shield")
	}
}

func TestPlayerCanBaseAttackRequiresRangedAmmunition(t *testing.T) {
	player := &model.Player{Inventory: model.NewInventory(nil)}
	equipWeapon := func(typeID4 int) {
		player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{
			TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: typeID4,
		}}}
		delete(player.Inventory.Items, model.SlotSecondaryWeapon)
	}
	equipAmmo := func(typeID4 int) {
		player.Inventory.Items[model.SlotSecondaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{
			TypeID1: 3, TypeID2: 3, TypeID3: 4, TypeID4: typeID4,
		}}, StackSize: 20}
	}

	equipWeapon(6)
	if playerCanBaseAttack(player) {
		t.Fatal("bow base attack was admitted without arrows")
	}
	equipAmmo(1)
	if !playerCanBaseAttack(player) {
		t.Fatal("bow base attack was rejected with arrows")
	}
	equipWeapon(12)
	if playerCanBaseAttack(player) {
		t.Fatal("crossbow base attack was admitted without bolts")
	}
	equipAmmo(2)
	if !playerCanBaseAttack(player) {
		t.Fatal("crossbow base attack was rejected with bolts")
	}
	equipWeapon(2)
	if !playerCanBaseAttack(player) {
		t.Fatal("melee base attack unexpectedly required ammunition")
	}
}

func TestBowBaseAttackWithoutArrowsReturnsStockAmmunitionError(t *testing.T) {
	player := &model.Player{Inventory: model.NewInventory(nil)}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{
		TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 6,
	}}}
	mob := &model.NPC{}
	mob.ConfigureCombat(100, 0)
	var sent []network.Packet
	handler := &CombatHandler{send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	if err := handler.executeAttack(context.Background(), &server.Session{}, player, mob); err != nil {
		t.Fatal(err)
	}
	if mob.CurrentHitPoints() != 100 {
		t.Fatalf("ammo-less bow attack changed mob HP to %d", mob.CurrentHitPoints())
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.EntitySkillCastBegin || !bytes.Equal(sent[0].Payload(), []byte{2, skillCastErrorInvalidAmmo, skillCastErrorCategory}) {
		t.Fatalf("ammo-less bow response = %#v, want B070 02 0E 30", sent)
	}
}

func TestCombatConsumesEquippedBowArrowsAndPublishesInventoryState(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 42}}
	player := &model.Player{ID: 42, Inventory: model.NewInventory(map[byte]model.Item{
		model.SlotPrimaryWeapon: {
			SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 6}},
		},
		model.SlotSecondaryWeapon: {
			ID:        77,
			StackSize: 2,
			SRObject:  model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 4, TypeID4: 1}},
		},
	})}
	ammo := &rangedAmmoCombatFake{remaining: []uint16{1, 0}}
	var sent []network.Packet
	handler := &CombatHandler{
		rangedAmmo: ammo,
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}

	if err := handler.consumeRangedAmmo(context.Background(), session, player); err != nil {
		t.Fatal(err)
	}
	if got := player.GetInventory().Items[model.SlotSecondaryWeapon].StackSize; got != 1 {
		t.Fatalf("arrow stack after first shot = %d, want 1", got)
	}
	if len(ammo.calls) != 1 || ammo.calls[0] != [2]int64{42, 77} {
		t.Fatalf("ammunition persistence calls = %#v, want character 42 item 77", ammo.calls)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.InventoryItemUpdate || !bytes.Equal(sent[0].Payload(), []byte{model.SlotSecondaryWeapon, 0x08, 0x01, 0x00}) {
		t.Fatalf("first arrow update = %#v, want 3040 quantity slot 7 count 1", sent)
	}

	if err := handler.consumeRangedAmmo(context.Background(), session, player); err != nil {
		t.Fatal(err)
	}
	if _, exists := player.GetInventory().Items[model.SlotSecondaryWeapon]; exists {
		t.Fatal("exhausted arrow stack remained equipped")
	}
	if len(sent) != 2 || sent[1].MessageID != opcode.ItemOperationResponse || !bytes.Equal(sent[1].Payload(), []byte{1, 0x0F, model.SlotSecondaryWeapon}) {
		t.Fatalf("last arrow update = %#v, want B034 server delete slot 7", sent)
	}
}

func TestCombatNormalAttackRejectsRepeatInsideReferenceCooldown(t *testing.T) {
	now := time.Unix(100, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{
		SRObject: model.SRObject{
			UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
		},
		Type: "NPC", Mutex: &sync.Mutex{},
	}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	mob.KnownObjectList = model.NewKnownObjectList(mob)
	player.GetKnownObjectList().AddObject(mob)

	var sent []network.Packet
	companions := &growthPetCombatFake{}
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			1: {ID: 1, ActionDuration: 600, ReuseDelay: 1000, CoolTime: 800},
		}}),
		nextAttack: make(map[uint32]time.Time),
		companions: companions,
		now:        func() time.Time { return now },
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	hitPointsAfterFirst := mob.CurrentHitPoints()
	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got := mob.CurrentHitPoints(); got != hitPointsAfterFirst {
		t.Fatalf("repeat attack inside cooldown changed HP from %d to %d", hitPointsAfterFirst, got)
	}
	if got := sent[len(sent)-1]; got.MessageID != opcode.EntityActionResponse || !bytes.Equal(got.Payload(), []byte{2, 0}) {
		t.Fatalf("cooldown response = opcode %04X payload % X, want B074 02 00", got.MessageID, got.Payload())
	}

	now = now.Add(time.Second)
	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got := mob.CurrentHitPoints(); got >= hitPointsAfterFirst {
		t.Fatalf("attack after cooldown left HP at %d, want below %d", got, hitPointsAfterFirst)
	}
	if companions.engagements != 2 || companions.engagedPlayer != player || companions.engagedTarget != mob {
		t.Fatalf("growth pet engagements = %d player=%p target=%p", companions.engagements, companions.engagedPlayer, companions.engagedTarget)
	}
}

func TestCombatNormalAttackRepeatsAtEquippedWeaponCooldown(t *testing.T) {
	now := time.Unix(100, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20},
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{
		SRObject: model.SRObject{
			UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
		},
		Type: "NPC", Mutex: &sync.Mutex{},
	}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	mob.KnownObjectList = model.NewKnownObjectList(mob)
	player.GetKnownObjectList().AddObject(mob)

	var delays []time.Duration
	var callbacks []func()
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			1: {ID: 1, ActionDuration: 600, ReuseDelay: 1000, CoolTime: 800},
		}}),
		nextAttack: make(map[uint32]time.Time),
		now:        func() time.Time { return now },
		schedule: func(delay time.Duration, fn func()) {
			delays = append(delays, delay)
			callbacks = append(callbacks, fn)
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	firstHP := mob.CurrentHitPoints()
	if len(callbacks) != 1 || delays[0] != time.Second {
		t.Fatalf("first repeat schedule = callbacks:%d delays:%v, want one at 1s", len(callbacks), delays)
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin}) {
		t.Fatalf("recurring first-hit packets = %04X, want turn, B074 active, then B070 without end", got)
	}
	now = now.Add(time.Second)
	callbacks[0]()
	if got := mob.CurrentHitPoints(); got >= firstHP {
		t.Fatalf("automatic repeat left HP at %d, want below %d", got, firstHP)
	}
	if len(callbacks) != 2 || delays[1] != time.Second {
		t.Fatalf("second repeat schedule = callbacks:%d delays:%v, want another at 1s", len(callbacks), delays)
	}
	handler.CancelPlayerAttack(session, player.GetUniqueID())
	if got := sent[len(sent)-1]; got.MessageID != opcode.EntityActionResponse || !bytes.Equal(got.Payload(), []byte{2, 0}) {
		t.Fatalf("movement cancellation packet = opcode %04X payload % X, want B074 02 00", got.MessageID, got.Payload())
	}
	hpAfterCancel := mob.CurrentHitPoints()
	now = now.Add(time.Second)
	callbacks[1]()
	if got := mob.CurrentHitPoints(); got != hpAfterCancel {
		t.Fatalf("cancelled automatic repeat changed HP from %d to %d", hpAfterCancel, got)
	}
}

func TestChineseSwordDamageStartsAsTwoHitB070(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Inventory: model.NewInventory(nil),
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{
		TypeID1: model.TypeID1Item, TypeID2: model.TypeID2Wearable, TypeID3: model.TypeID3Weapon, TypeID4: 2,
	}}}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 104, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(1000, 10)
	mob.SetLifeState(model.Alive)
	mob.KnownObjectList = model.NewKnownObjectList(mob)
	player.GetKnownObjectList().AddObject(mob)

	if got := player.BaseAttackHitCount(); got != 2 {
		t.Fatalf("configured Chinese sword hit count = %d, want 2", got)
	}
	var delays []time.Duration
	var callbacks []func()
	var sent []network.Packet
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			2: {ID: 2, ActionDuration: 1200, ReuseDelay: 1200},
		}}),
		nextAttack: make(map[uint32]time.Time),
		repeating:  make(map[uint32]*pendingAttack),
		schedule: func(delay time.Duration, fn func()) {
			delays = append(delays, delay)
			callbacks = append(callbacks, fn)
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}

	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got := mob.CurrentHitPoints(); got >= 1000 {
		t.Fatalf("HP after immediate sword attack = %d, want damage", got)
	}
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityMovementTurnResponse, opcode.EntityActionResponse, opcode.EntitySkillCastBegin}) {
		t.Fatalf("sword start packets = %04X, want turn, B074, B070", got)
	}
	if len(callbacks) != 1 || delays[0] != 1200*time.Millisecond {
		t.Fatalf("repeat schedule = callbacks:%d delays:%v, want one at 1.2s", len(callbacks), delays)
	}
	attack := sent[2]
	for range 19 {
		if _, err := attack.ReadByte(); err != nil {
			t.Fatal(err)
		}
	}
	if hasDamage, _ := attack.ReadBool(); !hasDamage {
		t.Fatal("B070 has no damage block")
	}
	if hitCount, _ := attack.ReadByte(); hitCount != 2 {
		t.Fatalf("B070 hit count = %d, want 2", hitCount)
	}
	if targetCount, _ := attack.ReadByte(); targetCount != 1 {
		t.Fatalf("B070 target count = %d, want 1", targetCount)
	}
}

func packetOpcodesForCombat(packets []network.Packet) []uint16 {
	opcodes := make([]uint16, len(packets))
	for index := range packets {
		opcodes[index] = packets[index].MessageID
	}
	return opcodes
}

func TestCombatOutOfRangeAttackPursuesThenHitsOnArrival(t *testing.T) {
	previous := model.RefItems
	t.Cleanup(func() { model.RefItems = previous })
	model.RefItems = map[uint32]model.RefItem{3632: {
		RefObject: model.RefObject{ID: 3632}, Range: 6,
	}}

	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Inventory: model.NewInventory(nil),
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{
		RefObjectID: 3632,
		TypeInfo:    model.TypeInfo{TypeID1: model.TypeID1Item, TypeID2: model.TypeID2Wearable, TypeID3: model.TypeID3Weapon, TypeID4: 2},
	}}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{
		SRObject: model.SRObject{
			UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, 200, 0, 100),
		},
		Type: "NPC", Mutex: &sync.Mutex{},
	}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)
	world := &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}, distance: 100}

	var scheduled []func()
	var destination navmeshv2.RtNavmeshPosition
	var sent []network.Packet
	handler := &CombatHandler{
		world: world, pending: make(map[uint32]*pendingAttack),
		schedule: func(_ time.Duration, fn func()) { scheduled = append(scheduled, fn) },
		move: func(character *model.Player, target navmeshv2.RtNavmeshPosition) uint64 {
			destination = target
			movement := character.BeginMovement(model.MovementData{TargetPosition: target, HasDestination: true})
			return movement.Generation
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	packet := combatAttackPacket(mob.GetUniqueID())
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("out-of-range attack sent combat packets before arrival: %#v", sent)
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled pursuit polls = %d, want 1", len(scheduled))
	}
	if got := destination.GetGlobalCoordinates().X; math32.Abs(got-mob.GetNavmeshPosition().GetGlobalCoordinates().X) > 0.01 {
		t.Fatalf("pursuit destination x = %g, want target position", got)
	}

	// Repeated client clicks refresh the destination of the same queued task,
	// matching the original MoveToTarget behavior without queuing another hit.
	packet = combatAttackPacket(mob.GetUniqueID())
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if len(scheduled) != 1 {
		t.Fatalf("duplicate click scheduled %d polls, want 1", len(scheduled))
	}
	if movement := player.GetMovementData(); movement == nil || movement.Generation != 2 {
		t.Fatalf("refreshed pursuit movement = %#v, want generation 2", movement)
	}

	player.SetNavmeshPosition(combatTestPosition(24231, 190, 800, 100))
	scheduled[0]()
	if len(sent) != 0 {
		t.Fatalf("pursuit sent combat packets before movement completion: %#v", sent)
	}
	movement := player.GetMovementData()
	if movement == nil || !player.FinalizeMovement(movement.Generation, player.GetNavmeshPosition(), "arrived") {
		t.Fatal("failed to finalize pursuit movement")
	}
	scheduled[1]()
	if len(sent) != 4 || sent[0].MessageID != opcode.EntityMovementTurnResponse || sent[1].MessageID != opcode.EntityActionResponse || sent[2].MessageID != opcode.EntitySkillCastBegin || sent[3].MessageID != opcode.EntityActionResponse {
		t.Fatalf("arrival combat sequence = %#v", sent)
	}
	if mob.CurrentHitPoints() >= 100 {
		t.Fatal("queued attack did not damage mob on arrival")
	}
	if world.stops != 0 {
		t.Fatalf("pursuit movement stops = %d, want movement to finish naturally", world.stops)
	}
}

func TestCombatPursuitHasNoArtificialTimeoutOrRestartLimit(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Inventory: model.NewInventory(nil),
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{
		SRObject: model.SRObject{
			UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, 600, 0, 100),
		},
		Type: "NPC", Mutex: &sync.Mutex{},
	}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)

	world := &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}}
	var scheduled []func()
	var sent []network.Packet
	moves := 0
	handler := &CombatHandler{
		world: world, pending: make(map[uint32]*pendingAttack),
		schedule: func(_ time.Duration, fn func()) { scheduled = append(scheduled, fn) },
		move: func(character *model.Player, target navmeshv2.RtNavmeshPosition) uint64 {
			moves++
			movement := character.BeginMovement(model.MovementData{TargetPosition: target, HasDestination: true})
			return movement.Generation
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}

	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 6; restart++ {
		movement := player.GetMovementData()
		if movement == nil || !player.FinalizeMovement(movement.Generation, player.GetNavmeshPosition(), "arrived-stale-target") {
			t.Fatalf("restart %d could not finish pursuit movement", restart)
		}
		callback := scheduled[len(scheduled)-1]
		callback()
	}
	if moves != 7 {
		t.Fatalf("pursuit movements = %d, want initial plus 6 restarts", moves)
	}
	if len(handler.pending) != 1 {
		t.Fatal("valid moving target was dropped by an artificial pursuit limit")
	}
	if len(sent) != 0 {
		t.Fatalf("long pursuit sent premature action packets: %#v", sent)
	}
}

func TestCombatOutOfRangeAttackRejectsObstructedPursuitBeforeMovement(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Inventory: model.NewInventory(nil),
	}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{
		SRObject: model.SRObject{
			UniqueID: 200, RefObjectID: 1933, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
			RtNavmeshPosition: combatTestPosition(24231, 200, 0, 100),
		},
		Type: "NPC", Mutex: &sync.Mutex{},
	}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	player.GetKnownObjectList().AddObject(mob)

	moves := 0
	var sent []network.Packet
	handler := &CombatHandler{
		world:   &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}, blocked: true},
		pending: make(map[uint32]*pendingAttack),
		move: func(*model.Player, navmeshv2.RtNavmeshPosition) uint64 {
			moves++
			return 1
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}

	if err := handler.handlePacket(context.Background(), session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if moves != 0 {
		t.Fatalf("obstructed pursuit registered %d movements, want 0", moves)
	}
	if len(handler.pending) != 0 {
		t.Fatal("obstructed pursuit left a queued attack")
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.EntityActionResponse || !bytes.Equal(sent[0].Payload(), []byte{2, 0}) {
		t.Fatalf("obstructed pursuit response = %#v, want B074 false", sent)
	}
}

func combatAttackPacket(targetUID uint32) network.Packet {
	packet := network.EmptyClientPacket()
	packet.WriteByte(actionPhaseStart)
	packet.WriteByte(actionTypeAttack)
	packet.WriteUInt32(targetUID)
	packet.WriteByte(0)
	return packet
}

func combatSkillPacket(skillID, targetUID uint32) network.Packet {
	packet := network.EmptyClientPacket()
	packet.WriteByte(actionPhaseStart)
	packet.WriteByte(actionTypeAttack)
	packet.WriteUInt32(skillID)
	packet.WriteByte(1)
	packet.WriteUInt32(targetUID)
	return packet
}

func combatSelfSkillPacket(skillID uint32) network.Packet {
	packet := network.EmptyClientPacket()
	packet.WriteByte(actionPhaseStart)
	packet.WriteByte(actionTypeAttack)
	packet.WriteUInt32(skillID)
	packet.WriteByte(0)
	return packet
}

func combatCancelBuffPacket(skillID uint32) network.Packet {
	packet := network.EmptyClientPacket()
	packet.WriteByte(actionPhaseStart)
	packet.WriteByte(actionTypeDispel)
	packet.WriteUInt32(skillID)
	packet.WriteByte(0)
	return packet
}

func combatTestPosition(regionID int16, x, y, z float32) navmeshv2.RtNavmeshPosition {
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(regionID),
		Offset: math32.NewVector3(x, y, z),
	}
}

func TestBuildActionStateResponseMatchesVSRO188Parser(t *testing.T) {
	for _, test := range []struct {
		name   string
		active bool
		want   []byte
	}{
		{name: "enter action", active: true, want: []byte{1, 1}},
		{name: "exit rejected action", active: false, want: []byte{2, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := buildActionStateResponse(test.active)
			if packet.MessageID != opcode.EntityActionResponse {
				t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntityActionResponse)
			}
			if got := packet.Payload(); !bytes.Equal(got, test.want) {
				t.Fatalf("payload = % X, want % X", got, test.want)
			}
		})
	}
}

func TestBuildAttackBeginSerializesNormalAndLethalDamage(t *testing.T) {
	for _, test := range []struct {
		name        string
		killed      bool
		attackState byte
	}{
		{name: "normal hit", attackState: 0},
		{name: "lethal hit", killed: true, attackState: 128},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := buildAttackBegin(10, 20, 30, 0x010203, test.killed)
			if packet.MessageID != opcode.EntitySkillCastBegin {
				t.Fatalf("opcode = %#x", packet.MessageID)
			}
			for _, want := range []byte{1, 2, 0x30} {
				got, err := packet.ReadByte()
				if err != nil || got != want {
					t.Fatalf("prefix byte = %d, want %d: %v", got, want, err)
				}
			}
			if skill, _ := packet.ReadUInt32(); skill != 1 {
				t.Fatalf("skill = %d", skill)
			}
			if source, _ := packet.ReadUInt32(); source != 10 {
				t.Fatalf("source = %d", source)
			}
			if cast, _ := packet.ReadUInt32(); cast != 30 {
				t.Fatalf("cast = %d", cast)
			}
			if delegated, _ := packet.ReadUInt32(); delegated != 20 {
				t.Fatalf("target UID = %d, want 20", delegated)
			}
			hasDamage, _ := packet.ReadBool()
			if !hasDamage {
				t.Fatal("missing damage block")
			}
			if hitCount, _ := packet.ReadByte(); hitCount != 1 {
				t.Fatalf("hits = %d", hitCount)
			}
			if targetCount, _ := packet.ReadByte(); targetCount != 1 {
				t.Fatalf("targets = %d", targetCount)
			}
			if target, _ := packet.ReadUInt32(); target != 20 {
				t.Fatalf("damage target = %d", target)
			}
			if attackState, _ := packet.ReadByte(); attackState != test.attackState {
				t.Fatalf("attack state = %d", attackState)
			}
			if damageState, _ := packet.ReadByte(); damageState != 1 {
				t.Fatalf("damage state = %d", damageState)
			}
			if damage, _ := packet.ReadUInt32(); damage != 0x010203 {
				t.Fatalf("damage = %#x", damage)
			}
			if packet.Remaining() != 3 {
				t.Fatalf("status bytes = %d, want 3", packet.Remaining())
			}
		})
	}
}

func TestScaleCombatRewardUsesIntegerPercentRates(t *testing.T) {
	for _, test := range []struct {
		base uint64
		rate int
		want uint64
	}{{100, 100, 100}, {100, 200, 200}, {100, 50, 50}, {100, 0, 0}} {
		if got := scaleCombatReward(test.base, test.rate); got != test.want {
			t.Fatalf("scaleCombatReward(%d, %d) = %d, want %d", test.base, test.rate, got, test.want)
		}
	}
}
