package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const ItemOperationSpecialty byte = 0x2B

// SpecialtyRequest records the operation-0x2B package-selection fields.
// Stock GameServer 0x005266A0 uses FirstSelector to index the active package
// vector and SecondSelector to select a member through 0x00616780. Neutral API
// names are retained because the stock package catalog is not yet maintained.
type SpecialtyRequest struct {
	FirstSelector  byte
	SecondSelector byte
	Quantity       uint16
}

func BuildSpecialtyRequest(request SpecialtyRequest) network.Packet {
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.ItemOperationRequest
	packet.WriteByte(ItemOperationSpecialty)
	packet.WriteByte(request.FirstSelector)
	packet.WriteByte(request.SecondSelector)
	packet.WriteUInt16(request.Quantity)
	return packet
}

func DecodeSpecialtyRequest(packet *network.Packet) (SpecialtyRequest, error) {
	if packet == nil {
		return SpecialtyRequest{}, fmt.Errorf("0x2B request is nil")
	}
	operation, err := packet.ReadByte()
	if err != nil {
		return SpecialtyRequest{}, fmt.Errorf("read 0x2B operation: %w", err)
	}
	if operation != ItemOperationSpecialty {
		return SpecialtyRequest{}, fmt.Errorf("0x2B request operation is 0x%02X", operation)
	}
	request := SpecialtyRequest{}
	if request.FirstSelector, err = packet.ReadByte(); err != nil {
		return SpecialtyRequest{}, fmt.Errorf("read 0x2B first selector: %w", err)
	}
	if request.SecondSelector, err = packet.ReadByte(); err != nil {
		return SpecialtyRequest{}, fmt.Errorf("read 0x2B second selector: %w", err)
	}
	if request.Quantity, err = packet.ReadUInt16(); err != nil {
		return SpecialtyRequest{}, fmt.Errorf("read 0x2B quantity: %w", err)
	}
	if packet.Remaining() != 0 {
		return SpecialtyRequest{}, fmt.Errorf("0x2B request has %d trailing bytes", packet.Remaining())
	}
	return request, nil
}

// SpecialtySuccess is the fixed operation-0x2B success envelope. The final
// child payload is intentionally opaque: the recovered serializer proves a
// child count and child records, but not stable public field widths/names for
// those records.
type SpecialtySuccess struct {
	FirstSelector     byte
	SecondSelector    byte
	DestinationSlots  []byte
	Quantity          uint16
	ExtraChildCount   byte
	ExtraChildPayload []byte
}

func BuildSpecialtySuccess(success SpecialtySuccess) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(ItemOperationSpecialty)
	packet.WriteByte(success.FirstSelector)
	packet.WriteByte(success.SecondSelector)
	packet.WriteByte(byte(len(success.DestinationSlots)))
	_ = packet.WriteBytes(success.DestinationSlots)
	packet.WriteUInt16(success.Quantity)
	packet.WriteByte(success.ExtraChildCount)
	_ = packet.WriteBytes(success.ExtraChildPayload)
	return packet
}
