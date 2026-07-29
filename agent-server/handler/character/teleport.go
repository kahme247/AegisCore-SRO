package character

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/agent-server/handler/lobby"
	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

const normalTeleportType = byte(2)

type teleportWorld interface {
	selectEntityWorld
	GetRegion(int16) (navmeshv2.RtNavmeshTerrain, error)
	StopMovingCharacter(model.ICharacter, navmeshv2.RtNavmeshPosition, string, bool) bool
	FlushPlayerPosition(uint32) error
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

func RegisterTeleportHandler(target *server.Server, repository model.TeleportRepository) {
	if target == nil {
		return
	}
	handler := NewTeleportHandler(service.GetWorldServiceInstance(), repository)
	target.RegisterHandler(opcode.TeleportUseRequest, handler.handleUse)
	target.RegisterHandler(opcode.TeleportReadyResponse, handler.handleReady)
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
	h.world.StopMovingCharacter(player, destination, "teleport", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportSuccess()); err != nil {
		return err
	}
	ready := network.EmptyPacket()
	ready.MessageID = opcode.TeleportReadyRequest
	return h.sendPacket(session, ready)
}

func (h *TeleportHandler) handleReady(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 {
		return fmt.Errorf("teleport ready response contains %d trailing bytes", packet.Remaining())
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

func teleportErrorCode(err error) uint16 {
	if errors.Is(err, model.ErrTeleportInsufficientGold) {
		return 0x0002
	}
	return 0x0001
}
