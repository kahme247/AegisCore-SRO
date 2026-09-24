package chat

import (
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/server"
)

func handleGlobalMessage(request MessageRequest, session *server.Session) error {
	// TODO: Remove global from players inventory
	p, err := buildNamedChatUpdate(request, session.UserContext.CharName)
	if err != nil {
		return err
	}

	service.GetWorldServiceInstance().Broadcast(p)
	return nil
}
