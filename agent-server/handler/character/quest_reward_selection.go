package character

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type QuestRewardSelectionExecutor interface {
	SelectQuestReward(context.Context, int, uint32, []uint32) (model.QuestCompletionResult, error)
}

type QuestRewardSelectionHandler struct {
	world    selectEntityWorld
	executor QuestRewardSelectionExecutor
	send     func(*server.Session, network.Packet) error
}

// RegisterQuestRewardSelectionHandler installs the binary-confirmed 0x7515
// quest reward selector. The executor resolves each wire RefItemID against the
// owning quest and commits reward insertion and closure in one transaction.
func RegisterQuestRewardSelectionHandler(target *server.Server, executor QuestRewardSelectionExecutor) (*QuestRewardSelectionHandler, error) {
	if target == nil || executor == nil {
		return nil, model.ErrQuestRuntimeUnavailable
	}
	handler := &QuestRewardSelectionHandler{world: service.GetWorldServiceInstance(), executor: executor}
	target.RegisterHandler(opcode.QuestRewardSelectionRequest, handler.handle)
	return handler, nil
}

func (h *QuestRewardSelectionHandler) handle(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeQuestRewardSelectionRequest(&packet)
	if err != nil {
		return err
	}
	if request.RewardID == 0 {
		return h.sendResponse(session, false, 1)
	}
	world := h.world
	if world == nil {
		world = service.GetWorldServiceInstance()
	}
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}
	if h.executor == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if request.RewardID >= 10000 {
		eventExecutor, supported := h.executor.(model.EventRewardSelectionExecutor)
		if !supported {
			return h.sendResponse(session, false, 1)
		}
		result, eventErr := eventExecutor.SelectEventReward(ctx, player.ID, request.RewardID, append([]uint32(nil), request.EntryIDs...))
		if errors.Is(eventErr, model.ErrQuestReferenceMissing) || errors.Is(eventErr, model.ErrQuestEventNotFound) {
			return h.sendResponse(session, false, 1)
		}
		if errors.Is(eventErr, model.ErrQuestReferenceUnsupported) || errors.Is(eventErr, model.ErrQuestStateConflict) || errors.Is(eventErr, model.ErrQuestInventoryFull) {
			return h.sendResponse(session, false, 2)
		}
		if eventErr != nil {
			return eventErr
		}
		publisher := &NPCTalkHandler{send: h.send}
		if err = publisher.publishEventReward(session, player, 0, result); err != nil {
			return err
		}
		return h.sendResponse(session, true, 0)
	}
	quest, exists := playerQuest(player, int(request.RewardID))
	if !exists || quest.Status != model.QuestRewardReady {
		return h.sendResponse(session, false, 1)
	}
	result, err := h.executor.SelectQuestReward(ctx, player.ID, request.RewardID, append([]uint32(nil), request.EntryIDs...))
	if errors.Is(err, model.ErrQuestReferenceMissing) || errors.Is(err, model.ErrQuestEventNotFound) {
		return h.sendResponse(session, false, 1)
	}
	if errors.Is(err, model.ErrQuestReferenceUnsupported) || errors.Is(err, model.ErrQuestStateConflict) || errors.Is(err, model.ErrQuestInventoryFull) {
		return h.sendResponse(session, false, 2)
	}
	if err != nil {
		return err
	}
	publisher := &NPCTalkHandler{send: h.send}
	sourceUID := uint32(0)
	if interaction, ok := npcInteractions.Get(player.GetUniqueID()); ok {
		sourceUID = interaction.NPCUniqueID
	}
	if err = publisher.publishQuestCompletion(session, player, sourceUID, result); err != nil {
		return err
	}
	return h.sendResponse(session, true, 0)
}

func (h *QuestRewardSelectionHandler) sendResponse(session *server.Session, success bool, reason byte) error {
	packet := vsro188.BuildQuestRewardSelectionResponse(success, reason)
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	if session == nil {
		return fmt.Errorf("quest reward selection session is unavailable")
	}
	return session.Send(packet)
}
