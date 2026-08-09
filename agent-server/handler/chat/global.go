package chat

import (
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

func handleGlobalMessage(request MessageRequest, session *server.Session) error {
	// TODO: Remove global from players inventory
	p := network.EmptyPacket()
	p.MessageID = opcode.ChatUpdate
	p.WriteByte(request.ChatType)
	p.WriteString(session.UserContext.CharName)
	p.WriteString(request.Message)
	if err := p.WriteError(); err != nil {
		return err
	}

	service.GetWorldServiceInstance().Broadcast(p)
	return nil
}
