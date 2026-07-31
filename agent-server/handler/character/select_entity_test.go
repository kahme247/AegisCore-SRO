package character

import (
	"context"
	"errors"
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

type selectEntityWorldFake struct {
	player        *model.Player
	playerErr     error
	objects       map[uint32]model.ISRObject
	objectErr     error
	distance      float32
	playerLookups int
	objectLookups int
	distanceCalls int
}

type selectEntityPacketWriter struct{}

func (*selectEntityPacketWriter) Write(data []byte) (int, error)   { return len(data), nil }
func (*selectEntityPacketWriter) SetWriteDeadline(time.Time) error { return nil }

func (f *selectEntityWorldFake) GetPlayerByUniqueId(_ uint32) (*model.Player, error) {
	f.playerLookups++
	return f.player, f.playerErr
}

func (f *selectEntityWorldFake) GetObjectByUniqueId(uid uint32) (model.ISRObject, error) {
	f.objectLookups++
	if f.objectErr != nil {
		return nil, f.objectErr
	}
	object, ok := f.objects[uid]
	if !ok {
		return nil, fmt.Errorf("object %d not found", uid)
	}
	return object, nil
}

func (f *selectEntityWorldFake) DistanceTo(_, _ navmeshv2.RtNavmeshPosition) float32 {
	f.distanceCalls++
	return f.distance
}

func TestSelectEntityAdmitsKnownInRangeMobAndSendsResponse(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	mob := selectEntityNPC(npc.GetUniqueID(), model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1}, npc.GetNavmeshPosition())
	player.GetKnownObjectList().RemoveObject(npc)
	player.GetKnownObjectList().AddObject(mob)
	world := &selectEntityWorldFake{
		player:   player,
		objects:  map[uint32]model.ISRObject{mob.GetUniqueID(): mob},
		distance: selectionKnownRange,
	}

	err := NewSelectEntityHandler(world).handlePacket(context.Background(), session, selectEntityPacket(t, mob.GetUniqueID()))
	if err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	if world.playerLookups != 1 || world.objectLookups != 2 || world.distanceCalls != 1 {
		t.Fatalf("lookup calls = player:%d object:%d distance:%d, want player=1 object=2 distance=1", world.playerLookups, world.objectLookups, world.distanceCalls)
	}
	// Mobs are selectable for combat but must not open an NPC interaction menu.
}

func TestSelectEntityAdmitsKnownInRangePlayerAndSendsStockResponse(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	target := &model.Player{
		SRObject: model.SRObject{
			UniqueID:          102,
			TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 1},
			RtNavmeshPosition: selectEntityPosition(30000, 20, 0, 20),
		},
		ID:       43,
		CharName: "Target",
	}
	if !player.GetKnownObjectList().AddObject(target) {
		t.Fatal("fixture could not add player target")
	}
	world := &selectEntityWorldFake{
		player:   player,
		objects:  map[uint32]model.ISRObject{target.GetUniqueID(): target},
		distance: selectionKnownRange,
	}
	var sent network.Packet
	handler := NewSelectEntityHandler(world)
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = packet
		return nil
	}

	if err := handler.handlePacket(context.Background(), session, selectEntityPacket(t, target.GetUniqueID())); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	if sent.MessageID != opcode.EntitySelectResponse {
		t.Fatalf("message id = 0x%04X, want 0x%04X", sent.MessageID, opcode.EntitySelectResponse)
	}
	if got, want := sent.Payload(), []byte{1, 102, 0, 0, 0, 1, 5, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestSelectEntityIncludesActiveJobLevel(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	target := &model.Player{
		SRObject: model.SRObject{
			UniqueID:          103,
			TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 1},
			RtNavmeshPosition: selectEntityPosition(30000, 20, 0, 20),
		},
		ID:       44,
		CharName: "Hunter",
		Job:      model.PlayerJob{Type: model.PlayerJobHunter, Level: 6},
	}
	if !player.GetKnownObjectList().AddObject(target) {
		t.Fatal("fixture could not add player target")
	}
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{target.GetUniqueID(): target}, distance: selectionKnownRange}
	var sent network.Packet
	handler := NewSelectEntityHandler(world)
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	if err := handler.handlePacket(context.Background(), session, selectEntityPacket(t, target.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if got, want := sent.Payload(), []byte{1, 103, 0, 0, 0, 1, 5, model.PlayerJobHunter, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestBuildMobSelectionResponseIncludesCurrentHealth(t *testing.T) {
	packet := buildMobSelectionResponse(0x12345678, 1234)
	if packet.MessageID != opcode.EntitySelectResponse {
		t.Fatalf("message id = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntitySelectResponse)
	}
	if got, want := packet.Payload(), []byte{1, 0x78, 0x56, 0x34, 0x12, 1, 0xD2, 0x04, 0, 0, 0x01, 0x05}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestBuildNPCSelectionResponseUsesCountedMerchantActions(t *testing.T) {
	packet := buildNPCSelectionResponse(0x12345678, []byte{0, 1, 1, 0})
	if packet.MessageID != opcode.EntitySelectResponse {
		t.Fatalf("message id = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntitySelectResponse)
	}
	if got, want := packet.Payload(), []byte{1, 0x78, 0x56, 0x34, 0x12, 0, 1, 1, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestBuildNPCSelectionResponseUsesGachaTaskBody(t *testing.T) {
	packet := buildNPCSelectionResponse(0x12345678, model.NPCSelectionTalkData("NPC_CH_GACHA_MACHINE"))
	want := []byte{1, 0x78, 0x56, 0x34, 0x12, 0, 1, 0x11, 0}
	if got := packet.Payload(); !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestBuildEntitySelectionFailureClearsClientSelectionWait(t *testing.T) {
	packet := buildEntitySelectionFailure()
	if packet.MessageID != opcode.EntitySelectResponse {
		t.Fatalf("message id = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntitySelectResponse)
	}
	if got, want := packet.Payload(), []byte{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestBuildTeleportGateSelectionResponseUsesConditionalFortressTax(t *testing.T) {
	for _, test := range []struct {
		name string
		gate *model.TeleportGate
		want []byte
	}{
		{name: "active Jangan fortress", gate: &model.TeleportGate{TaxActive: true}, want: []byte{1, 0x78, 0x56, 0x34, 0x12, 2, 7, 8, 0, 0}},
		{name: "inactive Donwhang fortress", gate: &model.TeleportGate{}, want: []byte{1, 0x78, 0x56, 0x34, 0x12, 2, 7, 8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := buildNPCSelectionResponse(0x12345678, teleportGateSelectionData(test.gate))
			if packet.MessageID != opcode.EntitySelectResponse {
				t.Fatalf("message id = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntitySelectResponse)
			}
			if got := packet.Payload(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("payload = %x, want complete gate selection body %x", got, test.want)
			}
		})
	}
}

func TestTeleportGateIsSelectableOnlyWhenKnownAndInRange(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	gate := &model.TeleportGate{SRObject: model.SRObject{
		UniqueID: 222, RefObjectID: 2094, Name: "STORE_CH_GATE",
		TypeInfo:          model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1},
		RtNavmeshPosition: selectEntityPosition(30000, 20, 0, 20),
	}}
	if !player.GetKnownObjectList().AddObject(gate) {
		t.Fatal("fixture could not add teleport gate")
	}
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{gate.GetUniqueID(): gate}, distance: selectionKnownRange}
	if err := validateEntitySelection(world, player, gate.GetUniqueID()); err != nil {
		t.Fatalf("known teleport gate rejected: %v", err)
	}
	session.UserContext.UniqueID = player.GetUniqueID()
}

func TestSelectEntityRecordsNumberedCatalogTeleportGateInteraction(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	gate := &model.TeleportGate{SRObject: model.SRObject{
		UniqueID: 222, RefObjectID: 27754, Name: "STORE_SD_GATE2",
		TypeInfo:          model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1},
		RtNavmeshPosition: selectEntityPosition(30000, 20, 0, 20),
	}}
	if !player.GetKnownObjectList().AddObject(gate) {
		t.Fatal("fixture could not add numbered teleport gate")
	}
	t.Cleanup(func() { npcInteractions.Clear(player.GetUniqueID()) })
	world := &selectEntityWorldFake{
		player: player, objects: map[uint32]model.ISRObject{gate.GetUniqueID(): gate}, distance: selectionKnownRange,
	}

	if err := NewSelectEntityHandler(world).handlePacket(context.Background(), session, selectEntityPacket(t, gate.GetUniqueID())); err != nil {
		t.Fatalf("select numbered teleport gate: %v", err)
	}
	interaction, ok := npcInteractions.Get(player.GetUniqueID())
	if !ok || interaction.NPCUniqueID != gate.GetUniqueID() || interaction.NPCRefObjectID != gate.GetRefObjectID() || interaction.Action != teleportTaskUse {
		t.Fatalf("numbered teleport gate interaction = %+v/%v", interaction, ok)
	}
}

func TestTeleportGateUsesExtendedSelectionRange(t *testing.T) {
	_, player, _ := selectEntityFixture(t)
	gate := &model.TeleportGate{SRObject: model.SRObject{
		UniqueID: 223, RefObjectID: 2094, Name: "STORE_CH_GATE",
		TypeInfo:          model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1},
		RtNavmeshPosition: selectEntityPosition(30000, 20, 0, 20),
	}}
	if !player.GetKnownObjectList().AddObject(gate) {
		t.Fatal("fixture could not add teleport gate")
	}
	world := &selectEntityWorldFake{
		player: player, objects: map[uint32]model.ISRObject{gate.GetUniqueID(): gate}, distance: selectionKnownRange + 500,
	}
	if err := validateEntitySelection(world, player, gate.GetUniqueID()); err != nil {
		t.Fatalf("teleport gate inside extended range rejected: %v", err)
	}
}

func TestSelectEntityAdvertisesDimensionHoleTravelAction(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	portal := selectEntityDimensionHolePortal(t, player, 0x12345678, selectEntityPosition(30000, 20, 0, 20))
	if !player.GetKnownObjectList().AddObject(portal) {
		t.Fatal("fixture could not add Dimension Hole portal")
	}
	npcInteractions.Clear(player.GetUniqueID())
	t.Cleanup(func() { npcInteractions.Clear(player.GetUniqueID()) })
	world := &selectEntityWorldFake{
		player: player, objects: map[uint32]model.ISRObject{portal.GetUniqueID(): portal}, distance: selectionKnownRange + 500,
	}
	var sent network.Packet
	handler := NewSelectEntityHandler(world)
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }

	if err := handler.handlePacket(context.Background(), session, selectEntityPacket(t, portal.GetUniqueID())); err != nil {
		t.Fatalf("select Dimension Hole portal: %v", err)
	}
	if sent.MessageID != opcode.EntitySelectResponse {
		t.Fatalf("message id = 0x%04X, want 0x%04X", sent.MessageID, opcode.EntitySelectResponse)
	}
	if got, want := sent.Payload(), []byte{1, 0x78, 0x56, 0x34, 0x12, 1, npcTalkActionConversation}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want portal travel selection %x", got, want)
	}
	if interaction, ok := npcInteractions.Get(player.GetUniqueID()); ok {
		t.Fatalf("portal selection entered normal teleport interaction path: %+v", interaction)
	}
	if session.UserContext.SelectedNPCUniqueID != 0 || session.UserContext.SelectedNPCRefObjID != 0 {
		t.Fatalf("portal selection entered normal NPC state uid=%d ref=%d", session.UserContext.SelectedNPCUniqueID, session.UserContext.SelectedNPCRefObjID)
	}
}

func TestDimensionHolePortalSelectionRequiresExactKnownIdentity(t *testing.T) {
	_, player, _ := selectEntityFixture(t)
	position := selectEntityPosition(30000, 20, 0, 20)
	portal := selectEntityDimensionHolePortal(t, player, 224, position)
	stalePortal := selectEntityDimensionHolePortal(t, player, portal.GetUniqueID(), position)
	if !player.GetKnownObjectList().AddObject(stalePortal) {
		t.Fatal("fixture could not add stale Dimension Hole portal")
	}
	world := &selectEntityWorldFake{
		player: player, objects: map[uint32]model.ISRObject{portal.GetUniqueID(): portal}, distance: selectionKnownRange,
	}

	err := validateEntitySelection(world, player, portal.GetUniqueID())
	if !errors.Is(err, ErrSelectEntityTargetNotKnown) {
		t.Fatalf("validateEntitySelection() error = %v, want %v", err, ErrSelectEntityTargetNotKnown)
	}
}

func TestDimensionHolePortalUsesBoundedExtendedSelectionRange(t *testing.T) {
	_, player, _ := selectEntityFixture(t)
	portal := selectEntityDimensionHolePortal(t, player, 225, selectEntityPosition(30000, 20, 0, 20))
	if !player.GetKnownObjectList().AddObject(portal) {
		t.Fatal("fixture could not add Dimension Hole portal")
	}

	for _, test := range []struct {
		name     string
		distance float32
		want     error
	}{
		{name: "at extended boundary", distance: float32(model.RegionHeight)},
		{name: "past extended boundary", distance: float32(model.RegionHeight) + 1, want: ErrSelectEntityTargetOutOfRange},
	} {
		t.Run(test.name, func(t *testing.T) {
			world := &selectEntityWorldFake{
				player: player, objects: map[uint32]model.ISRObject{portal.GetUniqueID(): portal}, distance: test.distance,
			}
			err := validateEntitySelection(world, player, portal.GetUniqueID())
			if !errors.Is(err, test.want) {
				t.Fatalf("validateEntitySelection() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateNPCSelectionAdmitsKnownMobAcrossAdjacentRegionBoundary(t *testing.T) {
	_, player, npc := selectEntityFixture(t)
	npc.SetNavmeshPosition(selectEntityPosition(30001, 5, 0, 5))
	world := &selectEntityWorldFake{
		player:   player,
		objects:  map[uint32]model.ISRObject{npc.GetUniqueID(): npc},
		distance: 20,
	}

	if err := validateEntitySelection(world, player, npc.GetUniqueID()); err != nil {
		t.Fatalf("known adjacent-region mob rejected: %v", err)
	}
	if world.distanceCalls != 1 {
		t.Fatalf("adjacent-region selection distance checks = %d, want 1", world.distanceCalls)
	}
}

func TestSelectEntityRejectsMalformedAndTrailingPacketsBeforeWorldLookups(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	for name, packet := range map[string]network.Packet{
		"short":    network.EmptyClientPacket(),
		"trailing": selectEntityPacket(t, npc.GetUniqueID(), 0xFF),
	} {
		t.Run(name, func(t *testing.T) {
			world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}}
			err := NewSelectEntityHandler(world).handlePacket(context.Background(), session, packet)
			if err == nil {
				t.Fatal("handlePacket() accepted malformed selection packet")
			}
			if world.playerLookups != 0 || world.objectLookups != 0 || world.distanceCalls != 0 {
				t.Fatalf("malformed packet reached world: player:%d object:%d distance:%d", world.playerLookups, world.objectLookups, world.distanceCalls)
			}
		})
	}
}

func TestSelectEntityRejectsUnauthorisedOrUnsupportedTargets(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*server.Session, *model.Player)
		target   func(*model.NPC) model.ISRObject
		present  bool
		known    bool
		distance float32
		want     error
	}{
		{
			name: "missing session identity",
			mutate: func(session *server.Session, _ *model.Player) {
				session.UserContext.UniqueID = 0
			},
			target:  func(npc *model.NPC) model.ISRObject { return npc },
			present: true,
			known:   true,
			want:    ErrSelectEntitySessionIdentityInvalid,
		},
		{
			name:    "session does not own player",
			mutate:  func(session *server.Session, player *model.Player) { player.Session = &server.Session{} },
			target:  func(npc *model.NPC) model.ISRObject { return npc },
			present: true,
			known:   true,
			want:    ErrSelectEntitySessionPlayerMismatch,
		},
		{
			name:   "target absent",
			target: func(npc *model.NPC) model.ISRObject { return npc },
			known:  true,
			want:   ErrSelectEntityTargetNotFound,
		},
		{
			name:    "target not known",
			target:  func(npc *model.NPC) model.ISRObject { return npc },
			present: true,
			want:    ErrSelectEntityTargetNotKnown,
		},
		{
			name: "mob accepted for combat",
			target: func(npc *model.NPC) model.ISRObject {
				return selectEntityNPC(200, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1}, npc.GetNavmeshPosition())
			},
			present: true,
			known:   true,
		},
		{
			name: "known adjacent region",
			target: func(_ *model.NPC) model.ISRObject {
				return selectEntityNPC(201, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}, selectEntityPosition(30001, 5, 0, 5))
			},
			present: true,
			known:   true,
		},
		{
			name:     "outside established visibility range",
			target:   func(npc *model.NPC) model.ISRObject { return npc },
			present:  true,
			known:    true,
			distance: selectionKnownRange + 1,
			want:     ErrSelectEntityTargetOutOfRange,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, player, npc := selectEntityFixture(t)
			target := test.target(npc)
			knownList := player.GetKnownObjectList()
			if knownList == nil {
				t.Fatal("fixture player has no known-object list")
			}
			if test.known {
				knownList.AddObject(target)
			} else {
				knownList.RemoveObject(target)
			}
			if test.mutate != nil {
				test.mutate(session, player)
			}
			objects := make(map[uint32]model.ISRObject)
			if test.present {
				objects[target.GetUniqueID()] = target
			}
			world := &selectEntityWorldFake{player: player, objects: objects, distance: test.distance}
			err := NewSelectEntityHandler(world).handlePacket(context.Background(), session, selectEntityPacket(t, target.GetUniqueID()))
			if errors.Is(test.want, ErrSelectEntitySessionIdentityInvalid) || errors.Is(test.want, ErrSelectEntitySessionPlayerMismatch) {
				if !errors.Is(err, test.want) {
					t.Fatalf("handlePacket() error = %v, want %v", err, test.want)
				}
			} else if err != nil {
				t.Fatalf("handlePacket() error = %v, want stale or invalid target to be ignored", err)
			}
		})
	}
}

func TestSelectEntityRejectsInvalidCurrentPositions(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	// SetNavmeshPosition logs Position.String(), which intentionally assumes an
	// offset is present. Assign this deliberately malformed test state directly
	// so the guard itself, rather than the diagnostic formatter, is exercised.
	player.RtNavmeshPosition = navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(30000)}
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}}

	err := NewSelectEntityHandler(world).handlePacket(context.Background(), session, selectEntityPacket(t, npc.GetUniqueID()))
	if err != nil {
		t.Fatalf("handlePacket() error = %v, want invalid target position to be ignored", err)
	}
	if world.distanceCalls != 0 {
		t.Fatalf("invalid position reached range check %d times", world.distanceCalls)
	}
}

func selectEntityFixture(t *testing.T) (*server.Session, *model.Player, *model.NPC) {
	t.Helper()
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	session.Sender = server.NewSender(&selectEntityPacketWriter{}, func(network.Packet) ([]byte, error) {
		return []byte{0}, nil
	})
	t.Cleanup(session.Sender.Close)
	position := selectEntityPosition(30000, 10, 0, 10)
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: position},
		ID:       42,
		Session:  session,
	}
	player.KnownObjectList = model.NewKnownObjectList(player)
	npc := selectEntityNPC(101, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}, selectEntityPosition(30000, 20, 0, 20))
	if !player.GetKnownObjectList().AddObject(npc) {
		t.Fatal("fixture could not add normal NPC to player known-object list")
	}
	return session, player, npc
}

func selectEntityNPC(uid uint32, typeInfo model.TypeInfo, position navmeshv2.RtNavmeshPosition) *model.NPC {
	return &model.NPC{
		SRObject: model.SRObject{UniqueID: uid, TypeInfo: typeInfo, RtNavmeshPosition: position},
		Type:     "NPC",
		Mutex:    &sync.Mutex{},
	}
}

func selectEntityDimensionHolePortal(t *testing.T, owner *model.Player, uid uint32, position navmeshv2.RtNavmeshPosition) *model.DimensionHolePortal {
	t.Helper()
	owner.CharName = "PortalOwner"
	definition, ok := model.DimensionHoleDefinitionForItem(35883)
	if !ok {
		t.Fatal("Dimension Hole fixture definition is missing")
	}
	portal, err := model.NewDimensionHolePortal(definition, owner, position)
	if err != nil {
		t.Fatalf("create Dimension Hole portal fixture: %v", err)
	}
	portal.UniqueID = uid
	return portal
}

func selectEntityPosition(regionID int16, x, y, z float32) navmeshv2.RtNavmeshPosition {
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(regionID),
		Offset: math32.NewVector3(x, y, z),
	}
}

func selectEntityPacket(t *testing.T, uniqueID uint32, trailing ...byte) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt32(uniqueID); err != nil {
		t.Fatalf("write entity selection UID: %v", err)
	}
	if err := packet.WriteBytes(trailing); err != nil {
		t.Fatalf("write entity selection trailing payload: %v", err)
	}
	return packet
}
