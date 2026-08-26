package model

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	COSUpdateTerminated byte = 1
	COSUpdateInventory  byte = 2
	COSUpdateExperience byte = 3
	COSUpdateHGP        byte = 4
	COSUpdateName       byte = 5
	COSUpdateRefObject  byte = 7
)

// BuildCOSData creates the owner-only 0x30C8 snapshot. These branches mirror
// the pinned v188 RSBot readers. Empty ability/job inventories use the normal
// zero-capacity/count representation until COS inventory loading is attached.
func BuildCOSData(cos *COS) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSDataResponse
	if cos == nil {
		return p
	}
	s := cos.Snapshot()
	p.WriteUInt32(s.UniqueID)
	p.WriteUInt32(s.RefObjectID)
	p.WriteUInt32(uint32(s.Health))
	p.WriteUInt32(uint32(s.MaxHealth))
	switch s.Kind {
	case COSJobTransport:
		writeEmptyCOSInventory(&p, s.InventorySize)
		p.WriteUInt32(s.OwnerUniqueID)
	case COSGrowthPet:
		p.WriteUInt64(uint64(s.Experience))
		p.WriteByte(s.Level)
		p.WriteUInt16(s.HGP)
		p.WriteUInt32(s.Settings)
		p.WriteString(s.Name)
		p.WriteByte(0)
		p.WriteUInt32(s.OwnerUniqueID)
		p.WriteByte(s.InventorySlot)
	case COSAbilityPet:
		p.WriteUInt32(s.Settings)
		p.WriteString(s.Name)
		writeEmptyCOSInventory(&p, s.InventorySize)
		p.WriteUInt32(s.OwnerUniqueID)
		p.WriteByte(s.InventorySlot)
	case COSFellowPet:
		p.WriteUInt64(uint64(s.Experience))
		p.WriteByte(s.Level)
		p.WriteUInt32(uint32(s.Satiety))
		p.WriteUInt16(0)
		p.WriteUInt32(0) // stored SP
		p.WriteUInt32(0)
		p.WriteUInt32(s.Settings)
		p.WriteString(s.Name)
		writeEmptyCOSInventory(&p, s.InventorySize)
	}
	return p
}

func writeEmptyCOSInventory(p *network.Packet, capacity byte) {
	p.WriteByte(capacity)
	p.WriteByte(0) // item count
}

func BuildCOSTerminatedUpdate(uniqueID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUpdateResponse
	p.WriteUInt32(uniqueID)
	p.WriteByte(COSUpdateTerminated)
	return p
}

// BuildCOSItemStateUpdate publishes the state byte for the inventory item that
// owns a persistent COS. The same proven layout is used by manual unsummon and
// automatic fixed-period rental expiry.
func BuildCOSItemStateUpdate(slot byte, state COSItemState) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUpdate
	p.WriteByte(slot)
	p.WriteByte(64) // ItemUpdateFlag.State
	p.WriteByte(byte(state))
	return p
}

func BuildCOSNameUpdate(uniqueID uint32, name string) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUpdateResponse
	p.WriteUInt32(uniqueID)
	p.WriteByte(COSUpdateName)
	p.WriteString(name)
	return p
}

func BuildCOSExperienceUpdate(uniqueID uint32, gained uint64, sourceUID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUpdateResponse
	p.WriteUInt32(uniqueID)
	p.WriteByte(COSUpdateExperience)
	p.WriteUInt64(gained)
	p.WriteUInt32(sourceUID)
	return p
}

func BuildCOSRefObjectUpdate(uniqueID, refObjectID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUpdateResponse
	p.WriteUInt32(uniqueID)
	p.WriteByte(COSUpdateRefObject)
	p.WriteUInt32(refObjectID)
	return p
}
