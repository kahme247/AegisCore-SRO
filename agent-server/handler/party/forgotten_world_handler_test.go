package party

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type forgottenWorldRecallFake struct {
	member  *model.Player
	inviter *model.Player
}

func (f *forgottenWorldRecallFake) SubmitForgottenWorldRecall(_ context.Context, member, inviter *model.Player) error {
	f.member, f.inviter = member, inviter
	return nil
}

func TestForgottenWorldRecallBindsPartyMasterInvitationAndAcceptance(t *testing.T) {
	scope, err := model.NewGameWorldScope(7, 1)
	if err != nil {
		t.Fatal(err)
	}
	masterSession := &server.Session{UserContext: server.UserContext{UniqueID: 10}}
	memberSession := &server.Session{UserContext: server.UserContext{UniqueID: 20}}
	master := &model.Player{ID: 1, CharName: "Master", Session: masterSession, SRObject: model.SRObject{UniqueID: 10}}
	member := &model.Player{ID: 2, CharName: "Member", Session: memberSession, SRObject: model.SRObject{UniqueID: 20}}
	master.SetGameWorldID(uint32(scope))
	partyState := &model.Party{Number: 1, MasterUniqueID: 10, Members: []uint32{10, 20}}
	if err = master.AddToParty(partyState); err != nil {
		t.Fatal(err)
	}
	if err = member.AddToParty(partyState); err != nil {
		t.Fatal(err)
	}
	players := matchingHandlerPlayers{players: map[uint32]*model.Player{10: master, 20: member}}
	recall := &forgottenWorldRecallFake{}
	handler := &PartyHandler{world: players, forgottenWorldRecall: recall, pendingRecall: make(map[uint32]forgottenWorldRecallInvitation)}
	type delivery struct {
		session *server.Session
		packet  network.Packet
	}
	var sent []delivery
	handler.send = func(session *server.Session, packet network.Packet) error {
		sent = append(sent, delivery{session: session, packet: packet})
		return nil
	}

	request := network.EmptyClientPacket()
	request.WriteUInt32(20)
	if err = handler.handleForgottenWorldRecallMember(context.Background(), masterSession, request); err != nil {
		t.Fatalf("member request: %v", err)
	}
	if len(sent) != 2 || sent[0].session != memberSession || sent[0].packet.MessageID != opcode.FGWRecallRequest || sent[1].packet.MessageID != opcode.FGWRecallMemberResponse {
		t.Fatalf("deliveries = %#v", sent)
	}
	invitation := sent[0].packet
	recallID, err := invitation.ReadUInt32()
	if err != nil {
		t.Fatal(err)
	}
	inviterUID, err := invitation.ReadUInt32()
	if err != nil {
		t.Fatal(err)
	}
	answer := network.EmptyClientPacket()
	answer.WriteUInt32(recallID)
	answer.WriteUInt32(inviterUID)
	answer.WriteByte(1)
	if err = handler.handleForgottenWorldRecallResponse(context.Background(), memberSession, answer); err != nil {
		t.Fatalf("recall response: %v", err)
	}
	if recall.member != member || recall.inviter != master {
		t.Fatalf("recall = member %p inviter %p", recall.member, recall.inviter)
	}
	if len(sent) != 5 || sent[2].session != memberSession || sent[2].packet.MessageID != opcode.FGWRecallMemberAcceptedResponse || sent[3].packet.MessageID != opcode.FGWUpdate || sent[4].packet.MessageID != opcode.FGWUpdate {
		t.Fatalf("accepted delivery = %#v", sent)
	}
}
