package chat

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func handleStallMessage(request MessageRequest, session *server.Session) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.ChatResponse
	p.WriteByte(1)
	p.WriteByte(request.ChatType)
	p.WriteByte(request.ChatIndex)
	if err := p.WriteError(); err != nil {
		return err
	}
	return session.Send(p)
}
