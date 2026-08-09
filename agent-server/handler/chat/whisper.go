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
	p := network.EmptyPacket()
	p.MessageID = opcode.ChatResponse

	if receivingPlayer == nil {
		// Failed to send message
		logrus.Debugf("failed to send message from %s to %s\n", session.UserContext.CharName, request.Receiver)
		p.WriteByte(2)
		p.WriteUInt16(3)
		p.WriteByte(request.ChatType)
		p.WriteByte(request.ChatIndex)
		if err := p.WriteError(); err != nil {
			return err
		}
		return session.Send(p)
	}

	p.WriteByte(1)
	p.WriteByte(request.ChatType)
	p.WriteByte(request.ChatIndex)
	if err := p.WriteError(); err != nil {
		return err
	}
	if err := session.Send(p); err != nil {
		return err
	}

	p2 := network.EmptyPacket()
	p2.MessageID = opcode.ChatUpdate
	p2.WriteByte(request.ChatType)
	p2.WriteString(session.UserContext.CharName)
	p2.WriteString(request.Message)
	if err := p2.WriteError(); err != nil {
		return err
	}
	if err := receivingPlayer.Session.Send(p2); err != nil {
		logrus.WithError(err).Debugf("failed to deliver whisper to %s", request.Receiver)
	}
	return nil
}
