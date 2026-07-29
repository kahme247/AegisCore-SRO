package character

import (
	"context"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/sirupsen/logrus"
)

// NPCTalkHandler admits the stock 0x7046 request only for a nearby, known
// normal NPC. It intentionally does not invent a 0x30D4 quest-dialogue packet:
// those packet layouts have not yet been captured and a guessed response can
// desynchronise the stock client. The handler is the authoritative boundary
// that the script and quest runtime will use once its protocol encoder lands.
type NPCTalkHandler struct {
	mu    sync.Mutex
	world selectEntityWorld
	send  func(*server.Session, network.Packet) error
}

// npcTalkActionConversation is the stock menu's "Talk to this person"
// action. Stores, teleporters and future quest controls use other non-zero
// action values, so request admission must not reject them merely because
// their service packet has not been implemented yet.
const npcTalkActionConversation = byte(2)

func NewNPCTalkHandler(world selectEntityWorld) *NPCTalkHandler {
	return &NPCTalkHandler{world: world}
}

func RegisterNPCTalkHandler(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewNPCTalkHandler(service.GetWorldServiceInstance())
	target.RegisterHandler(opcode.EntityTalkRequest, handler.handlePacket)
}

func (h *NPCTalkHandler) handlePacket(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	payloadLength := packet.Remaining()
	if payloadLength != 5 && payloadLength != 8 {
		return fmt.Errorf("NPC talk request payload length %d, want 5 or 8", payloadLength)
	}
	uniqueID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read NPC talk target: %w", err)
	}
	action, err := packet.ReadByte()
	if err != nil {
		return fmt.Errorf("read NPC talk action: %w", err)
	}
	if action == 0 {
		return fmt.Errorf("NPC talk action must not be zero")
	}
	if packet.Remaining() > 0 {
		trailing, err := packet.ReadBytes(packet.Remaining())
		if err != nil {
			return fmt.Errorf("read NPC talk trailing action bytes: %w", err)
		}
		for _, value := range trailing {
			if value != 0 {
				return fmt.Errorf("NPC talk request has non-zero trailing action data")
			}
		}
	}

	world := h.world
	if world == nil {
		world = service.GetWorldServiceInstance()
	}
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}
	if err := validateNPCSelection(world, player, uniqueID); err != nil {
		logrus.Warnf("ignored NPC talk player=%s uid=%d: %v", player.CharName, uniqueID, err)
		return nil
	}
	object, err := world.GetObjectByUniqueId(uniqueID)
	if err != nil {
		return nil
	}
	isNormalNPC := false
	if npc, ok := object.(*model.NPC); ok {
		isNormalNPC = npc != nil && npc.GetTypeInfo().IsNPCNpc()
	}
	isTeleportGate := model.IsTeleportGateObject(object)
	if !isNormalNPC && !isTeleportGate {
		logrus.Warnf("ignored non-normal NPC talk player=%s uid=%d", player.CharName, uniqueID)
		return nil
	}
	if isTeleportGate && action != 8 {
		logrus.Warnf("ignored teleport gate action player=%s uid=%d action=%d", player.CharName, uniqueID, action)
		return nil
	}

	npcInteractions.Record(player.GetUniqueID(), object.GetUniqueID(), object.GetRefObjectID(), action)
	logrus.Debugf("accepted NPC interaction player=%s characterID=%d uid=%d refObjID=%d action=%d", player.CharName, player.ID, uniqueID, object.GetRefObjectID(), action)
	response := buildNPCTalkResponse(action)
	if h.send != nil {
		return h.send(session, response)
	}
	return session.Send(response)
}

func buildNPCTalkResponse(action byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityTalkResponse
	p.WriteByte(1)
	p.WriteByte(action)
	return p
}
