package vsro188

import (
	"fmt"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const CommunityMemoLimit = 20

type CommunityMemoSendRequest struct {
	TargetName string
	Message    string
}

type CommunityMemoGroupSendRequest struct {
	Mode       byte
	Recipients []string
	Message    string
}

type CommunityMemoListEntry struct {
	SenderName  string
	RefObjectID uint32
	Timestamp   time.Time
	Status      byte
}

type CommunityMemoGroupResult struct {
	Recipient string
	Code      uint16
}

func DecodeCommunityMemoSlotRequest(packet *network.Packet) (byte, error) {
	if packet == nil || packet.Remaining() != 1 {
		return 0, fmt.Errorf("invalid community memo slot request")
	}
	return packet.ReadByte()
}

func DecodeCommunityMemoSendRequest(packet *network.Packet) (CommunityMemoSendRequest, error) {
	if packet == nil {
		return CommunityMemoSendRequest{}, fmt.Errorf("invalid community memo send request")
	}
	target, err := packet.ReadString()
	if err != nil {
		return CommunityMemoSendRequest{}, err
	}
	message, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 || validateCommunityMemoText(target, message) != nil {
		return CommunityMemoSendRequest{}, fmt.Errorf("invalid community memo send request")
	}
	return CommunityMemoSendRequest{TargetName: target, Message: message}, nil
}

func DecodeCommunityMemoListRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid community memo list request")
	}
	return nil
}

func DecodeCommunityMemoGroupSendRequest(packet *network.Packet) (CommunityMemoGroupSendRequest, error) {
	if packet == nil || packet.Remaining() < 2 {
		return CommunityMemoGroupSendRequest{}, fmt.Errorf("invalid community memo group-send request")
	}
	mode, err := packet.ReadByte()
	if err != nil || (mode != 1 && mode != 2) {
		return CommunityMemoGroupSendRequest{}, fmt.Errorf("invalid community memo group mode")
	}
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > CommunityMemoLimit {
		return CommunityMemoGroupSendRequest{}, fmt.Errorf("invalid community memo recipient count")
	}
	recipients := make([]string, count)
	seen := make(map[string]struct{}, count)
	for i := range recipients {
		recipients[i], err = packet.ReadString()
		if err != nil || ValidateText(recipients[i], TextRules{Field: "memo recipient", MaxRunes: 64, RequireTrimmed: true}) != nil {
			return CommunityMemoGroupSendRequest{}, fmt.Errorf("invalid community memo recipient")
		}
		if _, exists := seen[recipients[i]]; exists {
			return CommunityMemoGroupSendRequest{}, fmt.Errorf("duplicate community memo recipient")
		}
		seen[recipients[i]] = struct{}{}
	}
	message, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 || ValidateText(message, TextRules{Field: "memo message", MaxRunes: 1000, RequireTrimmed: true}) != nil || message == "" {
		return CommunityMemoGroupSendRequest{}, fmt.Errorf("invalid community memo message")
	}
	return CommunityMemoGroupSendRequest{Mode: mode, Recipients: recipients, Message: message}, nil
}

func validateCommunityMemoText(target, message string) error {
	if target == "" || message == "" {
		return fmt.Errorf("empty community memo field")
	}
	if err := ValidateText(target, TextRules{Field: "memo recipient", MaxRunes: 64, RequireTrimmed: true}); err != nil {
		return err
	}
	return ValidateText(message, TextRules{Field: "memo message", MaxRunes: 1000, RequireTrimmed: true})
}

func buildCommunityMemoOperation(messageID uint16, errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	if errorCode == 0 {
		p.WriteByte(1)
	} else {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
	}
	return p
}

func BuildCommunityMemoOpen(slot byte, message string, errorCode uint16) (network.Packet, error) {
	p := buildCommunityMemoOperation(opcode.CommunityMemoOpenResponse, errorCode)
	if errorCode != 0 {
		return p, nil
	}
	if err := ValidateText(message, TextRules{Field: "memo message", MaxRunes: 1000}); err != nil {
		return network.Packet{}, err
	}
	p.WriteByte(slot)
	p.WriteString(message)
	return p, nil
}

func BuildCommunityMemoSendResult(errorCode uint16) network.Packet {
	return buildCommunityMemoOperation(opcode.CommunityMemoSendResponse, errorCode)
}

func BuildCommunityMemoDeleteResult(slot byte, errorCode uint16) network.Packet {
	p := buildCommunityMemoOperation(opcode.CommunityMemoDeleteResponse, errorCode)
	if errorCode == 0 {
		p.WriteByte(slot)
	}
	return p
}

func BuildCommunityMemoList(entries []CommunityMemoListEntry, errorCode uint16) (network.Packet, error) {
	p := buildCommunityMemoOperation(opcode.CommunityMemoListResponse, errorCode)
	if errorCode != 0 {
		return p, nil
	}
	if len(entries) > CommunityMemoLimit || len(entries) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many community memos")
	}
	p.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		unix := entry.Timestamp.Unix()
		if entry.RefObjectID == 0 || entry.Status > 1 || entry.Timestamp.IsZero() || unix < 0 || unix > math.MaxUint32 {
			return network.Packet{}, fmt.Errorf("invalid community memo list entry")
		}
		if err := ValidateText(entry.SenderName, TextRules{Field: "memo sender", MaxRunes: 64, RequireTrimmed: true}); err != nil {
			return network.Packet{}, err
		}
		p.WriteString(entry.SenderName)
		p.WriteUInt32(entry.RefObjectID)
		p.WriteUInt32(uint32(unix))
		p.WriteByte(entry.Status)
	}
	return p, nil
}

func BuildCommunityMemoGroupResult(results []CommunityMemoGroupResult, errorCode uint16) (network.Packet, error) {
	p := buildCommunityMemoOperation(opcode.CommunityMemoGroupSendResponse, errorCode)
	if errorCode != 0 {
		return p, nil
	}
	if len(results) > CommunityMemoLimit || len(results) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("too many community memo results")
	}
	p.WriteByte(byte(len(results)))
	for _, result := range results {
		if err := ValidateText(result.Recipient, TextRules{Field: "memo recipient", MaxRunes: 64, RequireTrimmed: true}); err != nil {
			return network.Packet{}, err
		}
		p.WriteString(result.Recipient)
		p.WriteUInt16(result.Code)
	}
	return p, nil
}

func BuildCommunityMemoNotification(entry CommunityMemoListEntry) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityUpdate
	unix := entry.Timestamp.Unix()
	if entry.RefObjectID == 0 || entry.Timestamp.IsZero() || unix < 0 || unix > math.MaxUint32 {
		return p, fmt.Errorf("invalid community memo notification")
	}
	if err := ValidateText(entry.SenderName, TextRules{Field: "memo sender", MaxRunes: 64, RequireTrimmed: true}); err != nil {
		return p, err
	}
	p.WriteByte(8)
	p.WriteString(entry.SenderName)
	p.WriteUInt32(entry.RefObjectID)
	p.WriteUInt32(uint32(unix))
	return p, nil
}
