package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// RepairResponse is AegisCore's maintained 0xB03E response envelope. A
// success contains only result=1; a failure contains result=2 and a u16
// result code. The stock code's result-code meanings remain unresolved.
type RepairResponse struct {
	Success bool
	Code    uint16
}

func BuildRepairSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemRepairResponse
	packet.WriteByte(1)
	return packet
}

func BuildRepairFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemRepairResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}

func DecodeRepairResponse(packet *network.Packet) (RepairResponse, error) {
	if packet == nil {
		return RepairResponse{}, fmt.Errorf("0xB03E packet is nil")
	}
	result, err := packet.ReadByte()
	if err != nil {
		return RepairResponse{}, fmt.Errorf("read 0xB03E result: %w", err)
	}
	switch result {
	case 1:
		if packet.Remaining() != 0 {
			return RepairResponse{}, fmt.Errorf("0xB03E success has %d trailing bytes", packet.Remaining())
		}
		return RepairResponse{Success: true}, nil
	case 2:
		code, readErr := packet.ReadUInt16()
		if readErr != nil {
			return RepairResponse{}, fmt.Errorf("read 0xB03E failure code: %w", readErr)
		}
		if packet.Remaining() != 0 {
			return RepairResponse{}, fmt.Errorf("0xB03E failure has %d trailing bytes", packet.Remaining())
		}
		return RepairResponse{Code: code}, nil
	default:
		return RepairResponse{}, fmt.Errorf("0xB03E result is %d, want 1 or 2", result)
	}
}
