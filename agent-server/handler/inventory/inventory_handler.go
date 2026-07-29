package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/sirupsen/logrus"
)

var (
	ErrInventoryHandlerPlayerLookupUnavailable = errors.New("inventory player lookup is unavailable")
	ErrInventoryHandlerSessionIdentityInvalid  = errors.New("inventory session character identity is invalid")
	ErrInventoryHandlerSessionPlayerMismatch   = errors.New("inventory session does not own the resolved player")
)

// PlayerLookup is the narrow world-facing dependency needed to resolve a
// packet's authenticated runtime owner. It deliberately accepts only a
// UniqueID lookup; character names are mutable display data and must not
// authorize inventory mutations.
type PlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type InventoryHandler struct {
	players PlayerLookup
	objects interface {
		GetObjectByUniqueId(uint32) (model.ISRObject, error)
	}
	relocator model.InventoryRelocator
	economy   model.EconomyRepository
	guild     model.GuildStorageRepository
	exchange  ExchangeOperations
	send      func(*server.Session, network.Packet) error
}

type ExchangeOperations interface {
	Busy(characterID int) bool
	HandleInventoryOperation(context.Context, *server.Session, *model.Player, byte, *network.Packet) error
}

type ItemInventoryOperation byte

const (
	MoveItemOperation ItemInventoryOperation = iota
	_
	PutItemIntoStorageOperation
	TakeItemFromStorageOperation
	PutItemIntoExchangeWindowOperation
	TakeItemFromExchangeWindowOperation // 5
	_
	DropItemOperation
	BuyItemOperation
	SellItemOperation
	DropGoldOperation // 10
	_
	_
	_
	_
	_ // 15
	_
	_
	BuyItemFromMallOperation
	BuySpecialtyGoodsOperation
	SellSpecialtyGoodsOperation // 20
	_
	_
	EquipAvatarItemOperation
	UnequipAvatarItemOperation
	_ // 25
	PutItemIntoPetInventoryOperation
	TakeItemFromPetInventoryOperation
	_
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
	RegisterInventoryHandlerWithGuildStorage(target, relocator, exchange, economyRepository, nil)
}

func RegisterInventoryHandlerWithGuildStorage(target *server.Server, relocator model.InventoryRelocator, exchange ExchangeOperations, economy model.EconomyRepository, guild model.GuildStorageRepository) {
	if target == nil {
		return
	}
	handler := NewInventoryHandler(service.GetWorldServiceInstance(), relocator, economy)
	handler.exchange = exchange
	handler.guild = guild
	target.RegisterHandler(opcode.ItemOperationRequest, handler.handlePacket)
	handler.registerRepairHandler(target)
	RegisterStorageHandler(target, economy)
	RegisterGuildStorageHandler(target, guild)
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
	switch ItemInventoryOperation(operationType) {
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
		return h.handleBuy(ctx, session, &packet)
	case SellItemOperation:
		return h.handleSell(ctx, session, &packet)
	case PutItemIntoStorageOperation, TakeItemFromStorageOperation, 1:
		return h.handleStorageMutation(ctx, session, ItemInventoryOperation(operationType), &packet)
	case GuildStorageMoveOperation, GuildStorageDepositItemOperation, GuildStorageWithdrawItemOperation,
		GuildStorageDepositGoldOperation, GuildStorageWithdrawGoldOperation:
		return h.handleGuildStorageMutation(ctx, session, ItemInventoryOperation(operationType), &packet)
	case MoveItemOperation:
		// Continue through the established relocation kernel below.
	default:
		return h.sendPacket(session, buildUnsupportedItemOperationResponse())
	}

	request, err := parseMoveItemRequest(&packet)
	if err != nil {
		return err
	}
	player, plan, err := h.relocate(ctx, session, request)
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
	return sendMoveItemSuccess(session, request)
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
		return h.handleEconomyError(session, err)
	}
	player.CommitEconomy(result.Gold, result.Inventory)
	return h.sendPacket(session, buildBuySuccess(tab, slot, amount, result.DestinationSlots, result.Item))
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
	result, err := h.economy.Sell(ctx, int64(player.ID), session.UserContext.UserID, source, amount)
	if err != nil {
		return h.handleEconomyError(session, err)
	}
	player.CommitEconomy(result.Gold, result.Inventory)
	return h.sendPacket(session, buildSellSuccess(source, amount, npcUID))
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

func buildBuySuccess(tab, slot byte, amount uint16, destinations []byte, item model.Item) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(1)
	p.WriteByte(byte(BuyItemOperation))
	p.WriteByte(tab)
	p.WriteByte(slot)
	p.WriteByte(byte(len(destinations)))
	p.WriteBytes(destinations)
	p.WriteUInt16(amount)
	model.WriteRentInfo(&p, item)
	return p
}
func buildSellSuccess(source byte, amount uint16, npcUID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(1)
	p.WriteByte(byte(SellItemOperation))
	p.WriteByte(source)
	p.WriteUInt16(amount)
	p.WriteUInt32(npcUID)
	p.WriteByte(0xFF)
	return p
}
func buildStorageTransferSuccess(operation ItemInventoryOperation, source, destination byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(1)
	p.WriteByte(byte(operation))
	p.WriteByte(source)
	p.WriteByte(destination)
	return p
}
func buildStorageMoveSuccess(operation ItemInventoryOperation, source, destination byte, amount uint16) network.Packet {
	p := buildStorageTransferSuccess(operation, source, destination)
	p.WriteUInt16(amount)
	return p
}

func buildGuildStorageTransferSuccess(operation ItemInventoryOperation, source, destination byte) network.Packet {
	return buildStorageTransferSuccess(operation, source, destination)
}

func buildGuildStorageMoveSuccess(operation ItemInventoryOperation, source, destination byte, amount uint16) network.Packet {
	return buildStorageMoveSuccess(operation, source, destination, amount)
}

func buildGuildStorageGoldSuccess(operation ItemInventoryOperation, amount uint64) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(1)
	p.WriteByte(byte(operation))
	p.WriteUInt64(amount)
	return p
}

func buildUnsupportedItemOperationResponse() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	packet.WriteByte(2)   // operation failed
	packet.WriteUInt16(0) // generic failure code
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

// relocate resolves the player exclusively from session-owned identity and
// calls the per-inventory commit boundary. In particular, it never uses the
// mutable CharName field that the legacy handler used as its authority key.
func (h *InventoryHandler) relocate(ctx context.Context, session *server.Session, request moveItemRequest) (*model.Player, model.InventoryRelocationPlan, error) {
	if h == nil || h.relocator == nil {
		return nil, model.InventoryRelocationPlan{}, model.ErrInventoryRelocationRepositoryUnavailable
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return nil, model.InventoryRelocationPlan{}, err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return nil, model.InventoryRelocationPlan{}, model.ErrInventoryRelocationConflict
	}
	plan, err := player.Inventory.CommitRelocation(player, request.sourceSlot, request.targetSlot, func(plan model.InventoryRelocationPlan) error {
		return h.relocator.Relocate(ctx, int64(session.UserContext.CharacterID), plan)
	})
	if err != nil {
		return nil, model.InventoryRelocationPlan{}, err
	}
	return player, plan, nil
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
		packet := network.EmptyPacket()
		packet.MessageID = opcode.EntityEquipItem
		if err := packet.WriteUInt32(player.GetUniqueID()); err != nil {
			return err
		}
		if err := packet.WriteByte(plan.TargetSlot); err != nil {
			return err
		}
		if err := packet.WriteUInt32(plan.SourceReferenceID); err != nil {
			return err
		}
		if err := packet.WriteBool(plan.SourceOneHanded); err != nil {
			return err
		}
		if err := session.Send(packet); err != nil {
			return err
		}
		player.SendStatsUpdate()
	case model.InventoryRelocationUnequip:
		packet := network.EmptyPacket()
		packet.MessageID = opcode.EntityUnequipItem
		if err := packet.WriteUInt32(player.GetUniqueID()); err != nil {
			return err
		}
		if err := packet.WriteByte(plan.SourceSlot); err != nil {
			return err
		}
		if err := packet.WriteUInt32(plan.SourceReferenceID); err != nil {
			return err
		}
		if err := session.Send(packet); err != nil {
			return err
		}
		player.SendStatsUpdate()
	}
	return nil
}

func sendMoveItemSuccess(session *server.Session, request moveItemRequest) error {
	if session == nil {
		return ErrInventoryHandlerSessionIdentityInvalid
	}

	// Preserve the legacy success payload exactly. Invalid actions return no
	// new failure packet until a sanitized capture establishes those bytes.
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemOperationResponse
	if err := packet.WriteBool(true); err != nil {
		return err
	}
	if err := packet.WriteByte(byte(MoveItemOperation)); err != nil {
		return err
	}
	if err := packet.WriteByte(request.sourceSlot); err != nil {
		return err
	}
	if err := packet.WriteByte(request.targetSlot); err != nil {
		return err
	}
	if err := packet.WriteUInt16(request.amount); err != nil {
		return err
	}
	if err := packet.WriteByte(0); err != nil {
		return err
	}
	return session.Send(packet)
}
