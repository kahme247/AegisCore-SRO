package handler

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/config"
)

// ShardlistPingHandler writes the documented 0xA106 success branch only after
// the farm and externally advertised Agent IPv4 endpoint have been validated.
type ShardlistPingHandler struct {
	LoadFarm     func() (config.Farm, error)
	LoadEndpoint func() (config.AgentEndpoint, error)
}

func NewShardlistPingHandler(
	loadFarm func() (config.Farm, error),
	loadEndpoint func() (config.AgentEndpoint, error),
) *ShardlistPingHandler {
	if loadFarm == nil {
		loadFarm = config.LoadFarm
	}
	if loadEndpoint == nil {
		loadEndpoint = config.LoadAgentEndpoint
	}
	return &ShardlistPingHandler{LoadFarm: loadFarm, LoadEndpoint: loadEndpoint}
}

func RegisterShardlistPingHandler(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewShardlistPingHandler(config.LoadFarm, config.LoadAgentEndpoint)
	target.RegisterHandler(opcode.ShardlistPing, handler.Handle)
}

func (h *ShardlistPingHandler) Handle(_ context.Context, session *server.Session, packet network.Packet) error {
	if err := validateEmptyGatewayRequest(packet, "shard-list ping"); err != nil {
		return err
	}
	loadFarm := h.LoadFarm
	if loadFarm == nil {
		loadFarm = config.LoadFarm
	}
	farm, err := loadFarm()
	if err != nil {
		return fmt.Errorf("load gateway farm: %w", err)
	}
	loadEndpoint := h.LoadEndpoint
	if loadEndpoint == nil {
		loadEndpoint = config.LoadAgentEndpoint
	}
	endpoint, err := loadEndpoint()
	if err != nil {
		return fmt.Errorf("load advertised Agent endpoint: %w", err)
	}
	response, err := buildShardlistPong(farm, endpoint)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("send shard-list ping response: missing session")
	}
	return session.Send(response)
}

func buildShardlistPong(farm config.Farm, endpoint config.AgentEndpoint) (network.Packet, error) {
	if err := config.ValidateFarm(farm); err != nil {
		return network.Packet{}, fmt.Errorf("invalid gateway farm: %w", err)
	}
	// The loader already checks the deployment environment. Validate the bare
	// wire shape again here so injected tests/callers cannot serialize IPv6 or
	// an unspecified address into the four-byte protocol field.
	if !endpoint.IP.IsValid() || !endpoint.IP.Is4() || endpoint.IP.IsUnspecified() || endpoint.IP.IsMulticast() || endpoint.Port == 0 {
		return network.Packet{}, fmt.Errorf("invalid advertised Agent IPv4 endpoint")
	}

	response := network.EmptyPacket()
	response.MessageID = opcode.ShardlistPong
	if err := response.WriteByte(1); err != nil { // result = successful
		return network.Packet{}, err
	}
	if err := response.WriteByte(farm.ID); err != nil {
		return network.Packet{}, err
	}
	ipBytes := endpoint.IP.As4()
	if err := response.WriteBytes(ipBytes[:]); err != nil {
		return network.Packet{}, err
	}
	return response, nil
}
