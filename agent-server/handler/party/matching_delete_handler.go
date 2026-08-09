package party

import (
	"context"

	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *PartyHandler) handleMatchingDelete(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	partyNumber, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	service.GetPartyServiceInstance().DeletePartyFromMatching(partyNumber, session.UserContext.UniqueID)
	return nil
}
