package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	AlchemyActionCancel byte = 1
	AlchemyActionFuse   byte = 2
)

// AlchemyStagedRequest is the stable request prefix shared by the recovered
// 0x7150/0x7151 families: action, type, count, and count inventory slots.
// Type-specific validation remains in the handler/repository layer.
type AlchemyStagedRequest struct {
	Action byte
	Type   byte
	Slots  []byte
}

func DecodeAlchemyStagedRequest(packet *network.Packet) (AlchemyStagedRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return AlchemyStagedRequest{}, fmt.Errorf("alchemy staged request is empty")
	}
	request := AlchemyStagedRequest{}
	var err error
	if request.Action, err = packet.ReadByte(); err != nil {
		return AlchemyStagedRequest{}, err
	}
	if request.Action != AlchemyActionFuse {
		if packet.Remaining() != 0 {
			return AlchemyStagedRequest{}, fmt.Errorf("alchemy cancel request has trailing bytes")
		}
		return request, nil
	}
	if request.Type, err = packet.ReadByte(); err != nil {
		return AlchemyStagedRequest{}, fmt.Errorf("read alchemy type: %w", err)
	}
	count, err := packet.ReadByte()
	if err != nil {
		return AlchemyStagedRequest{}, fmt.Errorf("read alchemy slot count: %w", err)
	}
	if packet.Remaining() != int(count) || count == 0 {
		return AlchemyStagedRequest{}, fmt.Errorf("alchemy slot count %d does not match payload", count)
	}
	request.Slots = make([]byte, count)
	for index := range request.Slots {
		request.Slots[index], err = packet.ReadByte()
		if err != nil {
			return AlchemyStagedRequest{}, fmt.Errorf("read alchemy slot %d: %w", index, err)
		}
	}
	return request, nil
}

func BuildAlchemyCancel(messageID uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	packet.WriteByte(1)
	packet.WriteByte(AlchemyActionCancel)
	return packet
}

func BuildAlchemyFailure(messageID uint16, code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}

func BuildAlchemyMaterialResult(responseByte byte, producedValue uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemyMaterialResponse
	packet.WriteByte(1)
	packet.WriteByte(AlchemyActionFuse)
	packet.WriteByte(responseByte)
	packet.WriteUInt32(producedValue)
	return packet
}

func BuildAlchemyDismantleSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemyDismantleResponse
	packet.WriteByte(1)
	return packet
}

// AlchemyTargetPayload is the variable TypeID-driven item body produced by
// the domain serializer. The response envelope is binary-confirmed here;
// keeping the body opaque prevents this protocol package from importing the
// domain model and creating an import cycle.
type AlchemyTargetPayload []byte

// BuildAlchemyElixirResult writes the confirmed 0xB150 response envelope.
// The target body is omitted for a destroyed result and otherwise appended
// unchanged after the fixed prefix.
func BuildAlchemyElixirResult(success bool, targetSlot byte, destroyed bool, target AlchemyTargetPayload) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemyElixirResponse
	packet.WriteByte(1)
	packet.WriteByte(AlchemyActionFuse)
	packet.WriteBool(success)
	packet.WriteByte(targetSlot)
	if !success {
		packet.WriteBool(destroyed)
	}
	if !destroyed {
		_ = packet.WriteBytes(target)
	}
	return packet
}

// BuildAlchemyStoneResult writes the confirmed 0xB151 stone-result envelope.
func BuildAlchemyStoneResult(success bool, targetSlot byte, target AlchemyTargetPayload) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemyStoneResponse
	packet.WriteByte(1)
	packet.WriteByte(AlchemyActionFuse)
	packet.WriteBool(success)
	packet.WriteByte(targetSlot)
	_ = packet.WriteBytes(target)
	return packet
}

// BuildAlchemySocketActionResult writes the confirmed socket action envelope.
// The action discriminator is retained because socket create/insert/erase
// share the response opcode but have different client continuations.
func BuildAlchemySocketActionResult(action, targetSlot byte, target AlchemyTargetPayload) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemySocketResponse
	packet.WriteByte(1)
	packet.WriteByte(action)
	packet.WriteByte(targetSlot)
	_ = packet.WriteBytes(target)
	return packet
}

// BuildAlchemySocketResult preserves the short socket-completion envelope
// used by the legacy socket path that carries no target body.
func BuildAlchemySocketResult() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemySocketResponse
	packet.WriteByte(1)
	packet.WriteByte(AlchemyActionFuse)
	return packet
}

// BuildAlchemySocketFailure preserves the socket family's action-bearing
// failure envelope (02, action=02, u16 code).
func BuildAlchemySocketFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemySocketResponse
	packet.WriteByte(2)
	packet.WriteByte(AlchemyActionFuse)
	packet.WriteUInt16(code)
	return packet
}

func BuildAlchemyAvatarResult(mutated bool) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemyAvatarResponse
	packet.WriteByte(1)
	packet.WriteByte(AlchemyActionFuse)
	packet.WriteBool(mutated)
	return packet
}

func BuildAlchemyAvatarGrantResult(mutated bool, slot byte, target AlchemyTargetPayload) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AlchemyAvatarGrantResponse
	packet.WriteByte(1)
	packet.WriteBool(mutated)
	if mutated {
		packet.WriteByte(slot)
		_ = packet.WriteBytes(target)
	}
	return packet
}
