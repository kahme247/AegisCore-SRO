package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// ItemUseRequest is the wire body carried by client opcode 0x704C. The
// optional selector is used by the recovered reverse-scroll forms; the
// handler decides whether a selected item is allowed to use it.
type ItemUseRequest struct {
	InventorySlot byte
	ItemType      uint16
	HasOption     bool
	Option        byte
	MapID         uint32
	// ItemData preserves item-family-specific bytes following the common
	// slot/type prefix. COS consumables use this for their target/object data.
	// Known reverse-scroll option forms are decoded into Option and MapID.
	ItemData       []byte
	HasCOSTarget   bool
	TargetObjectID uint32
	TargetRegion   uint16
	TargetX        uint32
	TargetY        uint32
	TargetZ        uint32
}

type ItemUseSuccess struct {
	SourceSlot byte
	NewAmount  uint16
	ItemType   uint16
}

func BuildItemUseSuccess(success ItemUseSuccess) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryItemUseResponse
	packet.WriteByte(1)
	packet.WriteByte(success.SourceSlot)
	packet.WriteUInt16(success.NewAmount)
	packet.WriteUInt16(success.ItemType)
	return packet
}

func BuildItemUseFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.InventoryItemUseResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}

func DecodeItemUseRequest(packet *network.Packet) (ItemUseRequest, error) {
	if packet == nil {
		return ItemUseRequest{}, fmt.Errorf("0x704C packet is nil")
	}
	request := ItemUseRequest{}
	var err error
	if request.InventorySlot, err = packet.ReadByte(); err != nil {
		return ItemUseRequest{}, fmt.Errorf("read 0x704C inventory slot: %w", err)
	}
	if request.ItemType, err = packet.ReadUInt16(); err != nil {
		return ItemUseRequest{}, fmt.Errorf("read 0x704C item type: %w", err)
	}
	remaining := packet.Remaining()
	if remaining == 0 {
		return request, nil
	}
	request.ItemData = make([]byte, remaining)
	for i := range request.ItemData {
		if request.ItemData[i], err = packet.ReadByte(); err != nil {
			return ItemUseRequest{}, fmt.Errorf("read 0x704C item data: %w", err)
		}
	}
	// Reverse-scroll selectors are the only currently confirmed one- and
	// five-byte forms. Other lengths are item-family payloads and must remain
	// opaque until the authoritative inventory reference is available.
	if remaining == 1 && (request.ItemData[0] == 2 || request.ItemData[0] == 3) {
		request.HasOption = true
		request.Option = request.ItemData[0]
		request.ItemData = nil
	} else if remaining == 5 && request.ItemData[0] == 7 {
		request.HasOption = true
		request.Option = 7
		request.MapID = uint32(request.ItemData[1]) | uint32(request.ItemData[2])<<8 |
			uint32(request.ItemData[3])<<16 | uint32(request.ItemData[4])<<24
		request.ItemData = nil
	} else if remaining == 1 {
		// TypeID4 12 and 15 COS service items carry one inventory-slot selector.
		// The codec cannot classify that byte without the authoritative item
		// reference, so preserve it for the item-use handler.
		return request, nil
	} else if remaining == 4 || remaining == 18 {
		request.HasCOSTarget = true
		request.TargetObjectID = uint32(request.ItemData[0]) | uint32(request.ItemData[1])<<8 |
			uint32(request.ItemData[2])<<16 | uint32(request.ItemData[3])<<24
		if remaining == 18 {
			request.TargetRegion = uint16(request.ItemData[4]) | uint16(request.ItemData[5])<<8
			request.TargetX = uint32(request.ItemData[6]) | uint32(request.ItemData[7])<<8 |
				uint32(request.ItemData[8])<<16 | uint32(request.ItemData[9])<<24
			request.TargetY = uint32(request.ItemData[10]) | uint32(request.ItemData[11])<<8 |
				uint32(request.ItemData[12])<<16 | uint32(request.ItemData[13])<<24
			request.TargetZ = uint32(request.ItemData[14]) | uint32(request.ItemData[15])<<8 |
				uint32(request.ItemData[16])<<16 | uint32(request.ItemData[17])<<24
		}
	} else {
		return ItemUseRequest{}, fmt.Errorf("unsupported 0x704C item-data length %d", remaining)
	}
	return request, nil
}

func BuildItemUseRequest(request ItemUseRequest) (network.Packet, error) {
	packet := network.EmptyPacket()
	if err := packet.WriteByte(request.InventorySlot); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteUInt16(request.ItemType); err != nil {
		return network.Packet{}, err
	}
	if len(request.ItemData) != 0 {
		if request.HasOption {
			return network.Packet{}, fmt.Errorf("0x704C request has both option and item data")
		}
		for _, value := range request.ItemData {
			if err := packet.WriteByte(value); err != nil {
				return network.Packet{}, err
			}
		}
	} else if request.HasOption {
		if request.Option != 2 && request.Option != 3 && request.Option != 7 {
			return network.Packet{}, fmt.Errorf("unsupported 0x704C option %d", request.Option)
		}
		if err := packet.WriteByte(request.Option); err != nil {
			return network.Packet{}, err
		}
		if request.Option == 7 {
			if err := packet.WriteUInt32(request.MapID); err != nil {
				return network.Packet{}, err
			}
		}
	}
	return packet, packet.WriteError()
}
