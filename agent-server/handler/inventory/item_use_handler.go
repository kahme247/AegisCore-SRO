package inventory

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
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

type ItemMonsterSummoner interface {
	SpawnItemSummonNPC(string, navmeshv2.RtNavmeshPosition, float32, model.MonsterRarity) (*model.NPC, error)
}

type itemSummonWorld interface {
	RemoveVisibleObject(model.ISRObject)
}

type ItemUseHandler struct {
	players           PlayerLookup
	useCase           service.ItemUseCase
	send              func(*server.Session, network.Packet) error
	publish           func(*model.Player, *network.Packet)
	schedule          func(time.Duration, func())
	now               func() time.Time
	randomIntn        func(int) int
	timedBuffDuration bool
	returnTeleport    ReturnTeleportCoordinator
	devilSpirit       model.AvatarInventoryRepository
	questInstances    *model.QuestInstanceLifecycle
	instanceEntries   *service.QuestInstanceEntryService
	instanceTeleport  QuestInstanceEntryCoordinator
	dimensionHoles    *service.DimensionHolePortalLifecycle
	party             *service.PartyService
	nextDungeonKey    func() uint32
	cos               interface {
		Summon(context.Context, *model.Player, uint64) (*model.COS, error)
	}
	summoner    ItemMonsterSummoner
	summonWorld itemSummonWorld
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

func (h *ItemUseHandler) ConfigureMonsterSummons(summoner ItemMonsterSummoner, world itemSummonWorld) {
	if h != nil {
		h.summoner = summoner
		h.summonWorld = world
	}
}

func NewItemUseHandler(players PlayerLookup, items model.ItemUseRepository) *ItemUseHandler {
	h := &ItemUseHandler{
		players: players, party: service.GetPartyServiceInstance(), nextDungeonKey: model.NextActionUID,
		now: time.Now, randomIntn: rand.Intn,
		summonWorld: service.GetWorldServiceInstance(),
	}
	itemUse := service.NewItemUseService(items)
	itemUse.ConfigureItemCureRuntime(
		func() time.Time { return h.itemUseNow() },
		func(limit int) int {
			if h.randomIntn != nil {
				return h.randomIntn(limit)
			}
			return rand.Intn(limit)
		},
	)
	h.useCase = itemUse
	return h
}

func (h *ItemUseHandler) ConfigureQuestInstanceEntry(lifecycle *model.QuestInstanceLifecycle, teleport QuestInstanceEntryCoordinator) {
	if h != nil {
		h.questInstances = lifecycle
		h.instanceEntries = service.NewQuestInstanceEntryService(lifecycle, h.party, h.nextDungeonKey)
		h.instanceTeleport = teleport
	}
}

func (h *ItemUseHandler) ConfigureDimensionHolePortals(lifecycle *service.DimensionHolePortalLifecycle) {
	if h != nil {
		h.dimensionHoles = lifecycle
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

func dimensionHoleOpenerEligible(player *model.Player, definition model.DimensionHoleDefinition) bool {
	if player == nil || player.GetUniqueID() == 0 {
		return false
	}
	player.Mutex.Lock()
	level := player.Level
	player.Mutex.Unlock()
	cos := player.COSState.Snapshot()
	if level < int(definition.MinLevel) || level > int(definition.MaxLevel) || model.HasActiveJobSuit(player) || cos.Mounted || player.GetPVPCape() != model.PvpCapeNone {
		return false
	}
	// The authored Dimension Hole description permits creating its gate only in
	// a town. Use the maintained vSRO 1.188 region catalog instead of accepting
	// an arbitrary safe-looking world position.
	if !model.IsTownRegionV188(player.GetNavmeshPosition().Region.ID) {
		return false
	}
	party := player.GetParty()
	return party != nil && party.MasterUniqueID == player.GetUniqueID()
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
	if itemExists && refExists && ref.IsUseable && ref.TypeID1 == model.TypeID1Item && ref.TypeID2 == 2 && ref.TypeID3 == 3 {
		if wireRequest.HasOption || wireRequest.HasCOSTarget || len(wireRequest.ItemData) != 0 ||
			itemType != model.InventoryItemType(ref) || item.ID <= 0 || !player.ActivateMagicCube(item.ID) {
			return h.sendFailure(session)
		}
		// Magic-cube activation selects linked storage without consuming the
		// container. The common stock B04C success therefore reports zero
		// consumed quantity while preserving the request's item type.
		return h.sendPacket(session, buildItemUseSuccess(slot, itemType, 0))
	}
	isReverse := itemExists && refExists && model.IsReverseReturnScroll(ref)
	if isReverse {
		switch {
		case wireRequest.HasOption:
			reverseOption = wireRequest.Option
			reverseMapID = wireRequest.MapID
		case len(wireRequest.ItemData) == 1 && (wireRequest.ItemData[0] == 2 || wireRequest.ItemData[0] == 3):
			reverseOption = wireRequest.ItemData[0]
			wireRequest.ItemData = nil
		default:
			return h.sendFailure(session)
		}
		if reverseOption == 7 && reverseMapID == 0 {
			return h.sendFailure(session)
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
	if refExists && model.IsPartyMonsterSummonScroll(ref) &&
		(h.summoner == nil || model.IsTownRegionV188(player.GetNavmeshPosition().Region.ID)) {
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
	var preparedDimensionHole *service.PreparedDimensionHolePortal
	dimensionHoleDefinition, isDimensionHole := model.DimensionHoleDefinitionForReference(ref)
	if refExists && isDimensionHole {
		if wireRequest.HasCOSTarget || len(wireRequest.ItemData) != 0 || itemType != model.InventoryItemType(ref) ||
			h.dimensionHoles == nil || !dimensionHoleOpenerEligible(player, dimensionHoleDefinition) {
			return h.sendFailure(session)
		}
		preparedDimensionHole, err = h.dimensionHoles.Prepare(ctx, dimensionHoleDefinition, player, player.GetNavmeshPosition())
		if err != nil {
			logrus.WithError(err).Debug("rejected Dimension Hole preparation")
			return h.sendFailure(session)
		}
	}
	var result model.ItemUseResult
	var publishedDimensionHole *service.DimensionHolePortalInstance
	usesCOSSlotSelector := refExists && (model.IsCOSExtension(ref) || model.IsCOSPetTimedService(ref) || model.IsCOSRevival(ref) || model.IsCOSCure(ref))
	usesInventorySlotSelector := refExists && (model.IsConfiguredEnhancementScroll(ref) || model.IsConfiguredOptionUpgradeScroll(ref))
	if preparedDimensionHole != nil {
		dimensionHoleUse, ok := h.useCase.(service.DimensionHoleItemUseCase)
		if !ok {
			_ = h.dimensionHoles.Abort(ctx, preparedDimensionHole)
			return h.sendFailure(session)
		}
		instance, publishErr := h.dimensionHoles.Publish(ctx, preparedDimensionHole)
		if publishErr != nil {
			logrus.WithError(publishErr).Debug("rejected Dimension Hole portal publication before item commit")
			return h.sendFailure(session)
		}
		publishedDimensionHole = &instance
		result, err = dimensionHoleUse.UseDimensionHoleItem(ctx, player, slot, itemType, dimensionHoleDefinition.ItemRefID)
	} else if usesCOSSlotSelector || usesInventorySlotSelector || wireRequest.HasCOSTarget {
		targeted, ok := h.useCase.(service.TargetedItemUseCase)
		if !ok {
			return h.sendFailure(session)
		}
		targetObjectID := wireRequest.TargetObjectID
		if usesInventorySlotSelector {
			if len(wireRequest.ItemData) != 1 || wireRequest.ItemData[0] == slot {
				return h.sendFailure(session)
			}
			targetObjectID = uint32(wireRequest.ItemData[0]) + 1
		} else if usesCOSSlotSelector {
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
	} else if len(wireRequest.ItemData) != 0 && !(refExists && len(wireRequest.ItemData) == 1) {
		return h.sendFailure(session)
	} else {
		result, err = h.useCase.UseItem(ctx, player, slot, itemType, reverseOption, reverseMapID)
	}
	if err != nil {
		if publishedDimensionHole != nil && !errors.Is(err, model.ErrItemUseOutcomeUnknown) {
			if destroyErr := h.dimensionHoles.Destroy(context.WithoutCancel(ctx), publishedDimensionHole.Portal.GetUniqueID()); destroyErr != nil {
				return errors.Join(model.ErrItemUseOutcomeUnknown, err, destroyErr)
			}
		} else if preparedDimensionHole != nil && publishedDimensionHole == nil {
			_ = h.dimensionHoles.Abort(ctx, preparedDimensionHole)
		}
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
	if preparedDimensionHole != nil {
		if result.DimensionHolePortal == nil || *result.DimensionHolePortal != preparedDimensionHole.Definition {
			return fmt.Errorf("%w: committed Dimension Hole item does not match its prepared portal", model.ErrItemUseOutcomeUnknown)
		}
	} else if result.DimensionHolePortal != nil {
		return fmt.Errorf("%w: committed Dimension Hole item has no prepared portal", model.ErrItemUseOutcomeUnknown)
	}
	if result.ItemCure != nil && result.ItemCureApplication.Changed {
		appliedAt := result.ItemCureAppliedAt
		if appliedAt.IsZero() {
			appliedAt = h.itemUseNow()
		}
		h.publishPlayerBadEffectState(player, appliedAt)
		h.scheduleItemCureRefreshes(player, appliedAt, result.ItemCureApplication.RefreshAt)
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
	if result.COSCurePersistentID != 0 && result.COSCure != nil {
		applier, ok := h.cos.(interface {
			ApplyPersistedCure(*model.Player, uint64, model.ItemCureEffect, time.Time) (*model.COS, model.ItemCureApplication, error)
		})
		if !ok {
			return fmt.Errorf("committed COS cure has no configured runtime")
		}
		now := h.itemUseNow()
		updated, application, applyErr := applier.ApplyPersistedCure(player, result.COSCurePersistentID, *result.COSCure, now)
		if applyErr != nil {
			return applyErr
		}
		if updated != nil && application.Changed {
			publish := func(at time.Time) {
				mask, levels := updated.CombatBadEffectState(at)
				packet := model.BuildBadEffectUpdate(updated.GetUniqueID(), mask, levels)
				player.SendToSelfAndKnown(&packet)
			}
			publish(now)
			for _, refreshAt := range application.RefreshAt {
				delay := refreshAt.Sub(now)
				if delay <= 0 {
					continue
				}
				at := refreshAt
				callback := func() { publish(at) }
				if h.schedule != nil {
					h.schedule(delay, callback)
				} else {
					time.AfterFunc(delay, callback)
				}
			}
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
	used := vsro188.BuildItemUsedNotification(vsro188.ItemUsedNotification{
		FirstValue:  player.GetUniqueID(),
		SecondValue: item.GetRefObjectID(),
	})
	h.publishPacket(player, &used)
	if result.HwanSnapshot != nil {
		hwan := model.BuildHwanPointUpdatePacket(result.HwanSnapshot.Points, player.GetUniqueID())
		h.publishPacket(player, &hwan)
	}
	if result.InventorySize != 0 {
		size := vsro188.BuildInventorySizeUpdate(vsro188.InventorySizeUpdate{FirstByte: 1, SecondByte: result.InventorySize})
		if err = h.sendPacket(session, size); err != nil {
			return err
		}
	}
	if result.SkillPoints > 0 {
		points := vsro188.BuildSkillPointUpdate(vsro188.SkillPointUpdate{Value: uint32(result.SkillPoints)})
		if err = h.sendPacket(session, points); err != nil {
			return err
		}
	}
	if result.OptLevelTargetSet {
		level := result.OptLevel
		update, buildErr := vsro188.BuildInventoryItemUpdate(vsro188.InventoryItemUpdate{
			ItemKind: result.OptLevelTargetSlot,
			Flags:    0x02,
			Flag02U8: &level,
		})
		if buildErr != nil {
			return buildErr
		}
		if err = h.sendPacket(session, update); err != nil {
			return err
		}
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
	if result.SummonMonster != nil {
		if err = h.executeMonsterSummon(player, *result.SummonMonster); err != nil {
			return fmt.Errorf("execute committed monster summon: %w", err)
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
	} else if result.EffectSkillID > 0 && result.EffectDuration > 0 && (result.BuffModifiers != (model.BuffModifiers{}) || result.EffectRecoveryHP > 0 || result.EffectRecoveryMP > 0 || result.EffectVisibilityRefresh) {
		token := model.NextActionUID()
		duration := time.Duration(result.EffectDuration) * time.Millisecond
		replacedToken, committed := player.CommitActiveBuff(result.EffectSkillID, token, duration, result.BuffModifiers)
		if !committed {
			return fmt.Errorf("commit item buff skill %d", result.EffectSkillID)
		}
		if result.EffectRecoveryHP > 0 || result.EffectRecoveryMP > 0 {
			player.ConfigureActiveBuffRecovery(token, result.EffectRecoveryHP, result.EffectRecoveryMP)
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
		if result.EffectVisibilityRefresh {
			service.GetWorldServiceInstance().ReconcileCharacterVisibility(player)
		}
		if result.EffectRecoveryHP > 0 || result.EffectRecoveryMP > 0 {
			h.scheduleItemBuffRecovery(player, token, result.EffectRecoveryHP, result.EffectRecoveryMP)
		}
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
	} else if result.ItemCure != nil {
		// Cure status publication is completed immediately after the committed
		// consume result, before the stock B04C success response.
		player.SendMovementSpeedUpdate()
		player.SendStatsUpdate()
	} else if result.ItemService != nil {
		h.publishItemService(player, *result.ItemService, result.ItemServiceToken, result.ReplacedItemServiceToken)
	} else {
		player.SendVitalsUpdate()
	}
	if player.GetLifeState() == model.Dead {
		life := model.BuildLifeStateUpdatePacket(player.GetUniqueID(), model.Dead)
		player.SendToSelfAndKnown(&life)
	}
	return nil
}

func (h *ItemUseHandler) executeMonsterSummon(player *model.Player, effect model.ItemSummonMonsterEffect) error {
	if h == nil || player == nil || h.summoner == nil || h.summonWorld == nil || len(effect.Candidates) == 0 ||
		effect.Radius <= 0 || effect.Lifetime <= 0 {
		return model.ErrItemUseUnsupported
	}
	totalWeight := 0
	for _, candidate := range effect.Candidates {
		if candidate.CodeName == "" || candidate.Weight <= 0 {
			return model.ErrItemUseUnsupported
		}
		totalWeight += candidate.Weight
	}
	count := 1
	if h.party != nil {
		count = h.party.MemberCountForPlayer(player.GetUniqueID())
	}
	position := player.GetNavmeshPosition()
	spawned := make([]*model.NPC, 0, count)
	for range count {
		randomIntn := h.randomIntn
		if randomIntn == nil {
			randomIntn = rand.Intn
		}
		roll := randomIntn(totalWeight)
		for _, candidate := range effect.Candidates {
			if roll < candidate.Weight {
				npc, spawnErr := h.summoner.SpawnItemSummonNPC(candidate.CodeName, position, effect.Radius, candidate.Rarity)
				if spawnErr != nil {
					for _, previous := range spawned {
						h.summonWorld.RemoveVisibleObject(previous)
					}
					return spawnErr
				}
				spawned = append(spawned, npc)
				break
			}
			roll -= candidate.Weight
		}
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	for _, npc := range spawned {
		spawnedNPC := npc
		schedule(effect.Lifetime, func() { h.summonWorld.RemoveVisibleObject(spawnedNPC) })
	}
	return nil
}

func (h *ItemUseHandler) itemUseNow() time.Time {
	if h != nil && h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h *ItemUseHandler) publishPlayerBadEffectState(player *model.Player, now time.Time) {
	mask, levels := player.CombatBadEffectState(now)
	packet := model.BuildBadEffectUpdate(player.GetUniqueID(), mask, levels)
	h.publishPacket(player, &packet)
}

func (h *ItemUseHandler) scheduleItemCureRefreshes(player *model.Player, now time.Time, refreshAt []time.Time) {
	if len(refreshAt) == 0 {
		return
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	for _, expiration := range refreshAt {
		delay := expiration.Sub(now)
		if delay < 0 {
			delay = 0
		}
		schedule(delay, func() {
			h.publishPlayerBadEffectState(player, h.itemUseNow())
			player.SendStatsUpdate()
		})
	}
}

func (h *ItemUseHandler) scheduleItemBuffRecovery(player *model.Player, token uint32, recoveryHP, recoveryMP int) {
	if h == nil || player == nil || token == 0 || recoveryHP < 0 || recoveryMP < 0 || recoveryHP == 0 && recoveryMP == 0 {
		return
	}
	recovery, ok := h.useCase.(service.ItemBuffRecoveryUseCase)
	if !ok {
		return
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	const interval = 5 * time.Second
	schedule(interval, func() {
		active := false
		for _, buff := range player.ActiveBuffSnapshot() {
			if buff.Token == token && (buff.ExpiresAt.IsZero() || buff.ExpiresAt.After(h.itemUseNow())) {
				active = true
				break
			}
		}
		if !active {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		changed, err := recovery.ApplyItemBuffRecovery(ctx, player, recoveryHP, recoveryMP)
		cancel()
		if err != nil {
			logrus.WithError(err).Warn("failed to persist item-buff recovery")
		} else if changed {
			player.SendVitalsUpdate()
		}
		h.scheduleItemBuffRecovery(player, token, recoveryHP, recoveryMP)
	})
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
			removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect)
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
		added := model.BuildItemPerkAdd(player.GetUniqueID(), effect, remaining)
		h.publishPacket(player, &added)
	}
	if model.ItemServiceTargetMatchesInventory(player, effect) {
		if update, exists, buildErr := model.BuildPlayerItemServiceTargetLevelUpdate(player, effect, true); buildErr == nil && exists {
			h.publishPacket(player, &update)
		}
	}
	if premiumAdded, exists := model.BuildPremiumDailyAdd(token, effect, remaining); exists {
		player.SendToSelf(&premiumAdded)
	}
	if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), token, effect, remaining); exists {
		h.publishPacket(player, &buffAdded)
	}
	if model.ItemServiceAffectsStats(effect) {
		player.SendStatsUpdate()
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	schedule(remaining, func() {
		if player.ClearItemService(effect.Kind, token) {
			if model.ItemServiceUsesItemPerk(effect) {
				removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect)
				h.publishPacket(player, &removed)
			}
			if model.ItemServiceTargetMatchesInventory(player, effect) {
				if update, exists, buildErr := model.BuildPlayerItemServiceTargetLevelUpdate(player, effect, false); buildErr == nil && exists {
					h.publishPacket(player, &update)
				}
			}
			if premiumRemoved, exists := model.BuildPremiumDailyRemove(token, effect); exists {
				player.SendToSelf(&premiumRemoved)
			}
			if model.ItemServiceBuffSkillID(effect) != 0 {
				buffRemoved := model.BuildBuffRemove(token)
				h.publishPacket(player, &buffRemoved)
			}
			if model.ItemServiceAffectsStats(effect) {
				player.SendStatsUpdate()
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
		EncodedSlot:  sourceSlot,
		NewAmount:    newAmount,
		PackedTypeID: itemType,
	})
}

func buildItemUseFailure() network.Packet {
	return vsro188.BuildItemUseFailure(0)
}
