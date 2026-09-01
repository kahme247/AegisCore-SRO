package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const ItemMallModernOperation byte = 0x18

// BuildItemOperationFailure is the fixed failure envelope shared by the
// stock 0xB034 item-operation family: result=2 followed by a u16 code.
func BuildItemOperationFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}

func DecodeItemOperationFailure(packet *network.Packet) (uint16, error) {
	if packet == nil {
		return 0, fmt.Errorf("0xB034 failure packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read 0xB034 failure result: %w", err)
	}
	if result != 2 {
		return 0, fmt.Errorf("0xB034 result is %d, want failure result 2", result)
	}
	code, err := packet.ReadUInt16()
	if err != nil {
		return 0, fmt.Errorf("read 0xB034 failure code: %w", err)
	}
	if packet.Remaining() != 0 {
		return 0, fmt.Errorf("0xB034 failure has %d trailing bytes", packet.Remaining())
	}
	return code, nil
}

// BalanceStateUpdate is the neutral wire representation of server opcode
// 0x3153. The stock client reads three consecutive u32 values; the binary
// evidence does not make the account-column names part of this protocol type.
type BalanceStateUpdate struct {
	First  uint32
	Second uint32
	Third  uint32
}

func BuildBalanceStateUpdate(update BalanceStateUpdate) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemMallBalanceUpdate
	packet.WriteUInt32(update.First)
	packet.WriteUInt32(update.Second)
	packet.WriteUInt32(update.Third)
	return packet
}

func DecodeBalanceStateUpdate(packet *network.Packet) (BalanceStateUpdate, error) {
	if packet == nil {
		return BalanceStateUpdate{}, fmt.Errorf("0x3153 packet is nil")
	}
	update := BalanceStateUpdate{}
	var err error
	if update.First, err = packet.ReadUInt32(); err != nil {
		return BalanceStateUpdate{}, fmt.Errorf("read 0x3153 first value: %w", err)
	}
	if update.Second, err = packet.ReadUInt32(); err != nil {
		return BalanceStateUpdate{}, fmt.Errorf("read 0x3153 second value: %w", err)
	}
	if update.Third, err = packet.ReadUInt32(); err != nil {
		return BalanceStateUpdate{}, fmt.Errorf("read 0x3153 third value: %w", err)
	}
	if packet.Remaining() != 0 {
		return BalanceStateUpdate{}, fmt.Errorf("0x3153 has %d trailing bytes", packet.Remaining())
	}
	return update, nil
}

// CashNotification is the neutral wire representation of server opcode
// 0x3154: u8 reason, u8 cash type, u32 displayed value. The stock binary
// validates reason and cash-type ranges in the producer; callers may apply
// those policy checks at the domain boundary where the values are chosen.
type CashNotification struct {
	Reason         byte
	CashType       byte
	DisplayedValue uint32
}

func BuildCashNotification(notification CashNotification) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemMallCashNotification
	packet.WriteByte(notification.Reason)
	packet.WriteByte(notification.CashType)
	packet.WriteUInt32(notification.DisplayedValue)
	return packet
}

func DecodeCashNotification(packet *network.Packet) (CashNotification, error) {
	if packet == nil {
		return CashNotification{}, fmt.Errorf("0x3154 packet is nil")
	}
	notification := CashNotification{}
	var err error
	if notification.Reason, err = packet.ReadByte(); err != nil {
		return CashNotification{}, fmt.Errorf("read 0x3154 reason: %w", err)
	}
	if notification.CashType, err = packet.ReadByte(); err != nil {
		return CashNotification{}, fmt.Errorf("read 0x3154 cash type: %w", err)
	}
	if notification.DisplayedValue, err = packet.ReadUInt32(); err != nil {
		return CashNotification{}, fmt.Errorf("read 0x3154 displayed value: %w", err)
	}
	if packet.Remaining() != 0 {
		return CashNotification{}, fmt.Errorf("0x3154 has %d trailing bytes", packet.Remaining())
	}
	return notification, nil
}

// ItemMallModernPurchaseSuccess is the operation-0x18 success body inside a
// stock 0xB034 response. In the confirmed response mode, the client reads the
// destination/result byte vector and trailing u16, then stops; it does not
// read a variable rent/item body after that point.
type ItemMallModernPurchaseSuccess struct {
	ShopGroupID  uint16
	GroupIndex   byte
	TabIndex     byte
	SlotIndex    byte
	ResultBytes  []byte
	PackageCount uint16
}

// ItemMallLegacyPurchaseSuccess is the fixed selector/slot/count envelope
// used by the ordinary operation-0x12 response path.
type ItemMallLegacyPurchaseSuccess struct {
	Operation        byte
	MallVersion      byte
	Page             byte
	GroupIndex       byte
	TabIndex         byte
	SlotIndex        byte
	DestinationSlots []byte
	Quantity         uint16
}

func BuildItemMallLegacyPurchaseSuccess(success ItemMallLegacyPurchaseSuccess) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(success.Operation)
	packet.WriteByte(success.MallVersion)
	packet.WriteByte(success.Page)
	packet.WriteByte(success.GroupIndex)
	packet.WriteByte(success.TabIndex)
	packet.WriteByte(success.SlotIndex)
	packet.WriteByte(byte(len(success.DestinationSlots)))
	_ = packet.WriteBytes(success.DestinationSlots)
	packet.WriteUInt16(success.Quantity)
	return packet
}

// BuildItemMallModernPurchaseSuccess writes the confirmed response envelope:
// result=1, operation=0x18, shop/group/tab/slot selectors, result-byte count,
// result bytes, and trailing package/count u16. No variable item body follows
// this mode-zero response grammar.
func BuildItemMallModernPurchaseSuccess(success ItemMallModernPurchaseSuccess) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(ItemMallModernOperation)
	packet.WriteUInt16(success.ShopGroupID)
	packet.WriteByte(success.GroupIndex)
	packet.WriteByte(success.TabIndex)
	packet.WriteByte(success.SlotIndex)
	packet.WriteByte(byte(len(success.ResultBytes)))
	for _, value := range success.ResultBytes {
		packet.WriteByte(value)
	}
	packet.WriteUInt16(success.PackageCount)
	return packet
}

// DecodeItemMallModernPurchaseSuccess decodes the confirmed mode-zero
// operation-0x18 response grammar. The client consumes no bytes after the
// trailing package/count u16, so trailing rent/item data is rejected rather
// than silently assigned to a different packet.
func DecodeItemMallModernPurchaseSuccess(packet *network.Packet) (ItemMallModernPurchaseSuccess, error) {
	if packet == nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("0xB034 operation-0x18 packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 result: %w", err)
	}
	if result != 1 {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("0x18 result is %d, want success result 1", result)
	}
	operation, err := packet.ReadByte()
	if err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 operation: %w", err)
	}
	if operation != ItemMallModernOperation {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("0x18 operation is 0x%02X", operation)
	}
	success := ItemMallModernPurchaseSuccess{}
	if success.ShopGroupID, err = packet.ReadUInt16(); err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 shop group: %w", err)
	}
	if success.GroupIndex, err = packet.ReadByte(); err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 group index: %w", err)
	}
	if success.TabIndex, err = packet.ReadByte(); err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 tab index: %w", err)
	}
	if success.SlotIndex, err = packet.ReadByte(); err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 slot index: %w", err)
	}
	count, err := packet.ReadByte()
	if err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 result-byte count: %w", err)
	}
	success.ResultBytes = make([]byte, count)
	for index := range success.ResultBytes {
		if success.ResultBytes[index], err = packet.ReadByte(); err != nil {
			return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 result byte %d: %w", index, err)
		}
	}
	if success.PackageCount, err = packet.ReadUInt16(); err != nil {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("read 0x18 trailing package count: %w", err)
	}
	if packet.Remaining() != 0 {
		return ItemMallModernPurchaseSuccess{}, fmt.Errorf("0x18 response has %d trailing bytes", packet.Remaining())
	}
	return success, nil
}

// ItemMallModernRequest is the operation-0x18 payload carried inside the
// stock 0x7034 item-operation request. The two u32 fields are deliberately
// retained as one u64: the client serializer writes record +0xB0 as eight
// contiguous bytes. Their business meaning is not assigned here.
//
// Wire order:
//
//	u16 shopGroup
//	u8  groupIndex
//	u8  tabIndex
//	u8  slotIndex
//	str packageCodeName
//	u16 packageCount
//	u64 opaqueValue
//	u32 packageID
type ItemMallModernRequest struct {
	ShopGroupID  uint16
	GroupIndex   byte
	TabIndex     byte
	SlotIndex    byte
	PackageName  string
	PackageCount uint16
	OpaqueValue  uint64
	PackageID    uint32
}

// DecodeItemMallModernRequest decodes the binary-confirmed operation-0x18
// request body. Catalog ownership, price, currency, and package validity stay
// in the application/repository layer; this function only owns wire shape.
func DecodeItemMallModernRequest(packet *network.Packet) (ItemMallModernRequest, error) {
	if packet == nil {
		return ItemMallModernRequest{}, fmt.Errorf("item-mall operation 0x18 packet is nil")
	}
	request := ItemMallModernRequest{}
	var err error
	if request.ShopGroupID, err = packet.ReadUInt16(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall shop group: %w", err)
	}
	if request.GroupIndex, err = packet.ReadByte(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall group index: %w", err)
	}
	if request.TabIndex, err = packet.ReadByte(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall tab index: %w", err)
	}
	if request.SlotIndex, err = packet.ReadByte(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall slot index: %w", err)
	}
	if request.PackageName, err = packet.ReadString(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall package name: %w", err)
	}
	if request.PackageCount, err = packet.ReadUInt16(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall package count: %w", err)
	}
	if request.OpaqueValue, err = packet.ReadUInt64(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall opaque value: %w", err)
	}
	if request.PackageID, err = packet.ReadUInt32(); err != nil {
		return ItemMallModernRequest{}, fmt.Errorf("read item-mall package ID: %w", err)
	}
	if packet.Remaining() != 0 {
		return ItemMallModernRequest{}, fmt.Errorf("item-mall operation 0x18 has %d trailing bytes", packet.Remaining())
	}
	return request, nil
}

// BuildItemMallModernRequest is primarily useful for protocol fixtures and
// client-facing compatibility tests. It emits only the operation body; the
// caller owns the surrounding 0x7034 opcode and operation discriminator.
func BuildItemMallModernRequest(request ItemMallModernRequest) (network.Packet, error) {
	packet := network.EmptyPacket()
	if err := packet.WriteUInt16(request.ShopGroupID); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteByte(request.GroupIndex); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteByte(request.TabIndex); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteByte(request.SlotIndex); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteString(request.PackageName); err != nil {
		return network.Packet{}, fmt.Errorf("write item-mall package name: %w", err)
	}
	if err := packet.WriteUInt16(request.PackageCount); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt64(request.OpaqueValue); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt32(request.PackageID); err != nil {
		return network.Packet{}, err
	}
	return packet, packet.WriteError()
}
