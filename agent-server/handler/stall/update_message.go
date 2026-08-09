package stall

import (
	"errors"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func updateMessage(data server.PacketChannelData) error {
	if data.Session == nil {
		return errors.New("stall update has no session")
	}
	message, err1 := data.ReadString()
	if err1 != nil {
		return err1
	}
	if data.Remaining() != 0 {
		return errors.New("stall message update has trailing data")
	}
	if err := vsro188.ValidateText(message, vsro188.TextRules{Field: "stall message"}); err != nil {
		return err
	}

	p := network.EmptyPacket()
	p.MessageID = opcode.StallUpdateResponse
	if err := p.WriteByte(1); err != nil {
		return err
	}
	if err := p.WriteByte(StallMessage); err != nil {
		return err
	}
	if err := p.WriteString(message); err != nil {
		return err
	}
	return data.Session.Send(p)
}
