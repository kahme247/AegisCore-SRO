package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	CommunityBlockAdd    byte = 1
	CommunityBlockRemove byte = 2
)

type CommunityBlockRequest struct {
	TargetName string
	Action     byte
}

func DecodeCommunityBlockRequest(packet *network.Packet) (CommunityBlockRequest, error) {
	if packet == nil {
		return CommunityBlockRequest{}, fmt.Errorf("invalid community block request")
	}
	name, err := packet.ReadString()
	if err != nil {
		return CommunityBlockRequest{}, err
	}
	action, err := packet.ReadByte()
	if err != nil || packet.Remaining() != 0 || (action != CommunityBlockAdd && action != CommunityBlockRemove) {
		return CommunityBlockRequest{}, fmt.Errorf("invalid community block request")
	}
	if err = ValidateText(name, TextRules{Field: "blocked character", MaxRunes: 64, RequireTrimmed: true}); err != nil {
		return CommunityBlockRequest{}, err
	}
	return CommunityBlockRequest{TargetName: name, Action: action}, nil
}

func BuildCommunityBlockResult(action, result byte, targetName string) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.CommunityBlockUpdateResponse
	if (action != CommunityBlockAdd && action != CommunityBlockRemove) || result > 4 {
		return p, fmt.Errorf("invalid community block result")
	}
	p.WriteByte(action)
	p.WriteByte(result)
	if result == 0 {
		if err := ValidateText(targetName, TextRules{Field: "blocked character", MaxRunes: 64, RequireTrimmed: true}); err != nil {
			return network.Packet{}, err
		}
		p.WriteString(targetName)
	}
	return p, nil
}
