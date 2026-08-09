package chat

import (
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func handleAllMessage(request MessageRequest, session *server.Session) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.ChatUpdate
	p.WriteByte(request.ChatType)
	p.WriteUInt32(session.UserContext.UniqueID)
	p.WriteString(request.Message)
	if err := p.WriteError(); err != nil {
		return err
	}
	// TODO: Change all players to local region
	service.GetWorldServiceInstance().Broadcast(p)

	p1 := network.EmptyPacket()
	p1.MessageID = opcode.ChatResponse
	p1.WriteByte(1)
	p1.WriteByte(request.ChatType)
	p1.WriteByte(request.ChatIndex)
	if err := p1.WriteError(); err != nil {
		return err
	}
	return session.Send(p1)
}
