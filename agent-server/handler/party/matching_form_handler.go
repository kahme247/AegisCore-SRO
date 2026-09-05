package party

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func (h *PartyHandler) handleMatchingForm(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || session == nil || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("party matching form session is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	// The client sends MatchingID followed by Party.ID. A newly formed
	// listing uses zero for Party.ID; the server allocates the authoritative
	// matching/party number below rather than trusting either client value.
	if _, err := packet.ReadUInt32(); err != nil {
		return err
	}
	if _, err := packet.ReadUInt32(); err != nil {
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
		MasterJID:      partySessionJID(session),
		MasterUniqueID: session.UserContext.UniqueID,
		MasterName:     session.UserContext.CharName,
		// The TestIn handler reads this field from the matching record, but the
		// source of the value is not recovered. Keep the neutral country value.
		CountryType:       0,
		PartySettingsFlag: model.PartySetting(partySetting),
		PurposeType:       model.PartyPurpose(purposeType),
		LevelMin:          levelMin,
		LevelMax:          levelMax,
		Title:             title,
	}

	partyNumber := h.core().FormParty(party)

	// FormParty updates an existing listing only for its authenticated master;
	// otherwise it creates a new listing and live party atomically.
	p := network.EmptyPacket()
	p.MessageID = opcode.PartyMatchingFormResponse
	if partyNumber == 0 {
		return h.sendPacket(session, buildMatchingOperationFailure(opcode.PartyMatchingFormResponse))
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
