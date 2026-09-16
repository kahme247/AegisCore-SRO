package vsro188

import (
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const MaxOperatorRequestPayload = 1024

type OperatorRequest struct {
	Command uint16
	Payload []byte
}

func PackSRODateMinute(value time.Time) uint32 {
	value = value.UTC()
	year := value.Year() - 2000
	if year < 0 {
		year = 0
	} else if year > 0xFFF {
		year = 0xFFF
	}
	return uint32(year)<<20 | uint32(value.Month())<<16 | uint32(value.Day())<<11 | uint32(value.Hour())<<6 | uint32(value.Minute())
}

// DecodeOperatorRequest preserves the command-specific tail as opaque input.
// The v1.188 client confirms that the leading selector is u16; individual
// privileged command grammars must be decoded before any command is executed.
func DecodeOperatorRequest(packet *network.Packet) (OperatorRequest, error) {
	if packet == nil || packet.Remaining() < 2 || packet.Remaining() > MaxOperatorRequestPayload+2 {
		return OperatorRequest{}, fmt.Errorf("invalid operator request length")
	}
	command, err := packet.ReadUInt16()
	if err != nil || command == 0 {
		return OperatorRequest{}, fmt.Errorf("invalid operator command")
	}
	payload, err := packet.ReadBytes(packet.Remaining())
	if err != nil {
		return OperatorRequest{}, err
	}
	return OperatorRequest{Command: command, Payload: append([]byte(nil), payload...)}, nil
}

func BuildOperatorFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.OperatorResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}

func BuildOperatorSuccess(command uint16, payload []byte) (network.Packet, error) {
	if command == 0 || len(payload) > MaxOperatorRequestPayload {
		return network.Packet{}, fmt.Errorf("invalid operator response")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.OperatorResponse
	packet.WriteByte(1)
	packet.WriteUInt16(command)
	packet.WriteBytes(payload)
	return packet, nil
}

// BuildOperatorPunishment emits the exact v1.188 notification body. State 0
// and 1 select the two stock-client messages; PackedTime uses SRO's packed
// year/month/day/hour/minute bit field.
func BuildOperatorPunishment(state byte, packedTime uint32) (network.Packet, error) {
	if state > 1 {
		return network.Packet{}, fmt.Errorf("invalid operator punishment state")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.OperatorPunishment
	packet.WriteByte(state)
	packet.WriteUInt32(packedTime)
	return packet, nil
}
