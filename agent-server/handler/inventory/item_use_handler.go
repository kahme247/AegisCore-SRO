package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/questwire"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

type ReturnTeleportCoordinator interface {
	PrepareItemReturn(*server.Session, *model.Player) error
	AbortItemReturn(*server.Session)
	CompleteItemReturn(*server.Session, *model.Player, model.TeleportDestination) error
}

type QuestInstanceEntryCoordinator interface {
	SubmitQuestInstanceEntry(model.QuestInstance, []*model.Player) error
}

type preparedQuestInstanceEntry struct {
	instance model.QuestInstance
	members  []*model.Player
}

type ItemUseHandler struct {
	players           PlayerLookup
	useCase           service.ItemUseCase
	send              func(*server.Session, network.Packet) error
	publish           func(*model.Player, *network.Packet)
	schedule          func(time.Duration, func())
	timedBuffDuration bool
	returnTeleport    ReturnTeleportCoordinator
	devilSpirit       model.AvatarInventoryRepository
	questInstances    *model.QuestInstanceLifecycle
	instanceEntries   *service.QuestInstanceEntryService
	instanceTeleport  QuestInstanceEntryCoordinator
	party             *service.PartyService
	nextDungeonKey    func() uint32
	cos               interface {
		Summon(context.Context, *model.Player, uint64) (*model.COS, error)
	}
}

func (h *ItemUseHandler) ConfigureDevilSpirit(repository model.AvatarInventoryRepository) {
	if h != nil {
		h.devilSpirit = repository
	}
}

const devilSpiritUseType uint16 = 0x0F2D

func (h *ItemUseHandler) ConfigureCOS(runtime interface {
	Summon(context.Context, *model.Player, uint64) (*model.COS, error)
}) {
	if h != nil {
		h.cos = runtime
	}
}

func NewItemUseHandler(players PlayerLookup, items model.ItemUseRepository) *ItemUseHandler {
	return &ItemUseHandler{players: players, useCase: service.NewItemUseService(items), party: service.GetPartyServiceInstance(), nextDungeonKey: model.NextActionUID}
}

func (h *ItemUseHandler) ConfigureQuestInstanceEntry(lifecycle *model.QuestInstanceLifecycle, teleport QuestInstanceEntryCoordinator) {
	if h != nil {
		h.questInstances = lifecycle
		h.instanceEntries = service.NewQuestInstanceEntryService(lifecycle, h.party, h.nextDungeonKey)
		h.instanceTeleport = teleport
	}
}

func (h *ItemUseHandler) prepareQuestInstance(ctx context.Context, player *model.Player, questID int) (*preparedQuestInstanceEntry, error) {
	if h == nil || h.instanceTeleport == nil {
		return nil, model.ErrQuestInstanceInvalid
	}
	entries := h.instanceEntries
	if entries == nil {
		entries = service.NewQuestInstanceEntryService(h.questInstances, h.party, h.nextDungeonKey)
	}
	prepared, err := entries.Prepare(ctx, player, questID)
	if err != nil {
		return nil, err
	}
	return &preparedQuestInstanceEntry{instance: prepared.Instance, members: prepared.Members}, nil
}

func questInstanceEligible(player *model.Player, questID int) bool {
	return service.QuestInstanceEligible(player, questID)
}

func RegisterItemUseHandler(target *server.Server, items model.ItemUseRepository, returnTeleport ...ReturnTeleportCoordinator) *ItemUseHandler {
	if target == nil {
		return nil
	}
	h := NewItemUseHandler(service.GetWorldServiceInstance(), items)
	if len(returnTeleport) > 0 {
		h.returnTeleport = returnTeleport[0]
	}
	target.RegisterHandler(opcode.InventoryItemUseRequest, h.handlePacket)
	return h
}

func (h *ItemUseHandler) ConfigureTimedBuffDuration(enabled bool) {
	if h != nil {
		h.timedBuffDuration = enabled
	}
}

func (h *ItemUseHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.useCase == nil || packet.Remaining() < 3 {
		return h.sendFailure(session)
	}
	wireRequest, err := vsro188.DecodeItemUseRequest(&packet)
	if err != nil {
		return h.sendFailure(session)
	}
	slot := wireRequest.InventorySlot
	itemType := wireRequest.ItemType
	resolver := &InventoryHandler{players: h.players}
	player, err := resolver.resolvePlayer(session)
	if err != nil {
		return err
	}
	// A potion request can already be queued when lethal damage commits. Do not
	// let that late request restore HP while leaving the authoritative runtime
	// life state dead; resurrection owns that transition.
	if player.GetLifeState() == model.Dead {
		return h.sendFailure(session)
	}
	if itemType == devilSpiritUseType {
		if wireRequest.HasOption {
			return h.sendFailure(session)
		}
		return h.activateDevilSpirit(ctx, session, player, slot)
	}
	reverseOption := byte(0)
	var reverseMapID uint32
	inventory := player.GetInventory()
	item, itemExists := inventory.Items[slot]
	ref, refExists := model.RefItems[item.GetRefObjectID()]
	isReverse := itemExists && refExists && model.IsReverseReturnScroll(ref)
	if isReverse {
		if !wireRequest.HasOption {
			return h.sendFailure(session)
		}
		reverseOption = wireRequest.Option
		if reverseOption == 7 {
			reverseMapID = wireRequest.MapID
			if reverseMapID == 0 {
				return h.sendFailure(session)
			}
		}
	} else if wireRequest.HasOption {
		return h.sendFailure(session)
	}
	if refExists && model.IsCOSTransportScroll(ref) && player.COSState.Snapshot().TransportUniqueID != 0 {
		// Runtime creation happens after the item transaction commits. Reject a
		// second ride scroll before consumption while another transport is active.
		return h.sendFailure(session)
	}
	if refExists && model.IsCOSTransportScroll(ref) && !model.HasActiveJobSuit(player) {
		return h.sendFailure(session)
	}
	if refExists && model.IsTeleportScroll(ref) && h.returnTeleport == nil {
		// The repository mutates position and consumes the item atomically. Do
		// not allow that commit to happen when the runtime loading handshake is
		// absent; otherwise the character is persisted at the destination while
		// the client remains in the old world.
		return h.sendFailure(session)
	}
	preparedReturn := false
	if h.returnTeleport != nil {
		if item, exists := inventory.Items[slot]; exists {
			if ref, exists := model.RefItems[item.GetRefObjectID()]; exists && model.IsTeleportScroll(ref) {
				if err = h.returnTeleport.PrepareItemReturn(session, player); err != nil {
					logrus.WithError(err).Debug("rejected return-scroll preparation")
					return h.sendFailure(session)
				}
				preparedReturn = true
			}
		}
	}
	var preparedInstance *preparedQuestInstanceEntry
	questInstanceEntryQuestID, isQuestInstanceEntry := model.QuestInstanceEntryQuestID(ref.CodeName)
	if refExists && isQuestInstanceEntry {
		if wireRequest.HasCOSTarget || len(wireRequest.ItemData) != 0 {
			return h.sendFailure(session)
		}
		prepared, prepareErr := h.instanceEntries.Prepare(ctx, player, questInstanceEntryQuestID)
		err = prepareErr
		if err != nil {
			return h.sendFailure(session)
		}
		preparedInstance = &preparedQuestInstanceEntry{instance: prepared.Instance, members: prepared.Members}
	}
	var result model.ItemUseResult
	usesCOSSlotSelector := refExists && (model.IsCOSExtension(ref) || model.IsCOSPetTimedService(ref) || model.IsCOSRevival(ref))
	if usesCOSSlotSelector || wireRequest.HasCOSTarget {
		targeted, ok := h.useCase.(service.TargetedItemUseCase)
		if !ok {
			return h.sendFailure(session)
		}
		targetObjectID := wireRequest.TargetObjectID
		if usesCOSSlotSelector {
			// Timed growth-pet services are sent without an item-data selector by
			// the stock client and apply to the active summoned growth pet. Other
			// confirmed COS item families carry one normal-inventory slot.
			if model.IsCOSPetTimedService(ref) && len(wireRequest.ItemData) == 0 {
				resolver, supported := h.cos.(interface {
					ResolveActivePersistentTarget(*model.Player, model.COSKind) (uint64, bool)
				})
				if !supported {
					return h.sendFailure(session)
				}
				persistentID, owned := resolver.ResolveActivePersistentTarget(player, model.COSPetTimedServiceTargetKind(ref))
				if !owned || persistentID > uint64(^uint32(0)) {
					return h.sendFailure(session)
				}
				targetObjectID = uint32(persistentID)
			} else {
				if len(wireRequest.ItemData) != 1 {
					return h.sendFailure(session)
				}
				selected, exists := inventory.Items[wireRequest.ItemData[0]]
				if !exists || selected.Data == 0 || !selected.COS.Present {
					return h.sendFailure(session)
				}
				if !model.IsCOSRevival(ref) && selected.COS.State != model.COSItemSummoned {
					return h.sendFailure(session)
				}
				targetObjectID = selected.Data
			}
		} else if refExists && (model.IsCOSHealthPotion(ref) || model.IsCOSHGPotion(ref)) {
			resolver, supported := h.cos.(interface {
				ResolvePersistentTarget(*model.Player, uint32) (uint64, bool)
			})
			if !supported {
				return h.sendFailure(session)
			}
			persistentID, owned := resolver.ResolvePersistentTarget(player, targetObjectID)
			if !owned || persistentID > uint64(^uint32(0)) {
				return h.sendFailure(session)
			}
			targetObjectID = uint32(persistentID)
		} else if refExists && model.IsCOSRevival(ref) {
			// Dead COS instruments have no runtime world UID to resolve. Accept the
			// client selector as a summoned UID when available, an inventory slot
			// when it names a COS instrument, or an owned persistent ID. The SQL
			// transaction remains the final owner/dead-state authority.
			if resolver, supported := h.cos.(interface {
				ResolvePersistentTarget(*model.Player, uint32) (uint64, bool)
			}); supported {
				if persistentID, owned := resolver.ResolvePersistentTarget(player, targetObjectID); owned && persistentID <= uint64(^uint32(0)) {
					targetObjectID = uint32(persistentID)
				}
			}
			if targetObjectID <= uint32(^uint8(0)) {
				if selected, exists := inventory.Items[byte(targetObjectID)]; exists && selected.COS.Present && selected.Data != 0 {
					targetObjectID = selected.Data
				}
			}
		}
		result, err = targeted.UseTargetedItem(ctx, player, slot, itemType, reverseOption, reverseMapID, model.COSItemTarget{
			ObjectID: targetObjectID,
			Region:   wireRequest.TargetRegion,
			X:        wireRequest.TargetX,
			Y:        wireRequest.TargetY,
			Z:        wireRequest.TargetZ,
		})
	} else if len(wireRequest.ItemData) != 0 {
		return h.sendFailure(session)
	} else {
		result, err = h.useCase.UseItem(ctx, player, slot, itemType, reverseOption, reverseMapID)
	}
	if err != nil {
		if preparedInstance != nil {
			_ = h.questInstances.Destroy(ctx, preparedInstance.instance.Scope, preparedInstance.members)
		}
		if preparedReturn {
			h.returnTeleport.AbortItemReturn(session)
		}
		if errors.Is(err, model.ErrItemUseOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected inventory item use")
		return h.sendFailure(session)
	}
	if result.COSTemporaryRideRefObjectID != 0 {
		activator, ok := h.cos.(interface {
			ActivateTemporaryRide(*model.Player, uint32) (*model.COS, error)
		})
		if !ok {
			return fmt.Errorf("committed ride-scroll use has no configured temporary COS runtime")
		}
		summoned, activateErr := activator.ActivateTemporaryRide(player, result.COSTemporaryRideRefObjectID)
		if activateErr != nil {
			return activateErr
		}
		// Publish the complete owner snapshot before mounting. Client 0x009A1900
		// requires the ride transport's inventory-capacity byte after the common
		// 16-byte 0x30C8 prefix; omitting the packet leaves its COS interface
		// incomplete, while sending only the prefix crashes its packet reader.
		data, supported := model.BuildCOSData(summoned)
		if !supported {
			return fmt.Errorf("ride transport %d has no 0x30C8 layout", summoned.Kind)
		}
		player.SendToSelf(&data)
		// Mount only after the visibility tick has introduced the transport UID.
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
		}
		ownerUID := player.GetUniqueID()
		transportUID := summoned.GetUniqueID()
		schedule(100*time.Millisecond, func() {
			mounted := buildCOSMountStateSuccess(ownerUID, transportUID)
			h.publishPacket(player, &mounted)
		})
	}
	if result.COSPersistentID != 0 {
		if h.cos == nil {
			return fmt.Errorf("committed COS item use has no configured runtime")
		}
		summoned, summonErr := h.cos.Summon(ctx, player, result.COSPersistentID)
		if summonErr != nil {
			logrus.WithError(summonErr).Debug("rejected COS summon")
			return h.sendFailure(session)
		}
		if summoned != nil && summoned.Kind == model.COSRideTransport {
			if transportErr := player.COSState.ActivateTransport(summoned.UniqueID, summoned.RefObjectID, false); transportErr != nil {
				return transportErr
			}
		}
		// Publish the subtype-complete owner snapshot, including the ride
		// transport inventory-capacity byte required by client 0x009A1900.
		if data, supported := model.BuildCOSData(summoned); supported {
			player.SendToSelf(&data)
		}
		state := buildCOSItemStateUpdate(result.SourceSlot, model.COSItemSummoned)
		player.SendToSelf(&state)
	}
	if result.COSRevivedPersistentID != 0 {
		if h.cos == nil {
			return fmt.Errorf("committed COS revival has no configured runtime")
		}
		if reviver, ok := h.cos.(interface {
			RevivePersisted(*model.Player, uint64, int32) (*model.COS, error)
		}); ok {
			revived, reviveErr := reviver.RevivePersisted(player, result.COSRevivedPersistentID, result.COSRevivedHealth)
			if reviveErr != nil {
				return reviveErr
			}
			state := result.COSRevivedItemState
			if revived != nil {
				state = model.COSItemSummoned
			}
			statePacket := model.BuildCOSItemStateUpdate(findCOSInventorySlot(player, result.COSRevivedPersistentID), state)
			player.SendToSelf(&statePacket)
		} else {
			return fmt.Errorf("committed COS revival runtime does not support state application")
		}
	}
	if result.COSExtendedPersistentID != 0 {
		applier, ok := h.cos.(interface {
			ApplyPersistedRentEnd(*model.Player, uint64, time.Time) (*model.COS, error)
		})
		if !ok {
			return fmt.Errorf("committed COS rental extension has no configured runtime")
		}
		if _, applyErr := applier.ApplyPersistedRentEnd(player, result.COSExtendedPersistentID, result.COSExtendedRentEndsAt); applyErr != nil {
			return applyErr
		}
	}
	if result.COSTimedServicePersistentID != 0 {
		applier, ok := h.cos.(interface {
			ApplyPersistedTimedService(*model.Player, uint64, model.COSTimedService) (*model.COS, error)
		})
		if !ok {
			return fmt.Errorf("committed COS timed service has no configured runtime")
		}
		if _, applyErr := applier.ApplyPersistedTimedService(player, result.COSTimedServicePersistentID, model.COSTimedService{Category: result.COSTimedServiceCategory, JobID: result.COSTimedServiceJobID, Data1: result.COSTimedServiceData1, ExpiresAt: result.COSTimedServiceExpiresAt}); applyErr != nil {
			return applyErr
		}
	}
	if result.COSResourcePersistentID != 0 {
		applier, ok := h.cos.(interface {
			ApplyPersistedResources(*model.Player, uint64, int32, int) (*model.COS, error)
		})
		if !ok {
			return fmt.Errorf("committed COS resource mutation has no configured runtime")
		}
		updated, applyErr := applier.ApplyPersistedResources(player, result.COSResourcePersistentID, result.COSResourceHealth, result.COSResourceHGP)
		if applyErr != nil {
			return applyErr
		}
		if updated != nil && refExists {
			if model.IsCOSHGPotion(ref) {
				hgp := model.BuildCOSHGPUpdate(updated.GetUniqueID(), uint16(result.COSResourceHGP))
				player.SendToSelf(&hgp)
			} else if model.IsCOSHealthPotion(ref) {
				health := model.BuildCOSHealthUpdate(updated.GetUniqueID(), uint32(result.COSResourceHealth))
				player.SendToSelfAndKnown(&health)
			}
		}
	}
	if result.MaskRefCharID != 0 {
		character, exists := model.RefChars[result.MaskRefCharID]
		if !exists {
			return fmt.Errorf("committed monster mask references unavailable character %d", result.MaskRefCharID)
		}
		duration := result.MaskDuration
		if duration <= 0 {
			duration = 3 * time.Minute
		}
		mask := model.CharacterMask{
			RefCharID: character.ID,
			Scale:     byte(max(0, min(character.Scale, 255))),
			Skills:    characterMaskSkills(character),
			ExpiresAt: time.Now().Add(duration),
			Token:     model.NextActionUID(),
		}
		player.ApplyCharacterMask(mask)
		changed := model.BuildCharacterMaskPacket(player.GetUniqueID(), mask)
		h.publishPacket(player, &changed)
		expire := func() {
			if !player.RemoveCharacterMask(mask.Token) {
				return
			}
			removed := model.BuildCharacterMaskPacket(player.GetUniqueID(), model.CharacterMask{})
			h.publishPacket(player, &removed)
		}
		if h.schedule != nil {
			h.schedule(duration, expire)
		} else {
			time.AfterFunc(duration, expire)
		}
	}
	if err = h.sendPacket(session, buildItemUseSuccess(result.SourceSlot, result.ItemType, result.NewAmount)); err != nil {
		if preparedReturn && result.ReturnDestination == nil {
			h.returnTeleport.AbortItemReturn(session)
		}
		return err
	}
	if result.QuestInstanceEntryQuestID != 0 {
		if preparedInstance == nil || result.QuestInstanceEntryQuestID != preparedInstance.instance.QuestID || h.instanceTeleport == nil {
			return fmt.Errorf("committed quest instance item has no prepared entry")
		}
		if err = h.instanceTeleport.SubmitQuestInstanceEntry(preparedInstance.instance, preparedInstance.members); err != nil {
			return fmt.Errorf("submit quest instance entry: %w", err)
		}
	}
	for _, progress := range result.CollectionQuestProgress {
		if !progress.Changed {
			continue
		}
		player.CommitQuestMutation(progress.Quest)
		update, buildErr := questwire.BuildUpdate(progress.Quest, vsro188.QuestUpdateDelta)
		if buildErr != nil {
			return buildErr
		}
		if err = h.sendPacket(session, update); err != nil {
			return err
		}
	}
	if result.ReturnDestination != nil {
		if !preparedReturn || h.returnTeleport == nil {
			return fmt.Errorf("committed return-scroll use was not prepared for teleport")
		}
		destination := *result.ReturnDestination
		complete := func() {
			if completeErr := h.returnTeleport.CompleteItemReturn(session, player, destination); completeErr != nil {
				h.returnTeleport.AbortItemReturn(session)
				logrus.WithError(completeErr).Warn("failed to complete committed return-scroll teleport")
			}
		}
		if result.ReturnDelay <= 0 {
			complete()
		} else if h.schedule != nil {
			h.schedule(result.ReturnDelay, complete)
		} else {
			time.AfterFunc(result.ReturnDelay, complete)
		}
	} else if preparedReturn {
		h.returnTeleport.AbortItemReturn(session)
		return fmt.Errorf("prepared return-scroll produced no destination")
	} else if result.EffectSkillID > 0 && result.SpeedPercent > 0 && result.EffectDuration > 0 {
		token := model.NextActionUID()
		generation, replacedToken := player.ApplyMovementSpeedEffect(result.EffectSkillID, result.SpeedPercent, result.EffectExpiresAt, token)
		if replacedToken != 0 {
			removed := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &removed)
		}
		added := model.BuildBuffAdd(player.GetUniqueID(), result.EffectSkillID, token)
		if h.timedBuffDuration {
			added = model.BuildTimedBuffAdd(player.GetUniqueID(), result.EffectSkillID, token, uint32(result.EffectDuration))
		}
		h.publishPacket(player, &added)
		player.SendMovementSpeedUpdate()
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
		}
		schedule(time.Duration(result.EffectDuration)*time.Millisecond, func() {
			if expiredToken, cleared := player.ClearMovementSpeedEffect(generation); cleared {
				removed := model.BuildBuffRemove(expiredToken)
				h.publishPacket(player, &removed)
				player.SendMovementSpeedUpdate()
			}
		})
	} else if result.EffectSkillID > 0 && result.EffectDuration > 0 && result.BuffModifiers != (model.BuffModifiers{}) {
		token := model.NextActionUID()
		duration := time.Duration(result.EffectDuration) * time.Millisecond
		replacedToken, committed := player.CommitActiveBuff(result.EffectSkillID, token, duration, result.BuffModifiers)
		if !committed {
			return fmt.Errorf("commit item buff skill %d", result.EffectSkillID)
		}
		if replacedToken != 0 {
			removed := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &removed)
		}
		added := model.BuildBuffAdd(player.GetUniqueID(), result.EffectSkillID, token)
		if h.timedBuffDuration {
			added = model.BuildTimedBuffAdd(player.GetUniqueID(), result.EffectSkillID, token, uint32(result.EffectDuration))
		}
		h.publishPacket(player, &added)
		stats := player.BuildStatsUpdatePacket()
		player.SendToSelf(&stats)
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
		}
		schedule(duration, func() {
			if player.RemoveActiveBuff(token) {
				removed := model.BuildBuffRemove(token)
				h.publishPacket(player, &removed)
				stats := player.BuildStatsUpdatePacket()
				player.SendToSelf(&stats)
			}
		})
	} else if result.ItemService != nil {
		h.publishItemService(player, *result.ItemService, result.ItemServiceToken, result.ReplacedItemServiceToken)
	} else {
		player.SendVitalsUpdate()
	}
	return nil
}

func (h *ItemUseHandler) activateDevilSpirit(ctx context.Context, session *server.Session, player *model.Player, slot byte) error {
	if player == nil || h == nil || h.devilSpirit == nil || slot < 13 || slot >= model.BaseInventorySize {
		return h.sendFailure(session)
	}
	activation, err := h.devilSpirit.ActivateDevilSpirit(ctx, int64(player.ID), slot)
	if err != nil {
		logrus.WithError(err).Debug("rejected devil spirit activation")
		return h.sendFailure(session)
	}
	item := activation.Item
	player.CommitInventory(activation.Inventory)
	amount := item.StackSize
	if amount < 1 {
		amount = 1
	}
	if amount > int(^uint16(0)) {
		amount = int(^uint16(0))
	}
	if err := h.sendPacket(session, buildItemUseSuccess(slot, devilSpiritUseType, uint16(amount))); err != nil {
		return err
	}
	used := vsro188.BuildItemUsedNotification(vsro188.ItemUsedNotification{
		FirstValue:  player.GetUniqueID(),
		SecondValue: item.GetRefObjectID(),
	})
	h.publishPacket(player, &used)
	remaining := time.Until(activation.ExpiresAt)
	if remaining <= 0 {
		remaining = time.Millisecond
	}
	expire := func() {
		result, expireErr := h.devilSpirit.ExpireDevilSpirit(context.Background(), int64(player.ID), item.ID, time.Now())
		if expireErr != nil {
			logrus.WithError(expireErr).Warn("failed to expire devil spirit")
			return
		}
		if !result.Expired {
			return
		}
		player.CommitInventoryAndAvatar(result.Inventory, result.AvatarInventory)
		if result.WasEquipped {
			request := moveItemRequest{sourceSlot: result.AvatarSlot, targetSlot: result.InventorySlot}
			sendAvatarStateUpdate(player, UnequipAvatarItemOperation, request, result.AvatarTransferResult)
			if sendErr := sendAvatarTransferSuccess(session, UnequipAvatarItemOperation, request); sendErr != nil {
				logrus.WithError(sendErr).Debug("failed to publish expired devil spirit transfer")
			}
			player.SendStatsUpdate()
		}
	}
	if h.schedule != nil {
		h.schedule(remaining, expire)
	} else {
		time.AfterFunc(remaining, expire)
	}
	return nil
}

func findCOSInventorySlot(player *model.Player, persistentID uint64) byte {
	if player == nil || persistentID == 0 {
		return 0
	}
	for slot, item := range player.GetInventory().Items {
		if uint64(item.Data) == persistentID {
			return slot
		}
	}
	return 0
}

func buildCOSItemStateUpdate(slot byte, state model.COSItemState) network.Packet {
	return vsro188.BuildInventoryItemStateUpdate(slot, byte(state))
}

func characterMaskSkills(character model.RefChar) []uint32 {
	values := [...]int{
		character.DefaultSkill1, character.DefaultSkill2, character.DefaultSkill3, character.DefaultSkill4, character.DefaultSkill5,
		character.DefaultSkill6, character.DefaultSkill7, character.DefaultSkill8, character.DefaultSkill9, character.DefaultSkill10,
	}
	skills := make([]uint32, 0, len(values))
	for _, skillID := range values {
		if skillID > 0 {
			skills = append(skills, uint32(skillID))
		}
	}
	return skills
}

func (h *ItemUseHandler) publishItemService(player *model.Player, effect model.ItemServiceEffect, token, replacedToken uint32) {
	if player == nil || token == 0 {
		return
	}
	if replacedToken != 0 {
		if model.ItemServiceUsesItemPerk(effect) {
			removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, replacedToken)
			h.publishPacket(player, &removed)
		}
		if premiumRemoved, exists := model.BuildPremiumDailyRemove(replacedToken, effect); exists {
			player.SendToSelf(&premiumRemoved)
		}
		if model.ItemServiceBuffSkillID(effect) != 0 {
			buffRemoved := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &buffRemoved)
		}
	}
	remaining := time.Until(effect.ExpiresAt)
	if remaining <= 0 {
		return
	}
	if model.ItemServiceUsesItemPerk(effect) {
		added := model.BuildItemPerkAdd(player.GetUniqueID(), effect.SourceRefItemID, token, model.ItemServicePerkValue(effect), remaining)
		h.publishPacket(player, &added)
	}
	if premiumAdded, exists := model.BuildPremiumDailyAdd(token, effect, remaining); exists {
		player.SendToSelf(&premiumAdded)
	}
	if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), token, effect, remaining); exists {
		h.publishPacket(player, &buffAdded)
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	schedule(remaining, func() {
		if player.ClearItemService(effect.Kind, token) {
			if model.ItemServiceUsesItemPerk(effect) {
				removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, token)
				h.publishPacket(player, &removed)
			}
			if premiumRemoved, exists := model.BuildPremiumDailyRemove(token, effect); exists {
				player.SendToSelf(&premiumRemoved)
			}
			if model.ItemServiceBuffSkillID(effect) != 0 {
				buffRemoved := model.BuildBuffRemove(token)
				h.publishPacket(player, &buffRemoved)
			}
		}
	})
}

func (h *ItemUseHandler) publishPacket(player *model.Player, packet *network.Packet) {
	if h != nil && h.publish != nil {
		h.publish(player, packet)
		return
	}
	player.SendToSelfAndKnown(packet)
}

func buildCOSMountStateSuccess(ownerUniqueID, transportUniqueID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.COSUpdateMountStateResponse
	packet.WriteByte(1)
	packet.WriteUInt32(ownerUniqueID)
	packet.WriteBool(true)
	packet.WriteUInt32(transportUniqueID)
	return packet
}

func (h *ItemUseHandler) sendFailure(session *server.Session) error {
	return h.sendPacket(session, buildItemUseFailure())
}
func (h *ItemUseHandler) sendPacket(session *server.Session, p network.Packet) error {
	if session == nil {
		return fmt.Errorf("item-use session is nil")
	}
	if h != nil && h.send != nil {
		return h.send(session, p)
	}
	return session.Send(p)
}
func buildItemUseSuccess(sourceSlot byte, itemType, newAmount uint16) network.Packet {
	return vsro188.BuildItemUseSuccess(vsro188.ItemUseSuccess{
		SourceSlot: sourceSlot,
		NewAmount:  newAmount,
		ItemType:   itemType,
	})
}

func buildItemUseFailure() network.Packet {
	return vsro188.BuildItemUseFailure(0)
}
