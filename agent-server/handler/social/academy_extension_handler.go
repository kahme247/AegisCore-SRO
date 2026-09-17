package social

import (
	"context"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type academyInvitation struct {
	academyID int
	leaderID  int
	targetID  int
}

func (h *Handler) inviteAcademyPlayer(ctx context.Context, session *server.Session, target *model.Player, responseOpcode uint16) error {
	actor, err := socialCharacter(session)
	if err != nil || target == nil || target.Session == nil || target.ID <= 0 || target.ID == actor {
		return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(responseOpcode, socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	academy, err := h.repo.Academy(ctx, actor)
	if err != nil || academy.LeaderCharacter != actor {
		return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(responseOpcode, socialErrorCode(model.ErrSocialAccessDenied)))
	}
	if _, targetErr := h.repo.Academy(ctx, target.ID); targetErr == nil {
		return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(responseOpcode, socialErrorCode(model.ErrSocialConflict)))
	}
	invitation := academyInvitation{academyID: academy.ID, leaderID: actor, targetID: target.ID}
	h.academyMu.Lock()
	h.academyInvites[target.GetUniqueID()] = invitation
	h.academyMu.Unlock()
	if err = h.sendPacket(target.Session, vsro188.BuildAcademyInvitation(uint32(academy.ID), uint32(actor))); err != nil {
		h.academyMu.Lock()
		delete(h.academyInvites, target.GetUniqueID())
		h.academyMu.Unlock()
		return err
	}
	return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(responseOpcode, 0))
}

func (h *Handler) academyInvite(ctx context.Context, session *server.Session, packet network.Packet) error {
	targetUID, err := vsro188.DecodeAcademyInviteRequest(&packet)
	if err != nil || h.world == nil {
		return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(opcode.AcademyInviteResponse, socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	target, err := h.world.GetPlayerByUniqueId(targetUID)
	if err != nil {
		target = nil
	}
	return h.inviteAcademyPlayer(ctx, session, target, opcode.AcademyInviteResponse)
}

func (h *Handler) academyMemberAction(ctx context.Context, session *server.Session, packet network.Packet) error {
	name, err := vsro188.DecodeAcademyMemberActionRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(opcode.AcademyMemberActionResponse, socialErrorCode(model.ErrSocialInvalidRequest)))
	}
	lookup, ok := h.world.(interface {
		GetPlayerByCharName(string) (*model.Player, error)
	})
	if !ok {
		return h.sendPacket(session, vsro188.BuildAcademyExtensionResult(opcode.AcademyMemberActionResponse, socialErrorCode(model.ErrSocialRepositoryUnavailable)))
	}
	target, err := lookup.GetPlayerByCharName(name)
	if err != nil {
		target = nil
	}
	return h.inviteAcademyPlayer(ctx, session, target, opcode.AcademyMemberActionResponse)
}

func (h *Handler) academyInvitationResponse(ctx context.Context, session *server.Session, packet network.Packet) error {
	response, err := vsro188.DecodeAcademyInvitationResponse(&packet)
	actor, actorErr := socialCharacter(session)
	if err != nil || actorErr != nil {
		return err
	}
	h.academyMu.Lock()
	invitation, exists := h.academyInvites[session.UserContext.UniqueID]
	if exists {
		delete(h.academyInvites, session.UserContext.UniqueID)
	}
	h.academyMu.Unlock()
	if !exists || invitation.academyID != int(response.AcademyID) || invitation.leaderID != int(response.LeaderID) || invitation.targetID != actor {
		return model.ErrSocialInvalidRequest
	}
	if !response.Accepted {
		return nil
	}
	academy, err := h.repo.JoinAcademy(ctx, actor, invitation.academyID)
	if err != nil {
		return err
	}
	if err = h.publishAcademy(academy); err != nil {
		return err
	}
	update := vsro188.BuildAcademyUpdate(3, uint32(actor), 1)
	for _, member := range academy.Members {
		player, lookupErr := h.world.GetPlayerByCharacterID(member.CharacterID)
		if lookupErr == nil && player != nil && player.Session != nil {
			if err = h.sendPacket(player.Session, update); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Handler) academyTimedOperation(ctx context.Context, session *server.Session, packet network.Packet) error {
	if err := vsro188.DecodeAcademyUnknownOperationRequest(&packet); err != nil {
		response, _ := vsro188.BuildAcademyTimedOperation(nil, socialErrorCode(model.ErrSocialInvalidRequest))
		return h.sendPacket(session, response)
	}
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	buffs, err := h.repo.AcademyBuffs(ctx, actor, time.Now().UTC())
	if err != nil {
		response, _ := vsro188.BuildAcademyTimedOperation(nil, socialErrorCode(err))
		return h.sendPacket(session, response)
	}
	now := time.Now().UTC()
	updates := make([]vsro188.AcademyTimedUpdate, len(buffs))
	for i, buff := range buffs {
		minutes := uint32(math.Ceil(buff.EndsAt.Sub(now).Minutes()))
		value := buff.Value
		if value > math.MaxUint32 {
			value = math.MaxUint32
		}
		updates[i] = vsro188.AcademyTimedUpdate{DurationMinutes: minutes, Reference: uint32(buff.CharacterID), Value: uint32(value)}
	}
	response, err := vsro188.BuildAcademyTimedOperation(updates, 0)
	if err != nil {
		return err
	}
	if err = h.sendPacket(session, response); err != nil {
		return err
	}
	for _, update := range updates {
		if err = h.sendPacket(session, vsro188.BuildAcademyUnknownUpdate3C86(update.DurationMinutes)); err != nil {
			return err
		}
		if err = h.sendPacket(session, vsro188.BuildAcademyUnknownUpdate3C87(update.DurationMinutes, update.Reference)); err != nil {
			return err
		}
	}
	return nil
}
