package party

import (
	"sync"

	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

// PartyHandler serializes party matching operations until PartyService exposes
// a complete concurrency boundary of its own. The previous implementation had
// one worker per opcode; a single lock prevents the registry migration from
// widening concurrent map and party-member mutations.
type PartyHandler struct {
	mu      sync.Mutex
	service *service.PartyService
	send    func(*server.Session, network.Packet) error
}

func RegisterPartyHandlers(target *server.Server) {
	handler := &PartyHandler{service: service.GetPartyServiceInstance()}
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
