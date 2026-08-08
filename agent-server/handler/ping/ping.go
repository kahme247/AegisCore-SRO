package ping

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func RegisterHandler(target *server.Server) {
	if target != nil {
		target.RegisterHandler(opcode.AgentPingRequest, handleRequest)
	}
}

func handleRequest(_ context.Context, session *server.Session, packet network.Packet) error {
	token, err := readToken(packet)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("send Agent ping response: missing session")
	}
	response, err := buildResponse(token)
	if err != nil {
		return err
	}
	return session.Send(response)
}

func readToken(packet network.Packet) (uint32, error) {
	if packet.Remaining() != 4 {
		return 0, fmt.Errorf("Agent ping payload length %d, want 4", packet.Remaining())
	}
	token, err := packet.ReadUInt32()
	if err != nil {
		return 0, fmt.Errorf("read Agent ping token: %w", err)
	}
	return token, nil
}

func buildResponse(token uint32) (network.Packet, error) {
	response := network.EmptyPacket()
	response.MessageID = opcode.AgentPingResponse
	if err := response.WriteUInt32(token); err != nil {
		return network.Packet{}, fmt.Errorf("write Agent ping token: %w", err)
	}
	return response, nil
}
