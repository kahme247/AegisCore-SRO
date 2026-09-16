package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const SilkHistoryLimit = 100

type SilkHistoryRequest struct {
	Mode     byte
	Currency byte
}

type SilkHistoryEntry struct {
	Timestamp uint32
	Amount    uint32
}

type PremiumDecoration struct {
	Token     uint32
	Reference uint32
	Value     uint32
}

type PremiumUnknownUpdate struct {
	First  uint32
	Second uint32
	Third  uint32
	State  byte
}

type SilkUnknownEntry struct {
	Reference uint32
	Amount    uint16
}

type PremiumPauseRequest struct {
	Category byte
	Active   bool
}

func DecodePremiumPauseRequest(packet *network.Packet) (PremiumPauseRequest, error) {
	if packet == nil || packet.Remaining() != 2 {
		return PremiumPauseRequest{}, fmt.Errorf("invalid premium pause request")
	}
	category, err := packet.ReadByte()
	if err != nil {
		return PremiumPauseRequest{}, err
	}
	active, err := packet.ReadByte()
	if err != nil || category == 0 || active > 1 {
		return PremiumPauseRequest{}, fmt.Errorf("invalid premium pause state")
	}
	return PremiumPauseRequest{Category: category, Active: active == 1}, nil
}

func DecodeSilkHistoryRequest(packet *network.Packet) (SilkHistoryRequest, error) {
	if packet == nil || packet.Remaining() != 2 {
		return SilkHistoryRequest{}, fmt.Errorf("invalid Silk history request")
	}
	mode, err := packet.ReadByte()
	if err != nil {
		return SilkHistoryRequest{}, err
	}
	currency, err := packet.ReadByte()
	if err != nil || mode != 1 || currency < 1 || currency > 4 {
		return SilkHistoryRequest{}, fmt.Errorf("invalid Silk history selector")
	}
	return SilkHistoryRequest{Mode: mode, Currency: currency}, nil
}

func DecodePremiumDecorationUseRequest(packet *network.Packet) (uint32, uint32, error) {
	if packet == nil || packet.Remaining() != 8 {
		return 0, 0, fmt.Errorf("invalid premium decoration request")
	}
	first, err := packet.ReadUInt32()
	if err != nil {
		return 0, 0, err
	}
	second, err := packet.ReadUInt32()
	return first, second, err
}

func DecodeSilkUnknownRequest(packet *network.Packet) (uint16, error) {
	if packet == nil || packet.Remaining() != 2 {
		return 0, fmt.Errorf("invalid Silk request")
	}
	return packet.ReadUInt16()
}

func BuildSilkHistoryResponse(request SilkHistoryRequest, entries []SilkHistoryEntry, errorCode uint16) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.SilkHistoryResponse
	if request.Mode != 1 || request.Currency < 1 || request.Currency > 4 {
		return p, fmt.Errorf("invalid Silk history selector")
	}
	p.WriteByte(request.Currency)
	p.WriteByte(request.Mode)
	if errorCode != 0 {
		p.WriteByte(1)
		p.WriteUInt16(errorCode)
		return p, nil
	}
	if len(entries) > SilkHistoryLimit || len(entries) > math.MaxUint16 {
		return network.Packet{}, fmt.Errorf("too many Silk history entries")
	}
	p.WriteByte(0)
	p.WriteUInt16(uint16(len(entries)))
	for _, entry := range entries {
		p.WriteUInt32(entry.Timestamp)
		p.WriteUInt32(entry.Amount)
	}
	return p, nil
}

func BuildPremiumDecorationUseResponse(first, second uint32, errorCode byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.PremiumDecoUseResponse
	if errorCode == 0 {
		p.WriteByte(1)
		p.WriteUInt32(first)
		p.WriteUInt32(second)
	} else {
		p.WriteByte(2)
		p.WriteByte(errorCode)
	}
	return p
}

func BuildPremiumUnknownUpdate(update PremiumUnknownUpdate) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.PremiumUnknownUpdate315A
	p.WriteUInt32(update.First)
	p.WriteUInt32(update.Second)
	p.WriteUInt32(update.Third)
	p.WriteByte(update.State)
	return p
}

func BuildPremiumDailyReset(first, second uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.PremiumDailyReset
	p.WriteUInt32(first)
	p.WriteUInt32(second)
	return p
}

func buildPremiumDecorationUpdate(messageID uint16, decoration PremiumDecoration) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	p.WriteUInt32(decoration.Token)
	p.WriteUInt32(decoration.Reference)
	p.WriteUInt32(decoration.Value)
	return p
}

func BuildPremiumDecoration(decoration PremiumDecoration) network.Packet {
	return buildPremiumDecorationUpdate(opcode.PremiumDeco, decoration)
}

func BuildPremiumDecorationReset(decoration PremiumDecoration) network.Packet {
	return buildPremiumDecorationUpdate(opcode.PremiumDecoReset, decoration)
}

func BuildSilkUnknownResponse(group uint16, entries []SilkUnknownEntry, errorCode uint16) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.SilkUnknownResponse
	if errorCode != 0 {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
		return p, nil
	}
	if len(entries) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many Silk entries")
	}
	p.WriteByte(1)
	p.WriteUInt16(group)
	p.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		p.WriteUInt32(entry.Reference)
		p.WriteUInt16(entry.Amount)
	}
	return p, nil
}
