package party

import (
	"context"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/ferdoran/go-sro/protocol/vsro188"
)

func (h *PartyHandler) handleMatchingUpdate(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	partyNumber, err := packet.ReadUInt32()
	if err != nil {
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
		return fmt.Errorf("invalid party matching update")
	}

	party := model.Party{
		Number:            partyNumber,
		MasterJID:         session.UserContext.UniqueID,
		MasterName:        session.UserContext.CharName,
		MasterUniqueID:    session.UserContext.UniqueID,
		CountryType:       0, // TODO: Figure out
		PartySettingsFlag: model.PartySetting(partySetting),
		PurposeType:       model.PartyPurpose(purposeType),
		LevelMin:          levelMin,
		LevelMax:          levelMax,
		Title:             title,
		Mutex:             &sync.Mutex{},
	}

	service.GetPartyServiceInstance().UpdateParty(party)

	p := network.EmptyPacket()
	p.MessageID = opcode.PartyMatchingUpdateResponse
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
