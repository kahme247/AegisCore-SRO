package character

import (
	"context"
	"reflect"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func TestNPCTalkAdmitsKnownNearbyNormalNPC(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	world := &selectEntityWorldFake{
		player:   player,
		objects:  map[uint32]model.ISRObject{npc.GetUniqueID(): npc},
		distance: selectionKnownRange,
	}
	handler := NewNPCTalkHandler(world)
	handler.send = func(_ *server.Session, packet network.Packet) error {
		if packet.MessageID != opcode.EntityTalkResponse {
			t.Fatalf("message id = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntityTalkResponse)
		}
		if got, want := packet.Payload(), []byte{1, npcTalkActionConversation}; !reflect.DeepEqual(got, want) {
			t.Fatalf("payload = %x, want %x", got, want)
		}
		return nil
	}
	if err := handler.handlePacket(context.Background(), session, npcTalkPacket(t, npc.GetUniqueID())); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
}

func TestNPCTalkRejectsMalformedPayloadBeforeWorldLookup(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}}
	if err := NewNPCTalkHandler(world).handlePacket(context.Background(), session, network.EmptyClientPacket()); err == nil {
		t.Fatal("handlePacket accepted a malformed talk packet")
	}
	if world.playerLookups != 0 || world.objectLookups != 0 {
		t.Fatalf("malformed request reached world: player=%d object=%d", world.playerLookups, world.objectLookups)
	}
}

func TestNPCTalkAdmitsNonConversationMenuAction(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, distance: selectionKnownRange}
	packet := npcTalkPacket(t, npc.GetUniqueID())
	payload := packet.Payload()
	payload[len(payload)-1] = 1
	handler := NewNPCTalkHandler(world)
	handler.send = func(_ *server.Session, response network.Packet) error {
		if got, want := response.Payload(), []byte{1, 1}; !reflect.DeepEqual(got, want) {
			t.Fatalf("payload = %x, want %x", got, want)
		}
		return nil
	}
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	if world.playerLookups != 1 || world.objectLookups != 2 {
		t.Fatalf("menu action lookups = player=%d object=%d, want player=1 object=2", world.playerLookups, world.objectLookups)
	}
}

func TestNPCTalkAcceptsStorageUInt32ActionEncoding(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, distance: selectionKnownRange}
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt32(npc.GetUniqueID()); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := packet.WriteUInt32(4); err != nil {
		t.Fatalf("write storage action: %v", err)
	}
	handler := NewNPCTalkHandler(world)
	handler.send = func(_ *server.Session, response network.Packet) error {
		if got, want := response.Payload(), []byte{1, 4}; !reflect.DeepEqual(got, want) {
			t.Fatalf("payload = %x, want %x", got, want)
		}
		return nil
	}
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
}

func TestNPCTalkIgnoresMobTarget(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	mob := selectEntityNPC(npc.GetUniqueID(), model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1}, npc.GetNavmeshPosition())
	player.GetKnownObjectList().RemoveObject(npc)
	player.GetKnownObjectList().AddObject(mob)
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{mob.GetUniqueID(): mob}, distance: selectionKnownRange}
	if err := NewNPCTalkHandler(world).handlePacket(context.Background(), session, npcTalkPacket(t, mob.GetUniqueID())); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
}

func TestNPCTalkOpensKnownTeleportGateAction(t *testing.T) {
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
	handler := NewNPCTalkHandler(world)
	handler.send = func(_ *server.Session, response network.Packet) error {
		if got, want := response.Payload(), []byte{1, 8}; !reflect.DeepEqual(got, want) {
			t.Fatalf("payload = %x, want %x", got, want)
		}
		return nil
	}
	packet := npcTalkPacket(t, gate.GetUniqueID())
	payload := packet.Payload()
	payload[len(payload)-1] = 8
	if err := handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	interaction, ok := npcInteractions.Get(player.GetUniqueID())
	if !ok || interaction.NPCRefObjectID != 2094 || interaction.Action != 8 {
		t.Fatalf("teleport interaction = %#v, present=%t", interaction, ok)
	}
	npcInteractions.Clear(player.GetUniqueID())
}

func npcTalkPacket(t *testing.T, uniqueID uint32) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt32(uniqueID); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := packet.WriteByte(npcTalkActionConversation); err != nil {
		t.Fatalf("write action: %v", err)
	}
	return packet
}
