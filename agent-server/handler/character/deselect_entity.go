package character

import (
	"context"
	"fmt"

	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

// DeselectEntityHandler completes the stock client's NPC-dialog lifecycle.
// The client will not close shop, storage, teleport, or quest windows until it
// receives B04B for its 704B request.
type DeselectEntityHandler struct {
	world selectEntityWorld
	send  func(*server.Session, network.Packet) error
}

func NewDeselectEntityHandler(world selectEntityWorld) *DeselectEntityHandler {
	return &DeselectEntityHandler{world: world}
}

func RegisterDeselectEntityHandler(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewDeselectEntityHandler(service.GetWorldServiceInstance())
	target.RegisterHandler(opcode.EntityDeselectRequest, handler.handlePacket)
}

func (h *DeselectEntityHandler) handlePacket(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 4 {
		return fmt.Errorf("entity deselect payload length %d, want 4", packet.Remaining())
	}
	targetUID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read entity deselect target: %w", err)
	}
	world := h.world
	if world == nil {
		world = service.GetWorldServiceInstance()
	}
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}

	// Only clear an interaction when the client closes the same NPC. A stale
	// close packet must not erase a newer dialog that superseded it.
	if interaction, ok := npcInteractions.Get(player.GetUniqueID()); ok && interaction.NPCUniqueID == targetUID {
		npcInteractions.Clear(player.GetUniqueID())
	}

	response := buildEntityDeselectResponse()
	if h.send != nil {
		return h.send(session, response)
	}
	return session.Send(response)
}

func buildEntityDeselectResponse() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityDeselectResponse
	packet.WriteByte(1)
	return packet
}
