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

type questEventDispatcherStub struct {
	characterID int
	questID     int
	event       model.QuestAuthoredEvent
	err         error
}

func (d *questEventDispatcherStub) DispatchQuestEvent(_ context.Context, characterID, questID int, event model.QuestAuthoredEvent) error {
	d.characterID, d.questID, d.event = characterID, questID, event
	return d.err
}

func TestQuestActionHandlersDispatchBeforeSuccess(t *testing.T) {
	tests := []struct {
		name           string
		requestOpcode  uint16
		responseOpcode uint16
		event          model.QuestAuthoredEvent
		handle         func(*QuestActionHandler, context.Context, *server.Session, network.Packet) error
	}{
		{"interaction", opcode.QuestInteractionRequest, opcode.QuestInteractionResponse, model.QuestAuthoredInteraction, func(handler *QuestActionHandler, ctx context.Context, session *server.Session, packet network.Packet) error {
			return handler.handleInteraction(ctx, session, packet)
		}},
		{"local action", opcode.QuestLocalActionRequest, opcode.QuestLocalActionResponse, model.QuestAuthoredLocalAction, func(handler *QuestActionHandler, ctx context.Context, session *server.Session, packet network.Packet) error {
			return handler.handleLocalAction(ctx, session, packet)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := &questEventDispatcherStub{}
			handler := &QuestActionHandler{dispatcher: dispatcher}
			var response network.Packet
			handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
			session := &server.Session{}
			session.UserContext.CharacterID = 77
			request := network.EmptyClientPacket()
			request.MessageID = test.requestOpcode
			request.WriteUInt32(123)
			if err := test.handle(handler, context.Background(), session, request); err != nil {
				t.Fatal(err)
			}
			if dispatcher.characterID != 77 || dispatcher.questID != 123 || dispatcher.event != test.event {
				t.Fatalf("dispatch = character %d quest %d event %d", dispatcher.characterID, dispatcher.questID, dispatcher.event)
			}
			if response.MessageID != test.responseOpcode || !reflect.DeepEqual(response.Payload(), []byte{1, 123, 0, 0, 0}) {
				t.Fatalf("response = opcode 0x%04X payload %v", response.MessageID, response.Payload())
			}
		})
	}
}

func TestQuestActionHandlersMapLegacyFailures(t *testing.T) {
	tests := []struct {
		err    error
		reason byte
	}{
		{model.ErrQuestEventNotFound, 1},
		{model.ErrQuestStateConflict, 2},
	}
	for _, test := range tests {
		dispatcher := &questEventDispatcherStub{err: test.err}
		handler := &QuestActionHandler{dispatcher: dispatcher}
		var response network.Packet
		handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
		session := &server.Session{}
		session.UserContext.CharacterID = 77
		request := network.EmptyClientPacket()
		request.WriteUInt32(123)
		if err := handler.handleInteraction(context.Background(), session, request); err != nil {
			t.Fatal(err)
		}
		if want := []byte{2, test.reason}; !reflect.DeepEqual(response.Payload(), want) {
			t.Fatalf("response = %v, want %v", response.Payload(), want)
		}
	}
}

func TestQuestActionHandlerDoesNotAcknowledgeInternalFailure(t *testing.T) {
	dispatcher := &questEventDispatcherStub{err: errors.New("database failed")}
	handler := &QuestActionHandler{dispatcher: dispatcher}
	sent := false
	handler.send = func(_ *server.Session, _ network.Packet) error { sent = true; return nil }
	session := &server.Session{}
	session.UserContext.CharacterID = 77
	request := network.EmptyClientPacket()
	request.WriteUInt32(123)
	if err := handler.handleLocalAction(context.Background(), session, request); err == nil || sent {
		t.Fatalf("handleLocalAction error = %v, sent=%t", err, sent)
	}
}
