package stall

import (
	"errors"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func updateStallName(data server.PacketChannelData) error {
	if data.Session == nil {
		return errors.New("stall update has no session")
	}
	stallName, err1 := data.ReadString()
	if err1 != nil {
		return err1
	}
	if data.Remaining() != 0 {
		return errors.New("stall name update has trailing data")
	}
	if err := vsro188.ValidateText(stallName, vsro188.TextRules{Field: "stall name", RequireTrimmed: true}); err != nil {
		return err
	}

	p := network.EmptyPacket()
	p.MessageID = opcode.StallEntityNameResponse
	if err := p.WriteUInt32(data.Session.UserContext.UniqueID); err != nil {
		return err
	}
	if err := p.WriteString(stallName); err != nil {
		return err
	}
	return data.Session.Send(p)
}
