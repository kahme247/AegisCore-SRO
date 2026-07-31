package inventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
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

const jobSuitEquipDelay = 30 * time.Second

// PlayerLookup is the narrow world-facing dependency needed to resolve a
// packet's authenticated runtime owner. It deliberately accepts only a
// UniqueID lookup; character names are mutable display data and must not
// authorize inventory mutations.
type PlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type SafeTradeCOSResolver interface {
	ResolveActiveOwned(*model.Player, model.COSKind) (*model.COS, bool)
}

type SafeTradeDistance interface {
	DistanceTo(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) float32
}

type QuestCollectionProgressRepository interface {
	RecordCollectedItem(context.Context, int, uint32, uint32) ([]model.QuestProgressResult, error)
}

type OldWomanSearchGroundActionRepository interface {
	StartOldWomanSearchAction(context.Context, int) (model.OldWomanSearchStartResult, error)
	AdvanceOldWomanSearchAction(context.Context, int, byte) (model.OldWomanSearchActionResult, error)
}

type NativeCatchActionRepository interface {
	StartNativeCatchAction(context.Context, int, int) (model.NativeCatchStartResult, error)
	AdvanceNativeCatchAction(context.Context, int, int, byte) (model.NativeCatchActionResult, error)
}

type nativeCatchScheduleKey struct {
	characterID int
	questID     int
}

type InventoryHandler struct {
	players PlayerLookup
	objects interface {
		GetObjectByUniqueId(uint32) (model.ISRObject, error)
	}
	objectRemover interface {
		RemoveVisibleObject(model.ISRObject)
	}
	relocator         model.InventoryRelocator
	economy           model.EconomyRepository
	mall              model.ItemMallRepository
	guild             model.GuildStorageRepository
	avatar            model.AvatarInventoryRepository
	magicCube         model.MagicCubeRepository
	cosInventory      model.COSInventoryTransferRepository
	safeTrade         model.SafeTradeRepository
	packageItems      model.PackageItemCatalog
	safeTradeCOS      SafeTradeCOSResolver
	safeTradeDistance SafeTradeDistance
	groundPickup      model.GroundItemPickupRepository
	groundGold        model.GroundGoldPickupRepository
	groundDrop        model.GroundItemDropRepository
	groundItems       interface {
		Spawn(*model.Item, uint32) error
	}
	groundLifecycle       interface{ Cancel(*model.Item) }
	questProgress         QuestCollectionProgressRepository
	oldWomanSearch        OldWomanSearchGroundActionRepository
	oldWomanRoll          func() byte
	oldWomanSchedule      func(time.Duration, func())
	oldWomanMu            sync.Mutex
	oldWomanGeneration    map[int]uint64
	nativeCatch           NativeCatchActionRepository
	nativeCatchRoll       func() byte
	nativeCatchSchedule   func(time.Duration, func())
	nativeCatchMu         sync.Mutex
	nativeCatchGeneration map[nativeCatchScheduleKey]uint64
	jobSuitSchedule       func(time.Duration, func())
	jobSuitMu             sync.Mutex
	jobSuitPending        map[int]bool
	jobSuitTeleport       interface{ SubmitSamePositionReload(*model.Player) error }
	exchange              ExchangeOperations
	referenceData         *model.ReferenceDataCatalog
	send                  func(*server.Session, network.Packet) error
	buybackMu             sync.Mutex
	buyback               map[int]string
	buybackLog            sync.Once
	commandSequence       atomic.Uint64
	gameplayRates         interface{ Current() model.GameplayRates }
}

func (h *InventoryHandler) ConfigureGameplayRates(rates interface{ Current() model.GameplayRates }) {
	if h != nil {
		h.gameplayRates = rates
	}
}
func (h *InventoryHandler) pickupRange() float32 {
	if h != nil && h.gameplayRates != nil {
		if v := h.gameplayRates.Current().GroundPickupRange; v > 0 {
			return float32(v)
		}
	}
	return playerPickupCollectionRange
}
func (h *InventoryHandler) jobSuitDelay() time.Duration {
	if h != nil && h.gameplayRates != nil {
		if v := h.gameplayRates.Current().JobSuitEquipDelayMS; v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	}
	return jobSuitEquipDelay
}

func (h *InventoryHandler) advanceOldWomanSearchPeriodicEvent(ctx context.Context, session *server.Session, player *model.Player) (model.OldWomanSearchActionResult, error) {
	roll := byte(rand.IntN(101))
	if h.oldWomanRoll != nil {
		roll = h.oldWomanRoll()
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	result, err := h.oldWomanSearch.AdvanceOldWomanSearchAction(ctx, player.ID, roll)
	if err != nil {
		return model.OldWomanSearchActionResult{}, err
	}
	if result.Added != nil {
		player.CommitQuestItemGrant(model.QuestItemGrantResult{Quest: result.Quest, Inventory: result.Inventory, Added: *result.Added})
		if err = h.sendPacket(session, vsro188.BuildItemOperationTwoByteSuccess(vsro188.ItemOperationServerAdd, result.Added.Slot, 0x3C)); err != nil {
			return result, err
		}
	} else {
		player.CommitQuestMutation(result.Quest)
	}
	if result.Succeeded {
		update, buildErr := questwire.BuildUpdate(result.Quest, vsro188.QuestUpdateDelta)
		if buildErr != nil {
			return result, buildErr
		}
		return result, h.sendPacket(session, update)
	}
	if result.Resolved {
		notice, buildErr := vsro188.BuildQuestNotification(result.FailureTalkKey)
		if buildErr != nil {
			return result, buildErr
		}
		return result, h.sendPacket(session, notice)
	}
	return result, nil
}

func (h *InventoryHandler) beginOldWomanSearchEvents(session *server.Session, player *model.Player) {
	h.oldWomanMu.Lock()
	if h.oldWomanGeneration == nil {
		h.oldWomanGeneration = make(map[int]uint64)
	}
	h.oldWomanGeneration[player.ID]++
	generation := h.oldWomanGeneration[player.ID]
	h.oldWomanMu.Unlock()
	h.scheduleOldWomanSearchEvent(session, player, generation)
}

func (h *InventoryHandler) scheduleOldWomanSearchEvent(session *server.Session, player *model.Player, generation uint64) {
	if h.oldWomanSchedule == nil || session == nil || player == nil {
		return
	}
	h.oldWomanSchedule(time.Second, func() {
		h.oldWomanMu.Lock()
		current := h.oldWomanGeneration[player.ID]
		h.oldWomanMu.Unlock()
		if current != generation {
			return
		}
		result, err := h.advanceOldWomanSearchPeriodicEvent(context.Background(), session, player)
		if err != nil {
			if !errors.Is(err, model.ErrOldWomanSearchState) {
				logrus.WithError(err).WithField("character_id", player.ID).Error("QNO_RM_OLDWOMAN_5 periodic event failed")
			}
			h.finishOldWomanSearchEvents(player.ID, generation)
			return
		}
		if result.Resolved {
			h.finishOldWomanSearchEvents(player.ID, generation)
			return
		}
		h.scheduleOldWomanSearchEvent(session, player, generation)
	})
}

func (h *InventoryHandler) finishOldWomanSearchEvents(characterID int, generation uint64) {
	h.oldWomanMu.Lock()
	defer h.oldWomanMu.Unlock()
	if h.oldWomanGeneration[characterID] == generation {
		delete(h.oldWomanGeneration, characterID)
	}
}

func (h *InventoryHandler) beginNativeCatchEvents(session *server.Session, player *model.Player, questID int) {
	key := nativeCatchScheduleKey{characterID: player.ID, questID: questID}
	h.nativeCatchMu.Lock()
	if h.nativeCatchGeneration == nil {
		h.nativeCatchGeneration = make(map[nativeCatchScheduleKey]uint64)
	}
	h.nativeCatchGeneration[key]++
	generation := h.nativeCatchGeneration[key]
	h.nativeCatchMu.Unlock()
	h.scheduleNativeCatchEvent(session, player, key, generation)
}

func (h *InventoryHandler) scheduleNativeCatchEvent(session *server.Session, player *model.Player, key nativeCatchScheduleKey, generation uint64) {
	if h.nativeCatchSchedule == nil || h.nativeCatch == nil || session == nil || player == nil {
		return
	}
	h.nativeCatchSchedule(time.Second, func() {
		h.nativeCatchMu.Lock()
		current := h.nativeCatchGeneration[key]
		h.nativeCatchMu.Unlock()
		if current != generation {
			return
		}
		roll := byte(rand.IntN(101))
		if h.nativeCatchRoll != nil {
			roll = h.nativeCatchRoll()
		}
		player.LockEconomy()
		result, err := h.nativeCatch.AdvanceNativeCatchAction(context.Background(), player.ID, key.questID, roll)
		player.UnlockEconomy()
		if err != nil {
			if !errors.Is(err, model.ErrNativeCatchActionState) {
				logrus.WithError(err).WithFields(logrus.Fields{"character_id": player.ID, "quest_id": key.questID}).Error("native catch-action event failed")
			}
			h.finishNativeCatchEvents(key, generation)
			return
		}
		if err = h.publishNativeCatchResult(session, player, result); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"character_id": player.ID, "quest_id": key.questID}).Error("native catch-action publish failed")
			h.finishNativeCatchEvents(key, generation)
			return
		}
		if result.Resolved {
			h.finishNativeCatchEvents(key, generation)
			return
		}
		h.scheduleNativeCatchEvent(session, player, key, generation)
	})
}

func (h *InventoryHandler) finishNativeCatchEvents(key nativeCatchScheduleKey, generation uint64) {
	h.nativeCatchMu.Lock()
	defer h.nativeCatchMu.Unlock()
	if h.nativeCatchGeneration[key] == generation {
		delete(h.nativeCatchGeneration, key)
	}
}

func (h *InventoryHandler) publishNativeCatchResult(session *server.Session, player *model.Player, result model.NativeCatchActionResult) error {
	if result.Added != nil {
		player.CommitQuestItemGrant(model.QuestItemGrantResult{Quest: result.Quest, Inventory: result.Inventory, Added: *result.Added})
		if err := h.sendPacket(session, vsro188.BuildItemOperationTwoByteSuccess(vsro188.ItemOperationServerAdd, result.Added.Slot, 0x3C)); err != nil {
			return err
		}
	} else {
		player.CommitQuestMutation(result.Quest)
	}
	if result.Succeeded {
		update, err := questwire.BuildUpdate(result.Quest, vsro188.QuestUpdateDelta)
		if err != nil {
			return err
		}
		if err = h.sendPacket(session, update); err != nil {
			return err
		}
		if result.SuccessTalkKey != "" {
			notice, buildErr := vsro188.BuildQuestNotification(result.SuccessTalkKey)
			if buildErr != nil {
				return buildErr
			}
			return h.sendPacket(session, notice)
		}
	} else if result.Resolved && result.FailureTalkKey != "" {
		notice, err := vsro188.BuildQuestNotification(result.FailureTalkKey)
		if err != nil {
			return err
		}
		return h.sendPacket(session, notice)
	}
	return nil
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

func (h *InventoryHandler) ConfigureSafeTrade(repository model.SafeTradeRepository, resolver SafeTradeCOSResolver, distance SafeTradeDistance) {
	if h != nil {
		h.safeTrade = repository
		h.safeTradeCOS = resolver
		h.safeTradeDistance = distance
	}
}

func (h *InventoryHandler) ConfigurePackageItems(repository model.PackageItemCatalog) {
	if h != nil {
		h.packageItems = repository
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

func (h *InventoryHandler) ConfigureOldWomanSearch(repository OldWomanSearchGroundActionRepository) {
	if h != nil {
		h.oldWomanSearch = repository
	}
}

func (h *InventoryHandler) ConfigureNativeCatchActions(repository NativeCatchActionRepository) {
	if h != nil {
		h.nativeCatch = repository
	}
}

func (h *InventoryHandler) ConfigureJobSuitTeleport(teleport interface {
	SubmitSamePositionReload(*model.Player) error
}) {
	if h != nil {
		h.jobSuitTeleport = teleport
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
	SellItemFromCOSOperation       ItemInventoryOperation = 0x14
	BuyItemFromModernMallOperation ItemInventoryOperation = 0x18
	BuyTokenPackageItemOperation   ItemInventoryOperation = 0x2B
	// Stock v188's item-operation structure parser has an inclusive maximum
	// discriminator of 0x2B. Keep 0x2C named only as an explicit rejection;
	// it is not evidence of a specialty-goods sell request.
	RejectedItemOperation2C ItemInventoryOperation = 0x2C
	// BuyBackItemOperation is retained as stock updater vocabulary only. The
	// recovered evidence does not prove it as a client 0x7034 discriminator;
	// NPC buyback is handled through opcode.NPCBuyBackRequest below.
	BuyBackItemOperation              ItemInventoryOperation = 0x22
	UnequipAvatarItemOperation        ItemInventoryOperation = 0x23
	EquipAvatarItemOperation          ItemInventoryOperation = 0x24
	TakeItemFromPetInventoryOperation ItemInventoryOperation = 0x1A
	PutItemIntoPetInventoryOperation  ItemInventoryOperation = 0x1B
	MoveItemInPetInventoryOperation   ItemInventoryOperation = 0x10
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
	h := &InventoryHandler{
		players: players, relocator: relocator,
		oldWomanSchedule:    func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) },
		nativeCatchSchedule: func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) },
		jobSuitSchedule:     func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) },
		jobSuitPending:      make(map[int]bool),
	}
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
	target.RegisterHandler(opcode.InventoryUnknownRequest, handler.handleUnknownPacket)
	handler.registerRepairHandler(target)
	RegisterStorageHandler(target, economy)
	RegisterGuildStorageHandler(target, guild)
	return handler
}

func (h *InventoryHandler) handleUnknownPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	if _, err := vsro188.DecodeInventoryUnknownRequest(&packet); err != nil {
		return err
	}
	// SR_GameServer.exe 0x00513250 resolves this as ground-object type 0x46,
	// then its 0x703F-only callee at 0x0053E0F0 returns literal error 5. The
	// stock path is deliberately non-mutating in v1.188.
	return h.sendPacket(session, vsro188.BuildInventoryUnknownFailure(5))
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
	// Packet.Sequence is the security sequence and is not a unique gameplay
	// command identifier: stock clients reuse it across multiple 0x7034
	// requests. Include a server-side monotonic value so a completed move can
	// never make a later sell look like a conflicting replay.
	commandIdentity := model.InventoryCommandIdentity{Key: fmt.Sprintf("inventory:%s:%016x:%02x:%04x", session.ID, h.commandSequence.Add(1), packet.Sequence, packet.MessageID), Name: fmt.Sprintf("inventory.operation.%02x", operationType)}
	ctx = model.WithInventoryCommand(ctx, commandIdentity)
	// The pinned stock client emits ordinary character ground pickup as the
	// action-0 form `00 | targetUID:u32`. Action 0 is also the inventory-move
	// discriminator, so disambiguate only the exact four-byte body and only when
	// its UID resolves to a live ground item. Slot moves retain their established
	// parser and transaction below.
	if ItemInventoryOperation(operationType) == MoveItemOperation && packet.Remaining() == 4 && h.objects != nil {
		candidate := packet
		if targetUID, readErr := candidate.ReadUInt32(); readErr == nil && targetUID != 0 {
			if object, lookupErr := h.objects.GetObjectByUniqueId(targetUID); lookupErr == nil {
				if _, groundItem := groundPickupItem(object); groundItem {
					return h.handleGroundItemPickup(ctx, session, &packet)
				}
			}
		}
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
			// Its selector authority and package transaction are distinct from
			// the eight-byte ordinary NPC purchase above.
			return h.handlePackageItemInsertion(ctx, session, operationType, &packet)
		default:
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
	case BuyItemFromMallOperation, BuyItemFromModernMallOperation:
		return h.handleMallBuy(ctx, session, ItemInventoryOperation(operationType), &packet)
	case BuyTokenPackageItemOperation:
		// Stock v188 maps 0x2B to the generic package-item insertion handler;
		// it is not a separate COS safe-trade purchase opcode.
		return h.handlePackageItemInsertion(ctx, session, operationType, &packet)
	case RejectedItemOperation2C:
		return h.rejectItemOperation2C(session)
	case SellItemOperation:
		return h.handleSell(ctx, session, &packet)
	case SellItemFromCOSOperation:
		return h.handleCOSSell(ctx, session, &packet)
	case DepositStorageGoldOperation, WithdrawStorageGoldOperation:
		return h.handleStorageGold(ctx, session, ItemInventoryOperation(operationType), &packet)
	case EquipAvatarItemOperation, UnequipAvatarItemOperation:
		return h.handleAvatarTransfer(ctx, session, ItemInventoryOperation(operationType), &packet)
	case PutItemIntoPetInventoryOperation, TakeItemFromPetInventoryOperation, MoveItemInPetInventoryOperation:
		return h.handlePetInventoryTransfer(ctx, session, ItemInventoryOperation(operationType), &packet)
	case PutItemIntoStorageOperation, TakeItemFromStorageOperation, 1:
		return h.handleStorageMutation(ctx, session, ItemInventoryOperation(operationType), &packet)
	case GuildStorageMoveOperation, GuildStorageDepositItemOperation, GuildStorageWithdrawItemOperation,
		GuildStorageDepositGoldOperation, GuildStorageWithdrawGoldOperation:
		return h.handleGuildStorageMutation(ctx, session, ItemInventoryOperation(operationType), &packet)
	case ItemInventoryOperation(vsro188.ItemOperationMagicCubePush), ItemInventoryOperation(vsro188.ItemOperationMagicCubePop):
		return h.handleMagicCubeMove(ctx, session, operationType, &packet)
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
	if request.targetSlot == model.SlotExtra || request.sourceSlot == model.SlotExtra {
		return h.beginJobSuitChange(session, request)
	}
	if checker, ok := h.relocator.(interface {
		InventoryCommandCompleted(context.Context, int64, string) (bool, error)
	}); ok {
		completed, checkErr := checker.InventoryCommandCompleted(ctx, int64(session.UserContext.CharacterID), commandIdentity.Key)
		if checkErr != nil {
			return checkErr
		}
		if completed {
			return sendMoveItemSuccess(session, request, model.InventoryRelocationPlan{})
		}
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
		// The client applies inventory drags optimistically. Complete a rejected
		// command with B034 failure so it restores the source and destination
		// slots instead of retaining a client-only merge.
		logrus.WithError(err).Debug("rejected inventory relocation")
		return h.sendPacket(session, vsro188.BuildItemOperationFailure(0))
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

func (h *InventoryHandler) ConfigureMagicCube(repository model.MagicCubeRepository) {
	if h != nil {
		h.magicCube = repository
	}
}

func (h *InventoryHandler) handleMagicCubeMove(ctx context.Context, session *server.Session, operation byte, packet *network.Packet) error {
	if h.magicCube == nil || packet == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	request, err := vsro188.DecodeMagicCubeMoveRequestBody(operation, packet)
	if err != nil {
		return err
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	cubeID, active := player.ActiveMagicCube()
	if !active || (operation == vsro188.ItemOperationMagicCubePop && request.Context != uint32(cubeID)) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	var result model.MagicCubeMoveResult
	if operation == vsro188.ItemOperationMagicCubePush {
		result, err = h.magicCube.Push(ctx, player.ID, session.UserContext.UserID, cubeID, request.Source, request.Destination, request.Amount)
	} else {
		result, err = h.magicCube.Pop(ctx, player.ID, session.UserContext.UserID, cubeID, request.Source, request.Destination, request.Amount)
	}
	if err != nil {
		if errors.Is(err, model.ErrMagicCubeConflict) || errors.Is(err, model.ErrMagicCubeInvalid) {
			player.DeactivateMagicCube()
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		return err
	}
	player.CommitInventory(result.Inventory)
	response, err := vsro188.BuildMagicCubeSuccess(vsro188.MagicCubeSuccess{Operation: operation, Context: request.Context, Source: request.Source, Destination: request.Destination, Amount: request.Amount})
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

// beginJobSuitChange preserves the stock two-phase job-clothes lifecycle. The
// client starts its yellow pending-action gauge when it sends 0x7034 and ends
// it when B034 arrives, so neither the relocation nor its completion may be
// emitted until the 30-second wearing period expires. Both putting on and
// taking off job clothes require the player to remain in a town region.
func (h *InventoryHandler) beginJobSuitChange(session *server.Session, request moveItemRequest) error {
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	if !model.IsTownRegionV188(player.GetNavmeshPosition().Region.ID) {
		return nil
	}
	player.LockEconomy()
	_, err = model.PlanInventoryRelocation(player.Inventory.Items, request.sourceSlot, request.targetSlot, player)
	player.UnlockEconomy()
	if err != nil {
		logrus.WithError(err).Debug("rejected delayed job-suit equip")
		return nil
	}

	h.jobSuitMu.Lock()
	if h.jobSuitPending[player.ID] {
		h.jobSuitMu.Unlock()
		return nil
	}
	h.jobSuitPending[player.ID] = true
	h.jobSuitMu.Unlock()
	direction := vsro188.JobSuitCountdownEquip
	if request.sourceSlot == model.SlotExtra {
		direction = vsro188.JobSuitCountdownUnequip
	}
	delay := h.jobSuitDelay()
	start := vsro188.BuildJobSuitCountdownStart(player.GetUniqueID(), direction, byte(delay/time.Second))
	if err := h.sendPacket(session, start); err != nil {
		h.jobSuitMu.Lock()
		delete(h.jobSuitPending, player.ID)
		h.jobSuitMu.Unlock()
		return err
	}
	player.Broadcast(&start)

	h.jobSuitSchedule(delay, func() {
		h.jobSuitMu.Lock()
		delete(h.jobSuitPending, player.ID)
		h.jobSuitMu.Unlock()

		currentPlayer, resolveErr := h.resolvePlayer(session)
		if resolveErr != nil || !model.IsTownRegionV188(currentPlayer.GetNavmeshPosition().Region.ID) {
			stop := vsro188.BuildJobSuitCountdownStop(player.GetUniqueID(), vsro188.JobSuitCountdownCancel)
			player.SendToSelfAndKnown(&stop)
			return
		}
		completedPlayer, plan, removedBuffs, relocateErr := h.relocate(context.Background(), session, request)
		if relocateErr != nil {
			stop := vsro188.BuildJobSuitCountdownStop(player.GetUniqueID(), vsro188.JobSuitCountdownCancel)
			player.SendToSelfAndKnown(&stop)
			logrus.WithError(relocateErr).WithField("character_id", player.ID).Debug("delayed job-suit equip canceled")
			return
		}
		stop := vsro188.BuildJobSuitCountdownStop(completedPlayer.GetUniqueID(), vsro188.JobSuitCountdownFinish)
		completedPlayer.SendToSelfAndKnown(&stop)
		if sendErr := sendRelocationStateUpdates(session, completedPlayer, plan); sendErr != nil {
			logrus.WithError(sendErr).WithField("character_id", player.ID).Debug("send delayed job-suit state")
			return
		}
		for _, token := range removedBuffs {
			removed := model.BuildBuffRemove(token)
			completedPlayer.SendToSelfAndKnown(&removed)
		}
		if sendErr := sendMoveItemSuccess(session, request, plan); sendErr != nil {
			logrus.WithError(sendErr).WithField("character_id", player.ID).Debug("send delayed job-suit completion")
			return
		}
		if h.jobSuitTeleport != nil {
			if teleportErr := h.jobSuitTeleport.SubmitSamePositionReload(completedPlayer); teleportErr != nil {
				logrus.WithError(teleportErr).WithField("character_id", player.ID).Warn("reload character after job-suit change")
			}
		}
	})
	return nil
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
	if err != nil || !valid || item == nil || (item.ID <= 0 && !item.TransientGroundItem) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if owner := item.GetOwner(); owner != 0 && owner != player.GetUniqueID() {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	playerPosition, itemPosition := player.GetNavmeshPosition(), item.GetNavmeshPosition()
	if playerPosition.Region.ID != itemPosition.Region.ID || navmeshv2.HorizontalDistance(playerPosition, itemPosition) > h.pickupRange() {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if definitions := model.NativeCatchDefinitionsForTrigger(item.GetRefObjectID()); len(definitions) != 0 {
		if h.nativeCatch == nil {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		activeQuestID := 0
		quests := player.QuestSnapshot()
		for _, definition := range definitions {
			for _, quest := range quests {
				if quest.QuestID == definition.QuestID && quest.Status.ClientActive() {
					if activeQuestID != 0 {
						return h.sendPacket(session, buildUnsupportedItemOperationResponse())
					}
					activeQuestID = definition.QuestID
				}
			}
		}
		if activeQuestID == 0 {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		started, startErr := h.nativeCatch.StartNativeCatchAction(ctx, player.ID, activeQuestID)
		if startErr != nil {
			logrus.WithError(startErr).WithField("quest_id", activeQuestID).Debug("rejected native ground catch action")
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		player.CommitQuestMutation(started.Quest)
		capture, buildErr := vsro188.BuildQuestActionResult(opcode.QuestCatchResult, uint32(activeQuestID), started.Countdown)
		if buildErr != nil {
			return buildErr
		}
		if sendErr := h.sendPacket(session, capture); sendErr != nil {
			return sendErr
		}
		h.beginNativeCatchEvents(session, player, activeQuestID)
		return nil
	}
	if item.GetRefObjectID() == model.OldWomanSearchTriggerItemRefID {
		if h.oldWomanSearch == nil {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		started, startErr := h.oldWomanSearch.StartOldWomanSearchAction(ctx, player.ID)
		if startErr != nil {
			logrus.WithError(startErr).Debug("rejected QNO_RM_OLDWOMAN_5 ground action")
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		player.CommitQuestMutation(started.Quest)
		capture, buildErr := vsro188.BuildQuestActionResult(opcode.QuestCatchResult, uint32(model.OldWomanSearchQuestID), started.Countdown)
		if buildErr != nil {
			return buildErr
		}
		if sendErr := h.sendPacket(session, capture); sendErr != nil {
			return sendErr
		}
		h.beginOldWomanSearchEvents(session, player)
		return nil
	}
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
		animationErr := h.sendPacket(session, buildGroundPickupAnimation(player, item))
		h.cancelGroundItemLifecycle(object)
		h.objectRemover.RemoveVisibleObject(object)
		if animationErr != nil {
			return animationErr
		}
		return h.sendPacket(session, buildGoldPickupUpdate(gold.Gold))
	}
	if h.groundPickup == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	if item.TransientGroundItem && item.CreatorName != "" {
		return h.handleSpecialtyGroundItemPickup(ctx, session, player, object, item)
	}
	slot, available := player.Inventory.FirstFreeNormalSlot(player.GetInventorySize())
	if !available {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	var result model.GroundItemPickupResult
	if item.TransientGroundItem {
		repository, ok := h.groundPickup.(model.GroundTransientPickupRepository)
		if !ok {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		result, err = repository.PickupTransient(ctx, int64(player.ID), item, slot)
	} else {
		result, err = h.groundPickup.Pickup(ctx, int64(player.ID), int64(item.ID), slot)
	}
	if err != nil {
		if errors.Is(err, model.ErrGroundItemPickupOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected ground-item pickup")
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player.CommitInventory(result.Inventory)
	committedItem, exists := result.Inventory.Items[result.Slot]
	if !exists {
		return model.ErrGroundItemPickupOutcomeUnknown
	}
	// Stock SR_GameServer 0x004B29A0 publishes 0x3036 before the later
	// AQ_SlotUpdater success reaches 0x004E9480 and emits B034. The client
	// pickup branch at 0x0087BC1F gates its local activity message on that
	// source-0 transition, so preserve the same animation-then-completion order.
	if err := h.sendPackets(
		session,
		buildGroundPickupAnimation(player, item),
		buildGroundPickupInventoryUpdate(result.Slot, committedItem),
	); err != nil {
		return err
	}
	h.cancelGroundItemLifecycle(object)
	h.objectRemover.RemoveVisibleObject(object)
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

// HandleGroundItemAction adapts the stock client's 0x7074 kind-4 ground-item
// click to the same strict transaction used by the direct 0x7034 operation.
func (h *InventoryHandler) HandleGroundItemAction(ctx context.Context, session *server.Session, targetUID uint32) error {
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt32(targetUID); err != nil {
		return err
	}
	return h.handleGroundItemPickup(ctx, session, &packet)
}

func (h *InventoryHandler) handleSpecialtyGroundItemPickup(ctx context.Context, session *server.Session, player *model.Player, object model.ISRObject, item *model.Item) error {
	repository, ok := h.groundPickup.(model.GroundTransientPickupRepository)
	if !ok || h.safeTradeCOS == nil || h.safeTradeDistance == nil || !model.SpecialtyPickupAllowed(player.GetJob().Type, player.GetJob().Alias, item.CreatorName) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	transport, ok := h.safeTradeCOS.ResolveActiveOwned(player, model.COSJobTransport)
	if !ok || transport == nil || transport.PersistentID == 0 || transport.GetLifeState() == model.Dead {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	playerPosition, transportPosition := player.GetNavmeshPosition(), transport.GetNavmeshPosition()
	if playerPosition.Region.ID != transportPosition.Region.ID || h.safeTradeDistance.DistanceTo(playerPosition, transportPosition) >= 1000 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	slot, available := transport.Inventory.FirstFreeSlot(transport.InventorySize)
	if !available {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	result, err := repository.PickupTransientToJobCOS(ctx, int64(player.ID), transport.PersistentID, item, slot, transport.GetTradeScale())
	if err != nil {
		if errors.Is(err, model.ErrGroundItemPickupOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected specialty ground-item pickup")
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	inventory := transport.Inventory.Snapshot()
	if !inventory.Put(result.Slot, transport.InventorySize, result.Item) {
		return model.ErrGroundItemPickupOutcomeUnknown
	}
	transport.ReplaceInventory(inventory)
	transport.SetTradeScale(result.Scale)
	h.cancelGroundItemLifecycle(object)
	h.objectRemover.RemoveVisibleObject(object)
	packets := []network.Packet{
		buildGroundPickupAnimation(player, item),
		model.BuildCOSPickupInventoryUpdate(transport.GetUniqueID(), result.Slot, result.Item),
		mustTradeScalePacket(result.Scale),
	}
	if result.SafeTradeReset {
		reset, encodeErr := (vsro188.JobSafeTradeCodec{}).Encode(vsro188.SafeTradeUpdate{Code: vsro188.JobSafeTradeCodeTransition40})
		if encodeErr != nil {
			return encodeErr
		}
		packets = append(packets, reset)
	}
	return h.sendPackets(session, packets...)
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
	if refID := item.GetRefObjectID(); refID >= 1 && refID <= 3 {
		return true
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

func buildGroundPickupInventoryUpdate(slot byte, item model.Item) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(1)
	packet.WriteByte(vsro188.ItemOperationPickup)
	// Client 0x0087ED21 and stock GameServer 0x004F7AE6 serialize this action-6 destination as u8.
	packet.WriteByte(slot)
	// If the destination is empty, 0x0087BA20 immediately calls the inventory
	// item hydrator at 0x008BE4F0 using the remaining B034 payload.
	// 0x008BE4F0 first calls 0x008BB130, which consumes RentInfo before
	// RefObjectID. Omitting it shifts the object ID to zero and asserts in
	// GlobalDataManager.cpp:4935.
	model.WriteRentInfo(&packet, item)
	packet.WriteUInt32(item.GetRefObjectID())
	model.WriteInventoryItem(&packet, item)
	return packet
}

func (h *InventoryHandler) handlePetInventoryTransfer(ctx context.Context, session *server.Session, operation ItemInventoryOperation, packet *network.Packet) error {
	if h == nil || h.cosInventory == nil || h.objects == nil || packet == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	wantBody := 6
	if operation == MoveItemInPetInventoryOperation {
		wantBody = 8
	}
	if packet.Remaining() != wantBody {
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
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	var amount uint16
	if operation == MoveItemInPetInventoryOperation {
		amount, err = packet.ReadUInt16()
	}
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
	if operation == MoveItemInPetInventoryOperation {
		if source >= pet.InventorySize || destination >= pet.InventorySize {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		sourceItem, sourceHas := pet.Inventory.Snapshot().Items[source]
		stackSize := sourceItem.StackSize
		if stackSize < 1 {
			stackSize = 1
		}
		if !sourceHas || (amount != 0 && int(amount) != stackSize) {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		response := vsro188.BuildItemOperationObjectSlotPairQuantitySuccess(byte(operation), petUID, source, destination, amount)
		if source == destination {
			return h.sendPacket(session, response)
		}
		reorder, ok := h.cosInventory.(model.COSInventoryReorderRepository)
		if !ok {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		player.LockEconomy()
		defer player.UnlockEconomy()
		if h.exchange != nil && h.exchange.Busy(player.ID) {
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		cosInventory, reorderErr := reorder.ReorderItem(ctx, player.ID, pet.PersistentID, source, destination)
		if reorderErr != nil {
			if errors.Is(reorderErr, model.ErrInventoryRelocationOutcomeUnknown) {
				return reorderErr
			}
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		pet.ReplaceInventory(cosInventory)
		if inventoryUpdate, supported := model.BuildCOSInventoryUpdate(pet); supported {
			return h.sendPackets(session, response, inventoryUpdate)
		}
		return h.sendPacket(session, response)
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
	response := vsro188.BuildItemOperationObjectSlotPairSuccess(byte(operation), petUID, source, destination)
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
	if result.ActivatedDevilSpiritRefID != 0 {
		used := vsro188.BuildItemUsedNotification(vsro188.ItemUsedNotification{
			FirstValue:  player.GetUniqueID(),
			SecondValue: result.ActivatedDevilSpiritRefID,
		})
		player.SendToSelfAndKnown(&used)
	}
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
				removed := model.BuildItemPerkRemove(player.GetUniqueID(), *result.ItemService)
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
				added := model.BuildItemPerkAdd(player.GetUniqueID(), *result.ItemService, remaining)
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
						removed := model.BuildItemPerkRemove(player.GetUniqueID(), *result.ItemService)
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
			return h.sendPacket(session, buildStorageMoveSuccess(operation, source, destination, amount))
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
			return h.sendPacket(session, buildStorageTransferSuccess(operation, source, destination))
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
	if amount == 0 {
		return h.sendPacket(session, vsro188.BuildItemOperationFailure(vsro188.NPCBuyErrorInvalidQuantity))
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
	return h.sendPackets(session, goldUpdate, buildBuySuccess(tab, slot, amount, result.DestinationSlots))
}

func (h *InventoryHandler) handlePackageItemInsertion(ctx context.Context, session *server.Session, operation byte, packet *network.Packet) error {
	request, err := vsro188.DecodePackageItemRequestBody(packet)
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	fields := logrus.Fields{
		"operation": fmt.Sprintf("0x%02X", operation),
		"selectorA": fmt.Sprintf("0x%02X", request.SelectorA),
		"selectorB": fmt.Sprintf("0x%02X", request.SelectorB),
		"quantity":  request.Quantity,
	}
	if (operation == vsro188.ItemOperationPackageItem || operation == vsro188.ItemOperationPackageItemExtended) && h.packageItems != nil && session != nil && session.UserContext.SelectedNPCRefObjID != 0 {
		good, resolveErr := h.packageItems.ResolvePackageItem(ctx, session.UserContext.SelectedNPCRefObjID, request.SelectorA, request.SelectorB)
		if resolveErr != nil {
			logrus.WithError(resolveErr).WithFields(fields).Debug("rejected package-item selector authority")
			return h.sendPacket(session, buildUnsupportedItemOperationResponse())
		}
		fields["package"] = good.CodeName
		fields["members"] = len(good.Members)
		if purchaser, ok := h.packageItems.(model.PackageItemPurchaser); ok {
			player, playerErr := h.resolvePlayer(session)
			if playerErr != nil {
				return playerErr
			}
			if session.UserContext.SelectedNPCUniqueID == 0 {
				return h.sendPacket(session, buildUnsupportedItemOperationResponse())
			}
			if _, interactionErr := h.validateInteractionNPC(player, session.UserContext.SelectedNPCUniqueID, false); interactionErr != nil {
				return h.sendPacket(session, buildUnsupportedItemOperationResponse())
			}
			npc, objectErr := h.objects.GetObjectByUniqueId(session.UserContext.SelectedNPCUniqueID)
			if objectErr != nil || npc == nil || npc.GetRefObjectID() != session.UserContext.SelectedNPCRefObjID {
				return h.sendPacket(session, buildUnsupportedItemOperationResponse())
			}
			player.LockEconomy()
			defer player.UnlockEconomy()
			if h.exchange != nil && h.exchange.Busy(player.ID) {
				return h.sendPacket(session, buildUnsupportedItemOperationResponse())
			}
			var result model.PackageItemPurchaseResult
			var purchaseErr error
			if operation == vsro188.ItemOperationPackageItemExtended {
				result, purchaseErr = purchaser.PurchaseTokenPackageItem(ctx, int64(player.ID), npc.GetRefObjectID(), request.SelectorA, request.SelectorB, request.Quantity)
			} else {
				result, purchaseErr = purchaser.PurchasePackageItem(ctx, int64(player.ID), npc.GetRefObjectID(), request.SelectorA, request.SelectorB, request.Quantity)
			}
			if purchaseErr != nil {
				if errors.Is(purchaseErr, model.ErrPackageItemOutcomeUnknown) {
					return purchaseErr
				}
				logrus.WithError(purchaseErr).WithFields(fields).Debug("rejected package-item purchase")
				return h.handleEconomyError(session, purchaseErr)
			}
			continuations := make([]vsro188.PackageItemContinuation, len(result.Continuations))
			for index, continuation := range result.Continuations {
				continuations[index] = vsro188.PackageItemContinuation{Selector: continuation.Slot, Quantity: continuation.Remaining}
			}
			success := vsro188.BuildPackageItemSuccess(vsro188.PackageItemSuccess{Operation: operation, SelectorA: request.SelectorA, SelectorB: request.SelectorB, ResultBytes: result.DestinationSlots, Quantity: request.Quantity, Continuations: continuations})
			if operation == vsro188.ItemOperationPackageItemExtended {
				player.CommitInventory(result.Inventory)
				return h.sendPacket(session, success)
			}
			player.CommitEconomy(result.Gold, result.Inventory)
			return h.sendPackets(session, vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: result.Gold}), success)
		}
	}
	logrus.WithFields(fields).Debug("package-item operation is not enabled")
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
	npcObject, err := h.objects.GetObjectByUniqueId(npcUID)
	if err != nil || npcObject == nil || npcObject.GetRefObjectID() == 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	sessionID := buybackSessionID(session)
	resetBuyback := h.buybackSession(player.ID) != sessionID
	result, err := h.economy.Sell(ctx, int64(player.ID), session.UserContext.UserID, npcObject.GetRefObjectID(), source, amount, resetBuyback)
	if err != nil {
		return h.handleSpecialtyEconomyError(session, err)
	}
	h.setBuybackSession(player.ID, sessionID)
	player.CommitEconomy(result.Gold, result.Inventory)
	h.postTriJobReward(ctx, result)
	if err = h.sendPacket(session, buildSellSuccess(source, amount, npcUID, result.BuybackSlot)); err != nil {
		return err
	}
	return h.sendPacket(session, buildGoldUpdate(result.Gold))
}

func (h *InventoryHandler) handleCOSSell(ctx context.Context, session *server.Session, packet *network.Packet) error {
	repository, ok := h.economy.(model.COSEconomyRepository)
	if !ok || h.objects == nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	request, err := vsro188.DecodeCOSSellRequest(packet)
	if err != nil {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	object, err := h.objects.GetObjectByUniqueId(request.ObjectUniqueID)
	transport, valid := object.(*model.COS)
	if err != nil || !valid || transport == nil || transport.PersistentID == 0 || transport.Kind != model.COSJobTransport || transport.OwnerCharID != player.ID || transport.OwnerUniqueID != player.GetUniqueID() {
		return h.handleSpecialtyEconomyError(session, model.ErrSpecialtyNoCart)
	}
	if request.Source >= transport.InventorySize {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	playerPosition, transportPosition := player.GetNavmeshPosition(), transport.GetNavmeshPosition()
	if playerPosition.Region.ID != transportPosition.Region.ID || navmeshv2.HorizontalDistance(playerPosition, transportPosition) > 1000 {
		return h.handleSpecialtyEconomyError(session, model.ErrSpecialtyCartTooFar)
	}
	if _, err = h.validateInteractionNPC(player, request.NPCUniqueID, false); err != nil {
		return h.handleSpecialtyEconomyError(session, model.ErrSpecialtyNotBuyer)
	}
	npcObject, err := h.objects.GetObjectByUniqueId(request.NPCUniqueID)
	if err != nil || npcObject == nil || npcObject.GetRefObjectID() == 0 {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	playerPosition, npcPosition := player.GetNavmeshPosition(), npcObject.GetNavmeshPosition()
	if playerPosition.Region.ID != npcPosition.Region.ID || navmeshv2.HorizontalDistance(playerPosition, npcPosition) > 1000 {
		return h.handleSpecialtyEconomyError(session, model.ErrSpecialtyCartTooFar)
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}
	sessionID := buybackSessionID(session)
	result, err := repository.SellCOS(ctx, int64(player.ID), session.UserContext.UserID, npcObject.GetRefObjectID(), transport.PersistentID, request.Source, request.Amount, h.buybackSession(player.ID) != sessionID)
	if err != nil {
		return h.handleSpecialtyEconomyError(session, err)
	}
	h.setBuybackSession(player.ID, sessionID)
	transport.ReplaceInventory(result.COSInventory)
	transport.SetTradeScale(result.TradeScale)
	player.Mutex.Lock()
	player.Gold = result.Gold
	player.Mutex.Unlock()
	if result.JobAdvanced {
		player.SetJob(model.PlayerJob{Type: result.Job.Type, Alias: result.Job.Alias, Level: result.Job.Level, Experience: result.Job.Experience})
	}
	h.postTriJobReward(ctx, result)
	return h.sendPackets(session, buildCOSSellResultPackets(request, result)...)
}

func buildCOSSellResultPackets(request vsro188.COSSellRequest, result model.EconomyResult) []network.Packet {
	packets := []network.Packet{vsro188.BuildCOSSellSuccess(request, result.BuybackSlot), buildGoldUpdate(result.Gold)}
	if result.Specialty {
		packets = append(packets, mustTradeScalePacket(result.TradeScale))
	}
	if result.SafeTradeReset {
		packet, err := (vsro188.JobSafeTradeCodec{}).Encode(vsro188.SafeTradeUpdate{Code: vsro188.JobSafeTradeCodeTransition40})
		if err != nil {
			panic(err)
		}
		packets = append(packets, packet)
	}
	if result.JobAdvanced {
		packet, err := (vsro188.JobUpdateCodec{}).Encode(vsro188.JobState{Type: result.Job.Type, Level: result.Job.Level, Experience: uint32(min(result.Job.Experience, math.MaxUint32))})
		if err != nil {
			panic(err)
		}
		packets = append(packets, packet)
	}
	return packets
}

func (h *InventoryHandler) postTriJobReward(ctx context.Context, result model.EconomyResult) {
	if !result.Specialty || result.TriJobReward == 0 {
		return
	}
	if rewards, ok := h.economy.(interface {
		AddTriJobReward(context.Context, byte, uint64) error
	}); ok {
		if err := rewards.AddTriJobReward(ctx, result.TriJobType, result.TriJobReward); err != nil {
			logrus.WithError(err).Warn("failed to post independent tri-job reward update")
		}
	}
}

func (h *InventoryHandler) handleSpecialtyEconomyError(session *server.Session, err error) error {
	var code uint16
	switch {
	case errors.Is(err, model.ErrSpecialtyTraderOnly):
		code = vsro188.SpecialtyErrorTraderOnly
	case errors.Is(err, model.ErrSpecialtyNoCart):
		code = vsro188.SpecialtyErrorNoCart
	case errors.Is(err, model.ErrSpecialtyNotBuyer):
		code = vsro188.SpecialtyErrorNotBuyer
	case errors.Is(err, model.ErrSpecialtyCartTooFar):
		code = vsro188.SpecialtyErrorCartTooFar
	case errors.Is(err, model.ErrSpecialtyStolen):
		code = vsro188.SpecialtyErrorStolen
	default:
		return h.handleEconomyError(session, err)
	}
	return h.sendPacket(session, vsro188.BuildItemOperationFailure(code))
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
			item, exists := player.GetInventory().Items[source]
			if !exists || isActiveCOSItem(item) {
				return h.rejectStorageMutation(ctx, session, model.ErrEconomyConflict)
			}
			result, err = h.economy.Deposit(ctx, int64(player.ID), session.UserContext.UserID, source, destination)
		} else {
			result, err = h.economy.Withdraw(ctx, int64(player.ID), session.UserContext.UserID, source, destination)
		}
		if err == nil {
			player.CommitEconomy(result.Gold, result.Inventory)
			actualSource, actualDestination := source, destination
			if len(result.DestinationSlots) == 1 {
				if operation == PutItemIntoStorageOperation {
					actualDestination = result.DestinationSlots[0]
				} else {
					actualSource = result.SourceSlot
				}
			}
			return h.sendPacket(session, buildStorageTransferSuccess(operation, actualSource, actualDestination))
		}
	}
	return h.rejectStorageMutation(ctx, session, err)
}

func isActiveCOSItem(item model.Item) bool {
	return item.COS.Present && (item.COS.State == model.COSItemSummoned || item.COS.State == model.COSItemActive)
}

// rejectStorageMutation repairs the stock client's optimistic storage move.
// A bare B034 failure leaves some vSRO 1.188 clients with a stale page model;
// the following authoritative stream replaces that model with committed SQL
// state before another drag can reuse the wrong slot.
func (h *InventoryHandler) rejectStorageMutation(ctx context.Context, session *server.Session, cause error) error {
	if errors.Is(cause, model.ErrEconomyOutcomeUnknown) {
		return cause
	}
	logrus.WithError(cause).Warn("rejected storage operation; resynchronizing storage snapshot")
	if err := h.sendPacket(session, buildUnsupportedItemOperationResponse()); err != nil {
		return err
	}
	if h.economy == nil || session == nil {
		return nil
	}
	snapshot, err := h.economy.LoadStorage(ctx, session.UserContext.UserID)
	if err != nil {
		logrus.WithError(err).Warn("failed to load storage snapshot after rejected operation")
		return nil
	}
	for _, response := range buildStorageResponses(snapshot) {
		if err = h.sendPacket(session, response); err != nil {
			return err
		}
	}
	return nil
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
	plan, err := player.Inventory.CommitRelocationAmount(player, request.sourceSlot, request.targetSlot, request.amount, func(plan model.InventoryRelocationPlan) error {
		return h.relocator.Relocate(ctx, int64(session.UserContext.CharacterID), plan)
	})
	if err != nil {
		return nil, model.InventoryRelocationPlan{}, nil, err
	}
	if plan.Action == model.InventoryRelocationSplit {
		snapshots, ok := h.relocator.(interface {
			InventorySnapshot(context.Context, int64) (model.Inventory, error)
		})
		if !ok {
			return nil, model.InventoryRelocationPlan{}, nil, model.ErrInventoryRelocationOutcomeUnknown
		}
		inventory, loadErr := snapshots.InventorySnapshot(ctx, int64(session.UserContext.CharacterID))
		if loadErr != nil {
			return nil, model.InventoryRelocationPlan{}, nil, errors.Join(model.ErrInventoryRelocationOutcomeUnknown, loadErr)
		}
		player.CommitInventory(inventory)
	}
	var removedBuffs []uint32
	if plan.Action == model.InventoryRelocationEquip || plan.Action == model.InventoryRelocationUnequip {
		player.RecalculatePassiveSkillModifiers(h.referenceData)
		removedBuffs = player.RemoveInvalidFireShieldBuffs(h.referenceData)
	}
	return player, plan, removedBuffs, nil
}

func (h *InventoryHandler) rejectItemOperation2C(session *server.Session) error {
	// SR_GameServer 0x004D8BF0 and 0x004F76E0 reject discriminators above
	// 0x2B. Preserve an explicit response for 0x2C without assigning it an
	// invented sell lifecycle.
	return h.sendPacket(session, buildUnsupportedItemOperationResponse())
}

func mustTradeScalePacket(scale byte) network.Packet {
	packet, err := (vsro188.JobTradeScaleCodec{}).Encode(scale)
	if err != nil {
		return network.Packet{}
	}
	return packet
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
