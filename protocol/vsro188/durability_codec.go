package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// DurabilityUpdate is AegisCore's maintained 0x3052 server contract. The
// current server path writes a slot byte followed by a u32 value. The stock
// client handler body has not been recovered, so the value is intentionally
// not named as durability, maximum durability, or another semantic field.
type DurabilityUpdate struct {
	Slot  byte
	Value uint32
}

func BuildDurabilityUpdate(update DurabilityUpdate) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemDurabilityUpdate
	packet.WriteByte(update.Slot)
	packet.WriteUInt32(update.Value)
	return packet
}

func DecodeDurabilityUpdate(packet *network.Packet) (DurabilityUpdate, error) {
	if packet == nil {
		return DurabilityUpdate{}, fmt.Errorf("0x3052 packet is nil")
	}
	update := DurabilityUpdate{}
	var err error
	if update.Slot, err = packet.ReadByte(); err != nil {
		return DurabilityUpdate{}, fmt.Errorf("read 0x3052 slot: %w", err)
	}
	if update.Value, err = packet.ReadUInt32(); err != nil {
		return DurabilityUpdate{}, fmt.Errorf("read 0x3052 value: %w", err)
	}
	if packet.Remaining() != 0 {
		return DurabilityUpdate{}, fmt.Errorf("0x3052 has %d trailing bytes", packet.Remaining())
	}
	return update, nil
}
