package character

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

type QuestHandler struct {
	repository model.CharacterQuestRepository
	send       func(*server.Session, network.Packet) error
}

func RegisterQuestHandler(target *server.Server, repository model.CharacterQuestRepository) error {
	if target == nil || repository == nil {
		return model.ErrCharacterQuestRepositoryUnavailable
	}
	handler := &QuestHandler{repository: repository}
	target.RegisterHandler(opcode.QuestAbandonRequest, handler.handleAbandon)
	return nil
}

func (h *QuestHandler) handleAbandon(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.repository == nil {
		return model.ErrCharacterQuestRepositoryUnavailable
	}
	if session == nil || session.UserContext.CharacterID <= 0 {
		return fmt.Errorf("quest abandon requires a selected character")
	}
	questID, err := packet.ReadUInt32()
	if err != nil || questID == 0 || packet.Remaining() != 0 {
		return fmt.Errorf("malformed quest abandon request")
	}
	err = h.repository.Abandon(ctx, session.UserContext.CharacterID, int(questID))
	if err != nil && !errors.Is(err, model.ErrCharacterQuestNotActive) {
		return err
	}
	success := err == nil
	if success && session.UserContext.UniqueID != 0 {
		if object, lookupErr := service.GetWorldServiceInstance().GetObjectByUniqueId(session.UserContext.UniqueID); lookupErr == nil {
			if player, ok := object.(*model.Player); ok {
				player.MarkQuestAbandoned(int(questID))
			}
		}
	}
	response := buildQuestAbandonResponse(success, questID)
	if h.send != nil {
		return h.send(session, response)
	}
	return session.Send(response)
}

func buildQuestAbandonResponse(success bool, questID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestAbandonResponse
	if success {
		packet.WriteByte(1)
		packet.WriteUInt32(questID)
	} else {
		packet.WriteByte(2)
	}
	return packet
}
