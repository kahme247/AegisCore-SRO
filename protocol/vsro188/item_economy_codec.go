package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// BuildItemBuySuccess writes the confirmed ordinary operation-0x08 success
// envelope. RentPayload is supplied by the domain item serializer because
// rent-field interpretation belongs to the maintained item model.
func BuildItemBuySuccess(tab, slot byte, amount uint16, destinations []byte, rentPayload []byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(0x08)
	packet.WriteByte(tab)
	packet.WriteByte(slot)
	packet.WriteByte(byte(len(destinations)))
	_ = packet.WriteBytes(destinations)
	packet.WriteUInt16(amount)
	_ = packet.WriteBytes(rentPayload)
	return packet
}

// BuildItemSellSuccess writes the maintained ordinary operation-0x09 result
// envelope, including the NPC context and buyback destination slot.
func BuildItemSellSuccess(source byte, amount uint16, npcUniqueID uint32, buybackSlot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(0x09)
	packet.WriteByte(source)
	packet.WriteUInt16(amount)
	packet.WriteUInt32(npcUniqueID)
	packet.WriteByte(buybackSlot)
	return packet
}

// BuildItemBuyBackSuccess preserves AegisCore's maintained buyback response
// envelope. Stock evidence treats updater operation 0x22 separately from the
// ordinary client request table, so this codec does not claim a stock sender
// for it.
func BuildItemBuyBackSuccess(destination, source byte, amount uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(0x22)
	packet.WriteByte(destination)
	packet.WriteByte(source)
	packet.WriteUInt16(amount)
	return packet
}

// BuildNPCBuyBackResponse writes the separate NPC buyback acknowledgement
// that precedes the maintained item-operation completion.
func BuildNPCBuyBackResponse(success bool) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.NPCBuyBackResponse
	if success {
		packet.WriteByte(1)
	} else {
		packet.WriteByte(2)
	}
	return packet
}
