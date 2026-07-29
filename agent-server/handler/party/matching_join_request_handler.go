package party

import (
	"context"
	"fmt"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func (h *PartyHandler) handleMatchingJoin(_ context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || packet.Remaining() != 4 {
		return fmt.Errorf("invalid party matching join request")
	}
	partyNumber, err := packet.ReadUInt32()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid party matching number")
	}
	request, master, applicant, err := h.core().CreateMatchingJoinRequest(partyNumber, session.UserContext.UniqueID)
	if err != nil {
		return h.sendPacket(session, buildMatchingJoinResult(false, 2))
	}
	requestPacket := network.EmptyPacket()
	requestPacket.MessageID = opcode.PartyMatchingPlayerJoinRequest
	requestPacket.WriteUInt32(request.RequestID)
	requestPacket.WriteUInt32(applicant.GetUniqueID())
	requestPacket.WriteUInt32(request.PartyNumber)
	first, second := playerHighestMasteries(applicant)
	requestPacket.WriteUInt32(first)
	requestPacket.WriteUInt32(second)
	requestPacket.WriteByte(0x04)
	writePartyMember(&requestPacket, applicant)
	return h.sendPacket(master.GetSession(), requestPacket)
}
