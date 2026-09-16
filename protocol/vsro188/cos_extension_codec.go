package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type COSUnknownRecord struct {
	First  uint32
	Second uint32
	Third  uint32
	State  byte
}

type COSStateUpdate struct {
	UniqueID    uint32
	Mask        byte
	FirstState  byte
	SecondState byte
}

func BuildCOSUnknownTransferBegin(active bool) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUnknownUpdate0
	p.WriteBool(active)
	return p
}

func BuildCOSUnknownTransferData(data []byte) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUnknownUpdate1
	if len(data) == 0 {
		return network.Packet{}, fmt.Errorf("empty COS transfer data")
	}
	if err := p.WriteBytes(data); err != nil {
		return network.Packet{}, err
	}
	return p, nil
}

func BuildCOSUnknownRecords(records []COSUnknownRecord) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUnknownUpdate2
	if len(records) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many COS records")
	}
	p.WriteByte(byte(len(records)))
	for _, record := range records {
		p.WriteUInt32(record.First)
		p.WriteUInt32(record.Second)
		p.WriteUInt32(record.Third)
		p.WriteByte(record.State)
	}
	return p, nil
}

// BuildCOSStateUpdate mirrors sro_client.exe On30CA at 0x008A7690 and
// SR_GameServer.exe's producer at 0x004D1EA0. Only mask bits 0 and 1 are
// accepted by the vSRO 1.188 client parser.
func BuildCOSStateUpdate(update COSStateUpdate) (network.Packet, error) {
	if update.UniqueID == 0 || update.Mask == 0 || update.Mask&^byte(3) != 0 {
		return network.Packet{}, fmt.Errorf("invalid COS state update")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUpdateState
	p.WriteUInt32(update.UniqueID)
	p.WriteByte(update.Mask)
	if update.Mask&1 != 0 {
		p.WriteByte(update.FirstState)
	}
	if update.Mask&2 != 0 {
		p.WriteByte(update.SecondState)
	}
	return p, nil
}

func DecodeCOSUnknownRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid COS unknown request")
	}
	return nil
}

func BuildCOSUnknownResponseSuccess(state byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUnknownResponse
	p.WriteByte(1)
	p.WriteByte(state)
	return p
}

func BuildCOSUnknownResponseFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.COSUnknownResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}
