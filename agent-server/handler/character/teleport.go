package character

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/handler/lobby"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

const normalTeleportType = byte(2)

type teleportWorld interface {
	selectEntityWorld
	GetRegion(int16) (navmeshv2.RtNavmeshTerrain, error)
	StopMovingCharacter(model.ICharacter, navmeshv2.RtNavmeshPosition, string, bool) bool
	FlushPlayerPosition(uint32) error
	ResetPlayerVisibilityForTeleport(*model.Player)
}

type TeleportHandler struct {
	world      teleportWorld
	repository model.TeleportRepository
	send       func(*server.Session, network.Packet) error
	bootstrap  func(*server.Session, *model.Player) error
	mu         sync.Mutex
	pending    map[*server.Session]*model.Player
}

func NewTeleportHandler(world teleportWorld, repository model.TeleportRepository) *TeleportHandler {
	return &TeleportHandler{world: world, repository: repository, pending: make(map[*server.Session]*model.Player)}
}

func RegisterTeleportHandler(target *server.Server, repository model.TeleportRepository) *TeleportHandler {
	if target == nil {
		return nil
	}
	handler := NewTeleportHandler(service.GetWorldServiceInstance(), repository)
	target.RegisterHandler(opcode.TeleportDesignateRequest, handler.handleDesignate)
	target.RegisterHandler(opcode.TeleportUseRequest, handler.handleUse)
	target.RegisterHandler(opcode.TeleportCancelRequest, handler.handleCancel)
	target.RegisterHandler(opcode.TeleportReadyResponse, handler.handleReady)
	return handler
}

// handleDesignate implements the stock 0x7059 bind-point request. The wire
// payload is exactly the selected world-object UID; the response carries only
// the one-byte success/failure status recovered from the GameServer/client
// family. The catalog write is intentionally separate from teleport use:
// designating a gate does not charge gold or move the player.
func (h *TeleportHandler) handleDesignate(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 4 {
		return fmt.Errorf("teleport designation payload length %d, want 4", packet.Remaining())
	}
	npcUID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read teleport designation target: %w", err)
	}
	if h == nil {
		return fmt.Errorf("teleport designation handler is unavailable")
	}
	if h.world == nil || h.repository == nil {
		return h.rejectDesignation(session, 1)
	}
	designation, ok := h.repository.(model.TeleportDesignationRepository)
	if !ok {
		return h.rejectDesignation(session, 1)
	}
	player, err := resolveSelectingPlayer(h.world, session)
	if err != nil {
		return err
	}
	interaction, ok := npcInteractions.Get(player.GetUniqueID())
	if !ok || interaction.NPCUniqueID != npcUID || interaction.Action != teleportTaskUse {
		return h.rejectDesignation(session, 1)
	}
	if err = validateNPCSelection(h.world, player, npcUID); err != nil {
		return h.rejectDesignation(session, 1)
	}
	object, err := h.world.GetObjectByUniqueId(npcUID)
	if err != nil || object == nil {
		return h.rejectDesignation(session, 1)
	}
	// The stock selection data advertises designation only for a teleport
	// building. Ferry NPCs share the use/link path but expose no bind-point
	// task, so a forged 0x7059 for a ferry must not change RETURN_TELEPORT_ID.
	if gate, ok := object.(*model.TeleportGate); !ok || !model.IsTeleportGateObject(gate) {
		return h.rejectDesignation(session, 1)
	}
	if _, err = designation.Designate(ctx, int64(player.ID), session.UserContext.UserID, interaction.NPCRefObjectID); err != nil {
		if errors.Is(err, model.ErrTeleportOutcomeUnknown) {
			return fmt.Errorf("teleport designation outcome is unknown: %w", err)
		}
		return h.rejectDesignation(session, 1)
	}
	return h.sendPacket(session, buildTeleportDesignationSuccess())
}

func (h *TeleportHandler) handleCancel(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 {
		return fmt.Errorf("teleport cancel contains %d trailing bytes", packet.Remaining())
	}
	if h == nil || session == nil || session.LifecyclePhase() != server.SessionCharacterLoading {
		return fmt.Errorf("teleport cancel has no active cast")
	}
	h.mu.Lock()
	_, alreadyCommitted := h.pending[session]
	h.mu.Unlock()
	if alreadyCommitted {
		return fmt.Errorf("teleport cancel arrived after teleport commit")
	}
	session.AbortTeleportLoading()
	return nil
}

// PrepareItemReturn reserves the same loading lifecycle used by gate
// teleports, then drains the last movement checkpoint before the item-use
// transaction writes the bound-city position.
func (h *TeleportHandler) PrepareItemReturn(session *server.Session, player *model.Player) error {
	if h == nil || h.world == nil || session == nil || player == nil || player.Session != session {
		return fmt.Errorf("return-scroll teleport is unavailable")
	}
	h.mu.Lock()
	_, pending := h.pending[session]
	h.mu.Unlock()
	if pending || !session.BeginTeleportLoading() {
		return fmt.Errorf("return-scroll teleport could not begin while session is %s", session.LifecyclePhase())
	}
	h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "return-scroll-prepare", false)
	if err := h.world.FlushPlayerPosition(player.GetUniqueID()); err != nil {
		session.AbortTeleportLoading()
		return fmt.Errorf("flush pre-return-scroll position: %w", err)
	}
	return nil
}

func (h *TeleportHandler) AbortItemReturn(session *server.Session) {
	if session != nil {
		session.AbortTeleportLoading()
	}
}

// CompleteItemReturn stages the committed destination and starts the stock
// GAME_RESET/ready/bootstrap exchange. The item-use success packet is sent by
// the inventory handler before this method is called.
func (h *TeleportHandler) CompleteItemReturn(session *server.Session, player *model.Player, destination model.TeleportDestination) error {
	if h == nil || h.world == nil || session == nil || player == nil || session.LifecyclePhase() != server.SessionCharacterLoading {
		return fmt.Errorf("return-scroll teleport was not prepared")
	}
	position, err := h.resolveDestination(destination)
	if err != nil {
		return fmt.Errorf("resolve return-scroll world position: %w", err)
	}
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, position, "return-scroll", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	if h.pending[session] != nil {
		h.mu.Unlock()
		return fmt.Errorf("return-scroll teleport is already pending")
	}
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportReadyRequest(destination.Region)); err != nil {
		h.mu.Lock()
		delete(h.pending, session)
		h.mu.Unlock()
		return err
	}
	return nil
}

func (h *TeleportHandler) handleUse(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 9 {
		return fmt.Errorf("teleport request payload length %d, want 9", packet.Remaining())
	}
	npcUID, e1 := packet.ReadUInt32()
	teleportType, e2 := packet.ReadByte()
	targetID, e3 := packet.ReadUInt32()
	if err := errors.Join(e1, e2, e3); err != nil {
		return fmt.Errorf("read teleport request: %w", err)
	}
	if teleportType != normalTeleportType || targetID == 0 {
		return h.reject(session, 0x0001)
	}
	if h.world == nil || h.repository == nil {
		return h.reject(session, 0x0001)
	}
	h.mu.Lock()
	_, alreadyPending := h.pending[session]
	h.mu.Unlock()
	if alreadyPending {
		return h.reject(session, 0x0001)
	}
	player, err := resolveSelectingPlayer(h.world, session)
	if err != nil {
		return err
	}
	interaction, ok := npcInteractions.Get(player.GetUniqueID())
	if !ok || interaction.NPCUniqueID != npcUID {
		return h.reject(session, 0x0001)
	}
	if err = validateNPCSelection(h.world, player, npcUID); err != nil {
		logrus.WithError(err).Debug("rejected teleport from invalid NPC interaction")
		return h.reject(session, 0x0001)
	}
	object, err := h.world.GetObjectByUniqueId(npcUID)
	if err != nil || object == nil || object.GetRefObjectID() != interaction.NPCRefObjectID {
		return h.reject(session, 0x0001)
	}

	quote, err := h.repository.Quote(ctx, interaction.NPCRefObjectID, targetID)
	if err != nil {
		return h.reject(session, teleportErrorCode(err))
	}
	destination, err := h.resolveDestination(quote)
	if err != nil {
		logrus.WithError(err).Warn("rejected teleport to unavailable world position")
		return h.reject(session, 0x0001)
	}
	if !session.BeginTeleportLoading() {
		return fmt.Errorf("teleport could not begin loading while session is %s", session.LifecyclePhase())
	}
	teleportLoading := true
	defer func() {
		if teleportLoading {
			session.AbortTeleportLoading()
		}
	}()
	// Cancel movement and drain the last pre-teleport checkpoint before the
	// gold+position transaction. Otherwise an older queued movement snapshot
	// could commit after this transaction and move the character back.
	h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "teleport-prepare", false)
	if err = h.world.FlushPlayerPosition(player.GetUniqueID()); err != nil {
		return fmt.Errorf("flush pre-teleport position: %w", err)
	}
	result, err := h.repository.Commit(ctx, int64(player.ID), session.UserContext.UserID, interaction.NPCRefObjectID, targetID)
	if err != nil {
		if errors.Is(err, model.ErrTeleportOutcomeUnknown) {
			return fmt.Errorf("teleport commit outcome is unknown: %w", err)
		}
		return h.reject(session, teleportErrorCode(err))
	}
	if result.Destination != quote {
		return fmt.Errorf("teleport catalog changed during transaction")
	}

	player.SetGold(result.Gold)
	// GameReady activates only staged players. Re-enter that state before the
	// client and server both discard the old area's visibility state. The reset
	// must not queue ordinary despawns: 0x34B5 has already cleared those UIDs
	// from the client's global object table.
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, destination, "teleport", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportSuccess()); err != nil {
		return err
	}
	ready := buildTeleportReadyRequest(quote.Region)
	if err = h.sendPacket(session, ready); err != nil {
		return err
	}
	teleportLoading = false
	return nil
}

func buildTeleportReadyRequest(regionID int16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportReadyRequest
	// SERVER_AGENT_GAME_RESET carries the destination region. The stock client
	// reads this uint16 before choosing its loading screen and resetting world
	// state, even though clientless bot handlers commonly ignore the payload.
	packet.WriteUInt16(uint16(regionID))
	return packet
}

func (h *TeleportHandler) handleReady(_ context.Context, session *server.Session, packet network.Packet) error {
	if _, err := (vsro188.TeleportReadyResponseCodec{}).Decode(packet); err != nil {
		return err
	}
	h.mu.Lock()
	player := h.pending[session]
	delete(h.pending, session)
	h.mu.Unlock()
	if player == nil || session == nil || player.Session != session || player.GetUniqueID() != session.UserContext.UniqueID {
		return fmt.Errorf("teleport ready response has no pending teleport")
	}
	if h.bootstrap != nil {
		return h.bootstrap(session, player)
	}
	return lobby.SendCharacterBootstrap(session, player)
}

func (h *TeleportHandler) resolveDestination(destination model.TeleportDestination) (navmeshv2.RtNavmeshPosition, error) {
	region, err := h.world.GetRegion(destination.Region)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, err
	}
	offset := math32.NewVector3(destination.X, destination.Y, destination.Z)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, err
	}
	return navmeshv2.RtNavmeshPosition{Offset: offset, Heading: 0, Region: region.Region, Cell: &cell}, nil
}

func (h *TeleportHandler) reject(session *server.Session, code uint16) error {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportUseResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return h.sendPacket(session, packet)
}

func (h *TeleportHandler) rejectDesignation(session *server.Session, code uint16) error {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportDesignateResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return h.sendPacket(session, packet)
}

func (h *TeleportHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func buildTeleportSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportUseResponse
	packet.WriteByte(1)
	return packet
}

func buildTeleportDesignationSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportDesignateResponse
	packet.WriteByte(1)
	return packet
}

func teleportErrorCode(err error) uint16 {
	if errors.Is(err, model.ErrTeleportInsufficientGold) {
		return 0x0002
	}
	return 0x0001
}
