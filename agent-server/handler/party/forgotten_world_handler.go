package party

import (
	"context"
	"errors"
	"math"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	forgottenWorldInvalidRequest uint16 = 3
	forgottenWorldUnavailable    uint16 = 2
	forgottenWorldNoParty        uint16 = 7219
	forgottenWorldNotMaster      uint16 = 7220
	forgottenWorldSameRegion     uint16 = 7221
)

type forgottenWorldRecallInvitation struct {
	recallID   uint32
	inviterUID uint32
	targetUID  uint32
	scope      model.GameWorldScope
}

func (h *PartyHandler) partyPlayer(session *server.Session) (*model.Player, error) {
	if h == nil || h.world == nil || session == nil || session.UserContext.UniqueID == 0 {
		return nil, errors.New("invalid party session")
	}
	return h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
}

func forgottenWorldResultCode(err error) uint16 {
	if err == nil {
		return 0
	}
	if errors.Is(err, model.ErrQuestInstanceMissing) || errors.Is(err, model.ErrQuestInstanceNotMember) {
		return forgottenWorldNoParty
	}
	if errors.Is(err, model.ErrQuestInstanceMember) {
		return forgottenWorldSameRegion
	}
	return forgottenWorldUnavailable
}

func (h *PartyHandler) publishForgottenWorldState(player *model.Player, state byte) error {
	if h == nil || h.world == nil || player == nil || player.GetParty() == nil {
		return nil
	}
	update, err := vsro188.BuildForgottenWorldUpdate(vsro188.ForgottenWorldUpdate{MemberID: player.GetUniqueID(), State: state})
	if err != nil {
		return err
	}
	party := player.GetParty()
	if party.Mutex != nil {
		party.Mutex.Lock()
	}
	members := append([]uint32(nil), party.Members...)
	if party.Mutex != nil {
		party.Mutex.Unlock()
	}
	for _, uid := range members {
		member, lookupErr := h.world.GetPlayerByUniqueId(uid)
		if lookupErr == nil && member != nil && member.Session != nil {
			if err = h.sendPacket(member.Session, update); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *PartyHandler) handleForgottenWorldRecallList(_ context.Context, session *server.Session, packet network.Packet) error {
	if _, err := vsro188.DecodeForgottenWorldRecallListRequest(&packet); err != nil {
		response, _ := vsro188.BuildForgottenWorldRecallList(nil, forgottenWorldInvalidRequest)
		return h.sendPacket(session, response)
	}
	player, err := h.partyPlayer(session)
	if err != nil {
		response, _ := vsro188.BuildForgottenWorldRecallList(nil, forgottenWorldInvalidRequest)
		return h.sendPacket(session, response)
	}
	party := player.GetParty()
	if party == nil {
		response, _ := vsro188.BuildForgottenWorldRecallList(nil, forgottenWorldNoParty)
		return h.sendPacket(session, response)
	}
	party.Mutex.Lock()
	members := append([]uint32(nil), party.Members...)
	party.Mutex.Unlock()
	entries := make([]vsro188.ForgottenWorldRecallListEntry, 0, len(members))
	for _, uid := range members {
		member, lookupErr := h.world.GetPlayerByUniqueId(uid)
		if lookupErr != nil || member == nil || member.Session == nil {
			continue
		}
		member.Mutex.Lock()
		level := member.Level
		member.Mutex.Unlock()
		if level < 0 {
			level = 0
		} else if level > math.MaxUint8 {
			level = math.MaxUint8
		}
		state := byte(0)
		if member.GetGameWorldID() == player.GetGameWorldID() {
			state = 1
		}
		entries = append(entries, vsro188.ForgottenWorldRecallListEntry{MemberID: uid, State: state, Level: byte(level), RegionID: uint16(member.GetNavmeshPosition().Region.ID)})
	}
	response, err := vsro188.BuildForgottenWorldRecallList(entries, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *PartyHandler) handleForgottenWorldRecallMember(_ context.Context, session *server.Session, packet network.Packet) error {
	targetUID, err := vsro188.DecodeForgottenWorldRecallMemberRequest(&packet)
	if err != nil || targetUID == 0 {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldInvalidRequest))
	}
	inviter, err := h.partyPlayer(session)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldInvalidRequest))
	}
	party := inviter.GetParty()
	if party == nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldNoParty))
	}
	if party.MasterUniqueID != inviter.GetUniqueID() {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldNotMaster))
	}
	scope := model.GameWorldScope(inviter.GetGameWorldID())
	if !scope.ValidInstance() {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldUnavailable))
	}
	target, lookupErr := h.world.GetPlayerByUniqueId(targetUID)
	if lookupErr != nil || target == nil || target.Session == nil || target.GetParty() != party {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldInvalidRequest))
	}
	if target.GetGameWorldID() == uint32(scope) {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, forgottenWorldSameRegion))
	}
	h.mu.Lock()
	h.recallCounter++
	if h.recallCounter == 0 {
		h.recallCounter++
	}
	invitation := forgottenWorldRecallInvitation{recallID: h.recallCounter, inviterUID: inviter.GetUniqueID(), targetUID: targetUID, scope: scope}
	h.pendingRecall[targetUID] = invitation
	h.mu.Unlock()
	request, err := vsro188.BuildForgottenWorldRecallInvitation(invitation.recallID, invitation.inviterUID, uint32(scope), inviter.GetName())
	if err != nil {
		return err
	}
	if err = h.sendPacket(target.Session, request); err != nil {
		h.mu.Lock()
		delete(h.pendingRecall, targetUID)
		h.mu.Unlock()
		return err
	}
	return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberResponse, 0))
}

func (h *PartyHandler) handleForgottenWorldRecallResponse(ctx context.Context, session *server.Session, packet network.Packet) error {
	response, err := vsro188.DecodeForgottenWorldRecallResponse(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, forgottenWorldInvalidRequest))
	}
	target, err := h.partyPlayer(session)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, forgottenWorldInvalidRequest))
	}
	h.mu.Lock()
	invitation, exists := h.pendingRecall[target.GetUniqueID()]
	if exists && invitation.recallID == response.RecallID && invitation.inviterUID == response.MemberID {
		delete(h.pendingRecall, target.GetUniqueID())
	} else {
		exists = false
	}
	h.mu.Unlock()
	if !exists {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, forgottenWorldInvalidRequest))
	}
	if !response.Accepted {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, 0))
	}
	inviter, lookupErr := h.world.GetPlayerByUniqueId(invitation.inviterUID)
	if lookupErr != nil || inviter == nil || inviter.Session == nil || inviter.GetGameWorldID() != uint32(invitation.scope) || target.GetParty() != inviter.GetParty() || h.forgottenWorldRecall == nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, forgottenWorldUnavailable))
	}
	err = h.forgottenWorldRecall.SubmitForgottenWorldRecall(ctx, target, inviter)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, forgottenWorldResultCode(err)))
	}
	if err = h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWRecallMemberAcceptedResponse, 0)); err != nil {
		return err
	}
	return h.publishForgottenWorldState(target, 1)
}

func (h *PartyHandler) handleForgottenWorldExit(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := vsro188.DecodeForgottenWorldExitRequest(&packet); err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWExitResponse, forgottenWorldInvalidRequest))
	}
	player, err := h.partyPlayer(session)
	if err == nil && h.instanceExit == nil {
		err = model.ErrQuestInstanceInvalid
	} else if err == nil {
		err = h.instanceExit.SubmitPartyQuestInstanceExit(ctx, player)
	}
	if err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWExitResponse, forgottenWorldResultCode(err)))
	}
	if err = h.sendPacket(session, vsro188.BuildRecallResult(opcode.FGWExitResponse, 0)); err != nil {
		return err
	}
	return h.publishForgottenWorldState(player, 0)
}
