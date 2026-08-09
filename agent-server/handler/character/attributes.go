package character

import (
	"context"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type attributeWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type AttributeHandler struct {
	mu         sync.Mutex
	world      attributeWorld
	repository model.CharacterAttributeRepository
	send       func(*server.Session, network.Packet) error
}

func RegisterAttributeHandlers(target *server.Server, repository model.CharacterAttributeRepository) error {
	if target == nil || repository == nil {
		return model.ErrCharacterAttributeRepositoryUnavailable
	}
	handler := &AttributeHandler{world: service.GetWorldServiceInstance(), repository: repository, send: sendAttributePacket}
	target.RegisterHandler(opcode.CharacterIncreaseStrengthRequest, handler.increaseStrength)
	target.RegisterHandler(opcode.CharacterIncreaseIntellectRequest, handler.increaseIntellect)
	return nil
}

func (h *AttributeHandler) increaseStrength(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.increase(ctx, session, packet, model.CharacterStrength, opcode.CharacterIncreaseStrengthResponse)
}

func (h *AttributeHandler) increaseIntellect(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.increase(ctx, session, packet, model.CharacterIntellect, opcode.CharacterIncreaseIntellectResponse)
}

func (h *AttributeHandler) increase(ctx context.Context, session *server.Session, packet network.Packet, attribute model.CharacterAttribute, responseOpcode uint16) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if packet.Remaining() != 0 {
		return fmt.Errorf("character attribute payload length %d, want 0", packet.Remaining())
	}
	if h == nil || h.world == nil || session == nil || session.UserContext.UniqueID == 0 {
		return nil
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID <= 0 {
		return nil
	}
	spent, err := h.repository.SpendStatPoint(ctx, player.ID, attribute)
	if err != nil {
		return err
	}
	// The original server sends no failure response when the balance is zero.
	if !spent {
		return nil
	}
	if !player.CommitStatPoint(attribute) {
		return fmt.Errorf("persisted character attribute but in-memory stat balance is stale")
	}
	// The original inc_str/inc_int path recalculates and emits 0x303D before
	// acknowledging the spent point with B050/B051.
	if err := h.sendPacket(session, player.BuildStatsUpdatePacket()); err != nil {
		return err
	}
	response := network.EmptyPacket()
	response.MessageID = responseOpcode
	response.WriteByte(1)
	return h.sendPacket(session, response)
}

func (h *AttributeHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return sendAttributePacket(session, packet)
}

func sendAttributePacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return fmt.Errorf("character attribute session is unavailable")
	}
	return session.Send(packet)
}
