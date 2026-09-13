package party

import (
	"context"
	"fmt"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *PartyHandler) handleKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() != 4 {
		return fmt.Errorf("invalid party-kick request")
	}
	uniqueID, err := packet.ReadUInt32()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party-kick target")
	}
	transition, err := h.core().KickPartyMember(session.UserContext.UniqueID, uniqueID)
	if err != nil {
		return err
	}
	return publishRemoval(ctx, h, transition)
}
