package party

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func (h *PartyHandler) handleMatchingForm(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, err := packet.ReadUInt32(); err != nil { // TODO: possible party-number placeholder
		return err
	}
	if _, err := packet.ReadUInt32(); err != nil { // TODO: possible party-number placeholder
		return err
	}
	partySetting, err := packet.ReadByte()
	if err != nil {
		return err
	}
	purposeType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	levelMin, err := packet.ReadByte()
	if err != nil {
		return err
	}
	levelMax, err := packet.ReadByte()
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 || partySetting > 7 || purposeType > 3 || levelMin > levelMax ||
		vsro188.ValidateText(title, vsro188.TextRules{Field: "party title"}) != nil {
		return fmt.Errorf("invalid party matching form")
	}

	party := model.PartyFormRequest{
		MasterJID:         session.UserContext.UniqueID,
		MasterUniqueID:    session.UserContext.UniqueID,
		MasterName:        session.UserContext.CharName,
		CountryType:       0, // TODO: Figure out
		PartySettingsFlag: model.PartySetting(partySetting),
		PurposeType:       model.PartyPurpose(purposeType),
		LevelMin:          levelMin,
		LevelMax:          levelMax,
		Title:             title,
	}

	partyNumber := service.GetPartyServiceInstance().FormParty(party)

	// TODO Add check for existing party
	p := network.EmptyPacket()
	p.MessageID = opcode.PartyMatchingFormResponse
	if partyNumber == 0 {
		p.WriteByte(2)
		return h.sendPacket(session, p)
	}
	p.WriteByte(1)
	p.WriteUInt32(partyNumber)
	p.WriteUInt32(0)
	p.WriteByte(partySetting)
	p.WriteByte(purposeType)
	p.WriteByte(levelMin)
	p.WriteByte(levelMax)
	p.WriteString(title)
	return h.sendPacket(session, p)
}
