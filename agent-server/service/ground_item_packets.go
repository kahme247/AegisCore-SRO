package service

import (
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// BuildPartyDistributionPacket implements the recovered 0x3068 payload. The
// common prefix is memberInfo.JID followed by RefItemID; only equipment and
// ETC items carry the branch-specific suffix observed by the client.
func BuildPartyDistributionPacket(memberUID uint32, item *model.Item) (network.Packet, error) {
	if memberUID == 0 || item == nil || item.GetRefObjectID() == 0 {
		return network.Packet{}, ErrGroundItemInvalid
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyDistribution
	packet.WriteUInt32(memberUID)
	packet.WriteUInt32(item.GetRefObjectID())
	typeInfo := item.GetTypeInfo()
	switch {
	case typeInfo.TypeID1 != model.TypeID1Item:
		// The recovered common prefix is still valid for non-item references;
		// no branch-specific bytes are present.
	case typeInfo.IsGold():
		// The 0x3068 client parser consumes an eight-byte currency amount even
		// though the persisted ground-gold authority is the u32 Item.Data field.
		packet.WriteUInt64(uint64(item.Data))
	case typeInfo.TypeID2 == model.TypeID2Wearable:
		packet.WriteByte(item.OptLevel)
	case typeInfo.TypeID2 == 2:
		// COS item distribution has no message-specific suffix.
	case typeInfo.TypeID2 == 3:
		quantity := item.StackSize
		if quantity < 0 {
			quantity = 0
		}
		if quantity > 0xFFFF {
			quantity = 0xFFFF
		}
		packet.WriteUInt16(uint16(quantity))
	}
	if err := packet.WriteError(); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}
