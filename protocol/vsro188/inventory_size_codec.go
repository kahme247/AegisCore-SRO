package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// InventorySizeUpdate is the binary-confirmed wire grammar for 0x3092.
//
// The stock server writes two consecutive bytes and the stock client reads
// them as two consecutive bytes. Their domain meaning is intentionally not
// assigned here: the recovered code does not establish stable names for
// either field.
type InventorySizeUpdate struct {
	FirstByte  byte
	SecondByte byte
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
