package social

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type academyExtensionRepositoryFake struct {
	model.SocialRepository
	academy model.AcademySnapshot
	joined  int
}

func (f *academyExtensionRepositoryFake) Academy(_ context.Context, characterID int) (model.AcademySnapshot, error) {
	if characterID == f.academy.LeaderCharacter || f.joined == characterID {
		return f.academy, nil
	}
	return model.AcademySnapshot{}, model.ErrSocialNotFound
}

func (f *academyExtensionRepositoryFake) JoinAcademy(_ context.Context, characterID, academyID int) (model.AcademySnapshot, error) {
	if academyID != f.academy.ID {
		return model.AcademySnapshot{}, model.ErrSocialNotFound
	}
	f.joined = characterID
	f.academy.Members = append(f.academy.Members, model.AcademyMember{CharacterID: characterID, Name: "Student"})
	return f.academy, nil
}

func (f *academyExtensionRepositoryFake) AcademyBuffs(context.Context, int, time.Time) ([]model.AcademyBuffState, error) {
	return nil, nil
}

type academyExtensionWorldFake struct{ players map[int]*model.Player }

func (f academyExtensionWorldFake) GetPlayerByCharacterID(id int) (*model.Player, error) {
	if player := f.players[id]; player != nil {
		return player, nil
	}
	return nil, model.ErrSocialNotFound
}

func (f academyExtensionWorldFake) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	for _, player := range f.players {
		if player.GetUniqueID() == uid {
			return player, nil
		}
	}
	return nil, model.ErrSocialNotFound
}

func TestAcademyInvitationRequiresBoundAcceptanceAndPublishesUpdate(t *testing.T) {
	leaderSession := &server.Session{UserContext: server.UserContext{CharacterID: 1, UniqueID: 11}}
	studentSession := &server.Session{UserContext: server.UserContext{CharacterID: 2, UniqueID: 22}}
	leader := &model.Player{ID: 1, CharName: "Leader", Session: leaderSession, SRObject: model.SRObject{UniqueID: 11}}
	student := &model.Player{ID: 2, CharName: "Student", Session: studentSession, SRObject: model.SRObject{UniqueID: 22}}
	repository := &academyExtensionRepositoryFake{academy: model.AcademySnapshot{ID: 9, LeaderCharacter: 1, Members: []model.AcademyMember{{CharacterID: 1, Name: "Leader"}}}}
	handler := &Handler{repo: repository, world: academyExtensionWorldFake{players: map[int]*model.Player{1: leader, 2: student}}, academyInvites: make(map[uint32]academyInvitation)}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	invite := network.EmptyClientPacket()
	invite.WriteUInt32(22)
	if err := handler.academyInvite(context.Background(), leaderSession, invite); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.AcademyMatchingResponse || sent[1].MessageID != opcode.AcademyInviteResponse {
		t.Fatalf("invite packets = %#v", sent)
	}
	answer := network.EmptyClientPacket()
	answer.WriteUInt32(9)
	answer.WriteUInt32(1)
	answer.WriteByte(1)
	if err := handler.academyInvitationResponse(context.Background(), studentSession, answer); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if repository.joined != 2 {
		t.Fatalf("joined = %d", repository.joined)
	}
	foundUpdate := false
	for _, packet := range sent {
		foundUpdate = foundUpdate || packet.MessageID == opcode.AcademyUpdate
	}
	if !foundUpdate {
		t.Fatalf("packets lack academy update: %#v", sent)
	}
}
