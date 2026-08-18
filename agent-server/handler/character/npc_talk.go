package character

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/sirupsen/logrus"
)

// NPCTalkHandler admits the stock 0x7046 request only for a nearby, known
// normal NPC and dispatches conversation responses whose stock script contract
// has been recovered.
type NPCTalkHandler struct {
	mu         sync.Mutex
	world      selectEntityWorld
	repository model.CharacterQuestRepository
	runtime    model.QuestRuntimeRepository
	send       func(*server.Session, network.Packet) error
}

// npcTalkActionConversation is the stock menu's "Talk to this person"
// action. Stores, teleporters and future quest controls use other non-zero
// action values, so request admission must not reject them merely because
// their service packet has not been implemented yet.
const npcTalkActionConversation = byte(2)

const (
	chulsanRefObjectID    = uint32(2003)
	jingyoRefObjectID     = uint32(2037)
	questListSelectorBase = byte(5)
)

type chulsanQuestDefinition struct {
	questID      int
	nameKey      string
	talkKey      string
	acceptedKey  string
	deniedKey    string
	progressKey  string
	fullKey      string
	completedKey string
}

var chulsanQuests = []chulsanQuestDefinition{
	{questID: model.QuestWeaponListID, nameKey: "SN_QNO_CH_SMITH_1", talkKey: "SN_TALK_QNO_CH_SMITH_1_01", acceptedKey: "SN_TALK_QNO_CH_SMITH_1_02", deniedKey: "SN_TALK_QNO_CH_SMITH_1_03", progressKey: "SN_TALK_QNO_CH_SMITH_1_04", completedKey: "SN_TALK_QNO_CH_SMITH_1_08"},
	{questID: model.QuestChulsanAngerID, nameKey: "SN_QNO_CH_SMITH_2", talkKey: "SN_TALK_QNO_CH_SMITH_2_01", acceptedKey: "SN_TALK_QNO_CH_SMITH_2_02", deniedKey: "SN_TALK_QNO_CH_SMITH_2_03", progressKey: "SN_TALK_QNO_CH_SMITH_2_04", fullKey: "SN_TALK_QNO_CH_SMITH_2_05", completedKey: "SN_TALK_QNO_CH_SMITH_2_06"},
}

func NewNPCTalkHandler(world selectEntityWorld, repositories ...model.CharacterQuestRepository) *NPCTalkHandler {
	handler := &NPCTalkHandler{world: world}
	if len(repositories) != 0 {
		handler.repository = repositories[0]
	}
	return handler
}

func RegisterNPCTalkHandler(target *server.Server, repository model.CharacterQuestRepository, runtimes ...model.QuestRuntimeRepository) error {
	if target == nil {
		return fmt.Errorf("NPC talk server is required")
	}
	if repository == nil {
		return model.ErrCharacterQuestRepositoryUnavailable
	}
	handler := NewNPCTalkHandler(service.GetWorldServiceInstance(), repository)
	if len(runtimes) == 0 || runtimes[0] == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	handler.runtime = runtimes[0]
	target.RegisterHandler(opcode.EntityTalkRequest, handler.handlePacket)
	target.RegisterHandler(opcode.QuestTalk, handler.handleQuestTalkPacket)
	return nil
}

func (h *NPCTalkHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	payloadLength := packet.Remaining()
	if payloadLength != 5 && payloadLength != 8 {
		return fmt.Errorf("NPC talk request payload length %d, want 5 or 8", payloadLength)
	}
	uniqueID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read NPC talk target: %w", err)
	}
	action, err := packet.ReadByte()
	if err != nil {
		return fmt.Errorf("read NPC talk action: %w", err)
	}
	if action == 0 {
		return fmt.Errorf("NPC talk action must not be zero")
	}
	if packet.Remaining() > 0 {
		trailing, err := packet.ReadBytes(packet.Remaining())
		if err != nil {
			return fmt.Errorf("read NPC talk trailing action bytes: %w", err)
		}
		for _, value := range trailing {
			if value != 0 {
				return fmt.Errorf("NPC talk request has non-zero trailing action data")
			}
		}
	}

	world := h.world
	if world == nil {
		world = service.GetWorldServiceInstance()
	}
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}
	if err := validateNPCSelection(world, player, uniqueID); err != nil {
		logrus.Warnf("ignored NPC talk player=%s uid=%d: %v", player.CharName, uniqueID, err)
		return nil
	}
	object, err := world.GetObjectByUniqueId(uniqueID)
	if err != nil {
		return nil
	}
	isNormalNPC := false
	if npc, ok := object.(*model.NPC); ok {
		isNormalNPC = npc != nil && npc.GetTypeInfo().IsNPCNpc()
	}
	isTeleportGate := model.IsTeleportGateObject(object)
	if !isNormalNPC && !isTeleportGate {
		logrus.Warnf("ignored non-normal NPC talk player=%s uid=%d", player.CharName, uniqueID)
		return nil
	}
	if isTeleportGate && action != 8 {
		logrus.Warnf("ignored teleport gate action player=%s uid=%d action=%d", player.CharName, uniqueID, action)
		return nil
	}

	npcInteractions.Record(player.GetUniqueID(), object.GetUniqueID(), object.GetRefObjectID(), action)
	logrus.Debugf("accepted NPC interaction player=%s characterID=%d uid=%d refObjID=%d action=%d", player.CharName, player.ID, uniqueID, object.GetRefObjectID(), action)
	if err := h.sendPacket(session, buildNPCTalkResponse(action)); err != nil {
		return err
	}
	if action == npcTalkActionConversation && strings.EqualFold(object.GetName(), "NPC_CH_SOLDIER_SO1") {
		return h.handleJingyoTalk(ctx, session, player)
	}
	if action == npcTalkActionConversation && strings.EqualFold(object.GetName(), "NPC_CH_SMITH") {
		entries := make([]string, len(chulsanQuests))
		for index, quest := range chulsanQuests {
			entries[index] = quest.nameKey
		}
		menu, err := buildQuestTalkPacket(4, "", entries)
		if err != nil {
			return err
		}
		return h.sendPacket(session, menu)
	}
	return nil
}

func (h *NPCTalkHandler) handleQuestTalkPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || session.UserContext.UniqueID == 0 || packet.Remaining() != 1 {
		if session != nil {
			logrus.Infof("quest trace rejected selection characterID=%d playerUID=%d payloadBytes=%d", session.UserContext.CharacterID, session.UserContext.UniqueID, packet.Remaining())
		}
		return fmt.Errorf("malformed quest talk selection")
	}
	selector, err := packet.ReadByte()
	if err != nil {
		return fmt.Errorf("read quest talk selector: %w", err)
	}
	interaction, ok := npcInteractions.Get(session.UserContext.UniqueID)
	if !ok || interaction.NPCRefObjectID != chulsanRefObjectID ||
		!HasRecentNPCInteraction(session.UserContext.UniqueID, interaction.NPCUniqueID, npcTalkActionConversation, 5*time.Minute) {
		logrus.Infof("quest trace ignored selection characterID=%d playerUID=%d selector=%d interaction=%+v present=%t", session.UserContext.CharacterID, session.UserContext.UniqueID, selector, interaction, ok)
		return nil
	}
	logrus.Infof("quest trace selection characterID=%d playerUID=%d selector=%d pendingQuestID=%d", session.UserContext.CharacterID, session.UserContext.UniqueID, selector, interaction.PendingQuestID)
	if interaction.PendingQuestID != 0 {
		switch selector {
		case questListSelectorBase:
			return h.handleChulsanOfferSelection(ctx, session, interaction, 1)
		case questListSelectorBase + 1:
			return h.handleChulsanOfferSelection(ctx, session, interaction, 2)
		default:
			logrus.Infof("quest trace ignored offer selector characterID=%d selector=%d pendingQuestID=%d", session.UserContext.CharacterID, selector, interaction.PendingQuestID)
			return nil
		}
	}
	index := int(selector - questListSelectorBase)
	if selector < questListSelectorBase || index >= len(chulsanQuests) {
		return nil
	}
	quest := chulsanQuests[index]
	world := h.world
	if world == nil {
		world = service.GetWorldServiceInstance()
	}
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}
	if active, exists := playerQuest(player, quest.questID); exists {
		switch active.Status {
		case model.QuestReadyToTurnInByUser:
			return h.completeChulsanQuest(ctx, session, interaction, player, quest)
		case model.QuestStartedByUser:
			// Re-publish the record in case the client missed its original Add
			// notification while the NPC dialogue remained open.
			added, buildErr := buildQuestAddPacket(active)
			if buildErr != nil {
				return buildErr
			}
			if err := h.sendPacket(session, added); err != nil {
				return err
			}
			progress, buildErr := buildQuestTalkPacket(1, quest.progressKey, nil)
			if buildErr != nil {
				return buildErr
			}
			return h.sendPacket(session, progress)
		case model.QuestCompleted, model.QuestCompletedMultipleTimes:
			return nil
		}
	}
	if !npcInteractions.SelectQuest(session.UserContext.UniqueID, quest.questID) {
		return nil
	}
	offer, err := buildQuestTalkPacket(4, quest.talkKey, []string{
		"SN_TALK_COMMON_ACCEPT",
		"SN_TALK_COMMON_DENY",
	})
	if err != nil {
		return err
	}
	return h.sendPacket(session, offer)
}

func (h *NPCTalkHandler) handleChulsanOfferSelection(ctx context.Context, session *server.Session, interaction NPCInteraction, selector byte) error {
	quest, ok := chulsanQuestByID(interaction.PendingQuestID)
	if !ok {
		logrus.Infof("quest trace ignored offer selection characterID=%d selector=%d pendingQuestID=%d", session.UserContext.CharacterID, selector, interaction.PendingQuestID)
		return nil
	}
	if selector == 2 {
		npcInteractions.ClearSelectedQuest(session.UserContext.UniqueID)
		denied, err := buildQuestTalkPacket(1, quest.deniedKey, nil)
		if err != nil {
			return err
		}
		return h.sendPacket(session, denied)
	}
	if h.repository == nil {
		return model.ErrCharacterQuestRepositoryUnavailable
	}
	world := h.world
	if world == nil {
		world = service.GetWorldServiceInstance()
	}
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}
	if err := validateNPCSelection(world, player, interaction.NPCUniqueID); err != nil {
		logrus.Infof("quest trace rejected acceptance characterID=%d questID=%d: %v", session.UserContext.CharacterID, quest.questID, err)
		return nil
	}
	logrus.Infof("quest trace activating characterID=%d questID=%d", session.UserContext.CharacterID, quest.questID)
	activated, err := h.repository.Activate(ctx, session.UserContext.CharacterID, model.CharacterQuest{
		QuestID: quest.questID, Status: model.QuestStartedByUser, AchievementCount: 1,
	})
	if errors.Is(err, model.ErrCharacterQuestNotAvailable) {
		logrus.Infof("quest trace activation unavailable characterID=%d questID=%d", session.UserContext.CharacterID, quest.questID)
		npcInteractions.ClearSelectedQuest(session.UserContext.UniqueID)
		return nil
	}
	if err != nil {
		logrus.Infof("quest trace activation failed characterID=%d questID=%d: %v", session.UserContext.CharacterID, quest.questID, err)
		return err
	}
	logrus.Infof("quest trace activated characterID=%d questID=%d status=%d", session.UserContext.CharacterID, activated.QuestID, activated.Status)
	player.CommitQuestActivation(activated)
	npcInteractions.ClearSelectedQuest(session.UserContext.UniqueID)
	added, err := buildQuestAddPacket(activated)
	if err != nil {
		return err
	}
	if err := h.sendPacket(session, added); err != nil {
		return err
	}
	accepted, err := buildQuestTalkPacket(1, quest.acceptedKey, nil)
	if err != nil {
		return err
	}
	return h.sendPacket(session, accepted)
}

func (h *NPCTalkHandler) handleJingyoTalk(ctx context.Context, session *server.Session, player *model.Player) error {
	quest, exists := playerQuest(player, model.QuestWeaponListID)
	if !exists {
		return nil
	}
	if quest.Status == model.QuestReadyToTurnInByUser {
		message, err := buildQuestTalkPacket(1, "SN_TALK_QNO_CH_SMITH_1_07", nil)
		if err != nil {
			return err
		}
		return h.sendPacket(session, message)
	}
	if quest.Status != model.QuestStartedByUser {
		return nil
	}
	if h.runtime == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	result, err := h.runtime.GrantMissionItem(ctx, player.ID, model.QuestWeaponListID)
	if errors.Is(err, model.ErrQuestInventoryFull) {
		message, buildErr := buildQuestTalkPacket(1, "SN_TALK_QNO_CH_SMITH_1_05", nil)
		if buildErr != nil {
			return buildErr
		}
		return h.sendPacket(session, message)
	}
	if err != nil {
		return err
	}
	player.CommitQuestItemGrant(result)
	if err := h.sendPacket(session, buildQuestItemAdded(result.Added)); err != nil {
		return err
	}
	update, err := buildQuestStatePacket(result.Quest)
	if err != nil {
		return err
	}
	if err := h.sendPacket(session, update); err != nil {
		return err
	}
	message, err := buildQuestTalkPacket(1, "SN_TALK_QNO_CH_SMITH_1_09", nil)
	if err != nil {
		return err
	}
	return h.sendPacket(session, message)
}

func (h *NPCTalkHandler) completeChulsanQuest(ctx context.Context, session *server.Session, interaction NPCInteraction, player *model.Player, quest chulsanQuestDefinition) error {
	if h.runtime == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	result, err := h.runtime.Complete(ctx, player.ID, quest.questID)
	if errors.Is(err, model.ErrQuestInventoryFull) {
		if quest.fullKey == "" {
			return nil
		}
		message, buildErr := buildQuestTalkPacket(1, quest.fullKey, nil)
		if buildErr != nil {
			return buildErr
		}
		return h.sendPacket(session, message)
	}
	if err != nil {
		return err
	}
	previousGold := player.GoldSnapshot()
	player.CommitQuestCompletion(result)
	npcInteractions.ClearSelectedQuest(session.UserContext.UniqueID)
	if result.RemovedSlot != nil {
		if err := h.sendPacket(session, buildQuestItemRemoved(*result.RemovedSlot)); err != nil {
			return err
		}
	}
	if result.Added != nil {
		if err := h.sendPacket(session, buildQuestItemAdded(*result.Added)); err != nil {
			return err
		}
	}
	if result.Gold != previousGold {
		if err := h.sendPacket(session, buildQuestGoldUpdate(result.Gold)); err != nil {
			return err
		}
	}
	if err := h.sendPacket(session, buildExperienceUpdate(interaction.NPCUniqueID, result.Experience, result.SkillExp, result.Progress)); err != nil {
		return err
	}
	if result.Progress.LevelsGained > 0 {
		stats := player.BuildStatsUpdatePacket()
		if err := h.sendPacket(session, stats); err != nil {
			return err
		}
	}
	if err := h.sendPacket(session, buildQuestRemovePacket(result.Quest.QuestID)); err != nil {
		return err
	}
	message, err := buildQuestTalkPacket(1, quest.completedKey, nil)
	if err != nil {
		return err
	}
	return h.sendPacket(session, message)
}

func playerQuest(player *model.Player, questID int) (model.CharacterQuest, bool) {
	for _, quest := range player.QuestSnapshot() {
		if quest.QuestID == questID {
			return quest, true
		}
	}
	return model.CharacterQuest{}, false
}

func chulsanQuestByID(questID int) (chulsanQuestDefinition, bool) {
	for _, quest := range chulsanQuests {
		if quest.questID == questID {
			return quest, true
		}
	}
	return chulsanQuestDefinition{}, false
}

func buildQuestAddPacket(quest model.CharacterQuest) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestUpdate
	packet.WriteByte(1)
	packet.WriteUInt32(uint32(quest.QuestID))
	if err := model.WriteActiveQuestRecord(&packet, quest); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func buildQuestStatePacket(quest model.CharacterQuest) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestUpdate
	packet.WriteByte(2)
	packet.WriteUInt32(uint32(quest.QuestID))
	if err := model.WriteActiveQuestRecord(&packet, quest); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func buildQuestRemovePacket(questID int) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestUpdate
	packet.WriteByte(3)
	packet.WriteUInt32(uint32(questID))
	return packet
}

func buildQuestItemAdded(mutation model.QuestItemMutation) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(14) // SP_ADD_ITEM_BY_SERVER
	packet.WriteByte(mutation.Slot)
	packet.WriteByte(0x3C)
	model.WriteRentInfo(&packet, mutation.Item)
	packet.WriteUInt32(mutation.Item.GetRefObjectID())
	model.WriteInventoryItem(&packet, mutation.Item)
	return packet
}

func buildQuestItemRemoved(slot byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(15) // SP_DEL_ITEM_BY_SERVER
	packet.WriteByte(slot)
	return packet
}

func buildQuestGoldUpdate(gold uint64) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdatePoints
	packet.WriteByte(1)
	packet.WriteUInt64(gold)
	packet.WriteByte(0)
	return packet
}

func (h *NPCTalkHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func buildNPCTalkResponse(action byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityTalkResponse
	p.WriteByte(1)
	p.WriteByte(action)
	return p
}
