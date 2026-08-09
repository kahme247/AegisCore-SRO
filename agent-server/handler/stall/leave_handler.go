package stall

import (
	"context"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
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
