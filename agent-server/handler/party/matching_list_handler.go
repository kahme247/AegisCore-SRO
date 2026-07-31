package party

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *PartyHandler) handleMatchingList(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || session == nil || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("party matching list session is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	pageIndex, err := packet.ReadByte()
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid party matching list request")
	}

	parties := h.core().GetFormedParties(session.UserContext.UniqueID, pageIndex)
	return h.sendPacket(session, buildMatchingListResult(pageIndex, h.core().GetFormedPartiesPageCount(), parties))
}
