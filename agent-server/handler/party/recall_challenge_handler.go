package party

import (
	"context"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const recallChallengeTTL = time.Minute

type recallChallengeState struct {
	challengeID uint32
	token       uint32
	expiresAt   time.Time
	onAccept    func(context.Context) error
}

// IssueRecallChallenge publishes the stock B030/7030 confirmation exchange.
// A newer challenge for the same live character replaces the older one.
func (h *PartyHandler) IssueRecallChallenge(session *server.Session, contextValue uint32, onAccept func(context.Context) error) error {
	if h == nil || session == nil || session.UserContext.UniqueID == 0 || contextValue == 0 || onAccept == nil {
		return model.ErrQuestInstanceInvalid
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	challengeID := model.NextActionUID()
	token := model.NextActionUID()
	if challengeID == 0 || token == 0 {
		return model.ErrQuestInstanceInvalid
	}
	h.mu.Lock()
	if h.pendingChallenges == nil {
		h.pendingChallenges = make(map[uint32]recallChallengeState)
	}
	h.pendingChallenges[session.UserContext.UniqueID] = recallChallengeState{
		challengeID: challengeID, token: token, expiresAt: now.Add(model.RuntimeRecallConfirmationTTL()), onAccept: onAccept,
	}
	h.mu.Unlock()
	if err := h.sendPacket(session, vsro188.BuildRecallResult(opcode.RecallResponse, 0)); err != nil {
		h.mu.Lock()
		delete(h.pendingChallenges, session.UserContext.UniqueID)
		h.mu.Unlock()
		return err
	}
	return h.sendPacket(session, vsro188.BuildRecallChallenge(vsro188.RecallChallenge{ChallengeID: challengeID, Left: token, Right: contextValue}))
}

func (h *PartyHandler) handleRecallSolve(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeRecallSolveRequest(&packet)
	if err != nil || session == nil || session.UserContext.UniqueID == 0 || request.Mode > 1 {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.RecallSolveResponse, forgottenWorldInvalidRequest))
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	h.mu.Lock()
	state, exists := h.pendingChallenges[session.UserContext.UniqueID]
	if exists {
		delete(h.pendingChallenges, session.UserContext.UniqueID)
	}
	h.mu.Unlock()
	if !exists || state.challengeID != request.ChallengeID || state.token != request.Answer || !now.Before(state.expiresAt) {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.RecallSolveResponse, forgottenWorldInvalidRequest))
	}
	if request.Mode == 0 {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.RecallSolveResponse, 0))
	}
	if err = state.onAccept(ctx); err != nil {
		return h.sendPacket(session, vsro188.BuildRecallResult(opcode.RecallSolveResponse, forgottenWorldResultCode(err)))
	}
	return h.sendPacket(session, vsro188.BuildRecallResult(opcode.RecallSolveResponse, 0))
}
