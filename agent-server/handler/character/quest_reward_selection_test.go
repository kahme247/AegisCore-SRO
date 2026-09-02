package character

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type questRewardSelectionExecutorStub struct {
	characterID int
	rewardID    uint32
	entryIDs    []uint32
	result      model.QuestCompletionResult
	err         error
	calls       int
}

type eventRewardSelectionExecutorStub struct {
	questRewardSelectionExecutorStub
	eventResult model.EventRewardResult
}

func (s *eventRewardSelectionExecutorStub) SelectEventReward(_ context.Context, characterID int, eventID uint32, entryIDs []uint32) (model.EventRewardResult, error) {
	s.calls++
	s.characterID, s.rewardID = characterID, eventID
	s.entryIDs = append([]uint32(nil), entryIDs...)
	return s.eventResult, s.err
}

func (s *questRewardSelectionExecutorStub) SelectQuestReward(_ context.Context, characterID int, rewardID uint32, entryIDs []uint32) (model.QuestCompletionResult, error) {
	s.calls++
	s.characterID, s.rewardID = characterID, rewardID
	s.entryIDs = append([]uint32(nil), entryIDs...)
	return s.result, s.err
}

func questRewardSelectionPacket(rewardID uint32, entries ...uint32) network.Packet {
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(rewardID)
	packet.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		packet.WriteUInt32(entry)
	}
	return packet
}

func TestQuestRewardSelectionRejectsUnknownOrInactiveReward(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	executor := &questRewardSelectionExecutorStub{}
	handler := &QuestRewardSelectionHandler{world: &selectEntityWorldFake{player: player}, executor: executor}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }

	if err := handler.handle(context.Background(), session, questRewardSelectionPacket(58, 100)); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 || len(sent) != 1 || !reflect.DeepEqual(sent[0].Payload(), []byte{2, 1}) {
		t.Fatalf("executor calls=%d packets=%#v", executor.calls, sent)
	}
}

func TestQuestRewardSelectionMapsAuthoredMismatchWithoutFalseSuccess(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Quests: []model.CharacterQuest{{QuestID: 58, Status: model.QuestRewardReady}}}
	executor := &questRewardSelectionExecutorStub{err: model.ErrQuestReferenceUnsupported}
	handler := &QuestRewardSelectionHandler{world: &selectEntityWorldFake{player: player}, executor: executor}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }

	if err := handler.handle(context.Background(), session, questRewardSelectionPacket(58, 100, 101)); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || !reflect.DeepEqual(executor.entryIDs, []uint32{100, 101}) || len(sent) != 1 || !reflect.DeepEqual(sent[0].Payload(), []byte{2, 2}) {
		t.Fatalf("executor=%+v packets=%#v", executor, sent)
	}
}

func TestQuestRewardSelectionPublishesCommittedResultBeforeSuccess(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Quests: []model.CharacterQuest{{QuestID: 58, Status: model.QuestRewardReady}}}
	executor := &questRewardSelectionExecutorStub{result: model.QuestCompletionResult{
		Quest: model.CharacterQuest{QuestID: 58, Status: model.QuestClosed}, Inventory: model.NewInventory(nil),
	}}
	handler := &QuestRewardSelectionHandler{world: &selectEntityWorldFake{player: player}, executor: executor}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }

	if err := handler.handle(context.Background(), session, questRewardSelectionPacket(58, 123)); err != nil {
		t.Fatal(err)
	}
	if executor.characterID != 44 || executor.rewardID != 58 || len(sent) < 2 {
		t.Fatalf("executor=%+v packets=%#v", executor, sent)
	}
	last := sent[len(sent)-1]
	if last.MessageID != opcode.QuestRewardSelectionResponse || !reflect.DeepEqual(last.Payload(), []byte{1}) {
		t.Fatalf("last packet = opcode 0x%04X payload % X", last.MessageID, last.Payload())
	}
	if quest, exists := playerQuest(player, 58); !exists || quest.Status != model.QuestClosed {
		t.Fatalf("runtime quest = (%+v, %v)", quest, exists)
	}
}

func TestQuestRewardSelectionInternalFailureHasNoAcknowledgement(t *testing.T) {
	want := errors.New("database failed")
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Quests: []model.CharacterQuest{{QuestID: 58, Status: model.QuestRewardReady}}}
	handler := &QuestRewardSelectionHandler{world: &selectEntityWorldFake{player: player}, executor: &questRewardSelectionExecutorStub{err: want}}
	var sent int
	handler.send = func(*server.Session, network.Packet) error { sent++; return nil }
	if err := handler.handle(context.Background(), session, questRewardSelectionPacket(58, 1)); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("internal failure sent %d packets", sent)
	}
}

func TestQuestRewardSelectionDispatchesEventRewardWithoutQuestIDCoercion(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	executor := &eventRewardSelectionExecutorStub{eventResult: model.EventRewardResult{EventID: 10001, Inventory: model.NewInventory(nil)}}
	handler := &QuestRewardSelectionHandler{world: &selectEntityWorldFake{player: player}, executor: executor}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	if err := handler.handle(context.Background(), session, questRewardSelectionPacket(10001, 7001)); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.rewardID != 10001 || len(sent) != 2 {
		t.Fatalf("executor=%+v packets=%#v", executor, sent)
	}
	if last := sent[len(sent)-1]; last.MessageID != opcode.QuestRewardSelectionResponse || !reflect.DeepEqual(last.Payload(), []byte{1}) {
		t.Fatalf("last packet = opcode 0x%04X payload % X", last.MessageID, last.Payload())
	}
}
