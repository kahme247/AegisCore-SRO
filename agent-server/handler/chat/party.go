package chat

import (
	"github.com/Harvi8/AegisCore/agent-server/service"
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
		return sendChatResult(session, request, 2)
	}
	snapshot, err := service.GetPartyServiceInstance().SnapshotParty(party.Number)
	if err != nil {
		logrus.WithError(err).Debug("party chat rejected stale membership")
		return nil
	}
	if err := sendChatResult(session, request, 1); err != nil {
		return err
	}

	p1, err := buildNamedChatUpdate(request, session.UserContext.CharName)
	if err != nil {
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
