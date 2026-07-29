package party

import (
	"context"

	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func (h *PartyHandler) handleMatchingList(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	pageIndex, err := packet.ReadByte()
	if err != nil {
		return err
	}

	partyService := service.GetPartyServiceInstance()
	parties := partyService.GetFormedParties(session.UserContext.UniqueID, pageIndex)
	pageCount := partyService.GetFormedPartiesPageCount()

	p := network.EmptyPacket()
	p.MessageID = opcode.PartyMatchingListResponse
	p.WriteByte(1)
	p.WriteByte(pageCount) // pageCount TODO: calculate
	p.WriteByte(pageIndex)
	p.WriteByte(byte(len(parties)))
	for _, v := range parties {
		p.WriteUInt32(v.Number)
		p.WriteUInt32(v.MasterJID)
		p.WriteString(v.MasterName)
		p.WriteByte(v.CountryType)
		p.WriteByte(v.MemberCount)
		p.WriteByte(v.PartySettingsFlag.ToByte())
		p.WriteByte(v.PurposeType.ToByte())
		p.WriteByte(v.LevelMin)
		p.WriteByte(v.LevelMax)
		p.WriteString(v.Title)
	}
	return h.sendPacket(session, p)
}
