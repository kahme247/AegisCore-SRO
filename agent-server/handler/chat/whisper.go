package chat

import (
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func handleWhisperMessage(request MessageRequest, session *server.Session) error {
	world := service.GetWorldServiceInstance()
	receivingPlayer, err := world.GetPlayerByCharName(request.Receiver)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to send private message"))
		return nil
	}
	if receivingPlayer == nil {
		// Failed to send message
		logrus.Debugf("failed to send message from %s to %s\n", session.UserContext.CharName, request.Receiver)
		p := network.EmptyPacket()
		p.MessageID = opcode.ChatResponse
		p.WriteByte(2)
		p.WriteUInt16(3)
		p.WriteByte(request.ChatType)
		p.WriteByte(request.ChatIndex)
		if err := p.WriteError(); err != nil {
			return err
		}
		return session.Send(p)
	}

	if err := sendChatResult(session, request, 1); err != nil {
		return err
	}

	p2, err := buildNamedChatUpdate(request, session.UserContext.CharName)
	if err != nil {
		return err
	}
	if err := receivingPlayer.Session.Send(p2); err != nil {
		logrus.WithError(err).Debugf("failed to deliver whisper to %s", request.Receiver)
	}
	return nil
}
