package character

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

var (
	ErrSelectEntitySessionIdentityInvalid = errors.New("entity selection session identity is invalid")
	ErrSelectEntitySessionPlayerLookup    = errors.New("entity selection player lookup failed")
	ErrSelectEntitySessionPlayerMismatch  = errors.New("entity selection session does not own resolved player")
	ErrSelectEntityTargetNotFound         = errors.New("entity selection target does not exist")
	ErrSelectEntityTargetNotKnown         = errors.New("entity selection target is not known to player")
	ErrSelectEntityTargetUnsupported      = errors.New("entity selection target type is unsupported")
	ErrSelectEntityTargetPositionInvalid  = errors.New("entity selection target position is invalid")
	ErrSelectEntityTargetOutOfRange       = errors.New("entity selection target is out of range")
)

// selectionKnownRange deliberately matches the existing world known-object
// visibility threshold. There is no captured NPC interaction-radius fixture
// yet, so the admission guard must not invent a narrower protocol rule.
const (
	selectionKnownRange   = float32(model.RegionHeight) / 3
	teleportTaskDesignate = byte(7)
	teleportTaskUse       = byte(8)
)

// selectEntityWorld is the bounded domain surface needed before a selection
// can be admitted. It contains no packet or socket operations, which keeps
// the validation testable and prevents this pre-capture path from inventing a
// response layout.
type selectEntityWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetObjectByUniqueId(uint32) (model.ISRObject, error)
	DistanceTo(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) float32
}

type SelectEntityHandler struct {
	// Preserve the legacy one-at-a-time processing for world lookups. The
	// world service still has a mixture of locked and unlocked accessors.
	mu    sync.Mutex
	world selectEntityWorld
	send  func(*server.Session, network.Packet) error
}

// NewSelectEntityHandler permits focused tests and future region-command
// adapters to supply the world boundary explicitly. Production registration
// supplies the singleton below.
func NewSelectEntityHandler(world selectEntityWorld) *SelectEntityHandler {
	return &SelectEntityHandler{world: world}
}

func RegisterSelectEntityHandler(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewSelectEntityHandler(service.GetWorldServiceInstance())
	target.RegisterHandler(opcode.EntitySelectRequest, handler.handlePacket)
}

func (h *SelectEntityHandler) handlePacket(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// The legacy path consumed exactly one entity UID. Without a capture that
	// establishes an extension layout, any short or trailing request is invalid
	// and must reach the framework as a handler error so the session closes.
	request, err := (vsro188.EntitySelectRequestCodec{}).Decode(packet)
	if err != nil {
		return err
	}
	uniqueID := request.UniqueID

	world := h.worldService()
	player, err := resolveSelectingPlayer(world, session)
	if err != nil {
		return err
	}
	if err := validateEntitySelection(world, player, uniqueID); err != nil {
		// A stale visible NPC is normal at a region boundary: the client can
		// select it in the same frame that the server moves the player to the
		// adjacent region. Reject the interaction without tearing down the game
		// session; only malformed packets and invalid session ownership above
		// remain protocol errors.
		logrus.Warnf("ignored entity selection player=%s uid=%d: %v", player.CharName, uniqueID, err)
		clearSelectedNPC(session)
		return h.sendPacket(session, buildEntitySelectionFailure())
	}
	object, err := world.GetObjectByUniqueId(uniqueID)
	if err != nil || object == nil {
		clearSelectedNPC(session)
		return h.sendPacket(session, buildEntitySelectionFailure())
	}
	session.UserContext.SelectedEntityUniqueID = object.GetUniqueID()
	if targetPlayer, ok := object.(*model.Player); ok && targetPlayer != nil && targetPlayer.GetTypeInfo().IsPlayerCharacter() {
		job := targetPlayer.GetJob()
		response, err := (vsro188.EntityPlayerSelectResponseCodec{}).Encode(vsro188.EntityPlayerSelectResponse{
			UniqueID:    targetPlayer.GetUniqueID(),
			JobType:     job.Type,
			JobLevel:    job.Level,
			HasJobLevel: job.Active(),
		})
		if err != nil {
			return err
		}
		return h.sendPacket(session, response)
	}
	if mob, ok := object.(*model.NPC); ok && mob != nil && mob.GetTypeInfo().IsNPCMob() {
		return h.sendPacket(session, buildMobSelectionResponse(uniqueID, mob.CurrentHitPoints()))
	}
	if portal, ok := object.(*model.DimensionHolePortal); ok && model.IsDimensionHolePortalObject(portal) {
		return h.sendPacket(session, buildDimensionHolePortalSelectionResponse(uniqueID))
	}
	if object.GetTypeInfo().IsNPCNpc() || model.IsTeleportGateObject(object) {
		session.UserContext.SelectedNPCUniqueID = object.GetUniqueID()
		session.UserContext.SelectedNPCRefObjID = object.GetRefObjectID()
		selectionData := model.NPCSelectionTalkData(object.GetName())
		if gate, ok := object.(*model.TeleportGate); ok && model.IsTeleportGateObject(gate) {
			// TeleportBuilding has its own B045 layout: task count followed directly
			// by task bytes and, only for a fortress enabled by siegefortress.txt,
			// its tax rate. The client rejects both a missing active tax and an
			// unexpected tax on an inactive fortress gate.
			selectionData = teleportGateSelectionData(gate)
		}
		// Catalog-backed TeleportGate objects are authoritative even when their
		// media code name uses a numbered suffix such as STORE_SD_GATE2.
		if model.IsTeleportGateObject(object) || model.IsTeleportServiceCodeName(object.GetName()) {
			npcInteractions.Record(player.GetUniqueID(), object.GetUniqueID(), object.GetRefObjectID(), teleportTaskUse)
		}
		logrus.Infof("NPC_SELECT player=%s uid=%d code=%s selectionData=%x", player.CharName, uniqueID, object.GetName(), selectionData)
		return h.sendPacket(session, buildNPCSelectionResponse(uniqueID, selectionData))
	}
	clearSelectedNPC(session)
	return h.sendPacket(session, buildEntitySelectionFailure())
}

func clearSelectedNPC(session *server.Session) {
	if session == nil {
		return
	}
	session.UserContext.SelectedEntityUniqueID = 0
	session.UserContext.SelectedNPCUniqueID = 0
	session.UserContext.SelectedNPCRefObjID = 0
}

func buildEntitySelectionFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySelectResponse
	p.WriteByte(0)
	return p
}

func buildDimensionHolePortalSelectionResponse(uniqueID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySelectResponse
	p.WriteByte(1)
	p.WriteUInt32(uniqueID)
	p.WriteBytes([]byte{1, npcTalkActionConversation})
	return p
}

func (h *SelectEntityHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func teleportGateSelectionData(gate *model.TeleportGate) []byte {
	data := []byte{2, teleportTaskDesignate, teleportTaskUse}
	if gate != nil && gate.TaxActive {
		data = append(data, byte(gate.TaxRate), byte(gate.TaxRate>>8))
	}
	return data
}

func buildMobSelectionResponse(uniqueID uint32, hitPoints int) network.Packet {
	if hitPoints < 0 {
		hitPoints = 0
	}
	if hitPoints > math.MaxInt32 {
		hitPoints = math.MaxInt32
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySelectResponse
	p.WriteByte(1) // selection accepted
	p.WriteUInt32(uniqueID)
	p.WriteBool(true)
	p.WriteUInt32(uint32(hitPoints))
	// This is selection metadata, not the mob rarity. The stock vSRO client
	// branches on the first byte and consumes the second byte only when it is
	// set. Sending 00 05 leaves an unexpected trailing byte and crashes the
	// client parser. Runtime rarity belongs exclusively to the spawn packet.
	p.WriteByte(0x01)
	p.WriteByte(0x05)
	return p
}

func buildNPCSelectionResponse(uniqueID uint32, selectionData []byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySelectResponse
	p.WriteByte(1) // selection accepted
	p.WriteUInt32(uniqueID)
	// Unlike the spawn mask, B045 carries object-specific selection data.
	p.WriteBytes(selectionData)
	return p
}

func (h *SelectEntityHandler) worldService() selectEntityWorld {
	if h != nil && h.world != nil {
		return h.world
	}
	return service.GetWorldServiceInstance()
}

func resolveSelectingPlayer(world selectEntityWorld, session *server.Session) (*model.Player, error) {
	if world == nil {
		return nil, ErrSelectEntitySessionPlayerLookup
	}
	if session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return nil, ErrSelectEntitySessionIdentityInvalid
	}

	player, err := world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSelectEntitySessionPlayerLookup, err)
	}
	if player == nil || player.GetUniqueID() != session.UserContext.UniqueID || player.ID != session.UserContext.CharacterID || player.Session != session {
		return nil, ErrSelectEntitySessionPlayerMismatch
	}
	return player, nil
}

func validateNPCSelection(world selectEntityWorld, player *model.Player, uniqueID uint32) error {
	return validateEntitySelection(world, player, uniqueID)
}

func validateEntitySelection(world selectEntityWorld, player *model.Player, uniqueID uint32) error {
	if world == nil || player == nil || uniqueID == 0 {
		return ErrSelectEntityTargetNotFound
	}

	target, err := world.GetObjectByUniqueId(uniqueID)
	if err != nil || target == nil {
		return ErrSelectEntityTargetNotFound
	}
	knownList := player.GetKnownObjectList()
	if knownList == nil {
		return ErrSelectEntityTargetNotKnown
	}
	// Read the list's detached UID-keyed snapshot rather than invoking a
	// method on the target merely to reject it. That keeps a malformed world
	// object from turning a client-selected unknown UID into a handler panic.
	knownTarget, known := knownList.GetKnownObjects()[uniqueID]
	if !known || knownTarget == nil {
		return ErrSelectEntityTargetNotKnown
	}

	// Players, normal NPCs, mobs, and catalog-backed teleport structures share
	// the same UID-based selection admission. Combat performs its own stricter
	// alive/range validation before accepting damage. COS, siege objects, self
	// selection, and malformed generic objects remain closed.
	switch typed := target.(type) {
	case *model.Player:
		if typed == nil || typed == player || !typed.GetTypeInfo().IsPlayerCharacter() {
			return ErrSelectEntityTargetUnsupported
		}
		knownPlayer, ok := knownTarget.(*model.Player)
		if !ok || knownPlayer != typed {
			return ErrSelectEntityTargetNotKnown
		}
	case *model.NPC:
		if typed == nil || !typed.GetTypeInfo().IsNPC() {
			return ErrSelectEntityTargetUnsupported
		}
		knownNPC, ok := knownTarget.(*model.NPC)
		if !ok || knownNPC != typed {
			return ErrSelectEntityTargetNotKnown
		}
	case *model.TeleportGate:
		if !model.IsTeleportGateObject(typed) {
			return ErrSelectEntityTargetUnsupported
		}
		knownGate, ok := knownTarget.(*model.TeleportGate)
		if !ok || knownGate != typed {
			return ErrSelectEntityTargetNotKnown
		}
	case *model.DimensionHolePortal:
		if !model.IsDimensionHolePortalObject(typed) {
			return ErrSelectEntityTargetUnsupported
		}
		knownPortal, ok := knownTarget.(*model.DimensionHolePortal)
		if !ok || knownPortal != typed {
			return ErrSelectEntityTargetNotKnown
		}
	default:
		return ErrSelectEntityTargetUnsupported
	}
	if target.GetUniqueID() != uniqueID {
		return ErrSelectEntityTargetNotFound
	}

	playerPosition := player.GetNavmeshPosition()
	targetPosition := target.GetNavmeshPosition()
	if playerPosition.Offset == nil || targetPosition.Offset == nil {
		return ErrSelectEntityTargetPositionInvalid
	}
	// Region IDs are invisible 1920-unit map partitions, not interaction
	// boundaries. A player can legitimately see and select a nearby object in
	// an adjacent region. The original server searches the surrounding region
	// set; the known-list identity check above and global-coordinate distance
	// below provide the equivalent admission boundary here.
	rangeLimit := selectionKnownRange
	if model.IsTeleportGateObject(target) || model.IsDimensionHolePortalObject(target) {
		rangeLimit = float32(model.RegionHeight)
	}
	distance := world.DistanceTo(playerPosition, targetPosition)
	if math.IsNaN(float64(distance)) || math.IsInf(float64(distance), 0) || distance < 0 || distance > rangeLimit {
		return fmt.Errorf("%w: distance=%g maximum=%g", ErrSelectEntityTargetOutOfRange, distance, rangeLimit)
	}
	return nil
}
