package character

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/questwire"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type QuestActionHandler struct {
	dispatcher  model.QuestEventDispatcher
	send        func(*server.Session, network.Packet) error
	playerByUID func(uint32) (*model.Player, error)
}

// RegisterQuestActionHandler admits the two legacy authored-event requests
// only when a real dispatcher is supplied. The server must not register these
// opcodes with a nil/no-op dispatcher because legacy success means event 7 or
// 0x17 was actually executed.
func RegisterQuestActionHandler(target *server.Server, dispatcher model.QuestEventDispatcher) error {
	if target == nil || dispatcher == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	handler := &QuestActionHandler{dispatcher: dispatcher, playerByUID: service.GetWorldServiceInstance().GetPlayerByUniqueId}
	target.RegisterHandler(opcode.QuestInteractionRequest, handler.handleInteraction)
	target.RegisterHandler(opcode.QuestLocalActionRequest, handler.handleLocalAction)
	return nil
}

func (h *QuestActionHandler) handleInteraction(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.handle(ctx, session, packet, opcode.QuestInteractionResponse, model.QuestAuthoredInteraction)
}

func (h *QuestActionHandler) handleLocalAction(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.handle(ctx, session, packet, opcode.QuestLocalActionResponse, model.QuestAuthoredLocalAction)
}

func (h *QuestActionHandler) handle(ctx context.Context, session *server.Session, packet network.Packet, responseOpcode uint16, event model.QuestAuthoredEvent) error {
	if h == nil || h.dispatcher == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	if session == nil || session.UserContext.CharacterID <= 0 {
		return fmt.Errorf("quest action requires a selected character")
	}
	request, err := vsro188.DecodeQuestIDRequest(&packet)
	if err != nil {
		return fmt.Errorf("malformed quest action request: %w", err)
	}

	err = h.dispatcher.DispatchQuestEvent(ctx, session.UserContext.CharacterID, int(request.QuestID), event)
	success := err == nil
	reason := byte(0)
	if errors.Is(err, model.ErrQuestEventNotFound) {
		reason = 1
	} else if errors.Is(err, model.ErrQuestStateConflict) {
		reason = 2
	} else if err != nil {
		return err
	}
	if success && event == model.QuestAuthoredInteraction {
		if err = h.publishNativeInteraction(session, int(request.QuestID)); err != nil {
			return err
		}
	}
	response, buildErr := vsro188.BuildQuestResponse(responseOpcode, success, request.QuestID, reason)
	if buildErr != nil {
		return buildErr
	}
	if h.send != nil {
		return h.send(session, response)
	}
	return session.Send(response)
}

func (h *QuestActionHandler) publishNativeInteraction(session *server.Session, questID int) error {
	if key, exists := model.NativeInteractionTalkKey(questID); exists {
		packet, err := vsro188.BuildQuestNotification(key)
		if err != nil {
			return err
		}
		return h.sendPacket(session, packet)
	}
	if questID != model.MeetRocInteractionQuestID || session == nil || session.UserContext.UniqueID == 0 {
		return nil
	}
	if h.playerByUID == nil {
		return nil
	}
	player, err := h.playerByUID(session.UserContext.UniqueID)
	if err != nil || player == nil {
		return nil
	}
	count := uint64(0)
	for _, item := range player.GetInventory().Items {
		if item.GetRefObjectID() == model.MeetRocProgressItemRefID && item.StackSize > 0 {
			count += uint64(item.StackSize)
		}
	}
	for _, quest := range player.QuestSnapshot() {
		if quest.QuestID != 374 && quest.QuestID != 375 {
			continue
		}
		quest.Data1 = count
		packet, buildErr := questwire.BuildUpdate(quest, vsro188.QuestUpdateDelta)
		if buildErr != nil {
			return buildErr
		}
		if err = h.sendPacket(session, packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *QuestActionHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
