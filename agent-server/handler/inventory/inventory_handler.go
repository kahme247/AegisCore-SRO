package inventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
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

var (
	ErrInventoryHandlerPlayerLookupUnavailable = errors.New("inventory player lookup is unavailable")
	ErrInventoryHandlerSessionIdentityInvalid  = errors.New("inventory session character identity is invalid")
	ErrInventoryHandlerSessionPlayerMismatch   = errors.New("inventory session does not own the resolved player")
	ErrEconomyPacketInvalid                    = errors.New("economy packet is invalid")
)

const playerPickupCollectionRange = float32(160)

// PlayerLookup is the narrow world-facing dependency needed to resolve a
// packet's authenticated runtime owner. It deliberately accepts only a
// UniqueID lookup; character names are mutable display data and must not
// authorize inventory mutations.
type PlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type QuestCollectionProgressRepository interface {
	RecordCollectedItem(context.Context, int, uint32, uint32) ([]model.QuestProgressResult, error)
}

type InventoryHandler struct {
	players PlayerLookup
	objects interface {
		GetObjectByUniqueId(uint32) (model.ISRObject, error)
	}
	objectRemover interface {
		RemoveVisibleObject(model.ISRObject)
	}
	relocator    model.InventoryRelocator
	economy      model.EconomyRepository
	mall         model.ItemMallRepository
	guild        model.GuildStorageRepository
	avatar       model.AvatarInventoryRepository
	cosInventory model.COSInventoryTransferRepository
	groundPickup model.GroundItemPickupRepository
	groundGold   model.GroundGoldPickupRepository
	groundDrop   model.GroundItemDropRepository
	groundItems  interface {
		Spawn(*model.Item, uint32) error
	}
	groundLifecycle interface{ Cancel(*model.Item) }
	questProgress   QuestCollectionProgressRepository
	exchange        ExchangeOperations
	referenceData   *model.ReferenceDataCatalog
	send            func(*server.Session, network.Packet) error
	buybackMu       sync.Mutex
	buyback         map[int]string
	buybackLog      sync.Once
}

func (h *InventoryHandler) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) {
	if h != nil {
		h.referenceData = catalog
	}
}

func (h *InventoryHandler) ConfigureCOSRepository(repository model.COSInventoryTransferRepository) {
	if h != nil {
		h.cosInventory = repository
	}
}

func (h *InventoryHandler) ConfigureGroundItemPickupRepository(repository model.GroundItemPickupRepository) {
	if h != nil {
		h.groundPickup = repository
	}
}

func (h *InventoryHandler) ConfigureGroundGoldPickupRepository(repository model.GroundGoldPickupRepository) {
	if h != nil {
		h.groundGold = repository
	}
}

func (h *InventoryHandler) ConfigureGroundItemDropRepository(repository model.GroundItemDropRepository) {
	if h != nil {
		h.groundDrop = repository
	}
}

// ConfigureGroundItemService connects the persistence-first drop handler to
// the world admission boundary. The handler deliberately depends on the
// narrow Spawn method instead of the concrete service.
func (h *InventoryHandler) ConfigureGroundItemService(spawner interface {
	Spawn(*model.Item, uint32) error
}) {
	if h != nil {
		h.groundItems = spawner
	}
}

// ConfigureGroundItemLifecycle cancels ownership/despawn timers after the
// pickup transaction commits and before the object leaves the world.
func (h *InventoryHandler) ConfigureGroundItemLifecycle(lifecycle interface{ Cancel(*model.Item) }) {
	if h != nil {
		h.groundLifecycle = lifecycle
	}
}

func (h *InventoryHandler) ConfigureQuestProgressRepository(repository QuestCollectionProgressRepository) {
	if h != nil {
		h.questProgress = repository
	}
}

type ExchangeOperations interface {
	Busy(characterID int) bool
	HandleInventoryOperation(context.Context, *server.Session, *model.Player, byte, *network.Packet) error
}

type ItemInventoryOperation byte

const (
	MoveItemOperation                   ItemInventoryOperation = 0x00
	PutItemIntoStorageOperation         ItemInventoryOperation = 0x02
	TakeItemFromStorageOperation        ItemInventoryOperation = 0x03
	PutItemIntoExchangeWindowOperation  ItemInventoryOperation = 0x04
	TakeItemFromExchangeWindowOperation ItemInventoryOperation = 0x05
	DropItemOperation                   ItemInventoryOperation = 0x07
	PickupItemOperation                 ItemInventoryOperation = 0x06
	// Operation 0x08 is body-form overloaded. The maintained NPC-buy path is
	// the recovered eight-byte context form; the stock package-item sender is
	// a separate four-byte form and is demultiplexed in handlePacket.
	BuyItemOperation               ItemInventoryOperation = 0x08
	SellItemOperation              ItemInventoryOperation = 0x09
	DropGoldOperation              ItemInventoryOperation = 0x0A
	DepositStorageGoldOperation    ItemInventoryOperation = 0x0B
	WithdrawStorageGoldOperation   ItemInventoryOperation = 0x0C
	BuyItemFromMallOperation       ItemInventoryOperation = 0x12
	BuyItemFromModernMallOperation ItemInventoryOperation = 0x18
	BuySpecialtyGoodsOperation     ItemInventoryOperation = 0x2B
	SellSpecialtyGoodsOperation    ItemInventoryOperation = 0x2C
	// BuyBackItemOperation is retained as stock updater vocabulary only. The
	// recovered evidence does not prove it as a client 0x7034 discriminator;
	// NPC buyback is handled through opcode.NPCBuyBackRequest below.
	BuyBackItemOperation              ItemInventoryOperation = 0x22
	UnequipAvatarItemOperation        ItemInventoryOperation = 0x23
	EquipAvatarItemOperation          ItemInventoryOperation = 0x24
	TakeItemFromPetInventoryOperation ItemInventoryOperation = 0x1A
	PutItemIntoPetInventoryOperation  ItemInventoryOperation = 0x1B
	GuildStorageMoveOperation         ItemInventoryOperation = 0x1D
	GuildStorageDepositItemOperation  ItemInventoryOperation = 0x1E
	GuildStorageWithdrawItemOperation ItemInventoryOperation = 0x1F
	GuildStorageDepositGoldOperation  ItemInventoryOperation = 0x20
	GuildStorageWithdrawGoldOperation ItemInventoryOperation = 0x21
)

// NewInventoryHandler exposes the two dependencies needed for an inventory
// packet. Tests and future region-owned command adapters can inject both;
// production registration supplies the current world lookup and Agent-owned
// transaction repository.
func NewInventoryHandler(players PlayerLookup, relocator model.InventoryRelocator, economy ...model.EconomyRepository) *InventoryHandler {
	h := &InventoryHandler{players: players, relocator: relocator}
	h.objects, _ = players.(interface {
		GetObjectByUniqueId(uint32) (model.ISRObject, error)
	})
	h.objectRemover, _ = players.(interface {
		RemoveVisibleObject(model.ISRObject)
	})
	if len(economy) > 0 {
		h.economy = economy[0]
	}
	return h
}

// RegisterInventoryHandlerWithRepository registers the database-first
// relocation path. No request-scoped database fallback is available: a nil
// relocator leaves move requests fail-closed without touching the runtime map.
func RegisterInventoryHandlerWithRepository(target *server.Server, relocator model.InventoryRelocator, economy ...model.EconomyRepository) {
	RegisterInventoryHandlerWithExchange(target, relocator, nil, economy...)
}

func RegisterInventoryHandlerWithExchange(target *server.Server, relocator model.InventoryRelocator, exchange ExchangeOperations, economy ...model.EconomyRepository) {
	var economyRepository model.EconomyRepository
	if len(economy) > 0 {
		economyRepository = economy[0]
	}
	RegisterInventoryHandlerWithGuildStorage(target, relocator, exchange, economyRepository, nil, nil)
}

func RegisterInventoryHandlerWithGuildStorage(target *server.Server, relocator model.InventoryRelocator, exchange ExchangeOperations, economy model.EconomyRepository, guild model.GuildStorageRepository, avatar model.AvatarInventoryRepository, mall ...model.ItemMallRepository) *InventoryHandler {
	if target == nil {
		return nil
	}
	handler := NewInventoryHandler(service.GetWorldServiceInstance(), relocator, economy)
	handler.exchange = exchange
	handler.guild = guild
	handler.avatar = avatar
	if len(mall) > 0 {
		handler.mall = mall[0]
	}
	target.RegisterHandler(opcode.ItemOperationRequest, handler.handlePacket)
	target.RegisterHandler(opcode.NPCBuyBackRequest, handler.handleNPCBuyBackPacket)
	handler.registerRepairHandler(target)
	RegisterStorageHandler(target, economy)
	RegisterGuildStorageHandler(target, guild)
	return handler
}

func (h *InventoryHandler) handleNPCBuyBackPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.handleBuyBack(ctx, session, &packet)
}

// RegisterInventoryHandler remains only for integrations that have not yet
// been wired to the Agent's shard pool. It intentionally registers a handler
// with no relocator, so item moves cannot silently mutate memory without a
// transaction. AgentServer uses RegisterInventoryHandlerWithRepository.
func RegisterInventoryHandler(target *server.Server) {
	RegisterInventoryHandlerWithRepository(target, nil)
}

type moveItemRequest struct {
	sourceSlot byte
	targetSlot byte
	amount     uint16
}

func (h *InventoryHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() == 0 {
		// A successful exchange can emit an empty 0x7034 packet for each item.
		// There is no move request to parse or persist in that legacy case.
		return nil
	}

	operationType, err := packet.ReadByte()
	if err != nil {
		return fmt.Errorf("read inventory operation: %w", err)
	}
	if operationType == byte(BuyItemFromMallOperation) || operationType == byte(BuyItemFromModernMallOperation) {
		logrus.Infof("item mall request operation=0x%02X payload=%X", operationType, packet.Payload())
	}
	switch ItemInventoryOperation(operationType) {
	case DropItemOperation:
		return h.handleGroundItemDrop(ctx, session, &packet)
	case PickupItemOperation:
		return h.handleGroundItemPickup(ctx, session, &packet)
	case PutItemIntoExchangeWindowOperation, TakeItemFromExchangeWindowOperation, 13:
		if h.exchange == nil {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		player, resolveErr := h.resolvePlayer(session)
		if resolveErr != nil {
			return resolveErr
		}
		return h.exchange.HandleInventoryOperation(ctx, session, player, operationType, &packet)
	case BuyItemOperation:
		switch packet.Remaining() {
		case 8:
			// AegisCore's existing NPC economy contract is the exact
			// tab/slot/quantity/NPC-context body.
			return h.handleBuy(ctx, session, &packet)
		case vsro188.PackageItemRequestBodySize:
			// Stock operation 0x08 also has a package-item insertion form.
			// Its request lifecycle/catalog transaction is not implemented;
			// parse it exactly and fail closed rather than treating its fields
			// as an NPC purchase.
			return h.handlePackageItemInsertion(session, &packet)
		default:
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
	case BuyItemFromMallOperation, BuyItemFromModernMallOperation:
		return h.handleMallBuy(ctx, session, ItemInventoryOperation(operationType), &packet)
	case SellItemOperation:
		return h.handleSell(ctx, session, &packet)
	case DepositStorageGoldOperation, WithdrawStorageGoldOperation:
		return h.handleStorageGold(ctx, session, ItemInventoryOperation(operationType), &packet)
	case EquipAvatarItemOperation, UnequipAvatarItemOperation:
		return h.handleAvatarTransfer(ctx, session, ItemInventoryOperation(operationType), &packet)
	case PutItemIntoPetInventoryOperation, TakeItemFromPetInventoryOperation:
		return h.handlePetInventoryTransfer(ctx, session, ItemInventoryOperation(operationType), &packet)
	case PutItemIntoStorageOperation, TakeItemFromStorageOperation, 1:
		return h.handleStorageMutation(ctx, session, ItemInventoryOperation(operationType), &packet)
	case GuildStorageMoveOperation, GuildStorageDepositItemOperation, GuildStorageWithdrawItemOperation,
		GuildStorageDepositGoldOperation, GuildStorageWithdrawGoldOperation:
		return h.handleGuildStorageMutation(ctx, session, ItemInventoryOperation(operationType), &packet)
	case MoveItemOperation:
		// Continue through the established relocation kernel below.
	default:
		logrus.Infof("unsupported inventory operation=0x%02X payload=%X", operationType, packet.Payload())
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}

	request, err := parseMoveItemRequest(&packet)
	if err != nil {
		return err
	}
	player, plan, removedBuffs, err := h.relocate(ctx, session, request)
	if err != nil {
		if errors.Is(err, model.ErrInventoryRelocationOutcomeUnknown) {
			// Commit may have reached MySQL even though the caller observed an
			// error. A normal rejection is wire-silent, but retaining this
			// session would leave its runtime map potentially stale. Returning
			// an error makes the session dispatcher close it; a later join
			// reloads the canonical inventory from persistence.
			return fmt.Errorf("inventory relocation commit outcome is unknown: %w", err)
		}
		// Existing invalid inventory actions had no failure response. Preserve
		// that wire behaviour while retaining a classified error in logs and,
		// critically, leaving both persistent and runtime state unchanged.
		logrus.WithError(err).Debug("rejected inventory relocation")
		return nil
	}
	if err := sendRelocationStateUpdates(session, player, plan); err != nil {
		return err
	}
	for _, token := range removedBuffs {
		removed := model.BuildBuffRemove(token)
		player.SendToSelfAndKnown(&removed)
	}
	return sendMoveItemSuccess(session, request, plan)
}

func (h *InventoryHandler) handleGroundItemDrop(ctx context.Context, session *server.Session, packet *network.Packet) error {
	if h == nil || h.groundDrop == nil || h.groundItems == nil || packet == nil || packet.Remaining() != 1 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	slot, err := packet.ReadByte()
	if err != nil || packet.Remaining() != 0 || slot < model.InventoryEquipmentSlots || slot >= model.MaxInventoryWireSize {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()

	result, err := h.groundDrop.Drop(ctx, int64(player.ID), slot)
	if err != nil {
		if errors.Is(err, model.ErrGroundItemDropOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected ground-item drop")
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if result.Item == nil || result.Item.ID <= 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	result.Item.SetNavmeshPosition(player.GetNavmeshPosition())
	if err = h.groundItems.Spawn(result.Item, 0); err != nil {
		restoreErr := h.groundDrop.Restore(ctx, int64(player.ID), slot, int64(result.Item.ID))
		if restoreErr != nil {
			return fmt.Errorf("ground-item world admission failed: %w; restore failed: %v", err, restoreErr)
		}
		logrus.WithError(err).Debug("rejected ground-item world admission; inventory link restored")
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.CommitInventory(result.Inventory)
	return h.sendPacket(session, vsro188.BuildGroundDropSuccess(slot))
}

func (h *InventoryHandler) handleGroundItemPickup(ctx context.Context, session *server.Session, packet *network.Packet) error {
	if h == nil || h.objects == nil || h.objectRemover == nil || (h.groundPickup == nil && h.groundGold == nil) || packet == nil || packet.Remaining() != 4 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	targetUID, err := packet.ReadUInt32()
	if err != nil || targetUID == 0 || packet.Remaining() != 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	object, err := h.objects.GetObjectByUniqueId(targetUID)
	item, valid := groundPickupItem(object)
	if err != nil || !valid || item == nil || item.ID <= 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if owner := item.GetOwner(); owner != 0 && owner != player.GetUniqueID() {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	playerPosition, itemPosition := player.GetNavmeshPosition(), item.GetNavmeshPosition()
	if playerPosition.Region.ID != itemPosition.Region.ID || navmeshv2.HorizontalDistance(playerPosition, itemPosition) > playerPickupCollectionRange {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if isGeneratedGoldItem(item) {
		if h.groundGold == nil {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		gold, pickupErr := h.groundGold.PickupGold(ctx, int64(player.ID), int64(item.ID))
		if pickupErr != nil {
			if errors.Is(pickupErr, model.ErrGroundItemPickupOutcomeUnknown) {
				return pickupErr
			}
			logrus.WithError(pickupErr).Debug("rejected ground-gold pickup")
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		player.SetGold(gold.Gold)
		h.cancelGroundItemLifecycle(object)
		h.objectRemover.RemoveVisibleObject(object)
		if err := h.sendPacket(session, buildGroundPickupAnimation(player, item)); err != nil {
			return err
		}
		return h.sendPacket(session, buildGoldUpdate(gold.Gold))
	}
	if h.groundPickup == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	slot, available := player.Inventory.FirstFreeNormalSlot(player.GetInventorySize())
	if !available {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	result, err := h.groundPickup.Pickup(ctx, int64(player.ID), int64(item.ID), slot)
	if err != nil {
		if errors.Is(err, model.ErrGroundItemPickupOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected ground-item pickup")
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.CommitInventory(result.Inventory)
	h.cancelGroundItemLifecycle(object)
	h.objectRemover.RemoveVisibleObject(object)
	if err := h.sendPacket(session, buildGroundPickupAnimation(player, item)); err != nil {
		return err
	}
	if err := h.sendPacket(session, buildGroundPickupInventoryUpdate(result.Slot)); err != nil {
		return err
	}
	if h.questProgress == nil {
		return nil
	}
	quantity := item.StackSize
	if quantity <= 0 {
		quantity = 1
	}
	if uint64(quantity) > math.MaxUint32 {
		return model.ErrQuestStateConflict
	}
	progress, err := h.questProgress.RecordCollectedItem(ctx, player.ID, item.GetRefObjectID(), uint32(quantity))
	if err != nil {
		return err
	}
	for _, result := range progress {
		if !result.Changed {
			continue
		}
		player.CommitQuestMutation(result.Quest)
		update, buildErr := questwire.BuildUpdate(result.Quest, vsro188.QuestUpdateDelta)
		if buildErr != nil {
			return buildErr
		}
		if err = h.sendPacket(session, update); err != nil {
			return err
		}
	}
	return nil
}

func (h *InventoryHandler) cancelGroundItemLifecycle(object model.ISRObject) {
	if h == nil || h.groundLifecycle == nil {
		return
	}
	if concrete, ok := object.(*model.Item); ok {
		h.groundLifecycle.Cancel(concrete)
	} else if equipment, ok := object.(*model.Equipment); ok && equipment != nil {
		h.groundLifecycle.Cancel(&equipment.Item)
	}
}

func isGeneratedGoldItem(item *model.Item) bool {
	if item == nil {
		return false
	}
	typeInfo := item.GetTypeInfo()
	return typeInfo.TypeID1 == model.TypeID1Item && typeInfo.TypeID2 == 3 && typeInfo.TypeID3 == 5 && typeInfo.TypeID4 != 1
}

func groundPickupItem(object model.ISRObject) (*model.Item, bool) {
	switch item := object.(type) {
	case *model.Item:
		return item, item != nil
	case *model.Equipment:
		if item == nil {
			return nil, false
		}
		return &item.Item, true
	default:
		return nil, false
	}
}

func buildGroundPickupAnimation(player *model.Player, item *model.Item) network.Packet {
	return vsro188.BuildEntityPickupAnimation(player.GetUniqueID(), groundPickupHeading(player, item))
}

func groundPickupHeading(player *model.Player, item *model.Item) byte {
	if player == nil || item == nil {
		return 0
	}
	from := player.GetNavmeshPosition().GetGlobalCoordinates()
	to := item.GetNavmeshPosition().GetGlobalCoordinates()
	radians := math.Atan2(float64(to.Z-from.Z), float64(to.X-from.X))
	if radians < 0 {
		radians += 2 * math.Pi
	}
	return byte(math.Round(radians / (2 * math.Pi) * 255))
}

func buildGroundPickupInventoryUpdate(slot byte) network.Packet {
	return vsro188.BuildItemOperationSlotSuccess(vsro188.ItemOperationPickup, slot)
}

func (h *InventoryHandler) handlePetInventoryTransfer(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h == nil || h.cosInventory == nil || h.objects == nil || packet == nil || packet.Remaining() != 6 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	petUID, err := packet.ReadUInt32()
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	source, err := packet.ReadByte()
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	destination, err := packet.ReadByte()
	if err != nil || packet.Remaining() != 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	object, err := h.objects.GetObjectByUniqueId(petUID)
	pet, valid := object.(*model.COS)
	if err != nil || !valid || pet == nil || pet.InventorySize == 0 || pet.OwnerCharID != player.ID || pet.OwnerUniqueID != player.GetUniqueID() {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	toCOS := operation == PutItemIntoPetInventoryOperation
	if (toCOS && destination >= pet.InventorySize) || (!toCOS && source >= pet.InventorySize) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	playerInventory, cosInventory, err := h.cosInventory.TransferItem(ctx, player.ID, pet.PersistentID, source, destination, toCOS)
	if err != nil {
		if errors.Is(err, model.ErrInventoryRelocationOutcomeUnknown) {
			return err
		}
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.CommitInventory(playerInventory)
	pet.ReplaceInventory(cosInventory)
	response := vsro188.BuildItemOperationObjectSlotSuccess(byte(operation), petUID, destination)
	return h.sendPacket(session, response)
}

func (h *InventoryHandler) handleAvatarTransfer(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h == nil || h.avatar == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	request, err := parseAvatarTransferRequest(packet)
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.avatar.TransferAvatarItem(ctx, int64(player.ID), operation == EquipAvatarItemOperation, request.sourceSlot, request.targetSlot)
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			return err
		}
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.CommitInventoryAndAvatar(result.Inventory, result.AvatarInventory)
	sendAvatarStateUpdate(player, operation, request, result)
	player.SendStatsUpdate()
	return sendAvatarTransferSuccess(session, operation, request)
}

func sendAvatarStateUpdate(player *model.Player, operation ItemInventoryOperation, request moveItemRequest, result model.AvatarTransferResult) {
	if player == nil {
		return
	}
	packet := network.EmptyPacket()
	if operation == EquipAvatarItemOperation {
		item, exists := result.AvatarInventory.Items[request.targetSlot]
		if !exists {
			return
		}
		packet = vsro188.BuildEntityEquipItem(player.GetUniqueID(), request.targetSlot, item.GetRefObjectID(), false)
	} else {
		item, exists := result.Inventory.Items[request.targetSlot]
		if !exists {
			return
		}
		packet = vsro188.BuildEntityUnequipItem(player.GetUniqueID(), request.sourceSlot, item.GetRefObjectID())
	}
	player.SendToSelfAndKnown(&packet)
}

func (h *InventoryHandler) handleStorageGold(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h.economy == nil || packet == nil || packet.Remaining() != 8 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	amount, err := packet.ReadUInt64()
	if err != nil || amount == 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	var result model.EconomyResult
	if operation == DepositStorageGoldOperation {
		result, err = h.economy.DepositStorageGold(ctx, int64(player.ID), session.UserContext.UserID, amount)
	} else {
		result, err = h.economy.WithdrawStorageGold(ctx, int64(player.ID), session.UserContext.UserID, amount)
	}
	if err != nil {
		return h.handleEconomyError(session, err)
	}
	player.SetGold(result.Gold)
	return h.sendPacket(session, buildStorageGoldSuccess(operation, amount, result.Gold))
}

type itemMallPurchaseRequest struct {
	position      model.ItemMallPosition
	operation     ItemInventoryOperation
	mallVersion   byte
	page          byte
	packageName   string
	purchaseCount uint16
	clientPoints  uint32
	clientBonus   uint32
	packageID     uint32
}

func parseItemMallPurchaseRequest(operation ItemInventoryOperation, packet *network.Packet) (itemMallPurchaseRequest, error) {
	if packet == nil {
		return itemMallPurchaseRequest{}, errors.New("item mall purchase packet is unavailable")
	}
	request := itemMallPurchaseRequest{operation: operation}
	if operation == BuyItemFromModernMallOperation {
		wireRequest, err := vsro188.DecodeItemMallModernRequest(packet)
		if err != nil {
			return itemMallPurchaseRequest{}, err
		}
		request.position = model.ItemMallPosition{
			RefShopGroupID: wireRequest.ShopGroupID,
			GroupIndex:     wireRequest.GroupIndex,
			TabIndex:       wireRequest.TabIndex,
			SlotIndex:      wireRequest.SlotIndex,
		}
		request.packageName = wireRequest.PackageName
		request.purchaseCount = wireRequest.PackageCount
		request.clientPoints = uint32(wireRequest.OpaqueValue)
		request.clientBonus = uint32(wireRequest.OpaqueValue >> 32)
		request.packageID = wireRequest.PackageID
		if request.position.RefShopGroupID == 0 || request.purchaseCount == 0 {
			return itemMallPurchaseRequest{}, errors.New("item mall purchase contains invalid identifiers")
		}
		return request, nil
	}
	var err error
	if operation == BuyItemFromMallOperation {
		request.position.RefShopGroupID = 1051 // GROUP_MALL in the vSRO 1.188 Media catalog.
		if request.mallVersion, err = packet.ReadByte(); err != nil {
			return itemMallPurchaseRequest{}, fmt.Errorf("read item mall version: %w", err)
		}
		if request.page, err = packet.ReadByte(); err != nil {
			return itemMallPurchaseRequest{}, fmt.Errorf("read item mall page: %w", err)
		}
		if request.position.GroupIndex, err = packet.ReadByte(); err != nil {
			return itemMallPurchaseRequest{}, fmt.Errorf("read item mall section: %w", err)
		}
	} else {
		if request.position.RefShopGroupID, err = packet.ReadUInt16(); err != nil {
			return itemMallPurchaseRequest{}, fmt.Errorf("read item mall shop group: %w", err)
		}
		if request.position.GroupIndex, err = packet.ReadByte(); err != nil {
			return itemMallPurchaseRequest{}, fmt.Errorf("read item mall group index: %w", err)
		}
	}
	if request.position.TabIndex, err = packet.ReadByte(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall tab index: %w", err)
	}
	if request.position.SlotIndex, err = packet.ReadByte(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall slot index: %w", err)
	}
	if request.packageName, err = packet.ReadString(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall package name: %w", err)
	}
	if request.purchaseCount, err = packet.ReadUInt16(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall purchase count: %w", err)
	}
	if request.clientPoints, err = packet.ReadUInt32(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall client points: %w", err)
	}
	if request.clientBonus, err = packet.ReadUInt32(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall client bonus: %w", err)
	}
	if request.packageID, err = packet.ReadUInt32(); err != nil {
		return itemMallPurchaseRequest{}, fmt.Errorf("read item mall package ID: %w", err)
	}
	if packet.Remaining() != 0 {
		return itemMallPurchaseRequest{}, errors.New("item mall purchase contains trailing data")
	}
	if request.position.RefShopGroupID == 0 || request.purchaseCount == 0 || (operation == BuyItemFromMallOperation && request.mallVersion == 0) {
		return itemMallPurchaseRequest{}, errors.New("item mall purchase contains invalid identifiers")
	}
	return request, nil
}

func (h *InventoryHandler) handleMallBuy(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h.mall == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	request, err := parseItemMallPurchaseRequest(operation, packet)
	if err != nil {
		logrus.Infof("invalid item mall request operation=0x%02X: %v", operation, err)
		return err
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	result, err := h.mall.Purchase(ctx, int64(player.ID), session.UserContext.UserID, request.position, request.purchaseCount)
	if err != nil {
		if errors.Is(err, model.ErrItemMallOutcomeUnknown) {
			// The purchase may already have committed. Do not let the client
			// continue with a possibly stale inventory/balance snapshot; a fresh
			// session reloads the canonical state from persistence.
			session.Close()
			return fmt.Errorf("item mall purchase outcome is unknown: %w", err)
		}
		logrus.WithError(err).WithFields(logrus.Fields{"shop_group": request.position.RefShopGroupID, "group": request.position.GroupIndex, "tab": request.position.TabIndex, "slot": request.position.SlotIndex}).Info("rejected item mall purchase")
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.CommitInventory(result.Inventory)
	var serviceToken, replacedServiceToken uint32
	if result.ItemService != nil {
		serviceToken, replacedServiceToken = player.ApplyItemService(*result.ItemService)
	}
	purchaseSuccess := buildItemMallPurchaseSuccess(request, result)
	packets := []network.Packet{purchaseSuccess}
	if operation != BuyItemFromModernMallOperation {
		// The recovered operation-0x18 completion reaches the stock balance
		// updater with its state-only flag; that path updates server-side
		// balance state without directly emitting 0x3153. Keep the legacy
		// route's established balance-then-result ordering separate until its
		// own completion edge is capture-correlated.
		packets = []network.Packet{buildItemMallBalanceUpdate(result.Balances), purchaseSuccess}
	}
	if err := h.sendPackets(session, packets...); err != nil {
		return err
	}
	if result.ItemService != nil && serviceToken != 0 {
		if replacedServiceToken != 0 {
			if model.ItemServiceUsesItemPerk(*result.ItemService) {
				removed := model.BuildItemPerkRemove(player.GetUniqueID(), result.ItemService.SourceRefItemID, replacedServiceToken)
				player.SendToSelfAndKnown(&removed)
			}
			if premiumRemoved, exists := model.BuildPremiumDailyRemove(replacedServiceToken, *result.ItemService); exists {
				player.SendToSelf(&premiumRemoved)
			}
			if model.ItemServiceBuffSkillID(*result.ItemService) != 0 {
				buffRemoved := model.BuildBuffRemove(replacedServiceToken)
				player.SendToSelfAndKnown(&buffRemoved)
			}
		}
		remaining := time.Until(result.ItemService.ExpiresAt)
		if remaining > 0 {
			if model.ItemServiceUsesItemPerk(*result.ItemService) {
				added := model.BuildItemPerkAdd(player.GetUniqueID(), result.ItemService.SourceRefItemID, serviceToken, result.ItemService.DailyBudget, remaining)
				player.SendToSelfAndKnown(&added)
			}
			if premiumAdded, exists := model.BuildPremiumDailyAdd(serviceToken, *result.ItemService, remaining); exists {
				player.SendToSelf(&premiumAdded)
			}
			if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), serviceToken, *result.ItemService, remaining); exists {
				player.SendToSelfAndKnown(&buffAdded)
			}
			time.AfterFunc(remaining, func() {
				if player.ClearItemService(result.ItemService.Kind, serviceToken) {
					if model.ItemServiceUsesItemPerk(*result.ItemService) {
						removed := model.BuildItemPerkRemove(player.GetUniqueID(), result.ItemService.SourceRefItemID, serviceToken)
						player.SendToSelfAndKnown(&removed)
					}
					if premiumRemoved, exists := model.BuildPremiumDailyRemove(serviceToken, *result.ItemService); exists {
						player.SendToSelf(&premiumRemoved)
					}
					if model.ItemServiceBuffSkillID(*result.ItemService) != 0 {
						buffRemoved := model.BuildBuffRemove(serviceToken)
						player.SendToSelfAndKnown(&buffRemoved)
					}
				}
			})
		}
	}
	return nil
}

func (h *InventoryHandler) handleGuildStorageMutation(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h.guild == nil || packet == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}

	if operation == GuildStorageDepositGoldOperation || operation == GuildStorageWithdrawGoldOperation {
		if packet.Remaining() != 8 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		amount, readErr := packet.ReadUInt64()
		if readErr != nil || amount == 0 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		var result model.EconomyResult
		if operation == GuildStorageDepositGoldOperation {
			result, err = h.guild.DepositGuildGold(ctx, int64(player.ID), session.UserContext.UserID, amount)
		} else {
			result, err = h.guild.WithdrawGuildGold(ctx, int64(player.ID), session.UserContext.UserID, amount)
		}
		if err != nil {
			return h.handleEconomyError(session, err)
		}
		player.SetGold(result.Gold)
		return h.sendPacket(session, buildGuildStorageGoldSuccess(operation, amount))
	}

	var source, destination byte
	var amount uint16
	var npcUID uint32
	if operation == GuildStorageMoveOperation {
		if packet.Remaining() != 8 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		source, _ = packet.ReadByte()
		destination, _ = packet.ReadByte()
		amount, _ = packet.ReadUInt16()
		npcUID, _ = packet.ReadUInt32()
	} else {
		if packet.Remaining() != 6 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		source, _ = packet.ReadByte()
		destination, _ = packet.ReadByte()
		npcUID, _ = packet.ReadUInt32()
	}
	code, validationErr := h.validateInteractionNPC(player, npcUID, false)
	if validationErr != nil || !strings.Contains(strings.ToUpper(code), "GUILD") {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if operation == GuildStorageMoveOperation {
		err = h.guild.MoveGuildStorage(ctx, int64(player.ID), source, destination, amount)
		if err == nil {
			return h.sendPacket(session, buildGuildStorageMoveSuccess(operation, source, destination, amount))
		}
	} else {
		var result model.EconomyResult
		if operation == GuildStorageDepositItemOperation {
			result, err = h.guild.DepositGuildItem(ctx, int64(player.ID), session.UserContext.UserID, source, destination)
		} else {
			result, err = h.guild.WithdrawGuildItem(ctx, int64(player.ID), session.UserContext.UserID, source, destination)
		}
		if err == nil {
			player.CommitEconomy(result.Gold, result.Inventory)
			return h.sendPacket(session, buildGuildStorageTransferSuccess(operation, source, destination))
		}
	}
	return h.handleEconomyError(session, err)
}

func (h *InventoryHandler) handleBuy(ctx context.Context, session *server.Session, packet *network.Packet) error {
	if h.economy == nil || packet == nil || packet.Remaining() != 8 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	tab, e1 := packet.ReadByte()
	slot, e2 := packet.ReadByte()
	amount, e3 := packet.ReadUInt16()
	npcUID, e4 := packet.ReadUInt32()
	if errors.Join(e1, e2, e3, e4) != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	npcCode, err := h.validateInteractionNPC(player, npcUID, false)
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	result, err := h.economy.Buy(ctx, int64(player.ID), session.UserContext.UserID, npcCode, tab, slot, amount)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"npc": npcCode, "tab": tab, "slot": slot, "amount": amount,
		}).Warn("rejected NPC shop purchase")
		return h.handleEconomyError(session, err)
	}
	player.CommitEconomy(result.Gold, result.Inventory)
	goldUpdate := vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: result.Gold})
	return h.sendPackets(session, goldUpdate, buildBuySuccess(tab, slot, amount, result.DestinationSlots, result.Item))
}

func (h *InventoryHandler) handlePackageItemInsertion(session *server.Session, packet *network.Packet) error {
	request, err := vsro188.DecodePackageItemRequestBody(packet)
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	logrus.WithFields(logrus.Fields{
		"selectorA": fmt.Sprintf("0x%02X", request.SelectorA),
		"selectorB": fmt.Sprintf("0x%02X", request.SelectorB),
		"quantity":  request.Quantity,
	}).Debug("package-item operation 0x08 is not enabled")
	return h.sendPacket(session, buildUnsupportedItemOperationResponse())
}

func (h *InventoryHandler) handleSell(ctx context.Context, session *server.Session, packet *network.Packet) error {
	if h.economy == nil || packet == nil || packet.Remaining() != 7 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	source, e1 := packet.ReadByte()
	amount, e2 := packet.ReadUInt16()
	npcUID, e3 := packet.ReadUInt32()
	if errors.Join(e1, e2, e3) != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if _, err = h.validateInteractionNPC(player, npcUID, false); err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	sessionID := buybackSessionID(session)
	resetBuyback := h.buybackSession(player.ID) != sessionID
	result, err := h.economy.Sell(ctx, int64(player.ID), session.UserContext.UserID, source, amount, resetBuyback)
	if err != nil {
		return h.handleEconomyError(session, err)
	}
	h.setBuybackSession(player.ID, sessionID)
	player.CommitEconomy(result.Gold, result.Inventory)
	if err = h.sendPacket(session, buildSellSuccess(source, amount, npcUID, result.BuybackSlot)); err != nil {
		return err
	}
	return h.sendPacket(session, buildGoldUpdate(result.Gold))
}

func buybackSessionID(session *server.Session) string {
	if session == nil {
		return ""
	}
	if session.ID != "" {
		return session.ID
	}
	return fmt.Sprintf("%p", session)
}

func (h *InventoryHandler) buybackSession(characterID int) string {
	h.buybackMu.Lock()
	defer h.buybackMu.Unlock()
	return h.buyback[characterID]
}

func (h *InventoryHandler) setBuybackSession(characterID int, sessionID string) {
	h.buybackMu.Lock()
	defer h.buybackMu.Unlock()
	if h.buyback == nil {
		h.buyback = make(map[int]string)
	}
	h.buyback[characterID] = sessionID
}

func (h *InventoryHandler) handleBuyBack(ctx context.Context, session *server.Session, packet *network.Packet) error {
	if h.economy == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	h.buybackLog.Do(func() {
		if packet == nil {
			logrus.Info("observed first NPC buy-back request with nil packet")
			return
		}
		logrus.WithFields(logrus.Fields{
			"opcode":    fmt.Sprintf("0x%04X", opcode.NPCBuyBackRequest),
			"payload":   fmt.Sprintf("%X", packet.Payload()),
			"remaining": packet.Remaining(),
		}).Info("observed first NPC buy-back request")
	})
	request, parseErr := parseBuyBackRequest(packet)
	if parseErr != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if _, err = h.validateInteractionNPC(player, request.npcUID, false); err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	result, err := h.economy.BuyBack(ctx, int64(player.ID), session.UserContext.UserID, request.source)
	if err != nil {
		return h.handleEconomyError(session, err)
	}
	if len(result.DestinationSlots) != 1 {
		return model.ErrEconomyOutcomeUnknown
	}
	player.CommitEconomy(result.Gold, result.Inventory)
	if err = h.sendPacket(session, buildNPCBuyBackResponse(true)); err != nil {
		return err
	}
	if err = h.sendPacket(session, buildBuyBackSuccess(result.DestinationSlots[0], request.source, result.Amount)); err != nil {
		return err
	}
	return h.sendPacket(session, buildGoldUpdate(result.Gold))
}

func buildNPCBuyBackResponse(success bool) network.Packet {
	return vsro188.BuildNPCBuyBackResponse(success)
}

type buyBackRequest struct {
	npcUID uint32
	source byte
}

func parseBuyBackRequest(packet *network.Packet) (buyBackRequest, error) {
	if packet == nil || packet.Remaining() != 5 {
		return buyBackRequest{}, ErrEconomyPacketInvalid
	}
	npcUID, e1 := packet.ReadUInt32()
	source, e2 := packet.ReadByte()
	if err := errors.Join(e1, e2); err != nil {
		return buyBackRequest{}, err
	}
	if npcUID == 0 || source >= 5 {
		return buyBackRequest{}, ErrEconomyPacketInvalid
	}
	return buyBackRequest{npcUID: npcUID, source: source}, nil
}

func (h *InventoryHandler) handleStorageMutation(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h.economy == nil || packet == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	var source, destination byte
	var amount uint16
	var npcUID uint32
	if operation == 1 {
		if packet.Remaining() != 8 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		source, _ = packet.ReadByte()
		destination, _ = packet.ReadByte()
		amount, _ = packet.ReadUInt16()
		npcUID, _ = packet.ReadUInt32()
	} else {
		if packet.Remaining() != 6 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		source, _ = packet.ReadByte()
		destination, _ = packet.ReadByte()
		npcUID, _ = packet.ReadUInt32()
	}
	if _, err = h.validateInteractionNPC(player, npcUID, true); err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if operation == 1 {
		err = h.economy.MoveStorage(ctx, session.UserContext.UserID, source, destination, amount)
		if err == nil {
			return h.sendPacket(session, buildStorageMoveSuccess(operation, source, destination, amount))
		}
	} else {
		var result model.EconomyResult
		if operation == PutItemIntoStorageOperation {
			result, err = h.economy.Deposit(ctx, int64(player.ID), session.UserContext.UserID, source, destination)
		} else {
			result, err = h.economy.Withdraw(ctx, int64(player.ID), session.UserContext.UserID, source, destination)
		}
		if err == nil {
			player.CommitEconomy(result.Gold, result.Inventory)
			return h.sendPacket(session, buildStorageTransferSuccess(operation, source, destination))
		}
	}
	return h.handleEconomyError(session, err)
}

func (h *InventoryHandler) validateInteractionNPC(player *model.Player, uid uint32, warehouse bool) (string, error) {
	if h.objects == nil || player == nil || uid == 0 {
		return "", ErrInventoryHandlerPlayerLookupUnavailable
	}
	known := player.GetKnownObjectList()
	if known == nil {
		return "", ErrInventoryHandlerPlayerLookupUnavailable
	}
	knownObject, ok := known.GetKnownObjects()[uid]
	if !ok {
		return "", ErrInventoryHandlerPlayerLookupUnavailable
	}
	object, err := h.objects.GetObjectByUniqueId(uid)
	if err != nil || object == nil || object != knownObject {
		return "", ErrInventoryHandlerPlayerLookupUnavailable
	}
	npc, ok := object.(*model.NPC)
	if !ok || !npc.GetTypeInfo().IsNPCNpc() {
		return "", ErrInventoryHandlerPlayerLookupUnavailable
	}
	code := npc.GetName()
	upper := strings.ToUpper(code)
	if warehouse && !strings.Contains(upper, "WAREHOUSE") {
		return "", ErrInventoryHandlerPlayerLookupUnavailable
	}
	return code, nil
}

func (h *InventoryHandler) handleEconomyError(session *server.Session, err error) error {
	if errors.Is(err, model.ErrEconomyOutcomeUnknown) || errors.Is(err, model.ErrGuildStorageOutcomeUnknown) {
		return err
	}
	logrus.WithError(err).Warn("rejected economy operation")
	return h.sendPacket(session, buildUnsupportedItemOperationResponse())
}
func (h *InventoryHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

// sendPackets preserves the stock client's expected balance-then-purchase
// ordering as one sender job. Tests and small integrations may inject the
// single-packet callback; production uses Session.SendBatch so another
// handler cannot interleave a packet between the two related updates.
func (h *InventoryHandler) sendPackets(session *server.Session, packets ...network.Packet) error {
	if h.send != nil {
		for _, packet := range packets {
			if err := h.send(session, packet); err != nil {
				return err
			}
		}
		return nil
	}
	return session.SendBatch(packets)
}

func buildBuySuccess(tab, slot byte, amount uint16, destinations []byte, item model.Item) network.Packet {
	rent := network.EmptyPacket()
	model.WriteRentInfo(&rent, item)
	return vsro188.BuildItemBuySuccess(tab, slot, amount, destinations, rent.Payload())
}
func buildSellSuccess(source byte, amount uint16, npcUID uint32, buybackSlot byte) network.Packet {
	return vsro188.BuildItemSellSuccess(source, amount, npcUID, buybackSlot)
}

func buildGoldUpdate(gold uint64) network.Packet {
	return vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold})
}

func buildStorageGoldSuccess(operation ItemInventoryOperation, amount, characterGold uint64) network.Packet {
	value := amount
	if operation == DepositStorageGoldOperation {
		value = characterGold
	}
	return vsro188.BuildItemOperationUInt64Success(byte(operation), value)
}

func buildBuyBackSuccess(destination, source byte, amount uint16) network.Packet {
	return vsro188.BuildItemBuyBackSuccess(destination, source, amount)
}
func buildStorageTransferSuccess(operation ItemInventoryOperation, source, destination byte) network.Packet {
	return vsro188.BuildItemOperationSlotPairSuccess(byte(operation), source, destination)
}
func buildStorageMoveSuccess(operation ItemInventoryOperation, source, destination byte, amount uint16) network.Packet {
	return vsro188.BuildItemOperationSlotPairQuantitySuccess(byte(operation), source, destination, amount)
}

func buildGuildStorageTransferSuccess(operation ItemInventoryOperation, source, destination byte) network.Packet {
	return buildStorageTransferSuccess(operation, source, destination)
}

func buildGuildStorageMoveSuccess(operation ItemInventoryOperation, source, destination byte, amount uint16) network.Packet {
	return buildStorageMoveSuccess(operation, source, destination, amount)
}

func buildGuildStorageGoldSuccess(operation ItemInventoryOperation, amount uint64) network.Packet {
	return vsro188.BuildItemOperationUInt64Success(byte(operation), amount)
}

func buildUnsupportedItemOperationResponse() network.Packet {
	return vsro188.BuildItemOperationFailure(0)
}

func buildItemMallBalanceUpdate(balances model.ItemMallBalances) network.Packet {
	return vsro188.BuildBalanceStateUpdate(vsro188.BalanceStateUpdate{
		First:  balances.Silk,
		Second: balances.GiftSilk,
		Third:  balances.Point,
	})
}

func buildItemMallPurchaseSuccess(request itemMallPurchaseRequest, result model.ItemMallPurchaseResult) network.Packet {
	if request.operation == BuyItemFromModernMallOperation {
		return vsro188.BuildItemMallModernPurchaseSuccess(vsro188.ItemMallModernPurchaseSuccess{
			ShopGroupID:  result.Position.RefShopGroupID,
			GroupIndex:   result.Position.GroupIndex,
			TabIndex:     result.Position.TabIndex,
			SlotIndex:    result.Position.SlotIndex,
			ResultBytes:  result.DestinationSlots,
			PackageCount: request.purchaseCount,
		})
	}
	if request.operation == BuyItemFromMallOperation {
		return vsro188.BuildItemMallLegacyPurchaseSuccess(vsro188.ItemMallLegacyPurchaseSuccess{
			Operation:        byte(request.operation),
			MallVersion:      request.mallVersion,
			Page:             request.page,
			GroupIndex:       result.Position.GroupIndex,
			TabIndex:         result.Position.TabIndex,
			SlotIndex:        result.Position.SlotIndex,
			DestinationSlots: result.DestinationSlots,
			Quantity:         result.Quantity,
		})
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(byte(request.operation))
	packet.WriteUInt16(result.Position.RefShopGroupID)
	packet.WriteByte(result.Position.GroupIndex)
	packet.WriteByte(result.Position.TabIndex)
	packet.WriteByte(result.Position.SlotIndex)
	packet.WriteByte(byte(len(result.DestinationSlots)))
	for _, slot := range result.DestinationSlots {
		packet.WriteByte(slot)
	}
	// The mall response echoes the number of packages requested. The package's
	// delivered item quantity is already defined by the client Media catalog;
	// sending that expanded quantity here makes the client walk past the
	// destination list for multi-item packages.
	packet.WriteUInt16(request.purchaseCount)
	for range result.DestinationSlots {
		model.WriteRentInfo(&packet, result.Item)
	}
	return packet
}

func parseMoveItemRequest(packet *network.Packet) (moveItemRequest, error) {
	if packet == nil {
		return moveItemRequest{}, errors.New("inventory move request packet is nil")
	}
	sourceSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read inventory move source slot: %w", err)
	}
	targetSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read inventory move target slot: %w", err)
	}
	amount, err := packet.ReadUInt16()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read inventory move amount: %w", err)
	}
	if packet.Remaining() != 0 {
		return moveItemRequest{}, fmt.Errorf("inventory move request has %d trailing bytes", packet.Remaining())
	}
	return moveItemRequest{sourceSlot: sourceSlot, targetSlot: targetSlot, amount: amount}, nil
}

func parseAvatarTransferRequest(packet *network.Packet) (moveItemRequest, error) {
	if packet == nil {
		return moveItemRequest{}, errors.New("avatar transfer request packet is nil")
	}
	sourceSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read avatar transfer source slot: %w", err)
	}
	targetSlot, err := packet.ReadByte()
	if err != nil {
		return moveItemRequest{}, fmt.Errorf("read avatar transfer target slot: %w", err)
	}
	// vSRO 1.188 sends only the two slots. Accept the older four-byte form
	// only when its unused quantity is zero.
	if packet.Remaining() == 2 {
		amount, readErr := packet.ReadUInt16()
		if readErr != nil || amount != 0 {
			return moveItemRequest{}, errors.New("avatar transfer quantity must be zero")
		}
	}
	if packet.Remaining() != 0 {
		return moveItemRequest{}, fmt.Errorf("avatar transfer request has %d trailing bytes", packet.Remaining())
	}
	return moveItemRequest{sourceSlot: sourceSlot, targetSlot: targetSlot}, nil
}

// relocate resolves the player exclusively from session-owned identity and
// calls the per-inventory commit boundary. In particular, it never uses the
// mutable CharName field that the legacy handler used as its authority key.
func (h *InventoryHandler) relocate(ctx context.Context, session *server.Session, request moveItemRequest) (*model.Player, model.InventoryRelocationPlan, []uint32, error) {
	if h == nil || h.relocator == nil {
		return nil, model.InventoryRelocationPlan{}, nil, model.ErrInventoryRelocationRepositoryUnavailable
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return nil, model.InventoryRelocationPlan{}, nil, err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return nil, model.InventoryRelocationPlan{}, nil, model.ErrInventoryRelocationConflict
	}
	plan, err := player.Inventory.CommitRelocation(player, request.sourceSlot, request.targetSlot, func(plan model.InventoryRelocationPlan) error {
		return h.relocator.Relocate(ctx, int64(session.UserContext.CharacterID), plan)
	})
	if err != nil {
		return nil, model.InventoryRelocationPlan{}, nil, err
	}
	var removedBuffs []uint32
	if plan.Action == model.InventoryRelocationEquip || plan.Action == model.InventoryRelocationUnequip {
		player.RecalculatePassiveSkillModifiers(h.referenceData)
		removedBuffs = player.RemoveInvalidFireShieldBuffs(h.referenceData)
	}
	return player, plan, removedBuffs, nil
}

func (h *InventoryHandler) resolvePlayer(session *server.Session) (*model.Player, error) {
	if h == nil || h.players == nil {
		return nil, ErrInventoryHandlerPlayerLookupUnavailable
	}
	if session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return nil, ErrInventoryHandlerSessionIdentityInvalid
	}

	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return nil, fmt.Errorf("resolve inventory player: %w", err)
	}
	if player == nil || player.GetUniqueID() != session.UserContext.UniqueID || player.ID != session.UserContext.CharacterID || player.Session != session {
		return nil, ErrInventoryHandlerSessionPlayerMismatch
	}
	return player, nil
}

func sendRelocationStateUpdates(session *server.Session, player *model.Player, plan model.InventoryRelocationPlan) error {
	if session == nil || player == nil {
		return ErrInventoryHandlerSessionIdentityInvalid
	}

	switch plan.Action {
	case model.InventoryRelocationEquip:
		if !plan.SourceAmmunition {
			packet := vsro188.BuildEntityEquipItem(player.GetUniqueID(), plan.TargetSlot, plan.SourceReferenceID, plan.SourceOneHanded)
			if err := session.Send(packet); err != nil {
				return err
			}
		}
	case model.InventoryRelocationUnequip:
		if !plan.SourceAmmunition {
			packet := vsro188.BuildEntityUnequipItem(player.GetUniqueID(), plan.SourceSlot, plan.SourceReferenceID)
			if err := session.Send(packet); err != nil {
				return err
			}
		}
	}
	for _, move := range plan.AdditionalMoves {
		// Additional moves only arise when a paired body item is displaced by
		// an equip transition. Observers must receive its body-slot removal as
		// well as the newly equipped primary item.
		if !move.SourceAmmunition {
			packet := vsro188.BuildEntityUnequipItem(player.GetUniqueID(), move.SourceSlot, move.SourceReferenceID)
			if err := session.Send(packet); err != nil {
				return err
			}
		}
	}
	if plan.Action == model.InventoryRelocationEquip || plan.Action == model.InventoryRelocationUnequip || len(plan.AdditionalMoves) > 0 {
		player.SendStatsUpdate()
	}
	return nil
}

func sendMoveItemSuccess(session *server.Session, request moveItemRequest, plan model.InventoryRelocationPlan) error {
	if session == nil {
		return ErrInventoryHandlerSessionIdentityInvalid
	}
	if err := session.Send(vsro188.BuildItemOperationRelocationSuccess(request.sourceSlot, request.targetSlot, request.amount)); err != nil {
		return err
	}
	for _, move := range plan.AdditionalMoves {
		// Paired equipment displacement is a second real inventory relocation.
		// The entity unequip packet updates the character model, but the owning
		// client also needs B034 so its bag moves the item out of the body slot.
		if err := session.Send(vsro188.BuildItemOperationRelocationSuccess(move.SourceSlot, move.TargetSlot, 0)); err != nil {
			return err
		}
	}
	return nil
}

func sendAvatarTransferSuccess(session *server.Session, operation ItemInventoryOperation, request moveItemRequest) error {
	if session == nil {
		return ErrInventoryHandlerSessionIdentityInvalid
	}
	return session.Send(buildAvatarTransferSuccess(operation, request))
}

func buildAvatarTransferSuccess(operation ItemInventoryOperation, request moveItemRequest) network.Packet {
	return vsro188.BuildItemOperationAvatarTransferSuccess(byte(operation), request.sourceSlot, request.targetSlot)
}
