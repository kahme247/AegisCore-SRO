package chat

import (
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func handlePartyMessage(request MessageRequest, session *server.Session) error {
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to send party message"))
		return nil
	}
	party := player.GetParty()
	if party == nil {
		p := network.EmptyPacket()
		p.MessageID = opcode.ChatResponse
		p.WriteByte(2)
		p.WriteByte(request.ChatType)
		p.WriteByte(request.ChatIndex)
		if err := p.WriteError(); err != nil {
			return err
		}
		return session.Send(p)
	}
	snapshot, err := service.GetPartyServiceInstance().SnapshotParty(party.Number)
	if err != nil {
		logrus.WithError(err).Debug("party chat rejected stale membership")
		return nil
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.ChatResponse
	p.WriteByte(1)
	p.WriteByte(request.ChatType)
	p.WriteByte(request.ChatIndex)
	if err := p.WriteError(); err != nil {
		return err
	}
	if err := session.Send(p); err != nil {
		return err
	}

	p1 := network.EmptyPacket()
	p1.MessageID = opcode.ChatUpdate
	p1.WriteByte(request.ChatType)
	p1.WriteString(session.UserContext.CharName)
	p1.WriteString(request.Message)
	if err := p1.WriteError(); err != nil {
		return err
	}
	for _, receivingPlayer := range snapshot.Members {
		if receivingPlayer == nil || receivingPlayer.GetSession() == nil {
			continue
		}
		if err := receivingPlayer.GetSession().Send(p1); err != nil {
			logrus.WithError(err).Debugf("failed to send party message to party member %d", receivingPlayer.GetUniqueID())
		}
	}
	return nil
}
