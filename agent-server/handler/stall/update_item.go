package stall

import (
	"errors"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/server"
	"sync"
)

func updateItem(data server.PacketChannelData) error {
	if data.Session == nil {
		return errors.New("stall update has no session")
	}
	stallSlot, err := data.ReadByte()
	if err != nil {
		return err
	}

	stackCount, err2 := data.ReadUInt16()
	if err2 != nil {
		return err2
	}

	price, err3 := data.ReadUInt64()
	if err3 != nil {
		return err3
	}

	unkUShort0, err4 := data.ReadUInt16()
	if err4 != nil {
		return err4
	}

	stallEntry := model.StallEntry{
		StallSlot:  stallSlot,
		StackCount: stackCount,
		Price:      price,
		UnkUshort0: unkUShort0,
		Mutex:      &sync.Mutex{},
	}

	stallEntry = stallEntry.UpdateItem(data.UserContext.UniqueID)

	p, err := newStallUpdateResponse(StallUpdateItem)
	if err != nil {
		return err
	}
	if err := p.WriteByte(stallEntry.StallSlot); err != nil {
		return err
	}
	if err := p.WriteUInt16(stallEntry.StackCount); err != nil {
		return err
	}
	if err := p.WriteUInt64(stallEntry.Price); err != nil {
		return err
	}
	if err := p.WriteUInt16(0); err != nil { // TODO: Handle errors correctly
		return err
	}
	return data.Session.Send(p)
}
