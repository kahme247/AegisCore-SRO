package vsro188

import (
	"errors"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

var ErrInvalidCOSSellPayload = errors.New("invalid COS sell payload")

const (
	SpecialtyErrorTraderOnly   uint16 = 0x1860
	SpecialtyErrorNoCart       uint16 = 0x1861
	SpecialtyErrorNotBuyer     uint16 = 0x1863
	SpecialtyErrorCartTooFar   uint16 = 0x1865
	SpecialtyErrorStolen       uint16 = 0x1868
	NPCBuyErrorInvalidQuantity uint16 = 0x1829
)

// BuildItemBuySuccess writes the complete ordinary operation-0x08 success
// envelope consumed by the stock client.
func BuildItemBuySuccess(tab, slot byte, amount uint16, destinations []byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(0x08)
	packet.WriteByte(tab)
	packet.WriteByte(slot)
	packet.WriteByte(byte(len(destinations)))
	_ = packet.WriteBytes(destinations)
	packet.WriteUInt16(amount)
	// The stock client consumes this fixed u32 after the operation-0x08
	// completion record. Omitting it leaves its 0xB034 reader at EOF.
	packet.WriteUInt32(0)
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

// COSSellRequest is the stock mode-0 operation-0x14 body. ObjectUniqueID is
// the active COS resolved by SR_GameServer 0x005267F0.
type COSSellRequest struct {
	ObjectUniqueID uint32
	Source         byte
	Amount         uint16
	NPCUniqueID    uint32
}

func DecodeCOSSellRequest(packet *network.Packet) (COSSellRequest, error) {
	if packet == nil || packet.Remaining() != 11 {
		return COSSellRequest{}, ErrInvalidCOSSellPayload
	}
	objectID, e1 := packet.ReadUInt32()
	source, e2 := packet.ReadByte()
	amount, e3 := packet.ReadUInt16()
	npcID, e4 := packet.ReadUInt32()
	if err := errors.Join(e1, e2, e3, e4); err != nil || objectID == 0 || amount == 0 || npcID == 0 || packet.Remaining() != 0 {
		return COSSellRequest{}, ErrInvalidCOSSellPayload
	}
	return COSSellRequest{ObjectUniqueID: objectID, Source: source, Amount: amount, NPCUniqueID: npcID}, nil
}

func BuildCOSSellSuccess(request COSSellRequest, buybackSlot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(0x14)
	packet.WriteUInt32(request.ObjectUniqueID)
	packet.WriteByte(request.Source)
	packet.WriteUInt16(request.Amount)
	packet.WriteUInt32(request.NPCUniqueID)
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
