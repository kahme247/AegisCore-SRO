package character

import (
	"context"

	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
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
	request, err := (vsro188.EntityDeselectRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	targetUID := request.UniqueID
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
	if session.UserContext.SelectedEntityUniqueID == targetUID {
		session.UserContext.SelectedEntityUniqueID = 0
	}
	if session.UserContext.SelectedNPCUniqueID == targetUID {
		session.UserContext.SelectedNPCUniqueID = 0
		session.UserContext.SelectedNPCRefObjID = 0
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
