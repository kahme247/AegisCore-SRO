package ping

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
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
	request, err := (vsro188.AgentPingRequestCodec{}).Decode(packet)
	return request.Token, err
}

func buildResponse(token uint32) (network.Packet, error) {
	return (vsro188.AgentPingResponseCodec{}).Encode(vsro188.AgentPingResponse{Token: token})
}
