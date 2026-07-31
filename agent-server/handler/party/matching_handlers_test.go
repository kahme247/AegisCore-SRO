package party

import (
	"bytes"
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

type exchangeInvitationFallbackFake struct {
	handled  bool
	accepted bool
	session  *server.Session
}

func (f *exchangeInvitationFallbackFake) AnswerInvitation(_ context.Context, session *server.Session, accepted bool) (bool, error) {
	f.session, f.accepted = session, accepted
	return f.handled, nil
}

func (p matchingHandlerPlayers) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	player := p.players[uid]
	if player == nil {
		return nil, service.ErrPartyTargetUnavailable
	}
	return player, nil
}

func TestMatchingFormInitializesMasterPartyModeBeforeJoinUpdates(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharName: "master"}}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}
	master := &model.Player{ID: 100, CharName: "master", Level: 10, Session: session, SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: position}}
	handler := &PartyHandler{service: service.NewPartyService(matchingHandlerPlayers{players: map[uint32]*model.Player{10: master}})}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteUInt32(0)
	request.WriteUInt32(0)
	request.WriteByte(byte(model.ExpShareItemShare))
	request.WriteByte(byte(model.QuestingParty))
	request.WriteByte(1)
	request.WriteByte(30)
	request.WriteString("matching")
	if err := handler.handleMatchingForm(context.Background(), session, request); err != nil {
		t.Fatalf("matching form: %v", err)
	}
	if len(sent) != 3 || sent[0].MessageID != opcode.PartyMatchingFormResponse ||
		sent[1].MessageID != opcode.PartyMemberCountResponse || sent[2].MessageID != opcode.PartyCreatedFromMatchingResponse {
		t.Fatalf("form packets = %#v, want B069, B067, then 3065", sent)
	}
	if !bytes.Equal(sent[1].Payload(), []byte{1, 10, 0, 0, 0}) {
		t.Fatalf("master join acknowledgement payload = % X", sent[1].Payload())
	}
	r := newPartyPacketReader(t, sent[2].Payload())
	flags, number, masterUID, settings, count := r.byte(), r.uint32(), r.uint32(), r.byte(), r.byte()
	if flags != 0xFF || number == 0 || masterUID != master.GetUniqueID() || settings != byte(model.ExpShareItemShare) || count != 1 {
		t.Fatalf("master snapshot = flags %02X number %d master %d settings %d count %d", flags, number, masterUID, settings, count)
	}
	if member := decodePartyMember(t, r); member.uid != master.GetUniqueID() {
		t.Fatalf("snapshot member UID = %d, want %d", member.uid, master.GetUniqueID())
	}
	r.done()
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
	if len(sent) != 5 || recipients[1] != applicantSession || sent[1].MessageID != opcode.PartyMatchingJoinResponse {
		t.Fatalf("approval responses = %v, want applicant B06D response", sent)
	}
	if recipients[2] != applicantSession || sent[2].MessageID != opcode.PartyMemberCountResponse ||
		!bytes.Equal(sent[2].Payload(), []byte{1, 20, 0, 0, 0}) {
		t.Fatalf("join acknowledgement = recipient %p/opcode 0x%04X/payload % X", recipients[2], sent[2].MessageID, sent[2].Payload())
	}
	if recipients[3] != masterSession || sent[3].MessageID != opcode.PartyUpdateResponse {
		t.Fatalf("master join update = recipient %p/opcode 0x%04X", recipients[3], sent[3].MessageID)
	}
	if recipients[4] != applicantSession || sent[4].MessageID != opcode.PartyCreatedFromMatchingResponse {
		t.Fatalf("applicant snapshot = recipient %p/opcode 0x%04X", recipients[4], sent[4].MessageID)
	}
}

func TestInvitationAcceptanceAcknowledgesJoinBeforePartySnapshot(t *testing.T) {
	inviterSession := &server.Session{UserContext: server.UserContext{UniqueID: 10}}
	targetSession := &server.Session{UserContext: server.UserContext{UniqueID: 20}}
	inviter := &model.Player{ID: 100, CharName: "inviter", Session: inviterSession, SRObject: model.SRObject{UniqueID: 10}}
	target := &model.Player{ID: 200, CharName: "target", Session: targetSession, SRObject: model.SRObject{UniqueID: 20}}
	core := service.NewPartyService(matchingHandlerPlayers{players: map[uint32]*model.Player{10: inviter, 20: target}})
	if _, _, err := core.Invite(10, 20, model.ExpShareItemShare, false); err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	handler := &PartyHandler{service: core}
	var sent []network.Packet
	var recipients []*server.Session
	handler.send = func(session *server.Session, packet network.Packet) error {
		recipients = append(recipients, session)
		sent = append(sent, packet)
		return nil
	}
	answer := network.EmptyClientPacket()
	answer.WriteByte(1)
	answer.WriteByte(1)
	if err := handler.handleInvitationAnswer(context.Background(), targetSession, answer); err != nil {
		t.Fatalf("answer invitation: %v", err)
	}
	if len(sent) != 5 {
		t.Fatalf("sent %d packets, want inviter response, two join acknowledgements, and two snapshots", len(sent))
	}
	if recipients[0] != inviterSession || sent[0].MessageID != opcode.PartyCreateResponse {
		t.Fatalf("first response = recipient %p/opcode 0x%04X, want inviter/0x%04X", recipients[0], sent[0].MessageID, opcode.PartyCreateResponse)
	}
	for _, index := range []int{1, 2} {
		if sent[index].MessageID != opcode.PartyMemberCountResponse {
			t.Fatalf("join acknowledgement %d opcode = 0x%04X, want 0x%04X", index, sent[index].MessageID, opcode.PartyMemberCountResponse)
		}
	}
	if recipients[1] != inviterSession || !bytes.Equal(sent[1].Payload(), []byte{1, 10, 0, 0, 0}) {
		t.Fatalf("inviter acknowledgement = recipient %p/payload % X", recipients[1], sent[1].Payload())
	}
	if recipients[2] != targetSession || !bytes.Equal(sent[2].Payload(), []byte{1, 20, 0, 0, 0}) {
		t.Fatalf("target acknowledgement = recipient %p/payload % X", recipients[2], sent[2].Payload())
	}
	for _, index := range []int{3, 4} {
		if sent[index].MessageID != opcode.PartyCreate {
			t.Fatalf("snapshot %d opcode = 0x%04X, want 0x%04X", index, sent[index].MessageID, opcode.PartyCreate)
		}
	}
}

func TestExchangeInvitationAnswerFallsBackFromPartyHandler(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 20}}
	fallback := &exchangeInvitationFallbackFake{handled: true}
	handler := &PartyHandler{service: service.NewPartyService(nil), exchangeInvitationFallback: fallback}
	answer := network.EmptyClientPacket()
	answer.WriteByte(1)
	answer.WriteByte(1)
	if err := handler.handleInvitationAnswer(context.Background(), session, answer); err != nil {
		t.Fatalf("exchange invitation answer: %v", err)
	}
	if fallback.session != session || !fallback.accepted {
		t.Fatalf("exchange fallback = session %p accepted %v", fallback.session, fallback.accepted)
	}
}

func TestMatchingListSendsRecoveredV188PageLayout(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharName: "master"}}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}
	master := &model.Player{ID: 100, CharName: "master", Session: session, SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: position}}
	core := service.NewPartyService(matchingHandlerPlayers{players: map[uint32]*model.Player{10: master}})
	partyNumber := core.FormParty(model.PartyFormRequest{
		MasterUniqueID: 10, MasterJID: 100, MasterName: "master", CountryType: 2,
		PartySettingsFlag: model.ExpShareItemShare, PurposeType: model.QuestingParty,
		LevelMin: 10, LevelMax: 30, Title: "open party",
	})
	if partyNumber == 0 {
		t.Fatal("FormParty returned zero")
	}
	handler := &PartyHandler{service: core}
	var sent network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = packet
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteByte(0)
	if err := handler.handleMatchingList(context.Background(), session, request); err != nil {
		t.Fatalf("matching list: %v", err)
	}
	if sent.MessageID != opcode.PartyMatchingListResponse {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", sent.MessageID, opcode.PartyMatchingListResponse)
	}
	r := newPartyPacketReader(t, sent.Payload())
	if success, page, pages, count := r.byte(), r.byte(), r.byte(), r.byte(); success != 1 || page != 0 || pages != 1 || count != 1 {
		t.Fatalf("page header = success=%d page=%d pages=%d count=%d", success, page, pages, count)
	}
	if number, masterJID, masterName := r.uint32(), r.uint32(), r.string(); number != partyNumber || masterJID != 100 || masterName != "master" {
		t.Fatalf("listing identity = number=%d masterJID=%d name=%q", number, masterJID, masterName)
	}
	if country, members, settings, purpose, minLevel, maxLevel := r.byte(), r.byte(), r.byte(), r.byte(), r.byte(), r.byte(); country != 2 || members != 1 || settings != byte(model.ExpShareItemShare) || purpose != byte(model.QuestingParty) || minLevel != 10 || maxLevel != 30 {
		t.Fatalf("listing fields = country=%d members=%d settings=%d purpose=%d levels=%d-%d", country, members, settings, purpose, minLevel, maxLevel)
	}
	if title := r.string(); title != "open party" {
		t.Fatalf("listing title = %q", title)
	}
	r.done()
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

func TestBuildMatchingUpsertResultUsesFormAndUpdateWireLayouts(t *testing.T) {
	for _, test := range []struct {
		name   string
		opcode uint16
	}{
		{name: "form", opcode: opcode.PartyMatchingFormResponse},
		{name: "update", opcode: opcode.PartyMatchingUpdateResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := buildMatchingUpsertResult(test.opcode, 0x11223344, 1, 2, 3, 4, "open")
			if packet.MessageID != test.opcode {
				t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, test.opcode)
			}
			want := []byte{1, 0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0, 1, 2, 3, 4, 4, 0, 'o', 'p', 'e', 'n'}
			if got := packet.Payload(); !bytes.Equal(got, want) {
				t.Fatalf("payload = % X, want % X", got, want)
			}
		})
	}
}
