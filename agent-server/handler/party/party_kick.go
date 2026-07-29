package party

import (
	"context"
	"fmt"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/server"
)

func (h *PartyHandler) handleKick(_ context.Context, session *server.Session, packet network.Packet) error {
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
	return publishRemoval(h, transition)
}
