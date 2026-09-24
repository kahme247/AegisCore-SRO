package chat

import (
	"fmt"
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
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return fmt.Errorf("local chat sender %d is not in world: %w", session.UserContext.UniqueID, err)
	}
	if player == nil {
		return fmt.Errorf("local chat sender %d is not in world", session.UserContext.UniqueID)
	}
	player.SendToSelfAndKnown(&p)

	return sendChatResult(session, request, 1)
}
