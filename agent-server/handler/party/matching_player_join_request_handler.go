package party

import (
	"context"
	"fmt"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/server"
)

func (h *PartyHandler) handleMatchingPlayerJoin(_ context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() != 9 {
		return fmt.Errorf("invalid party matching answer")
	}
	requestID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	playerJID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	answer, err := packet.ReadByte()
	if err != nil || (answer != 1 && answer != 2) || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party matching answer code")
	}
	transition, err := h.core().AnswerMatchingJoin(requestID, playerJID, session.UserContext.UniqueID, answer == 1)
	if err != nil {
		return err
	}
	if err = h.sendPacket(transition.Target.GetSession(), buildMatchingJoinResult(true, answer)); err != nil {
		return err
	}
	if !transition.Accepted {
		return nil
	}
	if err = sendPartyPacket(h, transition.Existing, buildPartyJoin(transition.Target)); err != nil {
		return err
	}
	return h.sendPacket(transition.Target.GetSession(), buildPartySnapshot(transition.Party, true))
}
