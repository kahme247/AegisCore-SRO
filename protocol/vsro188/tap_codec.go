package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const maxTAPInfoEntries = 255

type TAPInfoRequest struct {
	Icon      bool
	Reference uint32
}

// TAPEntryType is the client UI presentation selected for a TAP period.
type TAPEntryType byte

const (
	TAPEntryThiefGuild TAPEntryType = 2
	TAPEntryShowAll    TAPEntryType = 3
)

type TAPTime struct {
	Month  byte
	Day    byte
	Hour   byte
	Minute byte
}

type TAPInfoEntry struct {
	Type              TAPEntryType
	From              TAPTime
	To                TAPTime
	TraderUnionPoints uint32
	ThiefUnionPoints  uint32
}

type TAPUpdateEntry struct {
	TraderUnionPoints uint32
	ThiefUnionPoints  uint32
}

func DecodeTAPInfoRequest(packet *network.Packet) (TAPInfoRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return TAPInfoRequest{}, fmt.Errorf("invalid TAP info request")
	}
	isIcon, err := packet.ReadByte()
	if err != nil || isIcon > 1 {
		return TAPInfoRequest{}, fmt.Errorf("invalid TAP info icon flag")
	}
	request := TAPInfoRequest{Icon: isIcon == 1}
	if !request.Icon {
		if packet.Remaining() != 4 {
			return TAPInfoRequest{}, fmt.Errorf("invalid TAP info NPC reference")
		}
		request.Reference, err = packet.ReadUInt32()
		if err != nil || request.Reference == 0 {
			return TAPInfoRequest{}, fmt.Errorf("invalid TAP info NPC reference")
		}
	} else if packet.Remaining() != 0 {
		return TAPInfoRequest{}, fmt.Errorf("invalid TAP info icon payload")
	}
	return request, nil
}

func DecodeTAPUpdateRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid TAP update request")
	}
	return nil
}

// BuildTAPInfoResponse builds a complete TAP snapshot. The entry count is also
// the number of pairs the client expects in subsequent update responses.
func BuildTAPInfoResponse(entries []TAPInfoEntry) (network.Packet, error) {
	if len(entries) > maxTAPInfoEntries {
		return network.Packet{}, fmt.Errorf("too many TAP info entries: %d", len(entries))
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.TapInfoResponse
	p.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		p.WriteByte(byte(entry.Type))
		writeTAPTime(&p, entry.From)
		writeTAPTime(&p, entry.To)
		p.WriteUInt32(entry.TraderUnionPoints)
		p.WriteUInt32(entry.ThiefUnionPoints)
	}
	return p, nil
}

// BuildTAPUpdateResponse builds point-only updates in the same order as the
// entries in the last info response. The wire format has no count or status.
func BuildTAPUpdateResponse(entries []TAPUpdateEntry) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.TapUpdateResponse
	for _, entry := range entries {
		p.WriteUInt32(entry.TraderUnionPoints)
		p.WriteUInt32(entry.ThiefUnionPoints)
	}
	return p
}

func BuildTAPIcon(displayed bool) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.TapIcon
	p.WriteBool(displayed)
	return p
}

func writeTAPTime(packet *network.Packet, value TAPTime) {
	packet.WriteByte(value.Month)
	packet.WriteByte(value.Day)
	packet.WriteByte(value.Hour)
	packet.WriteByte(value.Minute)
}
