package character

import (
	"context"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

type questRepositoryStub struct {
	characterID int
	questID     int
	err         error
}

func (r *questRepositoryStub) Save(context.Context, int, model.CharacterQuest) error { return nil }
func (r *questRepositoryStub) List(context.Context, int) ([]model.CharacterQuest, error) {
	return nil, nil
}
func (r *questRepositoryStub) Abandon(_ context.Context, characterID, questID int) error {
	r.characterID, r.questID = characterID, questID
	return r.err
}

func TestQuestAbandonUsesSelectedCharacterAndReturnsStockSuccess(t *testing.T) {
	repository := &questRepositoryStub{}
	handler := &QuestHandler{repository: repository}
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
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
	if response.MessageID != opcode.QuestAbandonResponse {
		t.Fatalf("response opcode = 0x%04X", response.MessageID)
	}
	if got, want := response.Payload(), []byte{1, 123, 0, 0, 0}; string(got) != string(want) {
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
