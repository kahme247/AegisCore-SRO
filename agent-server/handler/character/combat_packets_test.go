package character

import (
	"bytes"
	"context"
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

type combatVitalsFake struct {
	spendCalls int
	expectedMP int
	cost       int
	spent      bool
	saveCalls  int
	savedID    int
	savedHP    int
	savedMP    int
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
			if len(sent) != 3 || sent[0].MessageID != opcode.EntityActionResponse || sent[1].MessageID != opcode.EntitySkillCastBegin || sent[2].MessageID != opcode.EntityActionResponse {
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
	for _, mob := range []*model.NPC{secondary1, secondary2} {
		if got, want := mob.CurrentHitPoints(), 8612; got != want {
			t.Fatalf("secondary %d HP = %d, want %d", mob.GetUniqueID(), got, want)
		}
		status, exists := mob.CombatStatus(model.CombatStatusShock, now)
		if !exists || status.Power != 240 || !status.ExpiresAt.Equal(now.Add(5*time.Second)) {
			t.Fatalf("secondary %d shock = %+v exists=%t", mob.GetUniqueID(), status, exists)
		}
	}
	if outside.CurrentHitPoints() != 10000 {
		t.Fatalf("outside target HP = %d, want untouched", outside.CurrentHitPoints())
	}
	if status, exists := primary.CombatStatus(model.CombatStatusShock, now); !exists || status.Power != 240 {
		t.Fatalf("primary shock = %+v exists=%t", status, exists)
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
	if len(sent) != 1 || sent[0].MessageID != opcode.EntitySkillCastBegin {
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
			skillID: {ID: skillID, BasicActivity: 2, BasicChainCode: 323, ActionDuration: 428, ReuseDelay: 8000, AttackType: 5, AttackPercent: 30, AttackMin: 21, AttackMax: 26},
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
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityActionResponse, opcode.EntitySkillCastBegin, opcode.EntityActionResponse}) {
		t.Fatalf("skill cast packets = %04X, want B074 B070 B074", got)
	}
	cast := sent[1]
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
	for range 3 {
		if _, err := cast.ReadUInt32(); err != nil {
			t.Fatal(err)
		}
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
	if len(sent) != 4 || sent[3].MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("magical cast packets = %04X, want action, damage, end, vitals", packetOpcodesForCombat(sent))
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
	player.SetLifeState(model.Alive)
	vitals := &combatVitalsFake{spent: true}
	var sent []network.Packet
	var expire func()
	handler := &CombatHandler{
		world:  &combatWorldFake{player: player},
		vitals: vitals,
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			skillID: {ID: skillID, BasicActivity: 2, BuffDuration: 15000, ReuseDelay: 30000, RequiredWeapon1: 6, RequiredWeapon2: 255, ConsumeMP: 10, BuffModifiers: model.BuffModifiers{HP: 50, Strength: 5, PhysicalDefense: 12}},
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
	if len(player.ActiveBuffs) != 0 || player.BonusAttackStats.PhyAttackPercent != 0 {
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
		player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{TypeInfo: model.TypeInfo{
			TypeID1: 3,
			TypeID2: 1,
			TypeID3: 6,
			TypeID4: typeID4,
		}}}
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
	handler := &CombatHandler{
		world: &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{200: mob}},
		referenceData: model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
			1: {ID: 1, ActionDuration: 600, ReuseDelay: 1000, CoolTime: 800},
		}}),
		nextAttack: make(map[uint32]time.Time),
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
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityActionResponse, opcode.EntitySkillCastBegin}) {
		t.Fatalf("recurring first-hit packets = %04X, want B074 active then B070 without end", got)
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
	if got := packetOpcodesForCombat(sent); !reflect.DeepEqual(got, []uint16{opcode.EntityActionResponse, opcode.EntitySkillCastBegin}) {
		t.Fatalf("sword start packets = %04X, want B074 B070", got)
	}
	if len(callbacks) != 1 || delays[0] != 1200*time.Millisecond {
		t.Fatalf("repeat schedule = callbacks:%d delays:%v, want one at 1.2s", len(callbacks), delays)
	}
	attack := sent[1]
	for range 20 {
		if _, err := attack.ReadByte(); err != nil {
			t.Fatal(err)
		}
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
	if got := destination.GetGlobalCoordinates().X; math32.Abs(got-(mob.GetNavmeshPosition().GetGlobalCoordinates().X-10)) > 0.01 {
		t.Fatalf("pursuit destination x = %g, want target minus 10", got)
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
	if len(sent) != 3 || sent[0].MessageID != opcode.EntityActionResponse || sent[1].MessageID != opcode.EntitySkillCastBegin || sent[2].MessageID != opcode.EntityActionResponse {
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
			if target, _ := packet.ReadUInt32(); target != 20 {
				t.Fatalf("target = %d", target)
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
