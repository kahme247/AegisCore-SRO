package party

import (
	"context"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

// PartyHandler serializes party matching operations until PartyService exposes
// a complete concurrency boundary of its own. The previous implementation had
// one worker per opcode; a single lock prevents the registry migration from
// widening concurrent map and party-member mutations.
type PartyHandler struct {
	mu                 sync.Mutex
	service            *service.PartyService
	send               func(*server.Session, network.Packet) error
	invitationFallback interface {
		AnswerGuildInvitation(context.Context, *server.Session, bool) (bool, error)
	}
	instanceExit interface {
		SubmitPartyQuestInstanceExit(context.Context, *model.Player) error
	}
	forgottenWorldRecall interface {
		SubmitForgottenWorldRecall(context.Context, *model.Player, *model.Player) error
	}
	recallCounter uint32
	pendingRecall map[uint32]forgottenWorldRecallInvitation
	world         interface {
		GetPlayerByUniqueId(uint32) (*model.Player, error)
	}
	pendingChallenges map[uint32]recallChallengeState
	now               func() time.Time
}

func RegisterPartyHandlers(target *server.Server, dependencies ...any) {
	handler := &PartyHandler{
		service: service.GetPartyServiceInstance(), pendingRecall: make(map[uint32]forgottenWorldRecallInvitation),
		world: service.GetWorldServiceInstance(), pendingChallenges: make(map[uint32]recallChallengeState), now: time.Now,
	}
	for _, dependency := range dependencies {
		if fallback, ok := dependency.(interface {
			AnswerGuildInvitation(context.Context, *server.Session, bool) (bool, error)
		}); ok {
			handler.invitationFallback = fallback
		}
		if exit, ok := dependency.(interface {
			SubmitPartyQuestInstanceExit(context.Context, *model.Player) error
		}); ok {
			handler.instanceExit = exit
		}
		if recall, ok := dependency.(interface {
			SubmitForgottenWorldRecall(context.Context, *model.Player, *model.Player) error
		}); ok {
			handler.forgottenWorldRecall = recall
		}
	}
	target.RegisterHandler(opcode.PartyCreateRequest, handler.handleCreateInvite)
	target.RegisterHandler(opcode.PartyLeaveRequest, handler.handleLeave)
	target.RegisterHandler(opcode.PartyInviteRequest, handler.handleExistingInvite)
	target.RegisterHandler(opcode.PartyInvitation, handler.handleInvitationAnswer)
	target.RegisterHandler(opcode.PartyKickRequest, handler.handleKick)
	target.RegisterHandler(opcode.PartyMatchingFormRequest, handler.handleMatchingForm)
	target.RegisterHandler(opcode.PartyMatchingUpdateRequest, handler.handleMatchingUpdate)
	target.RegisterHandler(opcode.PartyMatchingDeleteRequest, handler.handleMatchingDelete)
	target.RegisterHandler(opcode.PartyMatchingListRequest, handler.handleMatchingList)
	target.RegisterHandler(opcode.PartyMatchingJoinRequest, handler.handleMatchingJoin)
	target.RegisterHandler(opcode.PartyMatchingPlayerJoinRequest, handler.handleMatchingPlayerJoin)
	target.RegisterHandler(opcode.PartyMatchingCompletionRequest, handler.handleMatchingPlayerJoin)
	target.RegisterHandler(opcode.FGWRecallListRequest, handler.handleForgottenWorldRecallList)
	target.RegisterHandler(opcode.FGWRecallRequest, handler.handleForgottenWorldRecallMember)
	target.RegisterHandler(opcode.FGWRecallResponse, handler.handleForgottenWorldRecallResponse)
	target.RegisterHandler(opcode.FGWExitRequest, handler.handleForgottenWorldExit)
	target.RegisterHandler(opcode.RecallSolveRequest, handler.handleRecallSolve)
}

func (h *PartyHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if err := packet.WriteError(); err != nil {
		return err
	}
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func (h *PartyHandler) core() *service.PartyService {
	if h != nil && h.service != nil {
		return h.service
	}
	return service.GetPartyServiceInstance()
}

// partySessionJID is the durable character identity used by party records.
// UniqueID is a live-world/session identity and must remain separate. The
// fallback keeps isolated packet tests that intentionally omit CharacterID
// working without allowing production sessions to conflate the two values.
func partySessionJID(session *server.Session) uint32 {
	if session == nil {
		return 0
	}
	if session.UserContext.CharacterID > 0 {
		return uint32(session.UserContext.CharacterID)
	}
	return session.UserContext.UniqueID
}
