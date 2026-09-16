package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const CommunityFriendLimit = 20

type CommunityFriendRecord struct {
	CharacterID uint32
	Name        string
	RefObjectID uint32
	Online      bool
}

type CommunityFriendResponse struct {
	RequesterID uint32
	TargetID    uint32
	Accepted    bool
}

func DecodeCommunityFriendAddRequest(packet *network.Packet) (string, error) {
	if packet == nil {
		return "", fmt.Errorf("invalid community friend-add request")
	}
	name, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 || ValidateText(name, TextRules{Field: "friend name", MaxRunes: 64, RequireTrimmed: true}) != nil {
		return "", fmt.Errorf("invalid community friend-add request")
	}
	return name, nil
}

func BuildCommunityFriendInvitation(requesterID, targetID uint32, requesterName string) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityFriendRequest
	if requesterID == 0 || targetID == 0 || requesterID == targetID {
		return p, fmt.Errorf("invalid community friend invitation")
	}
	if err := ValidateText(requesterName, TextRules{Field: "friend requester", MaxRunes: 64, RequireTrimmed: true}); err != nil {
		return p, err
	}
	p.WriteUInt32(requesterID)
	p.WriteUInt32(targetID)
	p.WriteString(requesterName)
	return p, nil
}

func DecodeCommunityFriendResponse(packet *network.Packet) (CommunityFriendResponse, error) {
	if packet == nil || packet.Remaining() != 9 {
		return CommunityFriendResponse{}, fmt.Errorf("invalid community friend response")
	}
	requester, err := packet.ReadUInt32()
	if err != nil {
		return CommunityFriendResponse{}, err
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return CommunityFriendResponse{}, err
	}
	decision, err := packet.ReadByte()
	if err != nil || requester == 0 || target == 0 || requester == target || decision > 1 {
		return CommunityFriendResponse{}, fmt.Errorf("invalid community friend response")
	}
	return CommunityFriendResponse{RequesterID: requester, TargetID: target, Accepted: decision == 1}, nil
}

func DecodeCommunityFriendDeleteRequest(packet *network.Packet) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid community friend-delete request")
	}
	id, err := packet.ReadUInt32()
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid community friend-delete request")
	}
	return id, nil
}

func BuildCommunityFriendInfo(friends []CommunityFriendRecord) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityFriendInfo
	if len(friends) > CommunityFriendLimit || len(friends) > math.MaxUint8 {
		return p, fmt.Errorf("too many community friends: %d", len(friends))
	}
	p.WriteByte(byte(len(friends)))
	for _, friend := range friends {
		if friend.CharacterID == 0 || friend.RefObjectID == 0 {
			return network.Packet{}, fmt.Errorf("invalid community friend record")
		}
		if err := ValidateText(friend.Name, TextRules{Field: "friend name", MaxRunes: 64, RequireTrimmed: true}); err != nil {
			return network.Packet{}, err
		}
		p.WriteUInt32(friend.CharacterID)
		p.WriteString(friend.Name)
		p.WriteUInt32(friend.RefObjectID)
		p.WriteBool(friend.Online)
	}
	return p, nil
}

func BuildCommunityFriendAdded(friend CommunityFriendRecord) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityUpdate
	if friend.CharacterID == 0 || friend.RefObjectID == 0 {
		return p, fmt.Errorf("invalid community friend record")
	}
	if err := ValidateText(friend.Name, TextRules{Field: "friend name", MaxRunes: 64, RequireTrimmed: true}); err != nil {
		return p, err
	}
	p.WriteByte(2)
	p.WriteUInt32(friend.CharacterID)
	p.WriteString(friend.Name)
	p.WriteUInt32(friend.RefObjectID)
	return p, nil
}

func BuildCommunityFriendRemoved(friendID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityUpdate
	p.WriteByte(3)
	p.WriteUInt32(friendID)
	return p
}

func BuildCommunityFriendState(friendID uint32, online bool) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityUpdate
	p.WriteByte(4)
	p.WriteUInt32(friendID)
	p.WriteBool(online)
	return p
}

func BuildCommunityFriendAddFailure(errorCode uint16, rejection byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityFriendAddResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	if errorCode == 0x640b {
		p.WriteByte(rejection)
	}
	return p
}

func BuildCommunityFriendDeleteResult(friendID uint32, errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityFriendDeleteResponse
	if errorCode != 0 {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
		return p
	}
	p.WriteByte(1)
	p.WriteUInt32(friendID)
	return p
}
