package stall

import (
	"errors"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func updateState(data server.PacketChannelData) error {
	if data.Session == nil {
		return errors.New("stall update has no session")
	}
	isOpen, err := data.ReadBool()
	if err != nil {
		return err
	}

	stallNetworkResult, err1 := data.ReadUInt16()
	if err1 != nil {
		return err1
	}

	p := network.EmptyPacket()
	p.MessageID = opcode.StallUpdateResponse
	if err := p.WriteByte(1); err != nil {
		return err
	}
	if err := p.WriteByte(StallState); err != nil {
		return err
	}
	if err := p.WriteBool(isOpen); err != nil {
		return err
	}
	if err := p.WriteUInt16(stallNetworkResult); err != nil {
		return err
	}
	return data.Session.Send(p)
}
