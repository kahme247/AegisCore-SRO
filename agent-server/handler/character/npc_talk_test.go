package character

import (
	"context"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type questRuntimeStub struct {
	grantResult    model.QuestItemGrantResult
	progressResult model.QuestProgressResult
	completeResult model.QuestCompletionResult
	err            error
}

func (r *questRuntimeStub) GrantMissionItem(context.Context, int, int) (model.QuestItemGrantResult, error) {
	return r.grantResult, r.err
}

func (r *questRuntimeStub) RecordMobKill(context.Context, int, uint32) (model.QuestProgressResult, error) {
	return r.progressResult, r.err
}

func (r *questRuntimeStub) Complete(context.Context, int, int) (model.QuestCompletionResult, error) {
	return r.completeResult, r.err
}

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

func TestNPCTalkOpensChulsanQuestList(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	npc.Name = "NPC_CH_SMITH"
	world := &selectEntityWorldFake{
		player:   player,
		objects:  map[uint32]model.ISRObject{npc.GetUniqueID(): npc},
		distance: selectionKnownRange,
	}
	type sentPacket struct {
		opcode  uint16
		payload []byte
	}
	var sent []sentPacket
	handler := NewNPCTalkHandler(world)
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, sentPacket{opcode: packet.MessageID, payload: append([]byte(nil), packet.Payload()...)})
		return nil
	}

	if err := handler.handlePacket(context.Background(), session, npcTalkPacket(t, npc.GetUniqueID())); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d packets, want 2", len(sent))
	}
	if sent[0].opcode != opcode.EntityTalkResponse {
		t.Fatalf("first opcode = 0x%04X, want 0x%04X", sent[0].opcode, opcode.EntityTalkResponse)
	}
	if got, want := sent[0].payload, []byte{1, npcTalkActionConversation}; !reflect.DeepEqual(got, want) {
		t.Fatalf("talk response payload = % X, want % X", got, want)
	}
	if sent[1].opcode != opcode.QuestTalk {
		t.Fatalf("second opcode = 0x%04X, want 0x%04X", sent[1].opcode, opcode.QuestTalk)
	}
	wantMenu := []byte{4, 0, 0, 2, 17, 0}
	wantMenu = append(wantMenu, "SN_QNO_CH_SMITH_1"...)
	wantMenu = append(wantMenu, 17, 0)
	wantMenu = append(wantMenu, "SN_QNO_CH_SMITH_2"...)
	if !reflect.DeepEqual(sent[1].payload, wantMenu) {
		t.Fatalf("quest menu payload = % X, want % X", sent[1].payload, wantMenu)
	}
}

func TestNPCTalkOpensSelectedChulsanQuestOffer(t *testing.T) {
	for _, test := range []struct {
		name     string
		selector byte
		talkKey  string
	}{
		{name: "first quest", selector: 5, talkKey: "SN_TALK_QNO_CH_SMITH_1_01"},
		{name: "second quest", selector: 6, talkKey: "SN_TALK_QNO_CH_SMITH_2_01"},
	} {
		t.Run(test.name, func(t *testing.T) {
			session, player, npc := selectEntityFixture(t)
			npc.Name = "NPC_CH_SMITH"
			npc.RefObjectID = chulsanRefObjectID
			world := &selectEntityWorldFake{
				player:   player,
				objects:  map[uint32]model.ISRObject{npc.GetUniqueID(): npc},
				distance: selectionKnownRange,
			}
			var sent []network.Packet
			handler := NewNPCTalkHandler(world)
			handler.send = func(_ *server.Session, packet network.Packet) error {
				sent = append(sent, packet)
				return nil
			}
			if err := handler.handlePacket(context.Background(), session, npcTalkPacket(t, npc.GetUniqueID())); err != nil {
				t.Fatalf("open quest list: %v", err)
			}
			selection := network.EmptyClientPacket()
			if err := selection.WriteByte(test.selector); err != nil {
				t.Fatalf("write quest selector: %v", err)
			}
			if err := handler.handleQuestTalkPacket(context.Background(), session, selection); err != nil {
				t.Fatalf("select quest: %v", err)
			}
			if len(sent) != 3 {
				t.Fatalf("sent %d packets, want talk response, quest list, and quest offer", len(sent))
			}
			if sent[2].MessageID != opcode.QuestTalk {
				t.Fatalf("offer opcode = 0x%04X, want 0x%04X", sent[2].MessageID, opcode.QuestTalk)
			}
			wantOffer := []byte{4, byte(len(test.talkKey)), 0}
			wantOffer = append(wantOffer, test.talkKey...)
			wantOffer = append(wantOffer, 2, 21, 0)
			wantOffer = append(wantOffer, "SN_TALK_COMMON_ACCEPT"...)
			wantOffer = append(wantOffer, 19, 0)
			wantOffer = append(wantOffer, "SN_TALK_COMMON_DENY"...)
			if got := sent[2].Payload(); !reflect.DeepEqual(got, wantOffer) {
				t.Fatalf("quest offer payload = % X, want % X", got, wantOffer)
			}
			npcInteractions.Clear(player.GetUniqueID())
		})
	}
}

func TestNPCTalkAcceptsSelectedChulsanQuest(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	npc.Name = "NPC_CH_SMITH"
	npc.RefObjectID = chulsanRefObjectID
	world := &selectEntityWorldFake{
		player:   player,
		objects:  map[uint32]model.ISRObject{npc.GetUniqueID(): npc},
		distance: selectionKnownRange,
	}
	repository := &questRepositoryStub{}
	var sent []network.Packet
	handler := NewNPCTalkHandler(world, repository)
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	if err := handler.handlePacket(context.Background(), session, npcTalkPacket(t, npc.GetUniqueID())); err != nil {
		t.Fatalf("open quest list: %v", err)
	}
	selectQuest := network.EmptyClientPacket()
	selectQuest.WriteByte(5)
	if err := handler.handleQuestTalkPacket(context.Background(), session, selectQuest); err != nil {
		t.Fatalf("select quest: %v", err)
	}
	accept := network.EmptyClientPacket()
	accept.WriteByte(questListSelectorBase)
	if err := handler.handleQuestTalkPacket(context.Background(), session, accept); err != nil {
		t.Fatalf("accept quest: %v", err)
	}

	if repository.characterID != player.ID || repository.questID != 3 {
		t.Fatalf("activated quest identity = character %d quest %d", repository.characterID, repository.questID)
	}
	if len(sent) != 5 {
		t.Fatalf("sent %d packets, want talk response, list, offer, add, and acceptance text", len(sent))
	}
	if sent[3].MessageID != opcode.QuestUpdate {
		t.Fatalf("quest add opcode = 0x%04X, want 0x%04X", sent[3].MessageID, opcode.QuestUpdate)
	}
	wantAdd := []byte{1, 3, 0, 0, 0, 1, 0, 0x18, 7, 1, 1, 1, 21, 0}
	wantAdd = append(wantAdd, "SN_CON_QNO_CH_SMITH_1"...)
	wantAdd = append(wantAdd, 0)
	if got := sent[3].Payload(); !reflect.DeepEqual(got, wantAdd) {
		t.Fatalf("quest add payload = % X, want % X", got, wantAdd)
	}
	if sent[4].MessageID != opcode.QuestTalk {
		t.Fatalf("acceptance opcode = 0x%04X, want 0x%04X", sent[4].MessageID, opcode.QuestTalk)
	}
	wantAccepted := []byte{1, 25, 0}
	wantAccepted = append(wantAccepted, "SN_TALK_QNO_CH_SMITH_1_02"...)
	if got := sent[4].Payload(); !reflect.DeepEqual(got, wantAccepted) {
		t.Fatalf("acceptance payload = % X, want % X", got, wantAccepted)
	}
	quests := player.QuestSnapshot()
	if len(quests) != 1 || quests[0].QuestID != 3 || quests[0].Status != model.QuestStartedByUser {
		t.Fatalf("player quests = %#v", quests)
	}
	npcInteractions.Clear(player.GetUniqueID())
}

func TestNPCTalkJingyoGrantsWeaponListAndAdvancesQuest(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	npc.Name = "NPC_CH_SOLDIER_SO1"
	npc.RefObjectID = jingyoRefObjectID
	player.Quests = []model.CharacterQuest{{QuestID: model.QuestWeaponListID, Status: model.QuestStartedByUser, AchievementCount: 1}}
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, distance: selectionKnownRange}
	quest := model.CharacterQuest{QuestID: model.QuestWeaponListID, Status: model.QuestReadyToTurnInByUser, AchievementCount: 1, Data1: 1}
	item := model.Item{SRObject: model.SRObject{RefObjectID: model.QuestWeaponListItemID}}
	runtime := &questRuntimeStub{grantResult: model.QuestItemGrantResult{
		Quest: quest, Inventory: model.NewInventory(map[byte]model.Item{13: item}), Added: model.QuestItemMutation{Slot: 13, Item: item},
	}}
	handler := NewNPCTalkHandler(world)
	handler.runtime = runtime
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }

	if err := handler.handlePacket(context.Background(), session, npcTalkPacket(t, npc.GetUniqueID())); err != nil {
		t.Fatalf("talk to Jingyo: %v", err)
	}
	if len(sent) != 4 {
		t.Fatalf("sent %d packets, want acknowledgement, item, quest update, and dialogue", len(sent))
	}
	wantOpcodes := []uint16{opcode.EntityTalkResponse, opcode.ItemOperationResponse, opcode.QuestUpdate, opcode.QuestTalk}
	for index, want := range wantOpcodes {
		if sent[index].MessageID != want {
			t.Fatalf("packet %d opcode = 0x%04X, want 0x%04X", index, sent[index].MessageID, want)
		}
	}
	got, exists := playerQuest(player, model.QuestWeaponListID)
	if !exists || got.Status != model.QuestReadyToTurnInByUser || got.Data1 != 1 {
		t.Fatalf("player quest = %#v, exists=%t", got, exists)
	}
	npcInteractions.Clear(player.GetUniqueID())
}

func TestNPCTalkCompletesReadyChulsanQuestAndPublishesRewards(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	npc.Name = "NPC_CH_SMITH"
	npc.RefObjectID = chulsanRefObjectID
	player.Level = 1
	player.Gold = 100
	player.Quests = []model.CharacterQuest{{QuestID: model.QuestWeaponListID, Status: model.QuestReadyToTurnInByUser, AchievementCount: 1, Data1: 1}}
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, distance: selectionKnownRange}
	completed := model.CharacterQuest{QuestID: model.QuestWeaponListID, Status: model.QuestCompleted, AchievementCount: 1}
	runtime := &questRuntimeStub{completeResult: model.QuestCompletionResult{
		Quest: completed, Inventory: model.NewInventory(nil), Gold: 305,
		Progress: model.ProgressionResult{Level: 1, Experience: 270}, Experience: 270,
		RemovedSlot: func() *byte { slot := byte(13); return &slot }(),
	}}
	handler := NewNPCTalkHandler(world)
	handler.runtime = runtime
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	if err := handler.handlePacket(context.Background(), session, npcTalkPacket(t, npc.GetUniqueID())); err != nil {
		t.Fatalf("open Chulsan list: %v", err)
	}
	selection := network.EmptyClientPacket()
	selection.WriteByte(5)
	if err := handler.handleQuestTalkPacket(context.Background(), session, selection); err != nil {
		t.Fatalf("complete quest: %v", err)
	}
	if len(sent) != 7 {
		t.Fatalf("sent %d packets, want acknowledgement, list, remove item, gold, EXP, quest removal, and dialogue", len(sent))
	}
	wantTail := []uint16{opcode.ItemOperationResponse, opcode.EntityUpdatePoints, opcode.EntityUpdateExperience, opcode.QuestUpdate, opcode.QuestTalk}
	for index, want := range wantTail {
		if sent[index+2].MessageID != want {
			t.Fatalf("completion packet %d opcode = 0x%04X, want 0x%04X", index, sent[index+2].MessageID, want)
		}
	}
	if player.GoldSnapshot() != 305 {
		t.Fatalf("player gold = %d, want 305", player.GoldSnapshot())
	}
	got, exists := playerQuest(player, model.QuestWeaponListID)
	if !exists || got.Status != model.QuestCompleted {
		t.Fatalf("completed player quest = %#v, exists=%t", got, exists)
	}
	npcInteractions.Clear(player.GetUniqueID())
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
