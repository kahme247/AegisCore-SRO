package cos

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
	log "github.com/sirupsen/logrus"
)

type lifecycleService interface {
	Owned(*model.Player, uint32) (*model.COS, error)
	Unsummon(context.Context, *model.Player, uint32) error
	SetAttackTarget(*model.Player, uint32, uint32, uint32) error
	ClearAttackTarget(*model.Player, uint32) error
	MoveTo(*model.Player, uint32, navmeshv2.RtNavmeshPosition) error
	Follow(*model.Player, uint32) error
	Pickup(*model.Player, uint32, uint32) error
	Rename(context.Context, *model.Player, uint32, string) error
	UpdateSettings(context.Context, *model.Player, uint32, uint32) error
}

type LifecycleHandler struct {
	players playerLookup
	cos     lifecycleService
	send    func(*server.Session, network.Packet) error
}

func (h *LifecycleHandler) Service() *service.COSService {
	if h == nil {
		return nil
	}
	runtime, _ := h.cos.(*service.COSService)
	return runtime
}

func NewLifecycleHandler(players playerLookup, cosService lifecycleService) *LifecycleHandler {
	return &LifecycleHandler{players: players, cos: cosService}
}

func RegisterLifecycleHandlers(target *server.Server, repository model.COSRepository) *LifecycleHandler {
	if target == nil || repository == nil {
		return nil
	}
	world := service.GetWorldServiceInstance()
	h := NewLifecycleHandler(world, service.NewCOSService(repository, world))
	target.RegisterHandler(opcode.COSTerminationRequest, h.handleTermination)
	target.RegisterHandler(opcode.COSUnsummonRequest, h.handleUnsummon)
	target.RegisterHandler(opcode.COSCommandRequest, h.handleCommand)
	target.RegisterHandler(opcode.COSNameRequest, h.handleName)
	target.RegisterHandler(opcode.COSSettingsRequest, h.handleSettings)
	return h
}

func (h *LifecycleHandler) handleCommand(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.cos == nil {
		return h.sendCommandFailure(session, 0, 0, 3)
	}
	request, err := (vsro188.COSCommandRequestCodec{}).Decode(packet)
	if err != nil {
		return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	switch request.Command {
	case vsro188.COSCommandMove:
		if !request.Movement.HasDestination {
			return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
		}
		destination := request.Movement.Destination
		packetPosition := navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(destination.RegionID),
			Offset: math32.NewVector3(float32(destination.X), float32(destination.Y), float32(destination.Z)),
		}
		world, ok := h.players.(interface {
			NewPosFromGlobalCoordinates(*math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
		})
		if !ok {
			return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
		}
		target, resolveErr := world.NewPosFromGlobalCoordinates(packetPosition.GetGlobalCoordinates())
		if resolveErr != nil || h.cos.MoveTo(player, request.PetUniqueID, target) != nil {
			return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
		}
	case vsro188.COSCommandAttack:
		if attackErr := h.cos.SetAttackTarget(player, request.PetUniqueID, request.TargetUniqueID, 0); attackErr != nil {
			log.WithError(attackErr).Warnf("rejected COS attack pet=%d target=%d", request.PetUniqueID, request.TargetUniqueID)
			return nil
		}
	case vsro188.COSCommandFollow:
		if h.cos.Follow(player, request.PetUniqueID) != nil {
			return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
		}
	case vsro188.COSCommandCastSkill:
		if h.cos.SetAttackTarget(player, request.PetUniqueID, request.TargetUniqueID, request.SkillID) != nil {
			return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
		}
	case vsro188.COSCommandPickup:
		if h.cos.Pickup(player, request.PetUniqueID, request.TargetUniqueID) != nil {
			return h.sendCommandFailure(session, request.PetUniqueID, request.Command, 3)
		}
	case vsro188.COSCommandCharm:
		// The stock client uses command 11 for the growth-pet stance toggle.
		// Its successful path has no B0C5 acknowledgement; sending a failure
		// response makes the client enter an incompatible UI/error path.
		return nil
	}
	return nil
}

func (h *LifecycleHandler) handleName(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := (vsro188.COSNameRequestCodec{}).Decode(packet)
	if err != nil || h == nil || h.cos == nil {
		return h.sendResult(session, opcode.COSNameResponse, false)
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	if err = h.cos.Rename(ctx, player, request.PetUniqueID, request.Name); err != nil {
		return h.sendResult(session, opcode.COSNameResponse, false)
	}
	if err = h.sendResult(session, opcode.COSNameResponse, true); err != nil {
		return err
	}
	update := model.BuildCOSNameUpdate(request.PetUniqueID, request.Name)
	player.SendToSelfAndKnown(&update)
	return nil
}

func (h *LifecycleHandler) handleSettings(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := (vsro188.COSSettingsRequestCodec{}).Decode(packet)
	if err != nil || h == nil || h.cos == nil {
		return h.sendSettingsResult(session, request, false)
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	if err = h.cos.UpdateSettings(ctx, player, request.PetUniqueID, request.Settings); err != nil {
		return h.sendSettingsResult(session, request, false)
	}
	// The vSRO client handles this stance switch optimistically. Its B420
	// success handler asserts the runtime COS subtype and crashes when the
	// summoned growth-pet object is not classified as the expected gold-pet
	// subtype. Persist the accepted setting without entering that handler.
	return nil
}

func (h *LifecycleHandler) sendSettingsResult(session *server.Session, request vsro188.COSSettingsRequest, success bool) error {
	if session == nil {
		return fmt.Errorf("COS settings session is nil")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.COSSettingsResponse
	if success {
		p.WriteByte(1)
		p.WriteUInt32(request.PetUniqueID)
		p.WriteByte(request.Setting)
		p.WriteUInt32(request.Settings)
	} else {
		p.WriteByte(2)
		p.WriteUInt16(3)
	}
	if h != nil && h.send != nil {
		return h.send(session, p)
	}
	return session.Send(p)
}

func (h *LifecycleHandler) sendCommandFailure(session *server.Session, petUID uint32, command byte, code uint16) error {
	if session == nil {
		return errors.New("COS command session is nil")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.COSCommandResponse
	p.WriteByte(2)
	p.WriteByte(command)
	p.WriteUInt16(code)
	p.WriteUInt32(petUID)
	if h != nil && h.send != nil {
		return h.send(session, p)
	}
	return session.Send(p)
}

func (h *LifecycleHandler) handleTermination(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.handleUnsummonRequest(ctx, session, packet, opcode.COSTerminationResponse)
}

func (h *LifecycleHandler) handleUnsummon(ctx context.Context, session *server.Session, packet network.Packet) error {
	return h.handleUnsummonRequest(ctx, session, packet, opcode.COSUnsummonResponse)
}

func (h *LifecycleHandler) handleUnsummonRequest(ctx context.Context, session *server.Session, packet network.Packet, responseOpcode uint16) error {
	if h == nil || h.players == nil || h.cos == nil {
		return h.sendResult(session, responseOpcode, false)
	}
	request, err := (vsro188.COSUIDRequestCodec{}).Decode(packet)
	if err != nil {
		return h.sendResult(session, responseOpcode, false)
	}
	uid := request.UniqueID
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	owned, err := h.cos.Owned(player, uid)
	if err != nil {
		if errors.Is(err, model.ErrCOSNotOwned) || errors.Is(err, service.ErrCOSNotSummoned) {
			return h.sendResult(session, responseOpcode, false)
		}
		return err
	}
	slot := owned.InventorySlot
	transport := player.COSState.Snapshot()
	if transport.TransportUniqueID == uid && transport.Mounted {
		// Clear the rider attachment before despawning the transport. Despawning
		// first leaves the v188 player model attached to a missing mount in midair.
		unmounted := buildMountStateSuccess(player.GetUniqueID(), false, uid)
		player.SendToSelfAndKnown(&unmounted)
	}
	if err = h.cos.Unsummon(ctx, player, uid); err != nil {
		if errors.Is(err, model.ErrCOSNotOwned) || errors.Is(err, service.ErrCOSNotSummoned) || errors.Is(err, model.ErrCOSNotFound) {
			return h.sendResult(session, responseOpcode, false)
		}
		return err
	}
	if err = h.sendResult(session, responseOpcode, true); err != nil {
		return err
	}
	update := model.BuildCOSTerminatedUpdate(uid)
	player.SendToSelfAndKnown(&update)
	if owned.PersistentID != 0 {
		itemState := model.BuildCOSItemStateUpdate(slot, model.COSItemActive)
		player.SendToSelf(&itemState)
	}
	return nil
}

func (h *LifecycleHandler) resolvePlayer(session *server.Session) (*model.Player, error) {
	if session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return nil, errors.New("invalid COS lifecycle session identity")
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return nil, err
	}
	if player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return nil, errors.New("COS lifecycle ownership mismatch")
	}
	return player, nil
}

func (h *LifecycleHandler) sendResult(session *server.Session, messageID uint16, success bool) error {
	if session == nil {
		return fmt.Errorf("COS lifecycle session is nil")
	}
	p := network.EmptyPacket()
	p.MessageID = messageID
	if success {
		p.WriteByte(1)
	} else {
		p.WriteByte(2)
	}
	if h != nil && h.send != nil {
		return h.send(session, p)
	}
	return session.Send(p)
}
