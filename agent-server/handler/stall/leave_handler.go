package stall

import (
	"context"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func RegisterStallLeaveHandler(target *server.Server) {
	target.RegisterHandler(opcode.StallLeaveRequest, handleStallLeave)
}

func handleStallLeave(_ context.Context, session *server.Session, _ network.Packet) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.StallLeaveResponse
	p.WriteByte(1)
	return session.Send(p)
}
