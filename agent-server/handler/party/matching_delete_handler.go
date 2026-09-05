package party

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *PartyHandler) handleMatchingDelete(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || session == nil || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("party matching delete session is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	partyNumber, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid party matching delete request")
	}
	err = h.core().DeleteMatchingEntry(partyNumber, session.UserContext.UniqueID)
	if err != nil {
		return h.sendPacket(session, buildMatchingDeleteResult(false, 0))
	}
	return h.sendPacket(session, buildMatchingDeleteResult(true, partyNumber))
}
