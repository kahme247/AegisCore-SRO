package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// InventorySizeUpdate is the binary-confirmed wire grammar for 0x3092.
//
// The stock server writes a target byte followed by the resulting capacity.
// Target 1 updates the player inventory and target 2 updates storage.
type InventorySizeUpdate struct {
	FirstByte  byte // 1 = player inventory, 2 = storage
	SecondByte byte // resulting capacity
}

func BuildInventorySizeUpdate(update InventorySizeUpdate) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventorySizeUpdate
	packet.WriteByte(update.FirstByte)
	packet.WriteByte(update.SecondByte)
	return packet
}

func DecodeInventorySizeUpdate(packet *network.Packet) (InventorySizeUpdate, error) {
	if packet == nil {
		return InventorySizeUpdate{}, fmt.Errorf("0x3092 packet is nil")
	}
	update := InventorySizeUpdate{}
	var err error
	if update.FirstByte, err = packet.ReadByte(); err != nil {
		return InventorySizeUpdate{}, fmt.Errorf("read 0x3092 first byte: %w", err)
	}
	if update.SecondByte, err = packet.ReadByte(); err != nil {
		return InventorySizeUpdate{}, fmt.Errorf("read 0x3092 second byte: %w", err)
	}
	if packet.Remaining() != 0 {
		return InventorySizeUpdate{}, fmt.Errorf("0x3092 has %d trailing bytes", packet.Remaining())
	}
	return update, nil
}
