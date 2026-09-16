package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// ItemOperationPickupSuccess is the stock completion form for a successful
// ground-item pickup. The B034 operation serializer writes only the operation
// discriminator and the destination inventory slot for operation 0x06.
const ItemOperationPickup byte = 0x06

const ItemOperationCOSTransfer byte = 0x17

// ItemOperationServerAdd is the stock discriminator used by server-generated
// item additions, including the quest-reward and generated-item paths. Its
// success body contains two bytes after the discriminator; the second byte's
// business meaning is not resolved by the binary evidence.
const ItemOperationServerAdd byte = 0x0E

// ItemOperationServerDelete is the maintained server-side item-removal
// discriminator used by quest and alchemy material-consumption paths. Its
// success body contains the source slot followed by one opaque byte.
const ItemOperationServerDelete byte = 0x0F

const ItemOperationRelocation byte = 0x00

// ItemOperationGroundDrop is the binary-confirmed client/server discriminator
// for the normal inventory-to-ground transition.
const ItemOperationGroundDrop byte = 0x07

// ItemOperationPackageItem is the overloaded ordinary-operation discriminator
// used by the stock package-item insertion sender. Its four-byte body is
// distinct from AegisCore's maintained eight-byte NPC-buy form, but the
// executable does not establish a public business name for these fields.
const ItemOperationPackageItem byte = 0x08

// ItemOperationPackageItemExtended is stock SP_BUY_ITEM_WITH_TOKEN. The client
// selects it for five token categories and its success envelope adds the token
// debit continuation vector. It enters the same package insertion engine as
// the gold/package operation 0x08.
const ItemOperationPackageItemExtended byte = 0x2B

const (
	ItemOperationMagicCubePush   byte = 0x27
	ItemOperationMagicCubePop    byte = 0x28
	ItemOperationMagicCubeDelete byte = 0x29
)

// MagicCubeMoveRequest preserves the exact 0x7034 parser fields. Context is
// present only for operation 0x28; the binary does not establish a safer
// public name for that u32 value.
type MagicCubeMoveRequest struct {
	Context     uint32
	Source      byte
	Destination byte
	Amount      uint16
}

func BuildMagicCubeMoveRequest(operation byte, request MagicCubeMoveRequest) (network.Packet, error) {
	if operation != ItemOperationMagicCubePush && operation != ItemOperationMagicCubePop {
		return network.Packet{}, fmt.Errorf("magic-cube move operation is 0x%02X", operation)
	}
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.ItemOperationRequest
	packet.WriteByte(operation)
	if operation == ItemOperationMagicCubePop {
		packet.WriteUInt32(request.Context)
	}
	packet.WriteByte(request.Source)
	packet.WriteByte(request.Destination)
	packet.WriteUInt16(request.Amount)
	return packet, nil
}

func DecodeMagicCubeMoveRequest(packet *network.Packet) (byte, MagicCubeMoveRequest, error) {
	if packet == nil {
		return 0, MagicCubeMoveRequest{}, fmt.Errorf("magic-cube move request is nil")
	}
	operation, err := packet.ReadByte()
	if err != nil || (operation != ItemOperationMagicCubePush && operation != ItemOperationMagicCubePop) {
		return 0, MagicCubeMoveRequest{}, fmt.Errorf("read magic-cube move operation: %w", err)
	}
	request, err := DecodeMagicCubeMoveRequestBody(operation, packet)
	return operation, request, err
}

// DecodeMagicCubeMoveRequestBody decodes a request after a shared 0x7034
// dispatcher has already consumed the operation discriminator.
func DecodeMagicCubeMoveRequestBody(operation byte, packet *network.Packet) (MagicCubeMoveRequest, error) {
	if packet == nil || (operation != ItemOperationMagicCubePush && operation != ItemOperationMagicCubePop) {
		return MagicCubeMoveRequest{}, fmt.Errorf("invalid magic-cube move body operation 0x%02X", operation)
	}
	want := 4
	if operation == ItemOperationMagicCubePop {
		want = 8
	}
	if packet.Remaining() != want {
		return MagicCubeMoveRequest{}, fmt.Errorf("magic-cube operation 0x%02X has %d body bytes, want %d", operation, packet.Remaining(), want)
	}
	request := MagicCubeMoveRequest{}
	var err error
	if operation == ItemOperationMagicCubePop {
		request.Context, err = packet.ReadUInt32()
	}
	if err == nil {
		request.Source, err = packet.ReadByte()
	}
	if err == nil {
		request.Destination, err = packet.ReadByte()
	}
	if err == nil {
		request.Amount, err = packet.ReadUInt16()
	}
	if err != nil {
		return MagicCubeMoveRequest{}, fmt.Errorf("decode magic-cube operation 0x%02X: %w", operation, err)
	}
	return request, nil
}

// MagicCubeSuccess is the semantic-neutral stock 0xB034 record shared by
// operations 0x27-0x29. Operation 0x29 is response-only: the GameServer's
// 0x0050CE80 startup table installs request handlers for 0x27 and 0x28 but
// leaves the 0x29 entry null. DeleteScalar and Context are emitted only by
// 0x29; Context is also emitted first by 0x28.
type MagicCubeSuccess struct {
	Operation    byte
	DeleteScalar byte
	Context      uint32
	Source       byte
	Destination  byte
	Amount       uint16
}

func BuildMagicCubeSuccess(success MagicCubeSuccess) (network.Packet, error) {
	if success.Operation < ItemOperationMagicCubePush || success.Operation > ItemOperationMagicCubeDelete {
		return network.Packet{}, fmt.Errorf("magic-cube success operation is 0x%02X", success.Operation)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(success.Operation)
	switch success.Operation {
	case ItemOperationMagicCubePop:
		packet.WriteUInt32(success.Context)
	case ItemOperationMagicCubeDelete:
		packet.WriteByte(success.DeleteScalar)
		packet.WriteUInt32(success.Context)
	}
	packet.WriteByte(success.Source)
	packet.WriteByte(success.Destination)
	packet.WriteUInt16(success.Amount)
	return packet, nil
}

// PackageItemRequestBodySize is the exact number of bytes after operation
// 0x08 in the recovered package-item sender.
const PackageItemRequestBodySize = 4

// PackageItemRequest keeps the recovered fields neutral. The stock record
// offsets are +0x02, +0x03, and +0x06; their catalog/business identities are
// not promoted beyond the proven wire widths.
type PackageItemRequest struct {
	SelectorA byte
	SelectorB byte
	Quantity  uint16
}

// BuildPackageItemRequest writes the exact stock package-item request body.
// It is a codec/fixture helper only; it does not imply that the domain
// transaction is currently available.
func BuildPackageItemRequest(request PackageItemRequest) network.Packet {
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.ItemOperationRequest
	packet.WriteByte(ItemOperationPackageItem)
	packet.WriteByte(request.SelectorA)
	packet.WriteByte(request.SelectorB)
	packet.WriteUInt16(request.Quantity)
	return packet
}

// DecodePackageItemRequestBody decodes the bytes after an already-consumed
// operation discriminator. It rejects every trailing byte so the caller can
// safely distinguish this four-byte form from the maintained eight-byte
// economy form that shares operation 0x08.
func DecodePackageItemRequestBody(packet *network.Packet) (PackageItemRequest, error) {
	if packet == nil {
		return PackageItemRequest{}, fmt.Errorf("0x08 package-item request is nil")
	}
	if packet.Remaining() != PackageItemRequestBodySize {
		return PackageItemRequest{}, fmt.Errorf("0x08 package-item request has %d body bytes, want %d", packet.Remaining(), PackageItemRequestBodySize)
	}
	request := PackageItemRequest{}
	var err error
	if request.SelectorA, err = packet.ReadByte(); err != nil {
		return PackageItemRequest{}, fmt.Errorf("read 0x08 selector A: %w", err)
	}
	if request.SelectorB, err = packet.ReadByte(); err != nil {
		return PackageItemRequest{}, fmt.Errorf("read 0x08 selector B: %w", err)
	}
	if request.Quantity, err = packet.ReadUInt16(); err != nil {
		return PackageItemRequest{}, fmt.Errorf("read 0x08 quantity: %w", err)
	}
	return request, nil
}

// DecodePackageItemRequest decodes a complete 0x7034 packet for fixtures and
// replay tests.
func DecodePackageItemRequest(packet *network.Packet) (PackageItemRequest, error) {
	if packet == nil {
		return PackageItemRequest{}, fmt.Errorf("0x08 package-item request is nil")
	}
	operation, err := packet.ReadByte()
	if err != nil {
		return PackageItemRequest{}, fmt.Errorf("read 0x08 package-item operation: %w", err)
	}
	if operation != ItemOperationPackageItem {
		return PackageItemRequest{}, fmt.Errorf("0x08 package-item operation is 0x%02X", operation)
	}
	return DecodePackageItemRequestBody(packet)
}

// PackageItemSuccess is the confirmed success envelope for the package-item
// operation-0x08 path. ResultBytes are opaque child-vector entries; they must
// not be renamed to destination slots without stronger evidence.
type PackageItemSuccess struct {
	Operation     byte
	SelectorA     byte
	SelectorB     byte
	ResultBytes   []byte
	Quantity      uint16
	Continuations []PackageItemContinuation
}

// PackageItemContinuation is the additional child-record vector emitted only
// by stock operation 0x2B. The server serializer reads the byte at child +3
// and the u16 at child +6; their wider business meaning remains neutral.
type PackageItemContinuation struct {
	Selector byte
	Quantity uint16
}

// BuildPackageItemSuccess writes the exact package-item success grammar. It
// intentionally has no rent/item tail; that tail belongs to a different
// maintained economy serializer and is not part of this response form.
func BuildPackageItemSuccess(success PackageItemSuccess) network.Packet {
	operation := success.Operation
	if operation == 0 {
		operation = ItemOperationPackageItem
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteByte(success.SelectorA)
	packet.WriteByte(success.SelectorB)
	packet.WriteByte(byte(len(success.ResultBytes)))
	_ = packet.WriteBytes(success.ResultBytes)
	packet.WriteUInt16(success.Quantity)
	if operation == ItemOperationPackageItemExtended {
		packet.WriteByte(byte(len(success.Continuations)))
		for _, continuation := range success.Continuations {
			packet.WriteByte(continuation.Selector)
			packet.WriteUInt16(continuation.Quantity)
		}
	}
	return packet
}

// DecodePackageItemSuccess decodes the exact success body and rejects trailing
// bytes so a package result cannot be mistaken for an ordinary buy response.
func DecodePackageItemSuccess(packet *network.Packet) (PackageItemSuccess, error) {
	if packet == nil {
		return PackageItemSuccess{}, fmt.Errorf("0xB034 package-item success packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return PackageItemSuccess{}, fmt.Errorf("read package-item result: %w", err)
	}
	if result != 1 {
		return PackageItemSuccess{}, fmt.Errorf("package-item result is %d, want success result 1", result)
	}
	operation, err := packet.ReadByte()
	if err != nil {
		return PackageItemSuccess{}, fmt.Errorf("read package-item operation: %w", err)
	}
	if operation != ItemOperationPackageItem && operation != ItemOperationPackageItemExtended {
		return PackageItemSuccess{}, fmt.Errorf("package-item operation is 0x%02X", operation)
	}
	success := PackageItemSuccess{Operation: operation}
	if success.SelectorA, err = packet.ReadByte(); err != nil {
		return PackageItemSuccess{}, fmt.Errorf("read package-item selector A: %w", err)
	}
	if success.SelectorB, err = packet.ReadByte(); err != nil {
		return PackageItemSuccess{}, fmt.Errorf("read package-item selector B: %w", err)
	}
	count, err := packet.ReadByte()
	if err != nil {
		return PackageItemSuccess{}, fmt.Errorf("read package-item result-byte count: %w", err)
	}
	success.ResultBytes = make([]byte, count)
	for index := range success.ResultBytes {
		if success.ResultBytes[index], err = packet.ReadByte(); err != nil {
			return PackageItemSuccess{}, fmt.Errorf("read package-item result byte %d: %w", index, err)
		}
	}
	if success.Quantity, err = packet.ReadUInt16(); err != nil {
		return PackageItemSuccess{}, fmt.Errorf("read package-item quantity: %w", err)
	}
	if operation == ItemOperationPackageItemExtended {
		continuationCount, readErr := packet.ReadByte()
		if readErr != nil {
			return PackageItemSuccess{}, fmt.Errorf("read 0x2B continuation count: %w", readErr)
		}
		success.Continuations = make([]PackageItemContinuation, continuationCount)
		for index := range success.Continuations {
			if success.Continuations[index].Selector, err = packet.ReadByte(); err != nil {
				return PackageItemSuccess{}, fmt.Errorf("read 0x2B continuation %d selector: %w", index, err)
			}
			if success.Continuations[index].Quantity, err = packet.ReadUInt16(); err != nil {
				return PackageItemSuccess{}, fmt.Errorf("read 0x2B continuation %d quantity: %w", index, err)
			}
		}
	}
	if packet.Remaining() != 0 {
		return PackageItemSuccess{}, fmt.Errorf("package-item success has %d trailing bytes", packet.Remaining())
	}
	return success, nil
}

type GroundDropRequest struct {
	Slot byte
}

// BuildGroundDropRequest writes the complete 0x7034 body, including the
// operation discriminator. It does not mutate persistence or world state.
func BuildGroundDropRequest(request GroundDropRequest) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationRequest
	packet.WriteByte(ItemOperationGroundDrop)
	packet.WriteByte(request.Slot)
	return packet
}

func DecodeGroundDropRequest(packet *network.Packet) (GroundDropRequest, error) {
	if packet == nil {
		return GroundDropRequest{}, fmt.Errorf("0x7034 ground-drop packet is nil")
	}
	operation, err := packet.ReadByte()
	if err != nil {
		return GroundDropRequest{}, fmt.Errorf("read 0x7034 ground-drop operation: %w", err)
	}
	if operation != ItemOperationGroundDrop {
		return GroundDropRequest{}, fmt.Errorf("0x7034 operation is 0x%02X, want 0x07", operation)
	}
	slot, err := packet.ReadByte()
	if err != nil {
		return GroundDropRequest{}, fmt.Errorf("read 0x7034 ground-drop slot: %w", err)
	}
	if packet.Remaining() != 0 {
		return GroundDropRequest{}, fmt.Errorf("0x7034 ground-drop packet has %d trailing bytes", packet.Remaining())
	}
	return GroundDropRequest{Slot: slot}, nil
}

func BuildGroundDropSuccess(slot byte) network.Packet {
	return BuildItemOperationSlotSuccess(ItemOperationGroundDrop, slot)
}

func BuildItemOperationSlotSuccess(operation, slot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteByte(slot)
	return packet
}

// BuildItemOperationTwoByteSuccess writes the mode-1 B034 form used by the
// stock operation-0x0E branch: operation followed by two scalar bytes. It is
// intentionally generic because the client decoder confirms widths/order but
// not universal semantic names for these fields.
func BuildItemOperationTwoByteSuccess(operation, first, second byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteByte(first)
	packet.WriteByte(second)
	return packet
}

// BuildItemOperationServerDeleteSuccess writes the complete stock client
// decoder contract for operation 0x0F. The trailing scalar's business meaning
// is unresolved; ordinary server-side removals use its zero/default value.
func BuildItemOperationServerDeleteSuccess(slot byte) network.Packet {
	return BuildItemOperationTwoByteSuccess(ItemOperationServerDelete, slot, 0)
}

// BuildItemOperationObjectSlotSuccess writes the mode-1 completion form used
// by the stock COS-inventory operation: operation, a u32 context/object value,
// and the destination slot. The item body is not part of this B034 branch.
func BuildItemOperationObjectSlotSuccess(operation byte, objectID uint32, slot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteUInt32(objectID)
	packet.WriteByte(slot)
	return packet
}

// BuildItemOperationObjectSlotPairSuccess writes the mode-1 completion used
// by player/COS inventory transfers: operation, COS unique ID, source slot,
// and destination slot.
func BuildItemOperationObjectSlotPairSuccess(operation byte, objectID uint32, source, destination byte) network.Packet {
	packet := BuildItemOperationObjectSlotSuccess(operation, objectID, source)
	packet.WriteByte(destination)
	return packet
}

// BuildItemOperationSlotPairSuccess writes the maintained storage-style
// completion body: result, operation, source slot, and destination slot.
// The slot roles are kept neutral because the same wire shape is used by
// multiple storage domains.
func BuildItemOperationSlotPairSuccess(operation, firstSlot, secondSlot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteByte(firstSlot)
	packet.WriteByte(secondSlot)
	return packet
}

// BuildItemOperationSlotPairQuantitySuccess extends the slot-pair completion
// body with the confirmed u16 quantity field used by storage move paths.
func BuildItemOperationSlotPairQuantitySuccess(operation, firstSlot, secondSlot byte, quantity uint16) network.Packet {
	packet := BuildItemOperationSlotPairSuccess(operation, firstSlot, secondSlot)
	packet.WriteUInt16(quantity)
	return packet
}

// BuildItemOperationUInt64Success writes the maintained operation completion
// form whose value is an opaque u64 (for example a storage/guild gold path).
func BuildItemOperationUInt64Success(operation byte, value uint64) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteUInt64(value)
	return packet
}

// BuildItemOperationRelocationSuccess writes the confirmed operation-0x00
// completion with an empty child vector: operation, source slot, destination
// slot, quantity, and child count zero.
func BuildItemOperationRelocationSuccess(sourceSlot, destinationSlot byte, quantity uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(ItemOperationRelocation)
	packet.WriteByte(sourceSlot)
	packet.WriteByte(destinationSlot)
	packet.WriteUInt16(quantity)
	packet.WriteByte(0)
	return packet
}

// BuildItemOperationAvatarTransferSuccess writes the maintained avatar
// equip/unequip completion envelope: success, operation, source, destination,
// zero quantity, and the reserved byte.
func BuildItemOperationAvatarTransferSuccess(operation, sourceSlot, destinationSlot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(operation)
	packet.WriteByte(sourceSlot)
	packet.WriteByte(destinationSlot)
	packet.WriteUInt16(0)
	packet.WriteByte(0)
	return packet
}

func DecodeItemOperationSlotSuccess(packet *network.Packet, operation byte) (byte, error) {
	if packet == nil {
		return 0, fmt.Errorf("0xB034 slot success packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read 0xB034 slot success result: %w", err)
	}
	if result != 1 {
		return 0, fmt.Errorf("0xB034 slot success result is %d, want 1", result)
	}
	gotOperation, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read 0xB034 slot success operation: %w", err)
	}
	if gotOperation != operation {
		return 0, fmt.Errorf("0xB034 slot success operation is 0x%02X, want 0x%02X", gotOperation, operation)
	}
	slot, err := packet.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read 0xB034 slot success slot: %w", err)
	}
	if packet.Remaining() != 0 {
		return 0, fmt.Errorf("0xB034 slot success has %d trailing bytes", packet.Remaining())
	}
	return slot, nil
}

func DecodeItemOperationTwoByteSuccess(packet *network.Packet, operation byte) (byte, byte, error) {
	if packet == nil {
		return 0, 0, fmt.Errorf("0xB034 two-byte success packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB034 two-byte success result: %w", err)
	}
	if result != 1 {
		return 0, 0, fmt.Errorf("0xB034 two-byte success result is %d, want 1", result)
	}
	gotOperation, err := packet.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB034 two-byte success operation: %w", err)
	}
	if gotOperation != operation {
		return 0, 0, fmt.Errorf("0xB034 two-byte success operation is 0x%02X, want 0x%02X", gotOperation, operation)
	}
	first, err := packet.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB034 two-byte success first byte: %w", err)
	}
	second, err := packet.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read 0xB034 two-byte success second byte: %w", err)
	}
	if packet.Remaining() != 0 {
		return 0, 0, fmt.Errorf("0xB034 two-byte success has %d trailing bytes", packet.Remaining())
	}
	return first, second, nil
}
