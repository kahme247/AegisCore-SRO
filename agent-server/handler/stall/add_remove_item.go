package stall

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/server"
)

func addRemoveItem(data server.PacketChannelData, updateType byte) error {
	if data.Session == nil {
		return errors.New("stall update has no session")
	}
	stallSlot, err := data.ReadByte()
	if err != nil {
		return err
	}

	var inventorySlot byte
	var stackCount uint16
	var fleaMarketNetworkTidGroup uint32
	var price uint64

	if updateType == StallAddItem {
		var extraErr error
		inventorySlot, stackCount, price, fleaMarketNetworkTidGroup, extraErr = getExtraData(data)
		if extraErr != nil {
			return extraErr
		}
	}

	unkUShort0, err := data.ReadUInt16()
	if err != nil {
		return err
	}

	stallEntry := model.StallEntry{
		StallSlot:                 stallSlot,
		InventorySlot:             inventorySlot,
		StackCount:                stackCount,
		Price:                     price,
		FleaMarketnetworkTidGroup: fleaMarketNetworkTidGroup,
		UnkUshort0:                unkUShort0,
		Mutex:                     &sync.Mutex{},
	}

	var playerStall *model.Stall
	switch updateType {
	case StallAddItem:
		playerStall = stallEntry.AddItem(data.UserContext.UniqueID)
	case StallRemoveItem:
		playerStall = stallEntry.RemoveItem(data.UserContext.UniqueID)
	default:
		return fmt.Errorf("unsupported stall item update type %d", updateType)
	}

	player, err := service.GetWorldServiceInstance().GetPlayerByUniqueId(data.UserContext.UniqueID)
	if err != nil {
		return fmt.Errorf("find player for stall update: %w", err)
	}
	playerInventoryItems := player.GetInventory().Items
	p, err := newStallUpdateResponse(updateType)
	if err != nil {
		return err
	}
	if err := p.WriteUInt16(0); err != nil { // TODO: Handle errors correctly
		return err
	}
	if playerStall != nil && playerStall.Entries != nil {
		for _, entry := range playerStall.Entries {
			item := playerInventoryItems[entry.InventorySlot]
			if err := p.WriteByte(entry.StallSlot); err != nil {
				return err
			}
			model.WriteRentInfo(&p, item)
			if err := p.WriteUInt32(item.GetRefObjectID()); err != nil {
				return err
			}
			model.WriteInventoryItem(&p, item)
			if err := p.WriteByte(entry.InventorySlot); err != nil {
				return err
			}
			if err := p.WriteUInt16(entry.StackCount); err != nil {
				return err
			}
			if err := p.WriteUInt64(entry.Price); err != nil {
				return err
			}
		}
	}
	if err := p.WriteByte(255); err != nil { // End of data
		return err
	}
	return data.Session.Send(p)
}

func getExtraData(data server.PacketChannelData) (byte, uint16, uint64, uint32, error) {
	inventorySlot, err := data.ReadByte()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	stackCount, err := data.ReadUInt16()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	price, err := data.ReadUInt64()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	fleaMarketNetworkTidGroup, err := data.ReadUInt32()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return inventorySlot, stackCount, price, fleaMarketNetworkTidGroup, nil
}
