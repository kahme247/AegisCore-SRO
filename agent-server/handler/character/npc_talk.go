package character

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/questwire"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
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
	offers     model.QuestOfferRepository
	countdowns *QuestCountdownService
	dayLimits  *QuestDayLimitService
	quotations interface {
		Prices(context.Context, uint32) (map[uint32]uint32, error)
	}
	safeTrade        model.SafeTradeRepository
	safeTradeOption  byte
	safeTradeLimit   byte
	send             func(*server.Session, network.Packet) error
	instanceEntries  *service.QuestInstanceEntryService
	instanceTeleport interface {
		SubmitQuestInstanceEntry(model.QuestInstance, []*model.Player) error
	}
	dimensionHoleEntry interface {
		Enter(context.Context, uint32, *model.Player) (service.DimensionHoleAdmission, error)
	}
}

// npcTalkActionConversation is the stock menu's "Talk to this person"
// action. Stores, teleporters and future quest controls use other non-zero
// action values, so request admission must not reject them merely because
// their service packet has not been implemented yet.
const npcTalkActionConversation = byte(2)

const questTalkNoticeChoice = "SN_TALK_COMMON_OK"

func safeTradeAdmissionUpdate(admission model.SafeTradeAdmission, limit byte) vsro188.SafeTradeUpdate {
	update := vsro188.SafeTradeUpdate{Code: admission.Code}
	if admission.Code == vsro188.JobSafeTradeCodeProgress {
		update.Active, update.Current, update.Limit = true, admission.State.AbleCount, limit
	}
	return update
}

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
	handler := &NPCTalkHandler{world: world, dayLimits: NewQuestDayLimitService(manager.GetGameTimeManagerInstance().CurrentWorldTime)}
	if len(repositories) != 0 {
		handler.repository = repositories[0]
	}
	return handler
}

func (h *NPCTalkHandler) ConfigureQuotations(repository interface {
	Prices(context.Context, uint32) (map[uint32]uint32, error)
}) {
	if h != nil {
		h.quotations = repository
	}
}

func RegisterNPCTalkHandler(target *server.Server, repository model.CharacterQuestRepository, runtime model.QuestRuntimeRepository, offers model.QuestOfferRepository, safeTrade model.SafeTradeRepository, safeTradeOption, safeTradeLimit byte, countdowns ...*QuestCountdownService) (*NPCTalkHandler, error) {
	if target == nil {
		return nil, fmt.Errorf("NPC talk server is required")
	}
	if repository == nil {
		return nil, model.ErrCharacterQuestRepositoryUnavailable
	}
	handler := NewNPCTalkHandler(service.GetWorldServiceInstance(), repository)
	if runtime == nil {
		return nil, model.ErrQuestRuntimeUnavailable
	}
	if offers == nil {
		return nil, model.ErrQuestOfferRepositoryUnavailable
	}
	handler.runtime = runtime
	handler.offers = offers
	if len(countdowns) != 0 {
		handler.countdowns = countdowns[0]
	}
	if safeTrade != nil {
		handler.ConfigureQuotations(safeTrade)
		handler.safeTrade = safeTrade
		handler.safeTradeOption = safeTradeOption
		handler.safeTradeLimit = safeTradeLimit
	}
	target.RegisterHandler(opcode.EntityTalkRequest, handler.handlePacket)
	target.RegisterHandler(opcode.QuestTalk, handler.handleQuestTalkPacket)
	return handler, nil
}

func (h *NPCTalkHandler) ConfigureQuestInstanceEntry(entries *service.QuestInstanceEntryService, teleport interface {
	SubmitQuestInstanceEntry(model.QuestInstance, []*model.Player) error
}) {
	if h != nil {
		h.instanceEntries, h.instanceTeleport = entries, teleport
	}
}

// ConfigureDimensionHoleEntry connects the native INS_QUEST_TELEPORT
// conversation action to its runtime-owned admission and loading boundary.
func (h *NPCTalkHandler) ConfigureDimensionHoleEntry(entry interface {
	Enter(context.Context, uint32, *model.Player) (service.DimensionHoleAdmission, error)
}) {
	if h != nil {
		h.dimensionHoleEntry = entry
	}
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
	portal, isDimensionHolePortal := object.(*model.DimensionHolePortal)
	isDimensionHolePortal = isDimensionHolePortal && model.IsDimensionHolePortalObject(portal)
	isNormalNPC := false
	if npc, ok := object.(*model.NPC); ok {
		isNormalNPC = npc != nil && npc.GetTypeInfo().IsNPCNpc()
	}
	isTeleportGate := model.IsTeleportGateObject(object)
	if !isNormalNPC && !isTeleportGate && !isDimensionHolePortal {
		logrus.Warnf("ignored non-normal NPC talk player=%s uid=%d", player.CharName, uniqueID)
		return nil
	}
	if isDimensionHolePortal {
		if action != npcTalkActionConversation || h.dimensionHoleEntry == nil {
			return nil
		}
		if err := h.sendPacket(session, buildNPCTalkResponse(action)); err != nil {
			return err
		}
		if _, entryErr := h.dimensionHoleEntry.Enter(ctx, portal.GetUniqueID(), player); entryErr != nil {
			logrus.WithError(entryErr).WithFields(logrus.Fields{
				"uid": player.GetUniqueID(), "portal_uid": portal.GetUniqueID(),
			}).Debug("rejected Dimension Hole entry")
		}
		return nil
	}
	if isTeleportGate && action != 8 {
		logrus.Warnf("ignored teleport gate action player=%s uid=%d action=%d", player.CharName, uniqueID, action)
		return nil
	}
	if !isTeleportGate && model.IsTeleportServiceCodeName(object.GetName()) && action == teleportTaskUse {
		npcInteractions.Record(player.GetUniqueID(), object.GetUniqueID(), object.GetRefObjectID(), teleportTaskUse)
	}

	npcInteractions.Record(player.GetUniqueID(), object.GetUniqueID(), object.GetRefObjectID(), action)
	logrus.Debugf("accepted NPC interaction player=%s characterID=%d uid=%d refObjID=%d action=%d", player.CharName, player.ID, uniqueID, object.GetRefObjectID(), action)
	if err := h.sendPacket(session, buildNPCTalkResponse(action)); err != nil {
		return err
	}
	if isNormalNPC {
		specialMerchant, priceErr := h.publishJobPrices(ctx, session, object.GetRefObjectID())
		if priceErr != nil {
			return priceErr
		}
		if specialMerchant && player.GetJob().Type == model.PlayerJobTrader && h.safeTrade != nil {
			admission, admitErr := h.safeTrade.Admit(ctx, player.ID, h.safeTradeOption, h.safeTradeLimit)
			if admitErr != nil {
				return admitErr
			}
			status, encodeErr := (vsro188.JobSafeTradeCodec{}).Encode(safeTradeAdmissionUpdate(admission, h.safeTradeLimit))
			if encodeErr != nil {
				return encodeErr
			}
			if err := h.sendPacket(session, status); err != nil {
				return err
			}
		}
	}
	if action == npcTalkActionConversation {
		if handled, instanceErr := h.handleQuestInstanceDialogue(session, player, object.GetName()); handled || instanceErr != nil {
			return instanceErr
		}
		delivered, deliveryErr := h.handleStandardDeliveries(ctx, session, player, object.GetRefObjectID())
		if deliveryErr != nil {
			return deliveryErr
		}
		if delivered {
			return nil
		}
		dialog, dialogErr := h.handleDialogObjectives(ctx, session, player, object.GetRefObjectID())
		if dialogErr != nil {
			return dialogErr
		}
		if dialog {
			return nil
		}
	}
	if action == npcTalkActionConversation && strings.EqualFold(object.GetName(), "NPC_CH_SOLDIER_SO1") {
		return h.handleJingyoTalk(ctx, session, player)
	}
	if action == npcTalkActionConversation {
		if h.offers == nil {
			if !strings.EqualFold(object.GetName(), "NPC_CH_SMITH") {
				return nil
			}
			entries := make([]string, len(chulsanQuests))
			questIDs := make([]int, len(chulsanQuests))
			for index, quest := range chulsanQuests {
				entries[index] = quest.nameKey
				questIDs[index] = quest.questID
			}
			_ = npcInteractions.SetQuestOffers(player.GetUniqueID(), questIDs)
			menu, menuErr := buildQuestTalkPacket(4, "", entries)
			if menuErr != nil {
				return menuErr
			}
			return h.sendPacket(session, menu)
		}
		offers, offerErr := h.offers.ListAvailable(ctx, player.ID, object.GetRefObjectID())
		if offerErr != nil {
			return offerErr
		}
		offers = h.availableDayLimitOffers(offers)
		if len(offers) == 0 {
			// npcchat.txt associates the ordinary conversation action with the
			// NPC's _QS speech key. Without this packet B046 acknowledges action
			// 2, but the client has no dialogue body to display and the button
			// appears inert whenever the NPC has no currently available quest.
			greeting, buildErr := buildQuestTalkPacket(2, npcQuestSpeechKey(object.GetName()), nil)
			if buildErr != nil {
				return buildErr
			}
			return h.sendPacket(session, greeting)
		}
		entries := make([]string, len(offers))
		questIDs := make([]int, len(offers))
		for index, offer := range offers {
			entries[index] = offer.NameKey
			questIDs[index] = offer.QuestID
		}
		if !npcInteractions.SetQuestOffers(player.GetUniqueID(), questIDs) {
			return nil
		}
		menu, err := buildQuestTalkPacket(4, "", entries)
		if err != nil {
			return err
		}
		return h.sendPacket(session, menu)
	}
	return nil
}

func npcQuestSpeechKey(codeName string) string {
	return "SN_" + strings.ToUpper(strings.TrimSpace(codeName)) + "_QS"
}

func (h *NPCTalkHandler) handleQuestInstanceDialogue(session *server.Session, player *model.Player, npcCodeName string) (bool, error) {
	if h == nil || h.instanceEntries == nil || h.instanceTeleport == nil || session == nil || player == nil {
		return false, nil
	}
	questID, promptKey, choices := 0, "", []string(nil)
	switch {
	case strings.EqualFold(npcCodeName, "NPC_RM_SLAVE2") && service.QuestInstanceEligible(player, 141):
		questID, promptKey = 141, "SN_TALK_QNO_RM_OLDWOMAN_6_07"
		choices = []string{"SN_TALK_QNO_RM_OLDWOMAN_6_28", "SN_TALK_COMMON_DENY"}
	case strings.EqualFold(npcCodeName, "NPC_CH_MOONSHADOW") && service.QuestInstanceEligible(player, 311):
		questID, promptKey = 311, "SN_TALK_QNO_TQ_BAEKRYEONG_17_24"
		choices = []string{"SN_TALK_QNO_TQ_BAEKRYEONG_17_23"}
	default:
		return false, nil
	}
	if !npcInteractions.SetInstanceEntry(player.GetUniqueID(), questID) {
		return true, nil
	}
	prompt, err := buildQuestTalkPacket(questTalkExtendedTypeMin, promptKey, choices)
	if err != nil {
		return true, err
	}
	return true, h.sendPacket(session, prompt)
}

func (h *NPCTalkHandler) handleDialogObjectives(ctx context.Context, session *server.Session, player *model.Player, npcRefObjectID uint32) (bool, error) {
	if h == nil || h.runtime == nil || session == nil || player == nil || npcRefObjectID == 0 {
		return false, nil
	}
	prompts, err := h.runtime.ListDialogsAtNPC(ctx, player.ID, npcRefObjectID)
	if err != nil {
		return false, err
	}
	if len(prompts) == 0 {
		return false, nil
	}
	if !npcInteractions.SetDialogs(player.GetUniqueID(), prompts) {
		return false, nil
	}
	entries := make([]string, len(prompts))
	for index, prompt := range prompts {
		entries[index] = prompt.DialogueKey
	}
	return true, h.sendQuestTalkMenu(session, entries)
}

func (h *NPCTalkHandler) publishJobPrices(ctx context.Context, session *server.Session, npcRefObjectID uint32) (bool, error) {
	if h == nil || h.quotations == nil || npcRefObjectID == 0 {
		return false, nil
	}
	prices, err := h.quotations.Prices(ctx, npcRefObjectID)
	if err != nil {
		return false, err
	}
	if len(prices) == 0 {
		return false, nil
	}
	entries := make([]vsro188.JobPrice, 0, len(prices))
	for refItemID, price := range prices {
		entries = append(entries, vsro188.JobPrice{RefItemID: refItemID, Price: price})
	}
	slices.SortFunc(entries, func(left, right vsro188.JobPrice) int {
		return cmp.Compare(left.RefItemID, right.RefItemID)
	})
	packet, err := (vsro188.JobPriceUpdateCodec{}).Encode(entries)
	if err != nil {
		return false, err
	}
	return true, h.sendPacket(session, packet)
}

func (h *NPCTalkHandler) handleStandardDeliveries(ctx context.Context, session *server.Session, player *model.Player, npcRefObjectID uint32) (bool, error) {
	if h == nil || h.runtime == nil || player == nil || npcRefObjectID == 0 {
		return false, nil
	}
	prompts, err := h.runtime.ListDeliveriesAtNPC(ctx, player.ID, npcRefObjectID)
	if err != nil {
		return false, err
	}
	if len(prompts) == 0 {
		return false, nil
	}
	if !npcInteractions.SetDeliveries(player.GetUniqueID(), prompts) {
		return false, nil
	}
	entries := make([]string, len(prompts))
	for index, prompt := range prompts {
		entries[index] = prompt.DialogueKey
	}
	return true, h.sendQuestTalkMenu(session, entries)
}

func (h *NPCTalkHandler) sendQuestTalkMenu(session *server.Session, entries []string) error {
	menu, err := buildQuestTalkPacket(questTalkExtendedTypeMin, "", entries)
	if err != nil {
		return err
	}
	return h.sendPacket(session, menu)
}

func (h *NPCTalkHandler) completeStandardDelivery(ctx context.Context, session *server.Session, player *model.Player, questID int, npcRefObjectID uint32) error {
	player.LockEconomy()
	defer player.UnlockEconomy()
	batch, err := h.runtime.DeliverItemsAtNPC(ctx, player.ID, questID, npcRefObjectID)
	if err != nil {
		return err
	}
	if len(batch.Results) != 1 {
		return model.ErrQuestStateConflict
	}
	player.CommitInventory(batch.Inventory)
	for _, result := range batch.Results {
		player.CommitQuestMutation(result.Quest)
	}
	for _, slot := range batch.ChangedSlots {
		if err = h.sendPacket(session, buildQuestItemRemoved(slot)); err != nil {
			return err
		}
		if item, exists := batch.Inventory.Items[slot]; exists {
			if err = h.sendPacket(session, buildQuestItemAdded(model.QuestItemMutation{Slot: slot, Item: item})); err != nil {
				return err
			}
		}
	}
	for _, result := range batch.Results {
		update, buildErr := buildQuestStatePacket(result.Quest)
		if buildErr != nil {
			return buildErr
		}
		if err = h.sendPacket(session, update); err != nil {
			return err
		}
	}
	for _, result := range batch.Results {
		if result.DialogueKey != "" {
			if err = h.sendQuestNotice(session, result.DialogueKey); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *NPCTalkHandler) handleQuestTalkPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || session.UserContext.UniqueID == 0 {
		if session != nil {
			logrus.Infof("quest trace rejected selection characterID=%d playerUID=%d payloadBytes=%d", session.UserContext.CharacterID, session.UserContext.UniqueID, packet.Remaining())
		}
		return fmt.Errorf("malformed quest talk selection")
	}
	selection, err := vsro188.DecodeQuestTalkSelection(&packet)
	if err != nil {
		return fmt.Errorf("read quest talk selector: %w", err)
	}
	selector := selection.Selection
	interaction, ok := npcInteractions.Get(session.UserContext.UniqueID)
	if !ok ||
		!HasRecentNPCInteraction(session.UserContext.UniqueID, interaction.NPCUniqueID, npcTalkActionConversation, 5*time.Minute) {
		logrus.Infof("quest trace ignored selection characterID=%d playerUID=%d selector=%d interaction=%+v present=%t", session.UserContext.CharacterID, session.UserContext.UniqueID, selector, interaction, ok)
		return nil
	}
	logrus.Infof("quest trace selection characterID=%d playerUID=%d selector=%d pendingQuestID=%d", session.UserContext.CharacterID, session.UserContext.UniqueID, selector, interaction.PendingQuestID)
	if interaction.PendingNoticeClose {
		if selector != questListSelectorBase {
			return nil
		}
		npcInteractions.Clear(session.UserContext.UniqueID)
		session.UserContext.SelectedNPCUniqueID = 0
		session.UserContext.SelectedNPCRefObjID = 0
		return h.sendPacket(session, buildEntityDeselectResponse())
	}
	if interaction.PendingInstanceQuestID != 0 {
		if selector != questListSelectorBase || h.instanceEntries == nil || h.instanceTeleport == nil {
			npcInteractions.Clear(session.UserContext.UniqueID)
			return nil
		}
		world := h.world
		if world == nil {
			world = service.GetWorldServiceInstance()
		}
		player, playerErr := resolveSelectingPlayer(world, session)
		if playerErr != nil {
			return playerErr
		}
		if err = validateNPCSelection(world, player, interaction.NPCUniqueID); err != nil {
			return nil
		}
		prepared, prepareErr := h.instanceEntries.Prepare(ctx, player, interaction.PendingInstanceQuestID)
		if prepareErr != nil {
			return nil
		}
		if err = h.instanceTeleport.SubmitQuestInstanceEntry(prepared.Instance, prepared.Members); err != nil {
			_ = h.instanceEntries.Abort(ctx, prepared)
			return err
		}
		npcInteractions.Clear(session.UserContext.UniqueID)
		return nil
	}
	if interaction.PendingDeliveryQuestID != 0 {
		if selector != questListSelectorBase || h.runtime == nil {
			return nil
		}
		world := h.world
		if world == nil {
			world = service.GetWorldServiceInstance()
		}
		player, playerErr := resolveSelectingPlayer(world, session)
		if playerErr != nil {
			return playerErr
		}
		if err = validateNPCSelection(world, player, interaction.NPCUniqueID); err != nil {
			return nil
		}
		err = h.completeStandardDelivery(ctx, session, player, interaction.PendingDeliveryQuestID, interaction.NPCRefObjectID)
		if errors.Is(err, model.ErrQuestStateConflict) {
			npcInteractions.ClearSelectedDelivery(session.UserContext.UniqueID)
			return nil
		}
		if err != nil {
			return err
		}
		npcInteractions.ClearSelectedDelivery(session.UserContext.UniqueID)
		return nil
	}
	if len(interaction.DeliveryQuestIDs) != 0 {
		index := int(selector - questListSelectorBase)
		if selector < questListSelectorBase || index >= len(interaction.DeliveryQuestIDs) || index >= len(interaction.DeliveryKeys) {
			return nil
		}
		if !npcInteractions.SelectDelivery(session.UserContext.UniqueID, interaction.DeliveryQuestIDs[index]) {
			return nil
		}
		prompt, buildErr := buildQuestTalkPacket(questTalkExtendedTypeMin, interaction.DeliveryKeys[index], []string{questTalkNoticeChoice})
		if buildErr != nil {
			return buildErr
		}
		return h.sendPacket(session, prompt)
	}
	if interaction.PendingDialogQuestID != 0 {
		if selector != questListSelectorBase || h.runtime == nil {
			return nil
		}
		progress, completeErr := h.runtime.CompleteDialog(ctx, session.UserContext.CharacterID, interaction.PendingDialogQuestID, interaction.NPCRefObjectID)
		if completeErr != nil {
			if errors.Is(completeErr, model.ErrQuestStateConflict) {
				npcInteractions.ClearSelectedDialog(session.UserContext.UniqueID)
				return nil
			}
			return completeErr
		}
		world := h.world
		if world == nil {
			world = service.GetWorldServiceInstance()
		}
		player, playerErr := resolveSelectingPlayer(world, session)
		if playerErr != nil {
			return playerErr
		}
		player.CommitQuestMutation(progress.Quest)
		update, buildErr := buildQuestStatePacket(progress.Quest)
		if buildErr != nil {
			return buildErr
		}
		if err := h.sendPacket(session, update); err != nil {
			return err
		}
		npcInteractions.ClearSelectedDialog(session.UserContext.UniqueID)
		return h.sendQuestNotice(session, questTalkNoticeChoice)
	}
	if len(interaction.DialogQuestIDs) != 0 {
		index := int(selector - questListSelectorBase)
		if selector < questListSelectorBase || index >= len(interaction.DialogQuestIDs) || index >= len(interaction.DialogKeys) {
			return nil
		}
		if !npcInteractions.SelectDialog(session.UserContext.UniqueID, interaction.DialogQuestIDs[index]) {
			return nil
		}
		prompt, buildErr := buildQuestTalkPacket(questTalkExtendedTypeMin, interaction.DialogKeys[index], []string{questTalkNoticeChoice})
		if buildErr != nil {
			return buildErr
		}
		return h.sendPacket(session, prompt)
	}
	if interaction.PendingQuestID != 0 {
		offer, exists, offerErr := h.availableOffer(ctx, session.UserContext.CharacterID, interaction.NPCRefObjectID, interaction.PendingQuestID)
		if offerErr != nil {
			return offerErr
		}
		if !exists {
			return nil
		}
		world := h.world
		if world == nil {
			world = service.GetWorldServiceInstance()
		}
		player, playerErr := resolveSelectingPlayer(world, session)
		if playerErr != nil {
			return playerErr
		}
		if active, activeExists := playerQuest(player, offer.QuestID); activeExists && active.Status == model.QuestRewardReady {
			quest := questDefinitionForOffer(offer)
			if offer.SelectionCount != 0 {
				return h.sendQuestRewardPrompt(session, quest.questID, quest.completedKey)
			}
			switch selector {
			case questListSelectorBase:
				return h.completeChulsanQuest(ctx, session, interaction, player, quest)
			case questListSelectorBase + 1:
				npcInteractions.Clear(session.UserContext.UniqueID)
				return nil
			default:
				return nil
			}
		}
		switch selector {
		case questListSelectorBase:
			return h.handleOfferSelection(ctx, session, interaction, offer, 1)
		case questListSelectorBase + 1:
			return h.handleOfferSelection(ctx, session, interaction, offer, 2)
		default:
			logrus.Infof("quest trace ignored offer selector characterID=%d selector=%d pendingQuestID=%d", session.UserContext.CharacterID, selector, interaction.PendingQuestID)
			return nil
		}
	}
	index := int(selector - questListSelectorBase)
	if selector < questListSelectorBase || index >= len(interaction.OfferedQuestIDs) {
		return nil
	}
	offer, exists, err := h.availableOffer(ctx, session.UserContext.CharacterID, interaction.NPCRefObjectID, interaction.OfferedQuestIDs[index])
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	quest := questDefinitionForOffer(offer)
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
		case model.QuestRewardReady:
			if !npcInteractions.SelectQuest(session.UserContext.UniqueID, quest.questID) {
				return nil
			}
			if offer.SelectionCount != 0 {
				return h.sendQuestRewardPrompt(session, quest.questID, quest.completedKey)
			}
			rewardMenu, buildErr := buildQuestTalkPacket(questTalkExtendedTypeMin, quest.completedKey, []string{
				"SN_TALK_COMMON_REWARD",
				"SN_TALK_COMMON_EXIT",
			})
			if buildErr != nil {
				return buildErr
			}
			return h.sendPacket(session, rewardMenu)
		case model.QuestActive, model.QuestProgressed:
			// Re-publish the record in case the client missed its original Add
			// notification while the NPC dialogue remained open.
			added, buildErr := buildQuestAddPacket(active)
			if buildErr != nil {
				return buildErr
			}
			if err := h.sendPacket(session, added); err != nil {
				return err
			}
			return h.sendQuestNotice(session, quest.progressKey)
		case model.QuestClosed:
			return nil
		}
	}
	if !npcInteractions.SelectQuest(session.UserContext.UniqueID, quest.questID) {
		return nil
	}
	offerPacket, err := buildQuestTalkPacket(4, quest.talkKey, []string{
		"SN_TALK_COMMON_ACCEPT",
		"SN_TALK_COMMON_DENY",
	})
	if err != nil {
		return err
	}
	return h.sendPacket(session, offerPacket)
}

func (h *NPCTalkHandler) handleOfferSelection(ctx context.Context, session *server.Session, interaction NPCInteraction, offer model.QuestOffer, selector byte) error {
	quest := questDefinitionForOffer(offer)
	if selector == 2 {
		return h.sendQuestNotice(session, quest.deniedKey)
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
	achievementCount := uint16(0)
	if existing, exists := playerQuest(player, quest.questID); exists {
		achievementCount = existing.AchievementCount
	}
	var objectiveKeys [4]string
	copy(objectiveKeys[:], offer.ObjectiveKeys)
	activationResult := model.QuestActivationResult{}
	if offer.DeliveryItems {
		if h.runtime == nil {
			return model.ErrQuestRuntimeUnavailable
		}
		player.LockEconomy()
		defer player.UnlockEconomy()
	}
	activate := func() (model.CharacterQuest, error) {
		status := model.QuestActive
		if offer.InstantReady {
			status = model.QuestRewardReady
		}
		questState := model.CharacterQuest{
			QuestID: quest.questID, Status: status, AchievementCount: achievementCount,
			QuestCodeName: offer.CodeName, ObjectiveKey: offer.ObjectiveKey, ObjectiveKeys: objectiveKeys, TaskCount: offer.TaskCount,
			TimerDuration: offer.TimerDuration, SharedProgress: offer.SharedProgress,
		}
		if offer.DeliveryItems {
			var activateErr error
			activationResult, activateErr = h.runtime.ActivateDeliveryQuest(ctx, session.UserContext.CharacterID, questState)
			return activationResult.Quest, activateErr
		}
		return h.repository.Activate(ctx, session.UserContext.CharacterID, questState)
	}
	activated, err := h.dayLimits.WithActivation(offer, activate)
	if errors.Is(err, model.ErrQuestInventoryFull) {
		npcInteractions.ClearSelectedQuest(session.UserContext.UniqueID)
		return h.sendQuestNotice(session, quest.fullKey)
	}
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
	if offer.DeliveryItems {
		player.CommitQuestActivationWithItems(activationResult)
	} else {
		player.CommitQuestActivation(activated)
	}
	added, err := buildQuestAddPacket(activated)
	if err != nil {
		return err
	}
	if err := h.sendPacket(session, added); err != nil {
		return err
	}
	for _, item := range activationResult.AddedItems {
		if err := h.sendPacket(session, buildQuestItemAdded(item)); err != nil {
			return err
		}
	}
	if h.countdowns != nil && activated.TimerDuration != 0 {
		if err := h.countdowns.Start(ctx, session, player, activated); err != nil {
			return err
		}
	}
	return h.sendQuestNotice(session, quest.acceptedKey)
}

func (h *NPCTalkHandler) availableOffer(ctx context.Context, characterID int, npcRefObjectID uint32, questID int) (model.QuestOffer, bool, error) {
	if h == nil || h.offers == nil {
		quest, ok := chulsanQuestByID(questID)
		if !ok || npcRefObjectID != chulsanRefObjectID {
			return model.QuestOffer{}, false, nil
		}
		taskCount := byte(0)
		objectiveKey := "SN_CON_QNO_CH_SMITH_1"
		if questID == model.QuestChulsanAngerID {
			taskCount = 1
			objectiveKey = "SN_CON_QNO_CH_SMITH_2"
		}
		return model.QuestOffer{
			QuestID: questID, NameKey: quest.nameKey, PayKey: quest.talkKey,
			PayContentsKey: quest.acceptedKey, ObjectiveKey: objectiveKey, TaskCount: taskCount,
		}, true, nil
	}
	offers, err := h.offers.ListAvailable(ctx, characterID, npcRefObjectID)
	if err != nil {
		return model.QuestOffer{}, false, err
	}
	for _, offer := range offers {
		if offer.QuestID == questID && (h.dayLimits == nil || h.dayLimits.Available(offer)) {
			return offer, true, nil
		}
	}
	return model.QuestOffer{}, false, nil
}

func (h *NPCTalkHandler) availableDayLimitOffers(offers []model.QuestOffer) []model.QuestOffer {
	if h == nil || h.dayLimits == nil {
		return offers
	}
	available := offers[:0]
	for _, offer := range offers {
		if h.dayLimits.Available(offer) {
			available = append(available, offer)
		}
	}
	return available
}

func questDefinitionForOffer(offer model.QuestOffer) chulsanQuestDefinition {
	if quest, ok := chulsanQuestByID(offer.QuestID); ok {
		return quest
	}
	talkKey := func(step int) string {
		return fmt.Sprintf("SN_TALK_%s_%02d", strings.ToUpper(strings.TrimSpace(offer.CodeName)), step)
	}
	return chulsanQuestDefinition{
		questID: offer.QuestID, nameKey: offer.NameKey, talkKey: talkKey(1),
		acceptedKey: talkKey(2), deniedKey: talkKey(3),
		progressKey: talkKey(4), fullKey: talkKey(5), completedKey: talkKey(6),
	}
}

func (h *NPCTalkHandler) handleJingyoTalk(ctx context.Context, session *server.Session, player *model.Player) error {
	quest, exists := playerQuest(player, model.QuestWeaponListID)
	if !exists {
		return nil
	}
	if quest.Status == model.QuestRewardReady {
		return h.sendQuestNotice(session, "SN_TALK_QNO_CH_SMITH_1_07")
	}
	if quest.Status != model.QuestActive && quest.Status != model.QuestProgressed {
		return nil
	}
	if h.runtime == nil {
		return model.ErrQuestRuntimeUnavailable
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	result, err := h.runtime.GrantMissionItem(ctx, player.ID, model.QuestWeaponListID)
	if errors.Is(err, model.ErrQuestInventoryFull) {
		return h.sendQuestNotice(session, "SN_TALK_QNO_CH_SMITH_1_05")
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
	return h.sendQuestNotice(session, "SN_TALK_QNO_CH_SMITH_1_09")
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
		return h.sendQuestNotice(session, quest.fullKey)
	}
	if err != nil {
		return err
	}
	return h.publishQuestCompletion(session, player, interaction.NPCUniqueID, result)
}

func (h *NPCTalkHandler) sendQuestRewardPrompt(session *server.Session, questID int, token string) error {
	if questID <= 0 {
		return model.ErrQuestStateConflict
	}
	prompt, err := vsro188.BuildQuestRewardPrompt(uint32(questID), 4, token)
	if err != nil {
		return err
	}
	return h.sendPacket(session, prompt)
}

func (h *NPCTalkHandler) publishQuestCompletion(session *server.Session, player *model.Player, sourceUID uint32, result model.QuestCompletionResult) error {
	if h == nil || player == nil {
		return model.ErrQuestStateConflict
	}
	previousGold := player.GoldSnapshot()
	player.CommitQuestCompletion(result)
	if session != nil {
		npcInteractions.ClearSelectedQuest(session.UserContext.UniqueID)
	}
	if result.RemovedSlot != nil {
		if err := h.sendPacket(session, buildQuestItemRemoved(*result.RemovedSlot)); err != nil {
			return err
		}
	}
	addedBySlot := make(map[byte]struct{}, len(result.AddedItems))
	for _, added := range result.AddedItems {
		addedBySlot[added.Slot] = struct{}{}
	}
	for _, slot := range result.ChangedSlots {
		if err := h.sendPacket(session, buildQuestItemRemoved(slot)); err != nil {
			return err
		}
		if _, replaced := addedBySlot[slot]; replaced {
			continue
		}
		if item, exists := result.Inventory.Items[slot]; exists {
			if err := h.sendPacket(session, buildQuestItemAdded(model.QuestItemMutation{Slot: slot, Item: item})); err != nil {
				return err
			}
		}
	}
	for _, added := range result.AddedItems {
		if err := h.sendPacket(session, buildQuestItemAdded(added)); err != nil {
			return err
		}
	}
	if result.Gold != previousGold {
		if err := h.sendPacket(session, buildQuestGoldUpdate(result.Gold)); err != nil {
			return err
		}
	}
	if err := h.sendPacket(session, buildExperienceUpdate(sourceUID, result.Experience, result.SkillExp, result.Progress)); err != nil {
		return err
	}
	if result.FixedSkillPoints != 0 {
		if err := h.sendPacket(session, buildSkillPointBalance(uint32(result.Progress.SkillPoints))); err != nil {
			return err
		}
	}
	if result.InventoryExpanded {
		if err := h.sendPacket(session, vsro188.BuildInventorySizeUpdate(vsro188.InventorySizeUpdate{FirstByte: 1, SecondByte: result.InventorySize})); err != nil {
			return err
		}
	}
	if result.HwanChanged {
		if err := h.sendPacket(session, model.BuildHwanLevelUpdatePacket(player.GetUniqueID(), result.HwanLevel)); err != nil {
			return err
		}
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
	// Do not open another quest dialogue after removing the selected quest.
	// The stock client releases that quest's PopData on the removal packet;
	// activating a dialogue for it afterwards dereferences the released record.
	npcInteractions.Clear(session.UserContext.UniqueID)
	return nil
}

func (h *NPCTalkHandler) publishEventReward(session *server.Session, player *model.Player, sourceUID uint32, result model.EventRewardResult) error {
	if h == nil || player == nil {
		return model.ErrQuestStateConflict
	}
	previousGold := player.GoldSnapshot()
	player.CommitEventReward(result)
	for _, added := range result.AddedItems {
		if err := h.sendPacket(session, buildQuestItemAdded(added)); err != nil {
			return err
		}
	}
	if result.Gold != previousGold {
		if err := h.sendPacket(session, buildQuestGoldUpdate(result.Gold)); err != nil {
			return err
		}
	}
	if err := h.sendPacket(session, buildExperienceUpdate(sourceUID, result.Experience, result.SkillExp, result.Progress)); err != nil {
		return err
	}
	if result.Progress.LevelsGained > 0 {
		if err := h.sendPacket(session, player.BuildStatsUpdatePacket()); err != nil {
			return err
		}
	}
	return nil
}

// sendQuestNotice uses the verified extended dialogue form with an explicit
// localized OK key. The stock client renders the implicit type-1 button blank
// and the implicit type-2 button in an incompatible locale for this Media set.
func (h *NPCTalkHandler) sendQuestNotice(session *server.Session, value string) error {
	packet, err := buildQuestTalkPacket(questTalkExtendedTypeMin, value, []string{questTalkNoticeChoice})
	if err != nil {
		return err
	}
	if session != nil {
		npcInteractions.SetNoticeClose(session.UserContext.UniqueID)
	}
	return h.sendPacket(session, packet)
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
	return buildTypedQuestUpdate(quest, vsro188.QuestUpdateCreate)
}

func buildQuestStatePacket(quest model.CharacterQuest) (network.Packet, error) {
	return buildTypedQuestUpdate(quest, vsro188.QuestUpdateDelta)
}

func buildTypedQuestUpdate(quest model.CharacterQuest, subtype byte) (network.Packet, error) {
	return questwire.BuildUpdate(quest, subtype)
}

func buildQuestRemovePacket(questID int) network.Packet {
	packet, err := vsro188.BuildQuestUpdate(vsro188.QuestUpdateMessage{Subtype: vsro188.QuestUpdateRetire, QuestID: uint32(questID)})
	if err != nil {
		return network.Packet{}
	}
	return packet
}

func buildQuestItemAdded(mutation model.QuestItemMutation) network.Packet {
	// The operation record is followed by the mode-1 item serialization. The
	// stock client first consumes the slot/0x3C scalars, then constructs the
	// new inventory item from rent info, reference ID, and its TypeID-driven
	// body. Omitting that body makes the client read beyond the packet.
	packet := vsro188.BuildItemOperationTwoByteSuccess(vsro188.ItemOperationServerAdd, mutation.Slot, 0x3C)
	model.WriteRentInfo(&packet, mutation.Item)
	packet.WriteUInt32(mutation.Item.GetRefObjectID())
	model.WriteInventoryItem(&packet, mutation.Item)
	return packet
}

func buildQuestItemRemoved(slot byte) network.Packet {
	return vsro188.BuildItemOperationServerDeleteSuccess(slot)
}

func buildQuestGoldUpdate(gold uint64) network.Packet {
	return vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold})
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
