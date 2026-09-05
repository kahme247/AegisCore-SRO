package party

import (
	"context"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func (h *PartyHandler) handleMatchingUpdate(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || session == nil || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("party matching update session is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	matchingID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	// Party.ID is present in the wire request but the authenticated live
	// party/master state is authoritative for updates.
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
		return fmt.Errorf("invalid party matching update")
	}

	party := model.Party{
		Number:         matchingID,
		MasterJID:      partySessionJID(session),
		MasterName:     session.UserContext.CharName,
		MasterUniqueID: session.UserContext.UniqueID,
		// The TestIn handler's source for CountryType is unresolved.
		CountryType:       0,
		PartySettingsFlag: model.PartySetting(partySetting),
		PurposeType:       model.PartyPurpose(purposeType),
		LevelMin:          levelMin,
		LevelMax:          levelMax,
		Title:             title,
		Mutex:             &sync.Mutex{},
	}

	if err := h.core().UpdateMatchingEntry(party); err != nil {
		return h.sendPacket(session, buildMatchingOperationFailure(opcode.PartyMatchingUpdateResponse))
	}

	p := network.EmptyPacket()
	p.MessageID = opcode.PartyMatchingUpdateResponse
	p.WriteByte(1)
	p.WriteUInt32(matchingID)
	p.WriteUInt32(0)
	p.WriteByte(partySetting)
	p.WriteByte(purposeType)
	p.WriteByte(levelMin)
	p.WriteByte(levelMax)
	p.WriteString(title)
	return h.sendPacket(session, p)
}
