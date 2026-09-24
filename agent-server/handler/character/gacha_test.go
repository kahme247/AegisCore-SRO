package character

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type gachaRepositoryFake struct {
	command         model.GachaPlayCommand
	result          model.GachaPlayResult
	err             error
	exchangeCommand model.GachaExchangeCommand
	exchangeResult  model.GachaExchangeResult
	exchangeErr     error
	exchangeResults []model.GachaExchangeResult
	exchangeCalls   int
}

func (f *gachaRepositoryFake) Exchange(_ context.Context, command model.GachaExchangeCommand) (model.GachaExchangeResult, error) {
	f.exchangeCommand = command
	if f.exchangeErr != nil {
		return model.GachaExchangeResult{}, f.exchangeErr
	}
	if f.exchangeCalls < len(f.exchangeResults) {
		result := f.exchangeResults[f.exchangeCalls]
		f.exchangeCalls++
		return result, nil
	}
	if f.exchangeCalls == 0 {
		f.exchangeCalls++
		return f.exchangeResult, nil
	}
	return model.GachaExchangeResult{}, model.ErrGachaCardNotFound
}

func (f *gachaRepositoryFake) Play(_ context.Context, command model.GachaPlayCommand) (model.GachaPlayResult, error) {
	f.command = command
	return f.result, f.err
}

type gachaWorldFake struct {
	player    *model.Player
	objects   map[uint32]model.ISRObject
	distance  float32
	broadcast func(network.Packet)
}

func (f gachaWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) { return f.player, nil }
func (f gachaWorldFake) GetObjectByUniqueId(uid uint32) (model.ISRObject, error) {
	return f.objects[uid], nil
}
func (f gachaWorldFake) DistanceTo(_, _ navmeshv2.RtNavmeshPosition) float32 { return f.distance }
func (f gachaWorldFake) Broadcast(packet network.Packet) {
	if f.broadcast != nil {
		f.broadcast(packet)
	}
}

func TestGachaHandlerConsumesCardAndPublishesAtomicReward(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44, UniqueID: 77}}
	reward := model.Item{ID: 900, SRObject: model.SRObject{RefObjectID: 4014}, StackSize: 2, Data: 2}
	inventory := model.NewInventory(map[byte]model.Item{13: reward})
	player := &model.Player{ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	player.SetUniqueID(77)
	player.RtNavmeshPosition = selectEntityPosition(30000, 10, 0, 10)
	player.KnownObjectList = model.NewKnownObjectList(player)
	npc := selectEntityNPC(1234, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}, selectEntityPosition(30000, 20, 0, 20))
	npc.Name = "NPC_CH_GACHA_MACHINE"
	player.KnownObjectList.AddObject(npc)
	repository := &gachaRepositoryFake{result: model.GachaPlayResult{
		CardSlot: 14, RewardSlot: 13, RewardRefItemID: 4014, RewardQuantity: 2,
		RewardItem: reward, Inventory: inventory, Won: true,
	}}
	var sent []network.Packet
	var announced network.Packet
	h := &GachaHandler{repository: repository, world: gachaWorldFake{player: player, objects: map[uint32]model.ISRObject{1234: npc}, broadcast: func(packet network.Packet) { announced = packet }}, send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(1234)
	packet.WriteUInt32(5678)
	packet.WriteByte(9)
	if err := h.handlePlay(context.Background(), session, packet); err != nil {
		t.Fatalf("play: %v", err)
	}
	if repository.command != (model.GachaPlayCommand{CharacterID: 44, EntryID: 5678, CardSlot: 9}) {
		t.Fatalf("command = %#v", repository.command)
	}
	if len(sent) != 3 || sent[0].MessageID != opcode.GachaPlayResponse || sent[1].MessageID != opcode.ItemOperationResponse || sent[2].MessageID != opcode.ItemOperationResponse || announced.MessageID != opcode.GachaAnnounce {
		t.Fatalf("sent packets = %#v", sent)
	}
	if got := sent[0].Payload(); len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("play response = %x", got)
	}
	if persisted, exists := player.Inventory.Items[13]; !exists || persisted.ID != 900 {
		t.Fatalf("committed inventory = %#v", player.Inventory.Items)
	}
}

func TestGachaHandlerRejectsNonGachaNPCBeforeRepositoryPlay(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44, UniqueID: 77}}
	player := &model.Player{ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	player.SetUniqueID(77)
	player.RtNavmeshPosition = selectEntityPosition(30000, 10, 0, 10)
	player.KnownObjectList = model.NewKnownObjectList(player)
	npc := selectEntityNPC(1234, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}, selectEntityPosition(30000, 20, 0, 20))
	npc.Name = "NPC_CH_POTION"
	player.KnownObjectList.AddObject(npc)
	repository := &gachaRepositoryFake{}
	h := &GachaHandler{repository: repository, world: gachaWorldFake{player: player, objects: map[uint32]model.ISRObject{1234: npc}}}
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(1234)
	packet.WriteUInt32(5678)
	packet.WriteByte(9)
	if err := h.handlePlay(context.Background(), session, packet); err == nil {
		t.Fatal("non-Gacha NPC accepted")
	}
	if repository.command != (model.GachaPlayCommand{}) {
		t.Fatalf("repository invoked with %#v", repository.command)
	}
}

func TestGachaHandlerRejectsUnvalidatedNPCBeforeRepositoryPlay(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44, UniqueID: 77}}
	player := &model.Player{ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	player.SetUniqueID(77)
	player.KnownObjectList = model.NewKnownObjectList(player)
	repository := &gachaRepositoryFake{}
	h := &GachaHandler{repository: repository, world: gachaWorldFake{player: player}}
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(1234)
	packet.WriteUInt32(5678)
	packet.WriteByte(9)
	if err := h.handlePlay(context.Background(), session, packet); err == nil {
		t.Fatal("unknown Gacha NPC accepted")
	}
	if repository.command != (model.GachaPlayCommand{}) {
		t.Fatalf("repository invoked with %#v", repository.command)
	}
}

func TestGachaExchangeConsumesAllWinningCardsInOneRequest(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44, UniqueID: 77}}
	rewardOne := model.Item{ID: 901, SRObject: model.SRObject{RefObjectID: 4014}, StackSize: 2, Data: 2}
	rewardTwo := model.Item{ID: 902, SRObject: model.SRObject{RefObjectID: 4015}, StackSize: 1, Data: 1}
	inventoryOne := model.NewInventory(map[byte]model.Item{14: rewardOne})
	inventoryFinal := model.NewInventory(map[byte]model.Item{14: rewardOne, 15: rewardTwo})
	player := &model.Player{ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	player.SetUniqueID(77)
	player.RtNavmeshPosition = selectEntityPosition(30000, 10, 0, 10)
	player.KnownObjectList = model.NewKnownObjectList(player)
	npc := selectEntityNPC(4, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}, selectEntityPosition(30000, 20, 0, 20))
	npc.Name = "NPC_CH_GACHA_OPERATOR"
	player.KnownObjectList.AddObject(npc)
	repository := &gachaRepositoryFake{exchangeResults: []model.GachaExchangeResult{
		{CardSlot: 13, RewardSlot: 14, RewardRefItemID: 4014, RewardQuantity: 2, RewardItem: rewardOne, Inventory: inventoryOne},
		{CardSlot: 16, RewardSlot: 15, RewardRefItemID: 4015, RewardQuantity: 1, RewardItem: rewardTwo, Inventory: inventoryFinal},
	}}
	var sent []network.Packet
	h := &GachaHandler{repository: repository, world: gachaWorldFake{player: player, objects: map[uint32]model.ISRObject{4: npc}}, send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(4)
	if err := h.handleExchange(context.Background(), session, packet); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if repository.exchangeCommand != (model.GachaExchangeCommand{CharacterID: 44}) {
		t.Fatalf("exchange command = %#v", repository.exchangeCommand)
	}
	if repository.exchangeCalls != 2 {
		t.Fatalf("exchange calls = %d, want 2 successful exchanges", repository.exchangeCalls)
	}
	wantOpcodes := []uint16{opcode.ItemOperationResponse, opcode.ItemOperationResponse, opcode.ItemOperationResponse, opcode.ItemOperationResponse, opcode.GachaExchangeResponse}
	if len(sent) != len(wantOpcodes) {
		t.Fatalf("sent packet count = %d, want %d", len(sent), len(wantOpcodes))
	}
	for i, want := range wantOpcodes {
		if sent[i].MessageID != want {
			t.Fatalf("sent[%d] opcode = 0x%04X, want 0x%04X", i, sent[i].MessageID, want)
		}
	}
	if got := sent[len(sent)-1].Payload(); len(got) != 3 || got[0] != 1 {
		t.Fatalf("exchange response = %x", got)
	}
	if len(player.Inventory.Items) != 2 || player.Inventory.Items[14].ID != rewardOne.ID || player.Inventory.Items[15].ID != rewardTwo.ID {
		t.Fatalf("committed exchange inventory = %#v", player.Inventory.Items)
	}
}
