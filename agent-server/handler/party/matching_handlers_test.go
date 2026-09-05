package party

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

type matchingHandlerPlayers struct{ players map[uint32]*model.Player }

func (p matchingHandlerPlayers) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	player := p.players[uid]
	if player == nil {
		return nil, service.ErrPartyTargetUnavailable
	}
	return player, nil
}

func TestMatchingApprovalUsesPendingApplicantAndDirectionalPromptOpcode(t *testing.T) {
	masterSession := &server.Session{UserContext: server.UserContext{UniqueID: 10}}
	applicantSession := &server.Session{UserContext: server.UserContext{UniqueID: 20}}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}
	master := &model.Player{ID: 100, CharName: "master", Session: masterSession, SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: position}}
	applicant := &model.Player{ID: 200, CharName: "applicant", Level: 10, Session: applicantSession, SRObject: model.SRObject{UniqueID: 20, RtNavmeshPosition: position}}
	players := matchingHandlerPlayers{players: map[uint32]*model.Player{10: master, 20: applicant}}
	core := service.NewPartyService(players)
	partyNumber := core.FormParty(model.PartyFormRequest{
		MasterUniqueID: 10, MasterJID: uint32(master.ID), MasterName: master.CharName,
		PartySettingsFlag: model.ExpShareItemShare, PurposeType: model.MonsterHuntingParty,
		LevelMin: 1, LevelMax: 110, Title: "open",
	})
	if partyNumber == 0 {
		t.Fatal("FormParty returned zero")
	}

	handler := &PartyHandler{service: core}
	var sent []network.Packet
	var recipients []*server.Session
	handler.send = func(session *server.Session, packet network.Packet) error {
		recipients = append(recipients, session)
		sent = append(sent, packet)
		return nil
	}
	join := network.EmptyClientPacket()
	join.WriteUInt32(partyNumber)
	if err := handler.handleMatchingJoin(context.Background(), applicantSession, join); err != nil {
		t.Fatalf("matching join: %v", err)
	}
	if len(sent) != 1 || recipients[0] != masterSession || sent[0].MessageID != opcode.PartyMatchingPlayerJoinNotification {
		t.Fatalf("prompt = recipient %p/opcode %#x, want master/%#x", recipients[0], sent[0].MessageID, opcode.PartyMatchingPlayerJoinNotification)
	}
	prompt := sent[0]
	requestID, err := prompt.ReadUInt32()
	if err != nil {
		t.Fatalf("read prompt request ID: %v", err)
	}
	if _, err = prompt.ReadUInt32(); err != nil {
		t.Fatalf("read prompt opaque field: %v", err)
	}

	// The client echoes an opaque request-type field. It is deliberately not
	// the applicant's unique ID; the handler must use the pending request.
	answer := network.EmptyClientPacket()
	answer.WriteUInt32(requestID)
	answer.WriteUInt32(0xDEADBEEF)
	answer.WriteByte(1)
	if err := handler.handleMatchingPlayerJoin(context.Background(), masterSession, answer); err != nil {
		t.Fatalf("matching approval: %v", err)
	}
	if applicant.GetParty() == nil || applicant.GetParty().Number != partyNumber {
		t.Fatalf("applicant party = %+v, want party %d", applicant.GetParty(), partyNumber)
	}
	if len(sent) < 3 || sent[1].MessageID != opcode.PartyMatchingJoinResponse {
		t.Fatalf("approval responses = %v, want applicant B06D response", sent)
	}
}

func TestMatchingFailureUsesTheTwoBytePartyNotFoundCode(t *testing.T) {
	packet := buildMatchingJoinResult(false, 0)
	if got := packet.Payload(); len(got) != 3 || got[0] != 2 || got[1] != byte(PartyMatchingErrorNotFound&0xff) || got[2] != byte(PartyMatchingErrorNotFound>>8) {
		t.Fatalf("matching failure payload = % X, want 02 plus %#x", got, PartyMatchingErrorNotFound)
	}
}

func TestMatchingJoinResultUsesTheDocumentedTwoByteJoinResult(t *testing.T) {
	accepted := buildMatchingJoinResult(true, 1)
	if got := accepted.Payload(); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 0 {
		t.Fatalf("accepted result = % X, want 01 02 00", got)
	}
	denied := buildMatchingJoinResult(true, 2)
	if got := denied.Payload(); len(got) != 3 || got[0] != 1 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("denied result = % X, want 01 01 00", got)
	}
}

func TestMatchingDeleteSuccessIncludesMatchingID(t *testing.T) {
	packet := buildMatchingDeleteResult(true, 77)
	if got := packet.Payload(); len(got) != 5 || got[0] != 1 || got[1] != 77 || got[2] != 0 || got[3] != 0 || got[4] != 0 {
		t.Fatalf("delete result = % X, want 01 4D 00 00 00", got)
	}
}
