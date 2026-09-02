package character

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type questRepositoryStub struct {
	characterID    int
	questID        int
	activatedQuest model.CharacterQuest
	err            error
}

func (r *questRepositoryStub) Save(context.Context, int, model.CharacterQuest) error { return nil }
func (r *questRepositoryStub) List(context.Context, int) ([]model.CharacterQuest, error) {
	return nil, nil
}
func (r *questRepositoryStub) Activate(_ context.Context, characterID int, quest model.CharacterQuest) (model.CharacterQuest, error) {
	r.characterID, r.questID, r.activatedQuest = characterID, quest.QuestID, quest
	return quest, r.err
}
func (r *questRepositoryStub) Abandon(_ context.Context, characterID, questID int) error {
	r.characterID, r.questID = characterID, questID
	return r.err
}

func TestQuestAbandonUsesSelectedCharacterAndReturnsStockSuccess(t *testing.T) {
	repository := &questRepositoryStub{}
	handler := &QuestHandler{repository: repository}
	var responses []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		responses = append(responses, packet)
		return nil
	}
	session := &server.Session{}
	session.UserContext.CharacterID = 77
	request := network.EmptyClientPacket()
	request.WriteUInt32(123)
	if err := handler.handleAbandon(context.Background(), session, request); err != nil {
		t.Fatalf("handleAbandon: %v", err)
	}
	if repository.characterID != 77 || repository.questID != 123 {
		t.Fatalf("abandon identity = (%d,%d)", repository.characterID, repository.questID)
	}
	if len(responses) != 1 {
		t.Fatalf("responses = %#v, want only the abandon acknowledgement", responses)
	}
	if responses[0].MessageID != opcode.QuestAbandonResponse {
		t.Fatalf("response opcode = 0x%04X", responses[0].MessageID)
	}
	if got, want := responses[0].Payload(), []byte{1, 123, 0, 0, 0}; string(got) != string(want) {
		t.Fatalf("response = %v, want %v", got, want)
	}
}

func TestQuestAbandonReturnsLegacyFailureReason(t *testing.T) {
	repository := &questRepositoryStub{err: model.ErrCharacterQuestNotActive}
	handler := &QuestHandler{repository: repository}
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		response = packet
		return nil
	}
	session := &server.Session{}
	session.UserContext.CharacterID = 77
	request := network.EmptyClientPacket()
	request.WriteUInt32(123)
	if err := handler.handleAbandon(context.Background(), session, request); err != nil {
		t.Fatalf("handleAbandon: %v", err)
	}
	if got, want := response.Payload(), []byte{2, 2}; string(got) != string(want) {
		t.Fatalf("response = %v, want %v", got, want)
	}
}

func TestQuestAbandonRejectsTrailingPayload(t *testing.T) {
	handler := &QuestHandler{repository: &questRepositoryStub{}}
	session := &server.Session{}
	session.UserContext.CharacterID = 77
	request := network.EmptyClientPacket()
	request.WriteUInt32(123)
	request.WriteByte(1)
	if err := handler.handleAbandon(context.Background(), session, request); err == nil {
		t.Fatal("handleAbandon accepted trailing payload")
	}
}
