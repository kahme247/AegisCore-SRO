package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type AgentPingRequest struct{ Token uint32 }
type AgentPingResponse struct{ Token uint32 }
type GameReadyRequest struct{}
type TeleportReadyResponse struct{}
type CharacterJoinRequest struct{ Name string }

const AgentAuthMACLength = 6

type AgentAuthRequest struct {
	QueueID   uint32
	Username  string
	ContentID byte
	MAC       [AgentAuthMACLength]byte
}

type AgentPingRequestCodec struct{}
type AgentPingResponseCodec struct{}
type GameReadyRequestCodec struct{}
type TeleportReadyResponseCodec struct{}
type CharacterJoinRequestCodec struct{}
type AgentAuthRequestCodec struct{}

func (AgentPingRequestCodec) Decode(packet network.Packet) (AgentPingRequest, error) {
	if packet.Remaining() != 4 {
		return AgentPingRequest{}, fmt.Errorf("Agent ping payload length %d, want 4", packet.Remaining())
	}
	token, err := packet.ReadUInt32()
	if err != nil {
		return AgentPingRequest{}, fmt.Errorf("read Agent ping token: %w", err)
	}
	if packet.Remaining() != 0 {
		return AgentPingRequest{}, fmt.Errorf("Agent ping request has %d trailing bytes", packet.Remaining())
	}
	return AgentPingRequest{Token: token}, nil
}

func (AgentPingResponseCodec) Encode(response AgentPingResponse) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AgentPingResponse
	if err := packet.WriteUInt32(response.Token); err != nil {
		return network.Packet{}, fmt.Errorf("write Agent ping token: %w", err)
	}
	return packet, nil
}

func (GameReadyRequestCodec) Decode(packet network.Packet) (GameReadyRequest, error) {
	if packet.Remaining() != 0 {
		return GameReadyRequest{}, fmt.Errorf("GameReady request contains %d trailing bytes", packet.Remaining())
	}
	return GameReadyRequest{}, nil
}

func (TeleportReadyResponseCodec) Decode(packet network.Packet) (TeleportReadyResponse, error) {
	if packet.Remaining() != 0 {
		return TeleportReadyResponse{}, fmt.Errorf("teleport ready response contains %d trailing bytes", packet.Remaining())
	}
	return TeleportReadyResponse{}, nil
}

func (CharacterJoinRequestCodec) Decode(packet network.Packet) (CharacterJoinRequest, error) {
	name, err := packet.ReadString()
	if err != nil {
		return CharacterJoinRequest{}, fmt.Errorf("read character join name: %w", err)
	}
	if err := ValidateText(name, TextRules{Field: "character name", MaxRunes: 32, RequireTrimmed: true}); err != nil {
		return CharacterJoinRequest{}, err
	}
	if packet.Remaining() != 0 {
		return CharacterJoinRequest{}, fmt.Errorf("character join request has %d trailing bytes", packet.Remaining())
	}
	return CharacterJoinRequest{Name: name}, nil
}

// Decode parses the stock Agent admission request while deliberately
// discarding the password field. Gateway has already authenticated it, and
// Agent must neither retain nor compare that secret.
func (AgentAuthRequestCodec) Decode(packet network.Packet) (AgentAuthRequest, error) {
	queueID, err := packet.ReadUInt32()
	if err != nil {
		return AgentAuthRequest{}, fmt.Errorf("read Agent auth queue ID: %w", err)
	}
	username, err := packet.ReadString()
	if err != nil {
		return AgentAuthRequest{}, fmt.Errorf("read Agent auth username: %w", err)
	}
	if err := ValidateText(username, TextRules{Field: "username"}); err != nil {
		return AgentAuthRequest{}, err
	}
	if _, err := packet.ReadString(); err != nil {
		return AgentAuthRequest{}, fmt.Errorf("read Agent auth compatibility password: %w", err)
	}
	contentID, err := packet.ReadByte()
	if err != nil {
		return AgentAuthRequest{}, fmt.Errorf("read Agent auth content ID: %w", err)
	}
	mac, err := packet.ReadBytes(AgentAuthMACLength)
	if err != nil {
		return AgentAuthRequest{}, fmt.Errorf("read Agent auth MAC: %w", err)
	}
	if packet.Remaining() != 0 {
		return AgentAuthRequest{}, fmt.Errorf("Agent auth request has %d trailing bytes", packet.Remaining())
	}
	request := AgentAuthRequest{QueueID: queueID, Username: username, ContentID: contentID}
	copy(request.MAC[:], mac)
	return request, nil
}
