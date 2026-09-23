package inventory

import (
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func buildBuySuccess(tab, slot byte, amount uint16, destinations []byte, item model.Item) network.Packet {
	rent := network.EmptyPacket()
	model.WriteRentInfo(&rent, item)
	return vsro188.BuildItemBuySuccess(tab, slot, amount, destinations, rent.Payload())
}

func buildSellSuccess(source byte, amount uint16, npcUID uint32, buybackSlot byte) network.Packet {
	return vsro188.BuildItemSellSuccess(source, amount, npcUID, buybackSlot)
}

func buildGoldUpdate(gold uint64) network.Packet {
	return vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold})
}

func buildGoldPickupUpdate(gold uint64) network.Packet {
	return vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold, TrailingFlag: 1})
}

func buildStorageGoldSuccess(operation ItemInventoryOperation, amount, characterGold uint64) network.Packet {
	value := amount
	if operation == DepositStorageGoldOperation {
		value = characterGold
	}
	return vsro188.BuildItemOperationUInt64Success(byte(operation), value)
}

func buildBuyBackSuccess(destination, source byte, amount uint16) network.Packet {
	return vsro188.BuildItemBuyBackSuccess(destination, source, amount)
}

func buildStorageTransferSuccess(operation ItemInventoryOperation, source, destination byte) network.Packet {
	return vsro188.BuildItemOperationSlotPairSuccess(byte(operation), source, destination)
}

func buildStorageMoveSuccess(operation ItemInventoryOperation, source, destination byte, amount uint16) network.Packet {
	return vsro188.BuildItemOperationSlotPairQuantitySuccess(byte(operation), source, destination, amount)
}

func buildGuildStorageGoldSuccess(operation ItemInventoryOperation, amount uint64) network.Packet {
	return vsro188.BuildItemOperationUInt64Success(byte(operation), amount)
}

func buildUnsupportedItemOperationResponse() network.Packet {
	return vsro188.BuildItemOperationFailure(0)
}

func buildItemMallBalanceUpdate(balances model.ItemMallBalances) network.Packet {
	return vsro188.BuildBalanceStateUpdate(vsro188.BalanceStateUpdate{
		First:  balances.Silk,
		Second: balances.GiftSilk,
		Third:  balances.Point,
	})
}

func buildItemMallPurchaseSuccess(request itemMallPurchaseRequest, result model.ItemMallPurchaseResult) network.Packet {
	if request.operation == BuyItemFromModernMallOperation {
		itemPayload := network.EmptyPacket()
		for range result.DestinationSlots {
			model.WriteRentInfo(&itemPayload, result.Item)
		}
		return vsro188.BuildItemMallModernPurchaseSuccess(vsro188.ItemMallModernPurchaseSuccess{
			ShopGroupID:  result.Position.RefShopGroupID,
			GroupIndex:   result.Position.GroupIndex,
			TabIndex:     result.Position.TabIndex,
			SlotIndex:    result.Position.SlotIndex,
			ResultBytes:  result.DestinationSlots,
			PackageCount: request.purchaseCount,
			ItemPayload:  itemPayload.Payload(),
		})
	}
	if request.operation == BuyItemFromMallOperation {
		return vsro188.BuildItemMallLegacyPurchaseSuccess(vsro188.ItemMallLegacyPurchaseSuccess{
			Operation:        byte(request.operation),
			MallVersion:      request.mallVersion,
			Page:             request.page,
			GroupIndex:       result.Position.GroupIndex,
			TabIndex:         result.Position.TabIndex,
			SlotIndex:        result.Position.SlotIndex,
			DestinationSlots: result.DestinationSlots,
			Quantity:         result.Quantity,
		})
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(byte(request.operation))
	packet.WriteUInt16(result.Position.RefShopGroupID)
	packet.WriteByte(result.Position.GroupIndex)
	packet.WriteByte(result.Position.TabIndex)
	packet.WriteByte(result.Position.SlotIndex)
	packet.WriteByte(byte(len(result.DestinationSlots)))
	for _, slot := range result.DestinationSlots {
		packet.WriteByte(slot)
	}
	// The mall response echoes the number of packages requested. The package's
	// delivered item quantity is already defined by the client Media catalog;
	// sending that expanded quantity here makes the client walk past the
	// destination list for multi-item packages.
	packet.WriteUInt16(request.purchaseCount)
	for range result.DestinationSlots {
		model.WriteRentInfo(&packet, result.Item)
	}
	return packet
}

func parseMoveItemRequest(packet *network.Packet) (moveItemRequest, error) {
	if packet == nil {
		return moveItemRequest{}, errors.New("inventory move request packet is nil")
	}
	sourceSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read inventory move source slot: %w", err)
	}
	targetSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read inventory move target slot: %w", err)
	}
	amount, err := packet.ReadUInt16()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read inventory move amount: %w", err)
	}
	if packet.Remaining() != 0 {
		return moveItemRequest{}, fmt.Errorf("inventory move request has %d trailing bytes", packet.Remaining())
	}
	return moveItemRequest{sourceSlot: sourceSlot, targetSlot: targetSlot, amount: amount}, nil
}

func parseAvatarTransferRequest(packet *network.Packet) (moveItemRequest, error) {
	if packet == nil {
		return moveItemRequest{}, errors.New("avatar transfer request packet is nil")
	}
	sourceSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read avatar transfer source slot: %w", err)
	}
	targetSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read avatar transfer target slot: %w", err)
	}
	// vSRO 1.188 sends only the two slots. Accept the older four-byte form
	// only when its unused quantity is zero.
	if packet.Remaining() == 2 {
		amount, readErr := packet.ReadUInt16()
		if readErr != nil || amount != 0 {
			return moveItemRequest{}, errors.New("avatar transfer quantity must be zero")
		}
	}
	if packet.Remaining() != 0 {
		return moveItemRequest{}, fmt.Errorf("avatar transfer request has %d trailing bytes", packet.Remaining())
	}
	return moveItemRequest{sourceSlot: sourceSlot, targetSlot: targetSlot}, nil
}
