package party

import (
	"context"
	"sync"

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
}

func RegisterPartyHandlers(target *server.Server, fallback ...interface {
	AnswerGuildInvitation(context.Context, *server.Session, bool) (bool, error)
}) {
	handler := &PartyHandler{service: service.GetPartyServiceInstance()}
	if len(fallback) > 0 {
		handler.invitationFallback = fallback[0]
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
