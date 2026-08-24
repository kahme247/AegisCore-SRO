package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type GatewayPatchRequest struct {
	ContentID  byte
	ModuleName string
	Version    uint32
}

type GatewayLoginRequest struct {
	ContentID byte
	Username  string
	Password  string
	ShardID   uint16
}

type GatewayNoticeRequest struct{ ContentID byte }
type GatewayShardListRequest struct{}

type GatewayEndpointResponse struct {
	Profile byte
	FarmID  byte
	IPv4    [4]byte
}

type GatewayPatchRequestCodec struct{}
type GatewayLoginRequestCodec struct{}
type GatewayNoticeRequestCodec struct{}
type GatewayShardListRequestCodec struct{}
type GatewayEndpointResponseCodec struct{}

func (GatewayPatchRequestCodec) Decode(packet network.Packet) (GatewayPatchRequest, error) {
	contentID, err := packet.ReadByte()
	if err != nil {
		return GatewayPatchRequest{}, fmt.Errorf("read patch content ID: %w", err)
	}
	moduleName, err := packet.ReadString()
	if err != nil {
		return GatewayPatchRequest{}, fmt.Errorf("read patch module name: %w", err)
	}
	version, err := packet.ReadUInt32()
	if err != nil {
		return GatewayPatchRequest{}, fmt.Errorf("read patch version: %w", err)
	}
	if packet.Remaining() != 0 {
		return GatewayPatchRequest{}, fmt.Errorf("patch request has %d trailing bytes", packet.Remaining())
	}
	return GatewayPatchRequest{ContentID: contentID, ModuleName: moduleName, Version: version}, nil
}

func (GatewayLoginRequestCodec) Decode(packet network.Packet) (GatewayLoginRequest, error) {
	contentID, err := packet.ReadByte()
	if err != nil {
		return GatewayLoginRequest{}, fmt.Errorf("read login content ID: %w", err)
	}
	username, err := packet.ReadString()
	if err != nil {
		return GatewayLoginRequest{}, fmt.Errorf("read login username: %w", err)
	}
	password, err := packet.ReadString()
	if err != nil {
		return GatewayLoginRequest{}, fmt.Errorf("read login password: %w", err)
	}
	shardID, err := packet.ReadUInt16()
	if err != nil {
		return GatewayLoginRequest{}, fmt.Errorf("read login shard ID: %w", err)
	}
	if packet.Remaining() != 0 {
		return GatewayLoginRequest{}, fmt.Errorf("login request has %d trailing bytes", packet.Remaining())
	}
	return GatewayLoginRequest{ContentID: contentID, Username: username, Password: password, ShardID: shardID}, nil
}

func (GatewayNoticeRequestCodec) Decode(packet network.Packet) (GatewayNoticeRequest, error) {
	contentID, err := packet.ReadByte()
	if err != nil {
		return GatewayNoticeRequest{}, fmt.Errorf("read notice content ID: %w", err)
	}
	if packet.Remaining() != 0 {
		return GatewayNoticeRequest{}, fmt.Errorf("notice request has %d trailing bytes", packet.Remaining())
	}
	return GatewayNoticeRequest{ContentID: contentID}, nil
}

func (GatewayShardListRequestCodec) Decode(packet network.Packet) (GatewayShardListRequest, error) {
	if packet.Remaining() != 0 {
		return GatewayShardListRequest{}, fmt.Errorf("shard-list request has %d trailing bytes", packet.Remaining())
	}
	return GatewayShardListRequest{}, nil
}

func (GatewayEndpointResponseCodec) Encode(response GatewayEndpointResponse) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ShardlistPong
	if err := packet.WriteByte(response.Profile); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteByte(response.FarmID); err != nil {
		return network.Packet{}, err
	}
	if err := packet.WriteBytes(response.IPv4[:]); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}
