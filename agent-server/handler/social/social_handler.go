package social

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	log "github.com/sirupsen/logrus"
)

const academyPageSize = 10

type worldLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetPlayerByCharacterID(int) (*model.Player, error)
}

type guildMercenaryTerminator interface {
	TerminateGuildMercenaries(context.Context, *model.Player) (int, error)
}

// Handler owns the GameServer-equivalent protocol and online-publication
// layer. Durable authority and transitions remain in SocialRepository.
type Handler struct {
	repo           model.SocialRepository
	war            model.GuildWarRepository
	election       model.GuildElectionRepository
	siege          model.GuildSiegeRepository
	mercenary      model.GuildMercenaryRepository
	fortress       model.FortressExpenditureRepository
	community      model.CommunityFriendRepository
	communityBlock model.CommunityBlockRepository
	memo           model.CommunityMemoRepository
	cos            guildMercenaryTerminator
	world          worldLookup
	safeTrade      model.SafeTradeRepository
	safeTradeLimit byte
	send           func(*server.Session, network.Packet) error
	startOnce      sync.Once
	academyMu      sync.Mutex
	academyInvites map[uint32]academyInvitation
}

const guildWarExpiryInterval = time.Minute

func (h *Handler) Start(ctx context.Context) {
	if h == nil || h.war == nil && h.election == nil {
		return
	}
	h.startOnce.Do(func() { go h.runGuildWarExpiry(ctx) })
}

func (h *Handler) runGuildWarExpiry(ctx context.Context) {
	ticker := time.NewTicker(guildWarExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if h.war != nil {
				if _, err := h.ExpireGuildWars(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
					log.WithError(err).Error("expire guild wars")
				}
			}
			if h.election != nil {
				if _, err := h.ExpireGuildElections(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
					log.WithError(err).Error("expire guild elections")
				}
			}
		}
	}
}

func (h *Handler) ExpireGuildWars(ctx context.Context, now time.Time) (int, error) {
	if h == nil || h.war == nil {
		return 0, model.ErrSocialRepositoryUnavailable
	}
	states, err := h.war.ExpireGuildWars(ctx, now, 100)
	if err != nil {
		return 0, err
	}
	for _, state := range states {
		goldUpdates := map[int]uint64{}
		if state.WinnerMaster == 0 {
			goldUpdates[state.ChallengerMaster], goldUpdates[state.TargetMaster] = state.ChallengerGold, state.TargetGold
		}
		for characterID, gold := range goldUpdates {
			h.commitGold(characterID, gold)
			if player, lookupErr := h.world.GetPlayerByCharacterID(characterID); lookupErr == nil && player != nil && player.Session != nil {
				if sendErr := h.sendPacket(player.Session, vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold})); sendErr != nil {
					return 0, sendErr
				}
			}
		}
	}
	return len(states), nil
}

func (h *Handler) ConfigureSafeTrade(repository model.SafeTradeRepository, limit uint) error {
	if h == nil || repository == nil || limit == 0 || limit > math.MaxUint8 {
		return model.ErrSafeTradeInvalidRequest
	}
	h.safeTrade = repository
	h.safeTradeLimit = byte(limit)
	return nil
}

func (h *Handler) ConfigureCOS(terminator guildMercenaryTerminator) {
	if h != nil {
		h.cos = terminator
	}
}

func safeTradeRestoreUpdate(state model.SafeTradeState, limit byte) vsro188.SafeTradeUpdate {
	if state.Status == 0 {
		return vsro188.SafeTradeUpdate{Code: vsro188.JobSafeTradeCodeTransition40}
	}
	return vsro188.SafeTradeUpdate{Active: true, Code: vsro188.JobSafeTradeCodeRestore51, Current: state.AbleCount, Limit: limit}
}

func RegisterHandlers(target *server.Server, repo model.SocialRepository) *Handler {
	if target == nil || repo == nil {
		return nil
	}
	h := &Handler{repo: repo, world: service.GetWorldServiceInstance(), academyInvites: make(map[uint32]academyInvitation)}
	h.war, _ = repo.(model.GuildWarRepository)
	h.election, _ = repo.(model.GuildElectionRepository)
	h.siege, _ = repo.(model.GuildSiegeRepository)
	h.mercenary, _ = repo.(model.GuildMercenaryRepository)
	h.fortress, _ = repo.(model.FortressExpenditureRepository)
	h.community, _ = repo.(model.CommunityFriendRepository)
	h.communityBlock, _ = repo.(model.CommunityBlockRepository)
	h.memo, _ = repo.(model.CommunityMemoRepository)
	target.RegisterHandler(opcode.GuildCreateRequest, h.guildCreate)
	target.RegisterHandler(opcode.GuildDisbandRequest, h.guildDisband)
	target.RegisterHandler(opcode.GuildLeaveRequest, h.guildLeave)
	target.RegisterHandler(opcode.GuildInviteRequest, h.guildInvite)
	target.RegisterHandler(opcode.GuildKickRequest, h.guildKick)
	target.RegisterHandler(opcode.GuildNoticeRequest, h.guildNotice)
	target.RegisterHandler(opcode.GuildPromoteRequest, h.guildPromote)
	target.RegisterHandler(opcode.GuildUnionInviteRequest, h.guildUnionInvite)
	target.RegisterHandler(opcode.GuildUnionLeaveRequest, h.guildUnionLeave)
	target.RegisterHandler(opcode.GuildUnionKickRequest, h.guildUnionKick)
	target.RegisterHandler(opcode.GuildTransferRequest, h.guildTransfer)
	target.RegisterHandler(opcode.GuildPermissionRequest, h.guildPermissions)
	target.RegisterHandler(opcode.GuildNicknameRequest, h.guildNickname)
	target.RegisterHandler(opcode.GuildDonateRequest, h.guildDonate)
	if h.war != nil {
		target.RegisterHandler(opcode.GuildWarRequest, h.guildWarPropose)
		target.RegisterHandler(opcode.GuildWarEndRequest, h.guildWarEnd)
		target.RegisterHandler(opcode.GuildWarCompensationClaimRequest, h.guildWarCompensationClaim)
		target.RegisterHandler(opcode.GuildWarRewardRequest, h.guildWarReward)
	}
	if h.election != nil {
		target.RegisterHandler(opcode.GuildElectionRequest, h.guildElectionStart)
		target.RegisterHandler(opcode.GuildElectionParticipateRequest, h.guildElectionOpen)
		target.RegisterHandler(opcode.GuildElectionVoteRequest, h.guildElectionVote)
	}
	if h.siege != nil {
		target.RegisterHandler(opcode.GuildUpdateSiegeAuthorityRequest, h.guildSiegeAuthority)
	}
	if h.mercenary != nil {
		target.RegisterHandler(opcode.GuildMercenaryAttrRequest, h.guildMercenaryAttribute)
	}
	target.RegisterHandler(opcode.GuildMercenaryTerminateRequest, h.guildMercenaryTerminate)
	if h.fortress != nil {
		target.RegisterHandler(opcode.GuildGPHistoryRequest, h.fortressExpenditureHistory)
	}
	if h.community != nil {
		target.RegisterHandler(opcode.CommunityFriendAddRequest, h.communityFriendAdd)
		target.RegisterHandler(opcode.CommunityFriendResponse, h.communityFriendResponse)
		target.RegisterHandler(opcode.CommunityFriendDeleteRequest, h.communityFriendDelete)
	}
	if h.communityBlock != nil {
		target.RegisterHandler(opcode.CommunityBlockUpdateRequest, h.communityBlockUpdate)
	}
	if h.memo != nil {
		target.RegisterHandler(opcode.CommunityMemoOpenRequest, h.communityMemoOpen)
		target.RegisterHandler(opcode.CommunityMemoSendRequest, h.communityMemoSend)
		target.RegisterHandler(opcode.CommunityMemoDeleteRequest, h.communityMemoDelete)
		target.RegisterHandler(opcode.CommunityMemoListRequest, h.communityMemoList)
		target.RegisterHandler(opcode.CommunityMemoGroupSendRequest, h.communityMemoGroupSend)
	}
	target.RegisterHandler(opcode.AcademyCreateRequest, h.academyCreate)
	target.RegisterHandler(opcode.AcademyDisbandRequest, h.academyDisband)
	target.RegisterHandler(opcode.AcademyKickRequest, h.academyKick)
	target.RegisterHandler(opcode.AcademyLeaveRequest, h.academyLeave)
	target.RegisterHandler(opcode.AcademyCommentRequest, h.academyComment)
	target.RegisterHandler(opcode.AcademyGradeRequest, h.academyGrade)
	target.RegisterHandler(opcode.AcademyHonorRankRequest, h.academyHonorRank)
	target.RegisterHandler(opcode.AcademyMatchingRegisterRequest, h.academyMatchingRegister)
	target.RegisterHandler(opcode.AcademyMatchingChangeRequest, h.academyMatchingRegister)
	target.RegisterHandler(opcode.AcademyMatchingDeleteRequest, h.academyMatchingDelete)
	target.RegisterHandler(opcode.AcademyMatchingListRequest, h.academyMatchingList)
	target.RegisterHandler(opcode.AcademyMatchingJoinRequest, h.academyMatchingJoin)
	target.RegisterHandler(opcode.AcademyInviteRequest, h.academyInvite)
	target.RegisterHandler(opcode.AcademyMemberActionRequest, h.academyMemberAction)
	target.RegisterHandler(opcode.AcademyMatchingResponse, h.academyInvitationResponse)
	target.RegisterHandler(opcode.AcademyUnknownOperationRequest, h.academyTimedOperation)
	target.RegisterHandler(opcode.JobJoinRequest, h.jobJoin)
	target.RegisterHandler(opcode.JobLeaveRequest, h.jobLeave)
	target.RegisterHandler(opcode.JobAliasRequest, h.jobAlias)
	target.RegisterHandler(opcode.JobRankingRequest, h.jobRanking)
	target.RegisterHandler(opcode.JobOutcomeRequest, h.jobOutcome)
	target.RegisterHandler(opcode.JobPreviousInfoRequest, h.jobPrevious)
	return h
}

func (h *Handler) sendPacket(session *server.Session, packet network.Packet) error {
	if err := packet.WriteError(); err != nil {
		return err
	}
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func socialCharacter(session *server.Session) (int, error) {
	if session == nil || session.UserContext.CharacterID <= 0 || session.UserContext.UniqueID == 0 {
		return 0, model.ErrSocialInvalidRequest
	}
	return session.UserContext.CharacterID, nil
}

func operationPacket(messageID uint16, err error) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	if err == nil {
		p.WriteByte(1)
		return p
	}
	p.WriteByte(2)
	p.WriteUInt16(socialErrorCode(err))
	return p
}

func socialErrorCode(err error) uint16 {
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, model.ErrSocialInsufficientLevel):
		return 0x4A01
	case errors.Is(err, model.ErrSocialInsufficientGold):
		return 0x4A02
	case errors.Is(err, model.ErrSocialAccessDenied):
		return 0x4A03
	case errors.Is(err, model.ErrSocialCapacity):
		return 0x4A04
	case errors.Is(err, model.ErrSocialConflict):
		return 0x4A05
	case errors.Is(err, model.ErrSocialNotFound):
		return 0x4A06
	default:
		return 0x4AFF
	}
}

func (h *Handler) guildCreate(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.create"})
	characterID, err := socialCharacter(session)
	if err != nil || packet.Remaining() < 6 {
		return fmt.Errorf("invalid guild create request")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	name, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild name")
	}
	guild, gold, err := h.repo.CreateGuild(ctx, characterID, name)
	if err != nil {
		return h.sendPacket(session, operationPacket(opcode.GuildCreateResponse, err))
	}
	response := operationPacket(opcode.GuildCreateResponse, nil)
	writeGuild(&response, guild)
	if err = h.sendPacket(session, response); err != nil {
		return err
	}
	h.commitGold(characterID, gold)
	if err = h.publishGuildEntityUpdate(characterID, guild); err != nil {
		return err
	}
	return h.publishGuildSnapshot(guild)
}

func (h *Handler) guildDisband(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.disband"})
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 4 {
		_, err = packet.ReadUInt32()
	}
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild disband request")
	}
	before, _ := h.repo.Guild(ctx, characterID)
	err = h.repo.DisbandGuild(ctx, characterID)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildDisbandResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildRemoval(before)
	}
	return nil
}

func (h *Handler) guildLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.leave"})
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild leave request")
	}
	before, _ := h.repo.Guild(ctx, characterID)
	err = h.repo.LeaveGuild(ctx, characterID)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildLeaveResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildRemovalFor(before, characterID, vsro188.GuildMemberLeft)
	}
	return nil
}

func (h *Handler) guildInvite(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.invite"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild invite request")
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	target, err := h.world.GetPlayerByUniqueId(uid)
	if err != nil || target == nil {
		return h.sendPacket(session, operationPacket(opcode.GuildInviteResponse, model.ErrSocialNotFound))
	}
	err = h.repo.InviteGuild(ctx, actor, target.ID)
	if err == nil {
		// Invitation type 5 is the stock guild invitation discriminator. The
		// durable invitation can be accepted by the generic invitation flow.
		invite := network.EmptyPacket()
		invite.MessageID = opcode.PlayerInvitationRequest
		invite.WriteByte(5)
		invite.WriteUInt32(session.UserContext.UniqueID)
		if sendErr := h.sendPacket(target.Session, invite); sendErr != nil {
			return sendErr
		}
	}
	return h.sendPacket(session, operationPacket(opcode.GuildInviteResponse, err))
}

// AnswerGuildInvitation plugs into the generic 0x3080 invitation dispatcher.
// Party invitations retain priority; this fallback consumes the response only
// when a durable, unexpired guild invitation exists for the character.
func (h *Handler) AnswerGuildInvitation(ctx context.Context, session *server.Session, accept bool) (bool, error) {
	characterID, err := socialCharacter(session)
	if err != nil {
		return false, err
	}
	guild, err := h.repo.AnswerGuildInvite(ctx, characterID, accept)
	if errors.Is(err, model.ErrSocialNotFound) {
		union, unionErr := h.repo.AnswerGuildUnionInvite(ctx, characterID, accept)
		if errors.Is(unionErr, model.ErrSocialNotFound) {
			return h.answerGuildWarInvitation(ctx, session, characterID, accept)
		}
		if sendErr := h.sendPacket(session, operationPacket(opcode.GuildUnionInviteResponse, unionErr)); sendErr != nil {
			return true, sendErr
		}
		if unionErr == nil && accept {
			return true, h.publishUnion(ctx, characterID, union)
		}
		return true, nil
	}
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildInviteResponse, err)); sendErr != nil {
		return true, sendErr
	}
	if err == nil && accept {
		if err = h.sendGuildSnapshot(characterID, guild); err != nil {
			return true, err
		}
		for _, member := range guild.Members {
			if member.CharacterID == characterID {
				if err = h.publishGuildUpdateExcept(guild, vsro188.BuildGuildMemberJoinUpdate(guildMemberUpdate(member)), characterID); err != nil {
					return true, err
				}
				break
			}
		}
		return true, h.publishGuildEntityUpdate(characterID, guild)
	}
	return true, nil
}

func (h *Handler) guildWarPropose(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h.war == nil {
		return h.sendPacket(session, vsro188.BuildGuildWarStartResult(socialErrorCode(model.ErrSocialNotFound)))
	}
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.war.propose"})
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	proposal, err := vsro188.DecodeGuildWarProposal(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarStartResult(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	state, err := h.war.ProposeGuildWar(ctx, characterID, model.GuildWarProposal{TargetGuildName: proposal.GuildName, WarPeriodID: proposal.WarPeriodID, VictoryPointID: proposal.VictoryPointID, Wager: proposal.Wager})
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarStartResult(socialErrorCode(err)))
	}
	h.commitGold(state.ChallengerMaster, state.ChallengerGold)
	target, lookupErr := h.world.GetPlayerByCharacterID(state.TargetMaster)
	if lookupErr != nil || target == nil || target.Session == nil {
		rejected, rejectErr := h.war.AnswerGuildWar(ctx, state.TargetMaster, false)
		if rejectErr == nil {
			h.commitGold(rejected.ChallengerMaster, rejected.ChallengerGold)
		}
		return h.sendPacket(session, vsro188.BuildGuildWarStartResult(socialErrorCode(model.ErrSocialNotFound)))
	}
	if err = h.sendPacket(session, vsro188.BuildGuildWarStartResult(0)); err != nil {
		return err
	}
	invite := vsro188.BuildGuildWarProposal(vsro188.GuildWarProposal{GuildName: state.ChallengerName, Mode: 0, WarPeriodID: state.WarPeriodID, VictoryPointID: state.VictoryPointID, Wager: state.Wager})
	return h.sendPacket(target.Session, invite)
}

func (h *Handler) answerGuildWarInvitation(ctx context.Context, session *server.Session, characterID int, accept bool) (bool, error) {
	if h.war == nil {
		return false, nil
	}
	state, err := h.war.AnswerGuildWar(ctx, characterID, accept)
	if errors.Is(err, model.ErrSocialNotFound) {
		return false, nil
	}
	code := uint16(0)
	if err != nil {
		code = socialErrorCode(err)
	}
	if sendErr := h.sendPacket(session, vsro188.BuildGuildWarStartResult(code)); sendErr != nil {
		return true, sendErr
	}
	if err != nil {
		return true, nil
	}
	if accept {
		h.commitGold(state.TargetMaster, state.TargetGold)
	} else {
		h.commitGold(state.ChallengerMaster, state.ChallengerGold)
	}
	if challenger, lookupErr := h.world.GetPlayerByCharacterID(state.ChallengerMaster); lookupErr == nil && challenger != nil && challenger.Session != nil {
		if sendErr := h.sendPacket(challenger.Session, vsro188.BuildGuildWarStartResult(0)); sendErr != nil {
			return true, sendErr
		}
	}
	if accept {
		return true, h.publishGuildWarInfo(ctx, state)
	}
	return true, nil
}

func guildWarInfoPacket(state model.GuildWarState) (network.Packet, error) {
	if state.ID == 0 || state.ID > math.MaxUint32 || state.ChallengerGuild <= 0 || state.TargetGuild <= 0 {
		return network.Packet{}, model.ErrSocialInvalidRequest
	}
	return vsro188.BuildGuildWarInfo(vsro188.GuildWarInfo{
		WarID: uint32(state.ID), WarPeriodID: state.WarPeriodID, VictoryPointID: state.VictoryPointID,
		Wager: state.Wager, ChallengerGuildID: uint32(state.ChallengerGuild), TargetGuildID: uint32(state.TargetGuild),
		ChallengerScore: state.ChallengerScore, TargetScore: state.TargetScore,
	})
}

func (h *Handler) RecordGuildWarKill(ctx context.Context, killerID, victimID int, victimLevel uint32) error {
	if h == nil || h.war == nil {
		return nil
	}
	state, err := h.war.RecordGuildWarKill(ctx, killerID, victimID, victimLevel)
	if errors.Is(err, model.ErrSocialNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	killerName, victimName := "", ""
	if killer, lookupErr := h.world.GetPlayerByCharacterID(killerID); lookupErr == nil && killer != nil {
		killerName = killer.CharName
	}
	if victim, lookupErr := h.world.GetPlayerByCharacterID(victimID); lookupErr == nil && victim != nil {
		victimName = victim.CharName
	}
	killerMaster, victimMaster := state.ChallengerMaster, state.TargetMaster
	if state.ScoringGuild == state.TargetGuild {
		killerMaster, victimMaster = state.TargetMaster, state.ChallengerMaster
	}
	for _, publication := range []struct {
		master    int
		side      byte
		character int
		name      string
	}{{killerMaster, 1, killerID, killerName}, {victimMaster, 2, victimID, victimName}} {
		guild, loadErr := h.repo.Guild(ctx, publication.master)
		if loadErr != nil {
			return loadErr
		}
		packet := vsro188.BuildGuildWarScoreUpdate(publication.side, uint32(state.ID), state.ScoreDelta, uint32(publication.character), publication.name)
		if sendErr := h.publishGuildUpdate(guild, packet); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

func (h *Handler) publishGuildWarInfo(ctx context.Context, state model.GuildWarState) error {
	packet, err := guildWarInfoPacket(state)
	if err != nil {
		return err
	}
	seen := make(map[int]struct{})
	for _, master := range []int{state.ChallengerMaster, state.TargetMaster} {
		guild, loadErr := h.repo.Guild(ctx, master)
		if loadErr != nil {
			return loadErr
		}
		for _, member := range guild.Members {
			if _, ok := seen[member.CharacterID]; ok {
				continue
			}
			seen[member.CharacterID] = struct{}{}
			if player, lookupErr := h.world.GetPlayerByCharacterID(member.CharacterID); lookupErr == nil && player != nil && player.Session != nil {
				if err = h.sendPacket(player.Session, packet); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (h *Handler) guildWarEnd(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h.war == nil {
		return h.sendPacket(session, vsro188.BuildGuildWarEndResult(socialErrorCode(model.ErrSocialNotFound)))
	}
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	warID, err := vsro188.DecodeGuildWarEndRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarEndResult(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	state, err := h.war.EndGuildWar(ctx, characterID, warID)
	code := uint16(0)
	if err != nil {
		code = socialErrorCode(err)
	}
	if sendErr := h.sendPacket(session, vsro188.BuildGuildWarEndResult(code)); sendErr != nil {
		return sendErr
	}
	if err != nil {
		return nil
	}
	otherMaster := state.ChallengerMaster
	if characterID == state.ChallengerMaster {
		otherMaster = state.TargetMaster
	}
	if other, lookupErr := h.world.GetPlayerByCharacterID(otherMaster); lookupErr == nil && other != nil && other.Session != nil {
		return h.sendPacket(other.Session, vsro188.BuildGuildWarEndResult(0))
	}
	return nil
}

func (h *Handler) guildWarReward(ctx context.Context, session *server.Session, packet network.Packet) error {
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	warID, err := vsro188.DecodeGuildWarRewardRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarRewardResult(0, socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	state, err := h.war.AvailableGuildWarReward(ctx, characterID, warID)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarRewardResult(0, socialErrorCode(err)))
	}
	return h.sendPacket(session, vsro188.BuildGuildWarRewardResult(state.RewardGold, 0))
}

func (h *Handler) guildWarCompensationClaim(ctx context.Context, session *server.Session, packet network.Packet) error {
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	reward, err := vsro188.DecodeGuildWarCompensationClaimRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarCompensationClaimResult(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	state, err := h.war.ClaimGuildWarReward(ctx, characterID, reward)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildWarCompensationClaimResult(socialErrorCode(err)))
	}
	h.commitGold(characterID, state.WinnerGold)
	if err = h.sendPacket(session, vsro188.BuildGuildWarCompensationClaimResult(0)); err != nil {
		return err
	}
	return h.sendPacket(session, vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: state.WinnerGold}))
}

func (h *Handler) guildElectionStart(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	masterID, err := vsro188.DecodeGuildElectionRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildElectionResult(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	state, err := h.election.StartGuildElection(ctx, actor, masterID, time.Now())
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildElectionResult(socialErrorCode(err)))
	}
	if err = h.sendPacket(session, vsro188.BuildGuildElectionResult(0)); err != nil {
		return err
	}
	guild, err := h.repo.Guild(ctx, actor)
	if err != nil {
		return err
	}
	return h.publishGuildUpdate(guild, vsro188.BuildGuildElectionOpened(state.ID, 1, uint32(state.EndsAt.Unix())))
}

func (h *Handler) guildSiegeAuthority(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := vsro188.DecodeGuildSiegeAuthorityRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildSiegeAuthorityFailure(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	guild, err := h.siege.UpdateGuildSiegeAuthority(ctx, actor, int(request.CharacterID), request.Authority)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildSiegeAuthorityFailure(socialErrorCode(err)))
	}
	return h.publishGuildUpdate(guild, vsro188.BuildGuildSiegeAuthorityUpdate(request.CharacterID, request.Authority))
}

func (h *Handler) guildMercenaryAttribute(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := vsro188.DecodeGuildMercenaryAttributeRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildMercenaryAttributeFailure(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	guild, gold, err := h.mercenary.UpdateGuildMercenaryAttribute(ctx, actor, request.Attribute)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildMercenaryAttributeFailure(socialErrorCode(err)))
	}
	h.commitGold(actor, gold)
	if err = h.publishGuildUpdate(guild, vsro188.BuildGuildMercenaryAttributeUpdate(guild.GatheredSP, guild.MercenaryAttr)); err != nil {
		return err
	}
	return h.sendPacket(session, vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold}))
}

func (h *Handler) guildMercenaryTerminate(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if vsro188.DecodeGuildMercenaryTerminateRequest(&packet) != nil {
		return h.sendPacket(session, vsro188.BuildGuildMercenaryTerminateFailure(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	if h.cos == nil {
		return h.sendPacket(session, vsro188.BuildGuildMercenaryTerminateFailure(socialErrorCode(model.ErrCOSRepositoryUnavailable)))
	}
	player, err := h.world.GetPlayerByCharacterID(actor)
	if err != nil || player == nil {
		return h.sendPacket(session, vsro188.BuildGuildMercenaryTerminateFailure(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	if _, err = h.cos.TerminateGuildMercenaries(ctx, player); err != nil {
		return h.sendPacket(session, vsro188.BuildGuildMercenaryTerminateFailure(socialErrorCode(err)))
	}
	return nil
}

func (h *Handler) fortressExpenditureHistory(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if vsro188.DecodeFortressExpenditureHistoryRequest(&packet) != nil {
		return h.sendPacket(session, vsro188.BuildFortressExpenditureHistoryFailure(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	history, err := h.fortress.FortressExpenditureHistory(ctx, actor, 255)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildFortressExpenditureHistoryFailure(socialErrorCode(err)))
	}
	entries := make([]vsro188.FortressExpenditureEntry, len(history))
	for i, entry := range history {
		entries[i] = vsro188.FortressExpenditureEntry{Timestamp: entry.Timestamp, CharacterName: entry.CharacterName, Amount: entry.Amount, Reason: byte(entry.Reason)}
	}
	response, err := vsro188.BuildFortressExpenditureHistory(entries)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func communityFriendRecord(friend model.CommunityFriend, online bool) vsro188.CommunityFriendRecord {
	return vsro188.CommunityFriendRecord{CharacterID: friend.CharacterID, Name: friend.Name, RefObjectID: friend.RefObjectID, Online: online}
}

func (h *Handler) communityFriendOnline(characterID uint32) bool {
	player, err := h.world.GetPlayerByCharacterID(int(characterID))
	return err == nil && player != nil && player.Session != nil
}

func (h *Handler) CommunityOffline(ctx context.Context, characterID int) error {
	if h == nil || h.community == nil || characterID <= 0 {
		return nil
	}
	friends, err := h.community.CommunityFriends(ctx, characterID)
	if err != nil {
		return err
	}
	for _, friend := range friends {
		player, lookupErr := h.world.GetPlayerByCharacterID(int(friend.CharacterID))
		if lookupErr == nil && player != nil && player.Session != nil {
			if err = h.sendPacket(player.Session, vsro188.BuildCommunityFriendState(uint32(characterID), false)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Handler) communityFriendAdd(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	targetName, err := vsro188.DecodeCommunityFriendAddRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityFriendAddFailure(socialErrorCode(model.ErrSocialInvalidRequest), 0))
	}
	request, err := h.community.RequestCommunityFriend(ctx, actor, targetName, time.Now())
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityFriendAddFailure(socialErrorCode(err), 0))
	}
	target, lookupErr := h.world.GetPlayerByCharacterID(int(request.Target.CharacterID))
	if lookupErr != nil || target == nil || target.Session == nil {
		return nil
	}
	invitation, err := vsro188.BuildCommunityFriendInvitation(request.Requester.CharacterID, request.Target.CharacterID, request.Requester.Name)
	if err != nil {
		return err
	}
	return h.sendPacket(target.Session, invitation)
}

func (h *Handler) communityFriendResponse(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	response, err := vsro188.DecodeCommunityFriendResponse(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityFriendAddFailure(socialErrorCode(model.ErrSocialInvalidRequest), 0))
	}
	state, err := h.community.RespondCommunityFriend(ctx, actor, int(response.RequesterID), int(response.TargetID), response.Accepted, time.Now())
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityFriendAddFailure(socialErrorCode(err), 0))
	}
	requester, _ := h.world.GetPlayerByCharacterID(int(state.Requester.CharacterID))
	if !response.Accepted {
		if requester != nil && requester.Session != nil {
			return h.sendPacket(requester.Session, vsro188.BuildCommunityFriendAddFailure(0x640b, 2))
		}
		return nil
	}
	requesterUpdate, err := vsro188.BuildCommunityFriendAdded(communityFriendRecord(state.Target, true))
	if err != nil {
		return err
	}
	targetUpdate, err := vsro188.BuildCommunityFriendAdded(communityFriendRecord(state.Requester, requester != nil && requester.Session != nil))
	if err != nil {
		return err
	}
	if requester != nil && requester.Session != nil {
		if err = h.sendPacket(requester.Session, requesterUpdate); err != nil {
			return err
		}
	}
	return h.sendPacket(session, targetUpdate)
}

func (h *Handler) communityFriendDelete(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	friendID, err := vsro188.DecodeCommunityFriendDeleteRequest(&packet)
	if err == nil {
		err = h.community.DeleteCommunityFriend(ctx, actor, int(friendID))
	}
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityFriendDeleteResult(friendID, socialErrorCode(err)))
	}
	if err = h.sendPacket(session, vsro188.BuildCommunityFriendDeleteResult(friendID, 0)); err != nil {
		return err
	}
	friend, _ := h.world.GetPlayerByCharacterID(int(friendID))
	if friend != nil && friend.Session != nil {
		return h.sendPacket(friend.Session, vsro188.BuildCommunityFriendRemoved(uint32(actor)))
	}
	return nil
}

func communityBlockResultCode(action byte, err error) byte {
	if err == nil {
		return 0
	}
	if errors.Is(err, model.ErrSocialNotFound) {
		if action == vsro188.CommunityBlockRemove {
			return 2
		}
		return 1
	}
	if errors.Is(err, model.ErrSocialConflict) {
		return 2
	}
	if errors.Is(err, model.ErrSocialCapacity) {
		return 3
	}
	return 4
}

func (h *Handler) communityBlockUpdate(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := vsro188.DecodeCommunityBlockRequest(&packet)
	if err != nil {
		response, encodeErr := vsro188.BuildCommunityBlockResult(vsro188.CommunityBlockAdd, 4, "")
		if encodeErr != nil {
			return encodeErr
		}
		return h.sendPacket(session, response)
	}
	blocked, updateErr := h.communityBlock.UpdateCommunityBlock(ctx, actor, request.TargetName, request.Action, time.Now())
	result := communityBlockResultCode(request.Action, updateErr)
	name := ""
	if result == 0 {
		name = blocked.Name
	}
	response, err := vsro188.BuildCommunityBlockResult(request.Action, result, name)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func communityMemoEntry(memo model.CommunityMemo) vsro188.CommunityMemoListEntry {
	return vsro188.CommunityMemoListEntry{SenderName: memo.SenderName, RefObjectID: memo.RefObjectID, Timestamp: memo.SentAt, Status: memo.Status}
}

func (h *Handler) communityMemoOpen(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	slot, err := vsro188.DecodeCommunityMemoSlotRequest(&packet)
	if err != nil {
		response, _ := vsro188.BuildCommunityMemoOpen(0, "", socialErrorCode(model.ErrSocialInvalidRequest))
		return h.sendPacket(session, response)
	}
	memo, err := h.memo.OpenCommunityMemo(ctx, actor, slot)
	if err != nil {
		response, _ := vsro188.BuildCommunityMemoOpen(0, "", socialErrorCode(err))
		return h.sendPacket(session, response)
	}
	response, err := vsro188.BuildCommunityMemoOpen(slot, memo.Message, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *Handler) sendCommunityMemo(ctx context.Context, actor int, targetName, message string, now time.Time) error {
	memo, err := h.memo.SendCommunityMemo(ctx, actor, targetName, message, now)
	if err != nil {
		return err
	}
	if h.world == nil {
		return nil
	}
	target, lookupErr := h.world.GetPlayerByCharacterID(int(memo.RecipientID))
	if lookupErr != nil || target == nil || target.Session == nil {
		return nil
	}
	notification, err := vsro188.BuildCommunityMemoNotification(communityMemoEntry(memo))
	if err != nil {
		return err
	}
	return h.sendPacket(target.Session, notification)
}

func (h *Handler) communityMemoSend(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := vsro188.DecodeCommunityMemoSendRequest(&packet)
	if err == nil {
		err = h.sendCommunityMemo(ctx, actor, request.TargetName, request.Message, time.Now())
	}
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityMemoSendResult(socialErrorCode(err)))
	}
	return h.sendPacket(session, vsro188.BuildCommunityMemoSendResult(0))
}

func (h *Handler) communityMemoDelete(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	slot, err := vsro188.DecodeCommunityMemoSlotRequest(&packet)
	if err == nil {
		err = h.memo.DeleteCommunityMemo(ctx, actor, slot)
	}
	if err != nil {
		return h.sendPacket(session, vsro188.BuildCommunityMemoDeleteResult(0, socialErrorCode(err)))
	}
	return h.sendPacket(session, vsro188.BuildCommunityMemoDeleteResult(slot, 0))
}

func (h *Handler) communityMemoList(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if err = vsro188.DecodeCommunityMemoListRequest(&packet); err != nil {
		response, _ := vsro188.BuildCommunityMemoList(nil, socialErrorCode(model.ErrSocialInvalidRequest))
		return h.sendPacket(session, response)
	}
	memos, err := h.memo.CommunityMemos(ctx, actor)
	if err != nil {
		response, _ := vsro188.BuildCommunityMemoList(nil, socialErrorCode(err))
		return h.sendPacket(session, response)
	}
	entries := make([]vsro188.CommunityMemoListEntry, len(memos))
	for i, memo := range memos {
		entries[i] = communityMemoEntry(memo)
	}
	response, err := vsro188.BuildCommunityMemoList(entries, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *Handler) communityMemoGroupSend(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := vsro188.DecodeCommunityMemoGroupSendRequest(&packet)
	if err != nil {
		response, _ := vsro188.BuildCommunityMemoGroupResult(nil, socialErrorCode(model.ErrSocialInvalidRequest))
		return h.sendPacket(session, response)
	}
	results := make([]vsro188.CommunityMemoGroupResult, len(request.Recipients))
	now := time.Now()
	for i, recipient := range request.Recipients {
		sendErr := h.sendCommunityMemo(ctx, actor, recipient, request.Message, now)
		results[i] = vsro188.CommunityMemoGroupResult{Recipient: recipient, Code: socialErrorCode(sendErr)}
	}
	response, err := vsro188.BuildCommunityMemoGroupResult(results, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *Handler) guildElectionOpen(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	electionID, err := vsro188.DecodeGuildElectionParticipateRequest(&packet)
	if err != nil {
		result, _ := vsro188.BuildGuildElectionOpenResult(0, 0, nil, socialErrorCode(model.ErrSocialInvalidRequest))
		return h.sendPacket(session, result)
	}
	state, err := h.election.GuildElection(ctx, actor, electionID, time.Now())
	if err != nil {
		result, _ := vsro188.BuildGuildElectionOpenResult(0, 0, nil, socialErrorCode(err))
		return h.sendPacket(session, result)
	}
	candidates := make([]vsro188.GuildElectionCandidate, len(state.Candidates))
	for i, candidate := range state.Candidates {
		candidates[i] = vsro188.GuildElectionCandidate{CharacterID: candidate.CharacterID, Votes: candidate.Votes}
	}
	result, err := vsro188.BuildGuildElectionOpenResult(state.ID, state.SelectedCandidateID, candidates, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, result)
}

func (h *Handler) guildElectionVote(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	vote, err := vsro188.DecodeGuildElectionVoteRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildElectionVoteResult(socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	state, err := h.election.VoteGuildElection(ctx, actor, vote.ElectionID, vote.CandidateID, vote.Selection, time.Now())
	if err != nil {
		return h.sendPacket(session, vsro188.BuildGuildElectionVoteResult(socialErrorCode(err)))
	}
	if err = h.sendPacket(session, vsro188.BuildGuildElectionVoteResult(0)); err != nil {
		return err
	}
	guild, err := h.repo.Guild(ctx, actor)
	if err != nil {
		return err
	}
	for _, candidate := range state.Candidates {
		if err = h.publishGuildUpdate(guild, vsro188.BuildGuildElectionCandidateState(candidate.CharacterID, 1, candidate.Votes, 0)); err != nil {
			return err
		}
	}
	if state.WinnerID != 0 {
		if err = h.publishGuildUpdate(guild, vsro188.BuildGuildElectionWinner(state.ID, uint32(state.WinnerID))); err != nil {
			return err
		}
		return h.publishGuildSnapshot(guild)
	}
	return nil
}

func (h *Handler) ExpireGuildElections(ctx context.Context, now time.Time) (int, error) {
	states, err := h.election.ExpireGuildElections(ctx, now, 100)
	if err != nil {
		return 0, err
	}
	for _, state := range states {
		guild, loadErr := h.repo.Guild(ctx, state.PreviousMasterID)
		if loadErr != nil {
			return 0, loadErr
		}
		if sendErr := h.publishGuildUpdate(guild, vsro188.BuildGuildElectionWinner(state.ID, 0)); sendErr != nil {
			return 0, sendErr
		}
	}
	return len(states), nil
}

func (h *Handler) guildKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.kick"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild kick request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	before, _ := h.repo.Guild(ctx, actor)
	err = h.repo.KickGuildMember(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildKickResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildRemovalFor(before, int(target), vsro188.GuildMemberKicked)
	}
	return nil
}

func (h *Handler) guildNotice(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.notice.update"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil {
		return err
	}
	body, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild notice request")
	}
	err = h.repo.UpdateGuildNotice(ctx, actor, title, body)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildNoticeResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			return h.publishGuildUpdate(guild, vsro188.BuildGuildNoticeUpdate(guild.NoticeTitle, guild.NoticeBody))
		}
	}
	return nil
}

func (h *Handler) guildPermissions(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.permissions.update"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > 50 {
		return fmt.Errorf("invalid guild permission count")
	}
	updates := make(map[int]uint32, count)
	for i := byte(0); i < count; i++ {
		id, readErr := packet.ReadUInt32()
		if readErr != nil {
			return readErr
		}
		permissions, readErr := packet.ReadUInt32()
		if readErr != nil {
			return readErr
		}
		updates[int(id)] = permissions
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("guild permission trailing data")
	}
	err = h.repo.UpdateGuildPermissions(ctx, actor, updates)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildPermissionResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			ids := make([]int, 0, len(updates))
			for characterID := range updates {
				ids = append(ids, characterID)
			}
			sort.Ints(ids)
			permissionUpdates := make([]vsro188.GuildPermissionUpdate, 0, len(ids))
			for _, characterID := range ids {
				permissionUpdates = append(permissionUpdates, vsro188.GuildPermissionUpdate{CharacterID: uint32(characterID), Permissions: updates[characterID]})
			}
			return h.publishGuildUpdate(guild, vsro188.BuildGuildPermissionUpdate(permissionUpdates))
		}
	}
	return nil
}

func (h *Handler) guildDonate(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.donate"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild donation")
	}
	amount, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	total, err := h.repo.DonateGuildSP(ctx, actor, amount)
	response := operationPacket(opcode.GuildDonateResponse, err)
	if err == nil {
		response.WriteUInt32(amount)
		response.WriteUInt32(total)
	}
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			if publishErr := h.publishGuildUpdate(guild, vsro188.BuildGuildGPUpdate(total)); publishErr != nil {
				return publishErr
			}
			for _, member := range guild.Members {
				if member.CharacterID == actor {
					return h.publishGuildUpdate(guild, vsro188.BuildGuildMemberGPUpdate(uint32(actor), member.Donation))
				}
			}
		}
	}
	return nil
}

func (h *Handler) guildPromote(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.promote"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 4 {
		_, err = packet.ReadUInt32()
	}
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild promote request")
	}
	guild, gold, err := h.repo.PromoteGuild(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildPromoteResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		h.commitGold(actor, gold)
		return h.publishGuildUpdate(guild, vsro188.BuildGuildPromoteUpdate(guild.Level, guild.GatheredSP))
	}
	return nil
}

func (h *Handler) guildTransfer(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.transfer"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 8 {
		if _, err = packet.ReadUInt32(); err != nil {
			return err
		}
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild transfer request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	guild, err := h.repo.TransferGuild(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildTransferResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildUpdate(guild, vsro188.BuildGuildLeaderTransferUpdate(uint32(actor), target))
	}
	return nil
}

func (h *Handler) guildNickname(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.nickname.update"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	nickname, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild nickname request")
	}
	err = h.repo.UpdateGuildNickname(ctx, actor, int(target), nickname)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildNicknameResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			for _, member := range guild.Members {
				if member.CharacterID == int(target) {
					return h.publishGuildNicknameUpdate(guild, member)
				}
			}
		}
	}
	return nil
}

func (h *Handler) guildUnionInvite(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.union.invite"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild union invite")
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	target, err := h.world.GetPlayerByUniqueId(uid)
	if err != nil || target == nil {
		return h.sendPacket(session, operationPacket(opcode.GuildUnionInviteResponse, model.ErrSocialNotFound))
	}
	err = h.repo.InviteGuildUnion(ctx, actor, target.ID)
	if err == nil {
		invite := network.EmptyPacket()
		invite.MessageID = opcode.PlayerInvitationRequest
		invite.WriteByte(6)
		invite.WriteUInt32(session.UserContext.UniqueID)
		if sendErr := h.sendPacket(target.Session, invite); sendErr != nil {
			return sendErr
		}
	}
	return h.sendPacket(session, operationPacket(opcode.GuildUnionInviteResponse, err))
}

func (h *Handler) guildUnionLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.union.leave"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild union leave")
	}
	recipients, _ := h.repo.SocialRecipients(ctx, actor, 11)
	err = h.repo.LeaveGuildUnion(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildUnionLeaveResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishUnionClear(recipients)
	}
	return nil
}

func (h *Handler) guildUnionKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.union.kick"})
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild union kick")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	recipients, _ := h.repo.SocialRecipients(ctx, actor, 11)
	err = h.repo.KickGuildUnion(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildUnionKickResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		_ = h.publishUnionClear(recipients)
		if union, loadErr := h.repo.GuildUnion(ctx, actor); loadErr == nil {
			return h.publishUnion(ctx, actor, union)
		}
	}
	return nil
}

func writeUnion(packet *network.Packet, union model.GuildUnionSnapshot) {
	packet.WriteUInt32(uint32(union.ID))
	packet.WriteUInt32(uint32(union.LeaderGuild))
	packet.WriteByte(byte(len(union.Guilds)))
	for _, guild := range union.Guilds {
		packet.WriteUInt32(uint32(guild.GuildID))
		packet.WriteString(guild.Name)
		packet.WriteByte(guild.Level)
		packet.WriteString(guild.MasterName)
		packet.WriteByte(guild.Members)
	}
}

func (h *Handler) publishUnion(ctx context.Context, actor int, union model.GuildUnionSnapshot) error {
	ids, err := h.repo.SocialRecipients(ctx, actor, 11)
	if err != nil {
		return err
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildUnionInfo
	writeUnion(&p, union)
	for _, id := range ids {
		player, lookupErr := h.world.GetPlayerByCharacterID(id)
		if lookupErr == nil && player != nil && player.Session != nil {
			if sendErr := h.sendPacket(player.Session, p); sendErr != nil {
				return sendErr
			}
		}
	}
	return nil
}

func (h *Handler) publishUnionClear(ids []int) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildUnionInfo
	p.WriteUInt32(0)
	for _, id := range ids {
		player, err := h.world.GetPlayerByCharacterID(id)
		if err == nil && player != nil && player.Session != nil {
			if sendErr := h.sendPacket(player.Session, p); sendErr != nil {
				return sendErr
			}
		}
	}
	return nil
}

func writeGuild(packet *network.Packet, guild model.GuildSnapshot) {
	packet.WriteUInt32(uint32(guild.ID))
	packet.WriteString(guild.Name)
	packet.WriteByte(guild.Level)
	packet.WriteUInt32(guild.GatheredSP)
	packet.WriteString(guild.NoticeTitle)
	packet.WriteString(guild.NoticeBody)
	packet.WriteUInt32(guild.CrestRevision)
	packet.WriteByte(guild.MercenaryAttr)
	packet.WriteByte(byte(len(guild.Members)))
	for _, m := range guild.Members {
		packet.WriteUInt32(uint32(m.CharacterID))
		packet.WriteString(m.Name)
		packet.WriteByte(m.Class)
		packet.WriteByte(m.Level)
		packet.WriteUInt32(m.Donation)
		packet.WriteUInt32(m.Permissions)
		packet.WriteUInt32(m.Contribution)
		packet.WriteUInt32(0)
		packet.WriteUInt32(0)
		packet.WriteString(m.Nickname)
		packet.WriteUInt32(m.RefObjectID)
		packet.WriteByte(m.SiegeAuthority)
		if m.Online {
			packet.WriteByte(0)
		} else {
			packet.WriteByte(1)
		}
	}
}

func guildMemberUpdate(member model.GuildMember) vsro188.GuildMemberUpdate {
	return vsro188.GuildMemberUpdate{
		CharacterID:    uint32(member.CharacterID),
		Name:           member.Name,
		Class:          member.Class,
		Level:          member.Level,
		Donation:       member.Donation,
		Permissions:    member.Permissions,
		Contribution:   member.Contribution,
		Nickname:       member.Nickname,
		RefObjectID:    member.RefObjectID,
		SiegeAuthority: member.SiegeAuthority,
		Online:         member.Online,
	}
}

func (h *Handler) publishGuildSnapshot(guild model.GuildSnapshot) error {
	for _, m := range guild.Members {
		if err := h.sendGuildSnapshot(m.CharacterID, guild); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) sendGuildSnapshot(characterID int, guild model.GuildSnapshot) error {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil || player.Session == nil {
		return nil
	}
	begin := network.EmptyPacket()
	begin.MessageID = opcode.GuildInfoBegin
	if err = h.sendPacket(player.Session, begin); err != nil {
		return err
	}
	data := network.EmptyPacket()
	data.MessageID = opcode.GuildInfoData
	writeGuild(&data, guild)
	if err = h.sendPacket(player.Session, data); err != nil {
		return err
	}
	end := network.EmptyPacket()
	end.MessageID = opcode.GuildInfoEnd
	return h.sendPacket(player.Session, end)
}

func (h *Handler) publishGuildUpdate(guild model.GuildSnapshot, packet network.Packet) error {
	return h.publishGuildUpdateExcept(guild, packet, 0)
}

func (h *Handler) publishGuildUpdateExcept(guild model.GuildSnapshot, packet network.Packet, excludedCharacterID int) error {
	for _, member := range guild.Members {
		if member.CharacterID == excludedCharacterID {
			continue
		}
		player, err := h.world.GetPlayerByCharacterID(member.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			continue
		}
		if err = h.sendPacket(player.Session, packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) publishGuildEntityUpdate(characterID int, guild model.GuildSnapshot) error {
	for _, member := range guild.Members {
		if member.CharacterID != characterID {
			continue
		}
		player, err := h.world.GetPlayerByCharacterID(characterID)
		if err != nil || player == nil {
			return nil
		}
		packet := vsro188.BuildGuildEntityUpdate(player.GetUniqueID(), uint32(guild.ID), guild.Name, member.Nickname)
		return h.publishVisiblePlayerPacket(player, packet)
	}
	return nil
}

func (h *Handler) publishGuildNicknameUpdate(guild model.GuildSnapshot, member model.GuildMember) error {
	packet := vsro188.BuildGuildEntityNicknameUpdate(uint32(guild.ID), member.Name, member.Nickname)
	recipients := make(map[*server.Session]struct{})
	for _, guildMember := range guild.Members {
		player, err := h.world.GetPlayerByCharacterID(guildMember.CharacterID)
		if err == nil && player != nil && player.Session != nil {
			recipients[player.Session] = struct{}{}
		}
	}
	if player, err := h.world.GetPlayerByCharacterID(member.CharacterID); err == nil && player != nil {
		collectVisiblePlayerSessions(player, recipients)
	}
	for session := range recipients {
		if err := h.sendPacket(session, packet); err != nil {
			return err
		}
	}
	return nil
}

func collectVisiblePlayerSessions(player *model.Player, recipients map[*server.Session]struct{}) {
	if player == nil {
		return
	}
	if player.Session != nil {
		recipients[player.Session] = struct{}{}
	}
	known := player.GetCharKnownObjectList()
	if known == nil {
		return
	}
	for _, object := range known.GetKnownObjects() {
		observer, ok := object.(model.IPlayer)
		if !ok || observer.GetSession() == nil {
			continue
		}
		observerKnown := observer.GetCharKnownObjectList()
		if observerKnown != nil && observerKnown.ClientKnows(player.GetUniqueID()) {
			recipients[observer.GetSession()] = struct{}{}
		}
	}
}

func (h *Handler) publishVisiblePlayerPacket(player *model.Player, packet network.Packet) error {
	recipients := make(map[*server.Session]struct{})
	collectVisiblePlayerSessions(player, recipients)
	for session := range recipients {
		if err := h.sendPacket(session, packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) publishGuildRemoval(guild model.GuildSnapshot) error {
	for _, m := range guild.Members {
		if err := h.sendGuildRemove(m.CharacterID); err != nil {
			return err
		}
	}
	return nil
}
func (h *Handler) publishGuildRemovalFor(guild model.GuildSnapshot, characterID int, reason vsro188.GuildMemberLeaveReason) error {
	if err := h.sendGuildRemove(characterID); err != nil {
		return err
	}
	return h.publishGuildUpdateExcept(guild, vsro188.BuildGuildMemberLeaveUpdate(uint32(characterID), reason), characterID)
}
func (h *Handler) sendGuildRemove(characterID int) error {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return nil
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildEntityRemove
	p.WriteUInt32(player.UniqueID)
	return h.publishVisiblePlayerPacket(player, p)
}

func (h *Handler) academyCreate(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 4 {
		_, err = packet.ReadUInt32()
	}
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy create request")
	}
	academy, gold, err := h.repo.CreateAcademy(ctx, actor)
	response := operationPacket(opcode.AcademyCreateResponse, err)
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		h.commitGold(actor, gold)
		return h.publishAcademy(academy)
	}
	return nil
}
func (h *Handler) academyDisband(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy disband request")
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.DisbandAcademy(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyDisbandResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemoval(before)
	}
	return nil
}
func (h *Handler) academyLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy leave request")
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.LeaveAcademy(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyLeaveResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemovalFor(before, actor)
	}
	return nil
}
func (h *Handler) academyKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid academy kick request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.KickAcademyMember(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyKickResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemovalFor(before, int(target))
	}
	return nil
}
func (h *Handler) academyComment(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil {
		return err
	}
	body, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy comment")
	}
	err = h.repo.UpdateAcademyComment(ctx, actor, title, body)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyCommentResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		academy, loadErr := h.repo.Academy(ctx, actor)
		if loadErr == nil {
			return h.publishAcademy(academy)
		}
	}
	return nil
}

func (h *Handler) academyGrade(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid academy grade request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	before, _ := h.repo.Academy(ctx, actor)
	commandKey := fmt.Sprintf("academy:graduate:%s:%02x:%d", session.ID, packet.Sequence, target)
	_, err = h.repo.GraduateAcademyMember(ctx, actor, int(target), commandKey)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyGradeResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemovalFor(before, int(target))
	}
	return nil
}

func (h *Handler) academyHonorRank(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := socialCharacter(session); err != nil {
		return err
	}
	switch packet.Remaining() {
	case 0:
		// Retain the empty legacy request accepted by older clients.
	case 1:
		if _, err := packet.ReadByte(); err != nil {
			return err
		}
	case 4:
		// The stock vSRO 1.188 client sends the selected academy NPC's runtime
		// UID. SR_GameServer's 0x00519AD0 handler consumes and validates this
		// four-byte request value before producing the honor ranking response.
		npcUID, err := packet.ReadUInt32()
		if err != nil {
			return err
		}
		if session.UserContext.SelectedNPCUniqueID != 0 && npcUID != session.UserContext.SelectedNPCUniqueID {
			return h.sendPacket(session, operationPacket(opcode.AcademyHonorRankResponse, model.ErrSocialInvalidRequest))
		}
	default:
		return fmt.Errorf("invalid academy honor rank request")
	}
	ranking, err := h.repo.AcademyHonorRanking(ctx, 20)
	response := operationPacket(opcode.AcademyHonorRankResponse, err)
	if err == nil {
		response.WriteByte(byte(len(ranking)))
		for i, m := range ranking {
			response.WriteByte(byte(i + 1))
			response.WriteString(m.LeaderName)
			response.WriteUInt32(m.GraduatedCount)
			response.WriteByte(m.HonorRank)
		}
	}
	return h.sendPacket(session, response)
}
func (h *Handler) academyMatchingRegister(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	country, err := packet.ReadByte()
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil {
		return err
	}
	display, err := packet.ReadByte()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy matching registration")
	}
	err = h.repo.RegisterAcademyMatch(ctx, actor, title, country, display)
	responseID := opcode.AcademyMatchingRegisterResponse
	if packet.MessageID == opcode.AcademyMatchingChangeRequest {
		responseID = opcode.AcademyMatchingChangeResponse
	}
	return h.sendPacket(session, operationPacket(responseID, err))
}
func (h *Handler) academyMatchingDelete(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy matching delete")
	}
	err = h.repo.DeleteAcademyMatch(ctx, actor)
	return h.sendPacket(session, operationPacket(opcode.AcademyMatchingDeleteResponse, err))
}
func (h *Handler) academyMatchingList(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := socialCharacter(session); err != nil {
		return err
	}
	if packet.Remaining() != 1 {
		return fmt.Errorf("invalid academy matching list")
	}
	page, err := packet.ReadByte()
	if err != nil {
		return err
	}
	matches, pages, err := h.repo.ListAcademyMatches(ctx, page, academyPageSize)
	response := buildAcademyMatchingListResponse(matches, pages, page, err, h.world)
	return h.sendPacket(session, response)
}

// buildAcademyMatchingListResponse follows the recovered 0xB47D entry shape.
// LeaderCharacter is a durable CHAR.id; the wire field is the live runtime
// UID and is therefore resolved only at serialization time.
func buildAcademyMatchingListResponse(matches []model.AcademyMatch, pages int, page byte, err error, world worldLookup) network.Packet {
	response := operationPacket(opcode.AcademyMatchingListResponse, err)
	if err != nil {
		return response
	}
	response.WriteByte(byte(pages))
	response.WriteByte(page)
	response.WriteByte(byte(len(matches)))
	for _, m := range matches {
		response.WriteUInt32(uint32(m.AcademyID))
		response.WriteUInt32(0)
		response.WriteByte(m.CountryType)
		response.WriteString(m.Title)
		response.WriteUInt32(0)
		response.WriteUInt32(0)
		response.WriteByte(m.DisplayLevel)
		response.WriteByte(m.LeaderLevel)
		leaderUID := uint32(0)
		if world != nil && m.LeaderCharacter > 0 {
			if leader, lookupErr := world.GetPlayerByCharacterID(m.LeaderCharacter); lookupErr == nil && leader != nil {
				leaderUID = leader.GetUniqueID()
			}
		}
		response.WriteUInt32(leaderUID)
		response.WriteString(m.LeaderName)
		response.WriteUInt32(m.GraduatedCount)
		response.WriteByte(m.HonorRank)
		response.WriteUInt64(0)
	}
	return response
}
func (h *Handler) academyMatchingJoin(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid academy matching join")
	}
	academyID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	academy, err := h.repo.JoinAcademy(ctx, actor, int(academyID))
	response := operationPacket(opcode.AcademyMatchingJoinResponse, err)
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademy(academy)
	}
	return nil
}
func (h *Handler) publishAcademy(a model.AcademySnapshot) error {
	packet := buildAcademyInfoPacket(a, h.world)
	for _, member := range a.Members {
		player, err := h.world.GetPlayerByCharacterID(member.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			continue
		}
		if err = h.sendPacket(player.Session, packet); err != nil {
			return err
		}
		buffs, loadErr := h.repo.AcademyBuffs(context.Background(), member.CharacterID, time.Now().UTC())
		if loadErr != nil {
			return loadErr
		}
		for _, buff := range buffs {
			if err = h.sendPacket(player.Session, buildAcademyBuffPacket(buff, time.Now().UTC())); err != nil {
				return err
			}
		}
	}
	return nil
}

// PublishAcademyBuffs sends committed mentor/apprentice accumulation state to
// whichever affected characters are online. Offline state remains durable.
func (h *Handler) PublishAcademyBuffs(states []model.AcademyBuffState) {
	now := time.Now().UTC()
	for _, state := range states {
		player, err := h.world.GetPlayerByCharacterID(state.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			continue
		}
		_ = h.sendPacket(player.Session, buildAcademyBuffPacket(state, now))
	}
}

func buildAcademyInfoPacket(academy model.AcademySnapshot, world worldLookup) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AcademyInfo
	packet.WriteUInt32(uint32(academy.ID))
	packet.WriteUInt32(uint32(academy.LeaderCharacter))
	packet.WriteUInt32(academy.GraduatedCount)
	packet.WriteUInt32(academy.HonorPoints)
	packet.WriteUInt32(uint32(academy.HonorRank))
	packet.WriteByte(academy.HonorRank)
	packet.WriteString(academy.CommentTitle)
	packet.WriteString(academy.CommentBody)
	members := academy.Members
	if len(members) > 8 {
		members = members[:8]
	}
	packet.WriteByte(byte(len(members)))
	for _, member := range members {
		runtimeUID := uint32(0)
		if world != nil {
			if player, err := world.GetPlayerByCharacterID(member.CharacterID); err == nil && player != nil {
				runtimeUID = player.GetUniqueID()
			}
		}
		packet.WriteUInt32(uint32(member.CharacterID))
		packet.WriteUInt32(member.RefObjectID)
		packet.WriteUInt32(runtimeUID)
		packet.WriteString(member.Name)
		packet.WriteByte(member.Class)
		packet.WriteByte(member.Level)
		for range 4 {
			packet.WriteUInt32(0)
		}
		if member.Online {
			packet.WriteByte(1)
		} else {
			packet.WriteByte(0)
		}
		packet.WriteByte(0)
		packet.WriteUInt32(uint32(min(member.AccumulatedExperience, math.MaxUint32)))
		packet.WriteByte(0)
		packet.WriteUInt32(uint32(member.MentorID))
		for range 4 {
			packet.WriteUInt16(0)
		}
		packet.WriteString(member.MentorName)
	}
	return packet
}

func buildAcademyBuffPacket(buff model.AcademyBuffState, now time.Time) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AcademyBuffUpdate
	packet.WriteByte(buff.Type)
	remaining := uint32(0)
	if buff.EndsAt.After(now) {
		minutes := (buff.EndsAt.Sub(now) + time.Minute - 1) / time.Minute
		remaining = uint32(min(minutes, time.Duration(math.MaxUint32)))
	}
	packet.WriteUInt32(remaining)
	packet.WriteUInt32(uint32(min(buff.Value, uint64(math.MaxUint32))))
	return packet
}
func (h *Handler) publishAcademyRemoval(a model.AcademySnapshot) error {
	_ = a
	return nil
}
func (h *Handler) publishAcademyRemovalFor(a model.AcademySnapshot, id int) error {
	_, _ = a, id
	return nil
}

func (h *Handler) jobJoin(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := (vsro188.JobJoinRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	job, gold, err := h.repo.JoinJob(ctx, actor, request.JobType, request.NPCUID)
	response := operationPacket(opcode.JobJoinResponse, err)
	if err == nil {
		if encodeErr := (vsro188.JobJoinResponseCodec{}).AppendSuccess(&response, jobProtocolState(job)); encodeErr != nil {
			return encodeErr
		}
	}
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		h.commitGold(actor, gold)
		h.commitJob(actor, job)
	}
	return nil
}

func (h *Handler) jobLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := (vsro188.JobLeaveRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	gold, err := h.repo.LeaveJob(ctx, actor, request.EnrollmentFee)
	if err == nil {
		h.commitGold(actor, gold)
		h.clearJob(actor)
	}
	return h.sendPacket(session, operationPacket(opcode.JobLeaveResponse, err))
}

func (h *Handler) jobAlias(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := (vsro188.JobAliasRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	response := network.EmptyPacket()
	response.MessageID = opcode.JobAliasResponse
	switch request.Action {
	case 0:
		available, checkErr := h.repo.JobAliasAvailable(ctx, request.Alias)
		if checkErr == nil && !available {
			checkErr = model.ErrSocialConflict
		}
		response = operationPacket(opcode.JobAliasResponse, checkErr)
		if checkErr == nil {
			if encodeErr := (vsro188.JobAliasResponseCodec{}).AppendSuccess(&response, request.Action, request.Alias); encodeErr != nil {
				return encodeErr
			}
		} else if encodeErr := (vsro188.JobAliasResponseCodec{}).AppendFailure(&response, request.Action, request.Alias); encodeErr != nil {
			return encodeErr
		}
	case 1:
		err = h.repo.SetJobAlias(ctx, actor, request.Alias)
		response = operationPacket(opcode.JobAliasResponse, err)
		if err == nil {
			h.commitJobAlias(actor, request.Alias)
			if encodeErr := (vsro188.JobAliasResponseCodec{}).AppendSuccess(&response, request.Action, request.Alias); encodeErr != nil {
				return encodeErr
			}
		} else if encodeErr := (vsro188.JobAliasResponseCodec{}).AppendFailure(&response, request.Action, request.Alias); encodeErr != nil {
			return encodeErr
		}
	}
	return h.sendPacket(session, response)
}

func (h *Handler) jobRanking(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := socialCharacter(session); err != nil {
		return err
	}
	request, err := (vsro188.JobRankingRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	ranking, err := h.repo.JobRanking(ctx, request.JobType, request.Kind == 1, 20)
	response := operationPacket(opcode.JobRankingResponse, err)
	if err == nil {
		entries := make([]vsro188.JobRankingEntry, 0, len(ranking))
		for i, job := range ranking {
			entries = append(entries, vsro188.JobRankingEntry{Rank: byte(i + 1), Alias: job.Alias, Level: job.Level, Experience: uint32(min(job.Experience, math.MaxUint32))})
		}
		if encodeErr := (vsro188.JobRankingResponseCodec{}).AppendSuccess(&response, request, entries); encodeErr != nil {
			return encodeErr
		}
	}
	return h.sendPacket(session, response)
}

func (h *Handler) jobOutcome(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	request, err := (vsro188.JobOutcomeRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	amount, gold, err := h.repo.ClaimJobReward(ctx, actor)
	response := operationPacket(opcode.JobOutcomeResponse, err)
	if err == nil {
		if encodeErr := (vsro188.JobOutcomeResponseCodec{}).AppendSuccess(&response, request.Action, amount); encodeErr != nil {
			return encodeErr
		}
		h.commitGold(actor, gold)
	} else if encodeErr := (vsro188.JobOutcomeResponseCodec{}).AppendFailure(&response, request.Action); encodeErr != nil {
		return encodeErr
	}
	return h.sendPacket(session, response)
}

func (h *Handler) jobPrevious(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if _, err = (vsro188.JobPreviousInfoRequestCodec{}).Decode(packet); err != nil {
		return err
	}
	job, err := h.repo.JobPreviousInfo(ctx, actor)
	response := operationPacket(opcode.JobPreviousInfoResponse, err)
	if err == nil {
		if encodeErr := (vsro188.JobPreviousInfoResponseCodec{}).AppendSuccess(&response, vsro188.JobPreviousInfo{
			Current:              jobProtocolState(job),
			PreviousType:         job.PreviousType,
			PreviousExperience:   uint32(min(job.PreviousExperience, math.MaxUint32)),
			PreviousContribution: uint32(min(job.PreviousContribution, math.MaxUint32)),
		}); encodeErr != nil {
			return encodeErr
		}
	}
	return h.sendPacket(session, response)
}

func jobProtocolState(job model.JobSnapshot) vsro188.JobState {
	return vsro188.JobState{Type: job.Type, Level: job.Level, Experience: uint32(min(job.Experience, math.MaxUint32))}
}
func (h *Handler) commitGold(characterID int, gold uint64) {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return
	}
	player.Mutex.Lock()
	player.Gold = gold
	player.Mutex.Unlock()
}

func (h *Handler) commitJob(characterID int, job model.JobSnapshot) {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return
	}
	player.SetJob(model.PlayerJob{Type: job.Type, Level: job.Level, Experience: job.Experience, Alias: job.Alias})
}

func (h *Handler) clearJob(characterID int) {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return
	}
	player.SetJob(model.PlayerJob{})
}

func (h *Handler) commitJobAlias(characterID int, alias string) {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return
	}
	job := player.GetJob()
	if !job.Active() {
		return
	}
	job.Alias = alias
	player.SetJob(job)
}

// PreparePlayer loads social state that is part of the player spawn contract.
// GameReady calls it while the player is still staged and invisible.
func (h *Handler) PreparePlayer(ctx context.Context, player *model.Player) error {
	if player == nil || player.ID <= 0 {
		return model.ErrSocialInvalidRequest
	}
	job, err := h.repo.Job(ctx, player.ID)
	if errors.Is(err, model.ErrSocialNotFound) {
		player.SetJob(model.PlayerJob{})
		return nil
	}
	if err != nil {
		return err
	}
	player.SetJob(model.PlayerJob{Type: job.Type, Level: job.Level, Experience: job.Experience, Alias: job.Alias})
	return nil
}

// Bootstrap sends the persistent social snapshots after the stock GameReady
// transition, when ordinary in-world packets are legal on the session.
func (h *Handler) Bootstrap(ctx context.Context, session *server.Session) error {
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if h.community != nil {
		friends, loadErr := h.community.CommunityFriends(ctx, characterID)
		if loadErr != nil {
			return loadErr
		}
		records := make([]vsro188.CommunityFriendRecord, len(friends))
		for i, friend := range friends {
			online := h.communityFriendOnline(friend.CharacterID)
			records[i] = communityFriendRecord(friend, online)
			if online {
				player, _ := h.world.GetPlayerByCharacterID(int(friend.CharacterID))
				if player != nil && player.Session != nil {
					if sendErr := h.sendPacket(player.Session, vsro188.BuildCommunityFriendState(uint32(characterID), true)); sendErr != nil {
						return sendErr
					}
				}
			}
		}
		info, encodeErr := vsro188.BuildCommunityFriendInfo(records)
		if encodeErr != nil {
			return encodeErr
		}
		if err = h.sendPacket(session, info); err != nil {
			return err
		}
		pending, loadErr := h.community.PendingCommunityFriendRequests(ctx, characterID)
		if loadErr != nil {
			return loadErr
		}
		for _, request := range pending {
			invitation, encodeErr := vsro188.BuildCommunityFriendInvitation(request.Requester.CharacterID, request.Target.CharacterID, request.Requester.Name)
			if encodeErr != nil {
				return encodeErr
			}
			if err = h.sendPacket(session, invitation); err != nil {
				return err
			}
		}
	}
	if h.war != nil {
		if war, loadErr := h.war.ActiveGuildWar(ctx, characterID); loadErr == nil {
			packet, encodeErr := guildWarInfoPacket(war)
			if encodeErr != nil {
				return encodeErr
			}
			if err = h.sendPacket(session, packet); err != nil {
				return err
			}
		} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
			return loadErr
		}
	}
	if h.election != nil {
		now := time.Now()
		if touchErr := h.election.TouchGuildMemberOnline(ctx, characterID, now); touchErr != nil {
			return touchErr
		}
		if election, loadErr := h.election.ActiveGuildElection(ctx, characterID, now); loadErr == nil {
			if err = h.sendPacket(session, vsro188.BuildGuildElectionOpened(election.ID, 1, uint32(election.EndsAt.Unix()))); err != nil {
				return err
			}
		} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
			return loadErr
		}
	}
	if guild, loadErr := h.repo.Guild(ctx, characterID); loadErr == nil {
		if err = h.publishGuildSnapshot(guild); err != nil {
			return err
		}
		if err = h.publishGuildUpdateExcept(guild, vsro188.BuildGuildMemberStateUpdate(uint32(characterID), true), characterID); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	if union, loadErr := h.repo.GuildUnion(ctx, characterID); loadErr == nil {
		if err = h.publishUnion(ctx, characterID, union); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	if academy, loadErr := h.repo.Academy(ctx, characterID); loadErr == nil {
		if err = h.publishAcademy(academy); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	if player, loadErr := h.world.GetPlayerByCharacterID(characterID); loadErr == nil && player != nil && player.GetJob().Active() {
		job := player.GetJob()
		// GameReady's 0x3013 body already initializes the local player's job
		// type and level. Replaying 0x30E6 during bootstrap races that object
		// initialization; the stock client asserts at 0x00884E67 when the two
		// types differ. Reserve 0x30E6 for later progression changes.
		if job.Type == model.PlayerJobTrader && h.safeTrade != nil {
			state, stateErr := h.safeTrade.State(ctx, characterID)
			if stateErr != nil {
				return stateErr
			}
			restore, encodeErr := (vsro188.JobSafeTradeCodec{}).Encode(safeTradeRestoreUpdate(state, h.safeTradeLimit))
			if encodeErr != nil {
				return encodeErr
			}
			return h.sendPacket(session, restore)
		}
	}
	return nil
}
