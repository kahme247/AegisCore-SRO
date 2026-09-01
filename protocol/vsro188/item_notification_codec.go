package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// ItemUsedNotification is the exact two-u32 body consumed by the stock
// 0x305C client handler. The field names are deliberately neutral because the
// client first resolves the first value and then uses the second value as an
// item/reference lookup key.
type ItemUsedNotification struct {
	FirstValue  uint32
	SecondValue uint32
}

func BuildItemUsedNotification(notification ItemUsedNotification) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityItemUsed
	packet.WriteUInt32(notification.FirstValue)
	packet.WriteUInt32(notification.SecondValue)
	return packet
}

func DecodeItemUsedNotification(packet *network.Packet) (ItemUsedNotification, error) {
	if packet == nil {
		return ItemUsedNotification{}, fmt.Errorf("0x305C packet is nil")
	}
	notification := ItemUsedNotification{}
	var err error
	if notification.FirstValue, err = packet.ReadUInt32(); err != nil {
		return ItemUsedNotification{}, fmt.Errorf("read 0x305C first value: %w", err)
	}
	if notification.SecondValue, err = packet.ReadUInt32(); err != nil {
		return ItemUsedNotification{}, fmt.Errorf("read 0x305C second value: %w", err)
	}
	if packet.Remaining() != 0 {
		return ItemUsedNotification{}, fmt.Errorf("0x305C has %d trailing bytes", packet.Remaining())
	}
	return notification, nil
}
