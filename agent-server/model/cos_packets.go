package model

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	COSUpdateTerminated byte = 1
	COSUpdateInventory  byte = 2
	COSUpdateExperience byte = 3
	COSUpdateHGP        byte = 4
	COSUpdateName       byte = 5
	COSUpdateRefObject  byte = 7
)

// BuildCOSData creates the owner-only 0x30C8 snapshot for the v188 COS
// subtypes whose layouts are proven by the stock client decoder at 0x009A1900.
// Ride transports are not prefix-only: after the common four u32 fields the
// decoder always consumes their inventory-capacity byte. SR_GameServer's
// 0x004FBAE0 builder reaches the base CGObjCOS serializer at 0x004D1CD0,
// which delegates that inventory segment through 0x004B8AD0.
func BuildCOSData(cos *COS) (packet network.Packet, ok bool) {
	if cos == nil {
		return network.EmptyPacket(), false
	}
	s := cos.Snapshot()
	if s.Kind != COSRideTransport && s.Kind != COSJobTransport && s.Kind != COSGrowthPet && s.Kind != COSAbilityPet && s.Kind != COSFellowPet {
		return network.EmptyPacket(), false
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.COSDataResponse
	p.WriteUInt32(s.UniqueID)
	p.WriteUInt32(s.RefObjectID)
	p.WriteUInt32(uint32(s.Health))
	p.WriteUInt32(uint32(s.MaxHealth))
	switch s.Kind {
	case COSRideTransport:
		writeCOSInventory(&p, s.InventorySize, s.Inventory)
	case COSJobTransport:
		writeCOSInventory(&p, s.InventorySize, s.Inventory)
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
		writeCOSInventory(&p, s.InventorySize, s.Inventory)
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
		writeCOSInventory(&p, s.InventorySize, s.Inventory)
	}
	return p, true
}

func writeEmptyCOSInventory(p *network.Packet, capacity byte) {
	p.WriteByte(capacity)
	// Client 0x009A1900 reads the item count only when capacity is nonzero.
	if capacity != 0 {
		p.WriteByte(0)
	}
}

func writeCOSInventory(p *network.Packet, capacity byte, inventory Inventory) {
	if capacity == 0 {
		writeEmptyCOSInventory(p, 0)
		return
	}
	snapshot := inventory.Snapshot()
	slots := SortedInventorySlots(snapshot.Items)
	valid := slots[:0]
	for _, slot := range slots {
		if slot < capacity {
			valid = append(valid, slot)
		}
	}
	p.WriteByte(capacity)
	p.WriteByte(byte(min(len(valid), int(^uint8(0)))))
	for _, slot := range valid {
		item := snapshot.Items[slot]
		p.WriteByte(slot)
		WriteRentInfo(p, item)
		p.WriteUInt32(item.GetRefObjectID())
		WriteInventoryItem(p, item)
	}
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
	return vsro188.BuildInventoryItemStateUpdate(slot, byte(state))
}

// BuildCOSHealthUpdate uses the client-confirmed 0x3057 entity-status grammar:
// entity UID, neutral u16 context, mask bit 1, then current HP as u32.
func BuildCOSHealthUpdate(uniqueID uint32, health uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityUpdateStatus
	p.WriteUInt32(uniqueID)
	p.WriteUInt16(0)
	p.WriteByte(1)
	p.WriteUInt32(health)
	return p
}

func BuildCOSHGPUpdate(uniqueID uint32, hgp uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUpdateResponse
	p.WriteUInt32(uniqueID)
	p.WriteByte(COSUpdateHGP)
	p.WriteUInt16(hgp)
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

func BuildCOSPickupInventoryUpdate(uniqueID uint32, slot byte, item Item) network.Packet {
	// The stock B034 operation-0x17 completion consumes only the COS/context
	// value and slot. The item argument remains in this compatibility wrapper
	// for callers that already have the hydrated item, but its variable body is
	// not part of this wire branch.
	return vsro188.BuildItemOperationObjectSlotSuccess(vsro188.ItemOperationCOSTransfer, uniqueID, slot)
}
