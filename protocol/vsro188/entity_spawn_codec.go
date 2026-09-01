package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// EntityGroupSpawnBegin is the fixed control prefix consumed by the stock
// 0x3017 handler at 0x008AC0E0. Object records are carried separately in one
// or more 0x3019 packets.
type EntityGroupSpawnBegin struct {
	Action byte
	Count  uint16
}

func DecodeEntityGroupSpawnBegin(packet network.Packet) (EntityGroupSpawnBegin, error) {
	var begin EntityGroupSpawnBegin
	var err error
	if begin.Action, err = packet.ReadByte(); err != nil {
		return EntityGroupSpawnBegin{}, fmt.Errorf("read 0x3017 action: %w", err)
	}
	if begin.Action != 1 && begin.Action != 2 {
		return EntityGroupSpawnBegin{}, fmt.Errorf("0x3017 action %d is not spawn(1) or remove(2)", begin.Action)
	}
	if begin.Count, err = packet.ReadUInt16(); err != nil {
		return EntityGroupSpawnBegin{}, fmt.Errorf("read 0x3017 object count: %w", err)
	}
	if packet.Remaining() != 0 {
		return EntityGroupSpawnBegin{}, fmt.Errorf("0x3017 has %d trailing bytes", packet.Remaining())
	}
	return begin, nil
}

func BuildEntityGroupSpawnBegin(begin EntityGroupSpawnBegin) (network.Packet, error) {
	if begin.Action != 1 && begin.Action != 2 {
		return network.Packet{}, fmt.Errorf("0x3017 action %d is not spawn(1) or remove(2)", begin.Action)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityGroupSpawnBegin
	packet.WriteByte(begin.Action)
	packet.WriteUInt16(begin.Count)
	if err := packet.WriteError(); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}
