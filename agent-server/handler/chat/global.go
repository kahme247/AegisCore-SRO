package chat

import (
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
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
