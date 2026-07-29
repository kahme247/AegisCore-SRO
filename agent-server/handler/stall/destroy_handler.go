package stall

import (
	"context"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func RegisterStallDestroyHandler(target *server.Server) {
	target.RegisterHandler(opcode.StallDestroyRequest, handleStallDestroy)
}

func handleStallDestroy(_ context.Context, session *server.Session, _ network.Packet) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.StallDestroyResponse
	p.WriteByte(1)
	if err := session.Send(p); err != nil {
		return err
	}

	p2 := network.EmptyPacket()
	p2.MessageID = opcode.StallEntityDestroyResponse
	p2.WriteUInt32(session.UserContext.UniqueID)
	p2.WriteUInt16(0) // Error Code. TODO: Possible Error Code 15383 - Closing
	return session.Send(p2)
}
