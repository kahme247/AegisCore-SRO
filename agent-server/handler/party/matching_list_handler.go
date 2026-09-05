package party

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
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

	partyService := h.core()
	parties := partyService.GetFormedParties(session.UserContext.UniqueID, pageIndex)
	pageCount := partyService.GetFormedPartiesPageCount()
	if pageIndex >= pageCount {
		p := network.EmptyPacket()
		p.MessageID = opcode.PartyMatchingListResponse
		p.WriteByte(1)
		p.WriteByte(pageCount)
		p.WriteByte(pageIndex)
		p.WriteByte(0)
		return h.sendPacket(session, p)
	}

	p := network.EmptyPacket()
	p.MessageID = opcode.PartyMatchingListResponse
	p.WriteByte(1)
	p.WriteByte(pageCount)
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
