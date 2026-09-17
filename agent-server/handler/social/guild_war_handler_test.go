package social

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type guildWarRepositoryFake struct {
	proposal   model.GuildWarProposal
	state      model.GuildWarState
	endActor   int
	endWarID   uint32
	killerID   int
	victimID   int
	points     uint32
	claimActor int
	claimWarID uint32
}

func (f *guildWarRepositoryFake) AvailableGuildWarReward(_ context.Context, actor int, warID uint32) (model.GuildWarState, error) {
	f.claimActor, f.claimWarID = actor, warID
	return f.state, nil
}

func (f *guildWarRepositoryFake) ProposeGuildWar(_ context.Context, _ int, proposal model.GuildWarProposal) (model.GuildWarState, error) {
	f.proposal = proposal
	return f.state, nil
}

func (f *guildWarRepositoryFake) AnswerGuildWar(context.Context, int, bool) (model.GuildWarState, error) {
	return f.state, nil
}

func (f *guildWarRepositoryFake) EndGuildWar(_ context.Context, actor int, warID uint32) (model.GuildWarState, error) {
	f.endActor = actor
	f.endWarID = warID
	return f.state, nil
}

func (f *guildWarRepositoryFake) ActiveGuildWar(context.Context, int) (model.GuildWarState, error) {
	return f.state, nil
}

func (f *guildWarRepositoryFake) ExpireGuildWars(context.Context, time.Time, int) ([]model.GuildWarState, error) {
	return []model.GuildWarState{f.state}, nil
}

func (f *guildWarRepositoryFake) RecordGuildWarKill(_ context.Context, killerID, victimID int, points uint32) (model.GuildWarState, error) {
	f.killerID, f.victimID, f.points = killerID, victimID, points
	return f.state, nil
}

func (f *guildWarRepositoryFake) ClaimGuildWarReward(_ context.Context, actor int, warID uint32) (model.GuildWarState, error) {
	f.claimActor, f.claimWarID = actor, warID
	return f.state, nil
}

type guildWarSocialFake struct {
	model.SocialRepository
	guilds map[int]model.GuildSnapshot
}

func (f guildWarSocialFake) Guild(_ context.Context, characterID int) (model.GuildSnapshot, error) {
	if guild, ok := f.guilds[characterID]; ok {
		return guild, nil
	}
	return model.GuildSnapshot{}, model.ErrSocialNotFound
}

func TestGuildWarProposalReservesWagerAndInvitesTargetMaster(t *testing.T) {
	challengerSession := &server.Session{ID: "challenger", UserContext: server.UserContext{CharacterID: 1, UniqueID: 101}}
	targetSession := &server.Session{ID: "target", UserContext: server.UserContext{CharacterID: 2, UniqueID: 202}}
	challenger := &model.Player{ID: 1, Session: challengerSession, Gold: 99}
	target := &model.Player{ID: 2, Session: targetSession}
	repository := &guildWarRepositoryFake{state: model.GuildWarState{ChallengerMaster: 1, TargetMaster: 2, ChallengerName: "Aegis", TargetName: "Target", WarPeriodID: 10 << 20, VictoryPointID: 2, Wager: 50, ChallengerGold: 49}}
	h := &Handler{war: repository, world: guildUpdateWorldFake{1: challenger, 2: target}}

	sent := make(map[string][]uint16)
	h.send = func(session *server.Session, packet network.Packet) error {
		sent[session.ID] = append(sent[session.ID], packet.MessageID)
		return nil
	}
	packet := vsro188.BuildGuildWarProposal(vsro188.GuildWarProposal{GuildName: "Target", WarPeriodID: 10 << 20, VictoryPointID: 2, Wager: 50})
	if err := h.guildWarPropose(context.Background(), challengerSession, packet); err != nil {
		t.Fatal(err)
	}
	if repository.proposal.TargetGuildName != "Target" || repository.proposal.Wager != 50 {
		t.Fatalf("repository proposal = %#v", repository.proposal)
	}
	if challenger.Gold != 49 {
		t.Fatalf("challenger gold = %d, want 49", challenger.Gold)
	}
	if got := sent["challenger"]; len(got) != 1 || got[0] != opcode.GuildWarStartResponse {
		t.Fatalf("challenger packets = %v", got)
	}
	if got := sent["target"]; len(got) != 1 || got[0] != opcode.GuildWarRequest {
		t.Fatalf("target packets = %v", got)
	}
}

func TestGuildWarEndPaysWinnerAndNotifiesBothMasters(t *testing.T) {
	challengerSession := &server.Session{ID: "challenger", UserContext: server.UserContext{CharacterID: 1, UniqueID: 101}}
	targetSession := &server.Session{ID: "target", UserContext: server.UserContext{CharacterID: 2, UniqueID: 202}}
	challenger := &model.Player{ID: 1, Session: challengerSession}
	target := &model.Player{ID: 2, Session: targetSession, Gold: 100}
	repository := &guildWarRepositoryFake{state: model.GuildWarState{ID: 7, ChallengerMaster: 1, TargetMaster: 2, WinnerMaster: 2, WinnerGold: 200, Status: "ended"}}
	h := &Handler{war: repository, world: guildUpdateWorldFake{1: challenger, 2: target}}
	sent := make(map[string][]uint16)
	h.send = func(session *server.Session, packet network.Packet) error {
		sent[session.ID] = append(sent[session.ID], packet.MessageID)
		return nil
	}

	packet := vsro188.BuildGuildWarEndRequest(7)
	if err := h.guildWarEnd(context.Background(), challengerSession, packet); err != nil {
		t.Fatal(err)
	}
	if repository.endActor != 1 || repository.endWarID != 7 {
		t.Fatalf("end call = actor %d war %d", repository.endActor, repository.endWarID)
	}
	if target.Gold != 100 {
		t.Fatalf("winner gold = %d, want escrow to remain claimable", target.Gold)
	}
	for _, sessionID := range []string{"challenger", "target"} {
		if got := sent[sessionID]; len(got) != 1 || got[0] != opcode.GuildWarEndResponse {
			t.Fatalf("%s packets = %v", sessionID, got)
		}
	}
}

func TestGuildWarExpiryRefundsEscrowAndPublishesGold(t *testing.T) {
	challengerSession := &server.Session{ID: "challenger", UserContext: server.UserContext{CharacterID: 1, UniqueID: 101}}
	targetSession := &server.Session{ID: "target", UserContext: server.UserContext{CharacterID: 2, UniqueID: 202}}
	challenger := &model.Player{ID: 1, Session: challengerSession, Gold: 10}
	target := &model.Player{ID: 2, Session: targetSession, Gold: 20}
	repository := &guildWarRepositoryFake{state: model.GuildWarState{ID: 7, ChallengerMaster: 1, TargetMaster: 2, ChallengerGold: 60, TargetGold: 70, Status: "ended"}}
	h := &Handler{war: repository, world: guildUpdateWorldFake{1: challenger, 2: target}}
	sent := make(map[string][]network.Packet)
	h.send = func(session *server.Session, packet network.Packet) error {
		sent[session.ID] = append(sent[session.ID], packet)
		return nil
	}

	count, err := h.ExpireGuildWars(context.Background(), time.Unix(100, 0))
	if err != nil || count != 1 {
		t.Fatalf("count = %d, error = %v", count, err)
	}
	if challenger.Gold != 60 || target.Gold != 70 {
		t.Fatalf("runtime gold = %d/%d", challenger.Gold, target.Gold)
	}
	for sessionID, wantGold := range map[string]uint64{"challenger": 60, "target": 70} {
		packets := sent[sessionID]
		if len(packets) != 1 || packets[0].MessageID != opcode.EntityUpdatePoints {
			t.Fatalf("%s packets = %#v", sessionID, packets)
		}
		update, decodeErr := vsro188.DecodeGoldPointUpdate(&packets[0])
		if decodeErr != nil || update.Value != wantGold {
			t.Fatalf("%s gold = %d, error = %v", sessionID, update.Value, decodeErr)
		}
	}
}

func TestGuildWarKillPublishesScoresToBothGuilds(t *testing.T) {
	challengerSession := &server.Session{ID: "challenger", UserContext: server.UserContext{CharacterID: 1, UniqueID: 101}}
	targetSession := &server.Session{ID: "target", UserContext: server.UserContext{CharacterID: 2, UniqueID: 202}}
	state := model.GuildWarState{ID: 7, ChallengerGuild: 10, TargetGuild: 20, ChallengerMaster: 1, TargetMaster: 2, WarPeriodID: 10 << 20, VictoryPointID: 2, ChallengerScore: 80, TargetScore: 40, ScoringGuild: 10, ScoreDelta: 80, Status: "active"}
	repository := &guildWarRepositoryFake{state: state}
	h := &Handler{
		war: repository,
		repo: guildWarSocialFake{guilds: map[int]model.GuildSnapshot{
			1: {ID: 10, Members: []model.GuildMember{{CharacterID: 1}}},
			2: {ID: 20, Members: []model.GuildMember{{CharacterID: 2}}},
		}},
		world: guildUpdateWorldFake{1: &model.Player{ID: 1, Session: challengerSession}, 2: &model.Player{ID: 2, Session: targetSession}},
	}
	sent := make(map[string][]uint16)
	h.send = func(session *server.Session, packet network.Packet) error {
		sent[session.ID] = append(sent[session.ID], packet.MessageID)
		return nil
	}
	if err := h.RecordGuildWarKill(context.Background(), 1, 2, 80); err != nil {
		t.Fatal(err)
	}
	if repository.killerID != 1 || repository.victimID != 2 || repository.points != 80 {
		t.Fatalf("record = %d/%d/%d", repository.killerID, repository.victimID, repository.points)
	}
	for _, sessionID := range []string{"challenger", "target"} {
		if got := sent[sessionID]; len(got) != 1 || got[0] != opcode.GuildUpdate {
			t.Fatalf("%s packets = %v", sessionID, got)
		}
	}
}

func TestGuildWarRewardClaimPublishesRewardAndGold(t *testing.T) {
	session := &server.Session{ID: "winner", UserContext: server.UserContext{CharacterID: 2, UniqueID: 202}}
	winner := &model.Player{ID: 2, Session: session, Gold: 100}
	repository := &guildWarRepositoryFake{state: model.GuildWarState{ID: 7, WinnerMaster: 2, RewardGold: 100, WinnerGold: 200, Status: "ended"}}
	h := &Handler{war: repository, world: guildUpdateWorldFake{2: winner}}
	var sent []uint16
	h.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet.MessageID)
		return nil
	}
	packet := vsro188.BuildGuildWarCompensationClaimRequest(100)
	if err := h.guildWarCompensationClaim(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if repository.claimActor != 2 || repository.claimWarID != 100 {
		t.Fatalf("claim = %d/%d", repository.claimActor, repository.claimWarID)
	}
	if winner.Gold != 200 {
		t.Fatalf("winner gold = %d", winner.Gold)
	}
	if len(sent) != 2 || sent[0] != opcode.GuildWarCompensationClaimResponse || sent[1] != opcode.EntityUpdatePoints {
		t.Fatalf("packets = %v", sent)
	}
}

func TestGuildWarRewardQueryDoesNotPayBeforeConfirmation(t *testing.T) {
	session := &server.Session{ID: "winner", UserContext: server.UserContext{CharacterID: 2, UniqueID: 202}}
	winner := &model.Player{ID: 2, Session: session, Gold: 100}
	repository := &guildWarRepositoryFake{state: model.GuildWarState{ID: 7, WinnerMaster: 2, RewardGold: 100, Status: "ended"}}
	h := &Handler{war: repository, world: guildUpdateWorldFake{2: winner}}
	var sent []network.Packet
	h.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	packet := vsro188.BuildGuildWarRewardRequest(7)
	if err := h.guildWarReward(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if winner.Gold != 100 || repository.claimActor != 2 || repository.claimWarID != 7 {
		t.Fatalf("query mutated gold or arguments: gold=%d actor=%d id=%d", winner.Gold, repository.claimActor, repository.claimWarID)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.GuildWarRewardResponse {
		t.Fatalf("packets = %#v", sent)
	}
}
