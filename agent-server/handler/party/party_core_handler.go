package party

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *PartyHandler) handleCreateInvite(_ context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() != 5 {
		return fmt.Errorf("invalid party-create request")
	}
	targetUID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	settings, err := packet.ReadByte()
	if err != nil || settings > 7 || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party-create settings")
	}
	invite, target, err := h.core().Invite(session.UserContext.UniqueID, targetUID, model.PartySetting(settings), false)
	if err != nil {
		return h.sendPacket(session, buildPartyOperationResponse(opcode.PartyCreateResponse, false))
	}
	return h.sendPacket(target.GetSession(), buildInvitation(invite))
}

func (h *PartyHandler) handleExistingInvite(_ context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() != 4 {
		return fmt.Errorf("invalid party-invite request")
	}
	targetUID, err := packet.ReadUInt32()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party-invite target")
	}
	invite, target, err := h.core().Invite(session.UserContext.UniqueID, targetUID, 0, true)
	if err != nil {
		return h.sendPacket(session, buildPartyOperationResponse(opcode.PartyInviteResponse, false))
	}
	return h.sendPacket(target.GetSession(), buildInvitation(invite))
}

func (h *PartyHandler) handleInvitationAnswer(ctx context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() < 2 {
		return fmt.Errorf("invalid party invitation answer")
	}
	action, err := packet.ReadByte()
	if err != nil {
		return err
	}
	accept := false
	switch action {
	case 1:
		answer, readErr := packet.ReadByte()
		if readErr != nil || answer != 1 || packet.Remaining() != 0 {
			return fmt.Errorf("invalid party invitation acceptance")
		}
		accept = true
	case 2:
		code, readErr := packet.ReadUInt16()
		if readErr != nil || code != 11276 || packet.Remaining() != 0 {
			return fmt.Errorf("invalid party invitation rejection")
		}
	default:
		return fmt.Errorf("unknown party invitation answer %d", action)
	}

	transition, err := h.core().AnswerInvitation(session.UserContext.UniqueID, accept)
	if err != nil {
		if errors.Is(err, service.ErrPartyInvitationMissing) && h.invitationFallback != nil {
			handled, fallbackErr := h.invitationFallback.AnswerGuildInvitation(ctx, session, accept)
			if handled || fallbackErr != nil {
				return fallbackErr
			}
		}
		return err
	}
	responseOpcode := opcode.PartyCreateResponse
	if transition.Invitation.PartyNumber != 0 {
		responseOpcode = opcode.PartyInviteResponse
	}
	if !transition.Accepted {
		return h.sendPacket(transition.Inviter.GetSession(), buildPartyOperationResponse(responseOpcode, false))
	}
	if err = h.sendPacket(transition.Inviter.GetSession(), buildPartyOperationResponse(responseOpcode, true)); err != nil {
		return err
	}
	full := buildPartySnapshot(transition.Party, false)
	if transition.Invitation.PartyNumber == 0 {
		return sendPartyPacket(h, []*model.Player{transition.Inviter, transition.Target}, full)
	}
	if err = sendPartyPacket(h, transition.Existing, buildPartyJoin(transition.Target)); err != nil {
		return err
	}
	return h.sendPacket(transition.Target.GetSession(), full)
}

func (h *PartyHandler) handleLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party-leave request")
	}
	transition, err := h.core().LeaveParty(session.UserContext.UniqueID, 2)
	if err != nil {
		return err
	}
	return publishRemoval(ctx, h, transition)
}

func publishRemoval(ctx context.Context, h *PartyHandler, transition service.PartyTransition) error {
	if h != nil && h.instanceExit != nil {
		affected := []*model.Player{transition.Target}
		if transition.Dismissed {
			affected = transition.AllBefore
		}
		for _, player := range affected {
			if player == nil {
				continue
			}
			if err := h.instanceExit.SubmitPartyQuestInstanceExit(ctx, player); err != nil {
				return err
			}
		}
	}
	if err := sendPartyPacket(h, transition.AllBefore, buildPartyLeave(transition.RemovedUID, transition.RemovalReason)); err != nil {
		return err
	}
	if transition.Dismissed {
		return sendPartyPacket(h, transition.AllBefore, buildPartyDismiss())
	}
	if transition.LeaderChanged {
		return sendPartyPacket(h, transition.Party.Members, buildPartyLeader(transition.Party.MasterUniqueID))
	}
	return nil
}

func sendPartyPacket(h *PartyHandler, players []*model.Player, packet network.Packet) error {
	var result error
	for _, player := range players {
		if player == nil || player.GetSession() == nil {
			continue
		}
		if err := h.sendPacket(player.GetSession(), packet); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// HandleDisconnect removes a closing player from party state before the world
// service drops its UID lookup, then publishes the same leave/dismiss updates
// as an explicit client leave.
func HandleDisconnect(playerUID uint32) {
	transition, err := service.GetPartyServiceInstance().LeaveParty(playerUID, 1)
	if err != nil {
		return
	}
	_ = publishRemoval(context.Background(), &PartyHandler{service: service.GetPartyServiceInstance()}, transition)
}
