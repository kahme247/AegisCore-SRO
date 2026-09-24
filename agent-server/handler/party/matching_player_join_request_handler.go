package party

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *PartyHandler) handleMatchingPlayerJoin(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || session == nil || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("party matching answer session is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if packet.Remaining() != 9 {
		return fmt.Errorf("invalid party matching answer")
	}
	requestID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	_, err = packet.ReadUInt32() // UserJID echoed by the client; request state is authoritative
	if err != nil {
		return err
	}
	answer, err := packet.ReadByte()
	if err != nil || (answer != 1 && answer != 2) || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party matching answer code")
	}
	request, err := h.core().GetMatchingJoinRequest(requestID)
	if err != nil {
		return err
	}
	transition, err := h.core().AnswerMatchingJoin(requestID, request.PlayerUniqueID, session.UserContext.UniqueID, answer == 1)
	if err != nil {
		return err
	}
	if err = h.sendPacket(transition.Target.GetSession(), buildMatchingJoinResult(true, answer)); err != nil {
		return err
	}
	if !transition.Accepted {
		return nil
	}
	if err = h.sendPacket(transition.Target.GetSession(), buildPartyJoinAcknowledgement(transition.Target.GetUniqueID())); err != nil {
		return err
	}
	if err = sendPartyPacket(h, transition.Existing, buildPartyJoin(transition.Target)); err != nil {
		return err
	}
	return h.sendPacket(transition.Target.GetSession(), buildPartySnapshot(transition.Party, true))
}
