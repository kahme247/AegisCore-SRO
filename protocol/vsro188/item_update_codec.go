package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// InventoryItemUpdate is the exact variable-width body consumed by the stock
// 0x3040 handler at 0x0088BF30. The field names remain wire-neutral: the
// client branches on the flag bits and item kind, but the binary does not
// establish stable English names for every destination.
type InventoryItemUpdate struct {
	ItemKind byte
	Flags    byte

	Flag01U32    *uint32
	Flag02U8     *byte
	Flag04U64    *uint64
	Flag08U16    *uint16
	Flag10U32    *uint32
	MagicOptions []uint64

	// The patched client accepts flag 0x80 for equipped slots too. Flag 0x40
	// remains a high-kind-only field; flag 0x80 is an independent u32 epoch.
	Flag40U8  *byte
	Flag80U32 *uint32
}

func BuildInventoryItemQuantityUpdate(slot byte, quantity uint16) network.Packet {
	return buildInventoryItemUpdateUnchecked(InventoryItemUpdate{
		ItemKind:  slot,
		Flags:     0x08,
		Flag08U16: &quantity,
	})
}

func BuildInventoryItemStateUpdate(slot, state byte) network.Packet {
	return buildInventoryItemUpdateUnchecked(InventoryItemUpdate{
		ItemKind: slot,
		Flags:    0x40,
		Flag40U8: &state,
	})
}

func buildInventoryItemUpdateUnchecked(update InventoryItemUpdate) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryItemUpdate
	packet.WriteByte(update.ItemKind)
	packet.WriteByte(update.Flags)
	if update.Flags&0x08 != 0 {
		packet.WriteUInt16(*update.Flag08U16)
	}
	if update.ItemKind >= 0x0D && update.Flags&0x40 != 0 {
		packet.WriteByte(*update.Flag40U8)
	}
	return packet
}

func DecodeInventoryItemUpdate(packet network.Packet) (InventoryItemUpdate, error) {
	var update InventoryItemUpdate
	if packet.Remaining() < 2 {
		return update, fmt.Errorf("0x3040 payload is truncated before kind/flags")
	}
	var err error
	if update.ItemKind, err = packet.ReadByte(); err != nil {
		return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 item kind: %w", err)
	}
	if update.Flags, err = packet.ReadByte(); err != nil {
		return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flags: %w", err)
	}

	if update.Flags&0x01 != 0 {
		value, readErr := packet.ReadUInt32()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x01 value: %w", readErr)
		}
		update.Flag01U32 = &value
	}
	if update.Flags&0x02 != 0 {
		value, readErr := packet.ReadByte()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x02 value: %w", readErr)
		}
		update.Flag02U8 = &value
	}
	if update.Flags&0x04 != 0 {
		value, readErr := packet.ReadUInt64()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x04 value: %w", readErr)
		}
		update.Flag04U64 = &value
	}
	if update.Flags&0x08 != 0 {
		value, readErr := packet.ReadUInt16()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x08 value: %w", readErr)
		}
		update.Flag08U16 = &value
	}
	if update.Flags&0x10 != 0 {
		value, readErr := packet.ReadUInt32()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x10 value: %w", readErr)
		}
		update.Flag10U32 = &value
	}
	if update.Flags&0x20 != 0 {
		count, readErr := packet.ReadByte()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 magic-option count: %w", readErr)
		}
		if count > 12 {
			return InventoryItemUpdate{}, fmt.Errorf("0x3040 magic-option count %d exceeds client maximum 12", count)
		}
		update.MagicOptions = make([]uint64, count)
		for index := range update.MagicOptions {
			update.MagicOptions[index], readErr = packet.ReadUInt64()
			if readErr != nil {
				return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 magic option %d: %w", index, readErr)
			}
		}
	}
	if update.ItemKind >= 0x0D && update.Flags&0x40 != 0 {
		value, readErr := packet.ReadByte()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x40 value: %w", readErr)
		}
		update.Flag40U8 = &value
	}
	if update.Flags&0x80 != 0 {
		value, readErr := packet.ReadUInt32()
		if readErr != nil {
			return InventoryItemUpdate{}, fmt.Errorf("read 0x3040 flag 0x80 value: %w", readErr)
		}
		update.Flag80U32 = &value
	}
	if packet.Remaining() != 0 {
		return InventoryItemUpdate{}, fmt.Errorf("0x3040 has %d trailing bytes", packet.Remaining())
	}
	return update, nil
}

func BuildInventoryItemUpdate(update InventoryItemUpdate) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryItemUpdate
	packet.WriteByte(update.ItemKind)
	packet.WriteByte(update.Flags)

	if err := write3040U32(&packet, update.Flags&0x01 != 0, update.Flag01U32, "0x01"); err != nil {
		return network.Packet{}, err
	}
	if err := write3040U8(&packet, update.Flags&0x02 != 0, update.Flag02U8, "0x02"); err != nil {
		return network.Packet{}, err
	}
	if update.Flags&0x04 != 0 {
		if update.Flag04U64 == nil {
			return network.Packet{}, fmt.Errorf("0x3040 flag 0x04 is set without a value")
		}
		packet.WriteUInt64(*update.Flag04U64)
	}
	if update.Flags&0x08 != 0 {
		if update.Flag08U16 == nil {
			return network.Packet{}, fmt.Errorf("0x3040 flag 0x08 is set without a value")
		}
		packet.WriteUInt16(*update.Flag08U16)
	}
	if err := write3040U32(&packet, update.Flags&0x10 != 0, update.Flag10U32, "0x10"); err != nil {
		return network.Packet{}, err
	}
	if update.Flags&0x20 != 0 {
		if len(update.MagicOptions) > 12 {
			return network.Packet{}, fmt.Errorf("0x3040 magic-option count %d exceeds client maximum 12", len(update.MagicOptions))
		}
		packet.WriteByte(byte(len(update.MagicOptions)))
		for _, option := range update.MagicOptions {
			packet.WriteUInt64(option)
		}
	} else if len(update.MagicOptions) != 0 {
		return network.Packet{}, fmt.Errorf("0x3040 magic options supplied while flag 0x20 is clear")
	}
	if update.ItemKind < 0x0D && update.Flag40U8 != nil {
		return network.Packet{}, fmt.Errorf("0x3040 high-kind field supplied for item kind 0x%02X", update.ItemKind)
	}
	if update.ItemKind >= 0x0D && update.Flags&0x40 != 0 {
		if update.Flag40U8 == nil {
			return network.Packet{}, fmt.Errorf("0x3040 flag 0x40 is set without a value")
		}
		packet.WriteByte(*update.Flag40U8)
	} else if update.Flag40U8 != nil {
		return network.Packet{}, fmt.Errorf("0x3040 flag 0x40 value supplied while flag is clear")
	}
	if update.Flags&0x80 != 0 {
		if update.Flag80U32 == nil {
			return network.Packet{}, fmt.Errorf("0x3040 flag 0x80 is set without a value")
		}
		packet.WriteUInt32(*update.Flag80U32)
	} else if update.Flag80U32 != nil {
		return network.Packet{}, fmt.Errorf("0x3040 flag 0x80 value supplied while flag is clear")
	}
	if packet.WriteError() != nil {
		return network.Packet{}, packet.WriteError()
	}
	return packet, nil
}

func write3040U8(packet *network.Packet, enabled bool, value *byte, flag string) error {
	if !enabled {
		if value != nil {
			return fmt.Errorf("0x3040 flag %s value supplied while flag is clear", flag)
		}
		return nil
	}
	if value == nil {
		return fmt.Errorf("0x3040 flag %s is set without a value", flag)
	}
	return packet.WriteByte(*value)
}

func write3040U32(packet *network.Packet, enabled bool, value *uint32, flag string) error {
	if !enabled {
		if value != nil {
			return fmt.Errorf("0x3040 flag %s value supplied while flag is clear", flag)
		}
		return nil
	}
	if value == nil {
		return fmt.Errorf("0x3040 flag %s is set without a value", flag)
	}
	return packet.WriteUInt32(*value)
}
