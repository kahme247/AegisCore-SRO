package chat

import (
	"github.com/Harvi8/AegisCore/framework/server"
)

func handleStallMessage(request MessageRequest, session *server.Session) error {
	return sendChatResult(session, request, 1)
}
