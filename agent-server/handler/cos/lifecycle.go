package cos

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type lifecycleService interface {
	Owned(*model.Player, uint32) (*model.COS, error)
	Unsummon(context.Context, *model.Player, uint32) error
	SetAttackTarget(*model.Player, uint32, uint32, uint32) error
	ClearAttackTarget(*model.Player, uint32) error
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
	return h
}

func (h *LifecycleHandler) handleCommand(_ context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.cos == nil || packet.Remaining() < 5 {
		return h.sendCommandFailure(session, 0, 3)
	}
	uid, err := packet.ReadUInt32()
	if err != nil || uid == 0 {
		return h.sendCommandFailure(session, 0, 3)
	}
	command, err := packet.ReadByte()
	if err != nil {
		return h.sendCommandFailure(session, 0, 3)
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	switch command {
	case 2: // attack with the current reference default skill
		if packet.Remaining() != 4 {
			return h.sendCommandFailure(session, command, 3)
		}
		targetUID, readErr := packet.ReadUInt32()
		if readErr != nil || h.cos.SetAttackTarget(player, uid, targetUID, 0) != nil {
			return h.sendCommandFailure(session, command, 3)
		}
	case 9: // stop combat and resume owner-follow behavior
		if packet.Remaining() != 0 || h.cos.ClearAttackTarget(player, uid) != nil {
			return h.sendCommandFailure(session, command, 3)
		}
	case 13: // cast an authored pet skill
		if packet.Remaining() != 8 {
			return h.sendCommandFailure(session, command, 3)
		}
		skillID, skillErr := packet.ReadUInt32()
		targetUID, targetErr := packet.ReadUInt32()
		if skillErr != nil || targetErr != nil || h.cos.SetAttackTarget(player, uid, targetUID, skillID) != nil {
			return h.sendCommandFailure(session, command, 3)
		}
	default:
		return h.sendCommandFailure(session, command, 3)
	}
	return nil
}

func (h *LifecycleHandler) sendCommandFailure(session *server.Session, command byte, code uint16) error {
	if session == nil {
		return errors.New("COS command session is nil")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.COSCommandResponse
	p.WriteByte(2)
	p.WriteByte(command)
	p.WriteUInt16(code)
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
	if h == nil || h.players == nil || h.cos == nil || packet.Remaining() != 4 {
		return h.sendResult(session, responseOpcode, false)
	}
	uid, err := packet.ReadUInt32()
	if err != nil || uid == 0 || packet.Remaining() != 0 {
		return h.sendResult(session, responseOpcode, false)
	}
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
	itemState := network.EmptyPacket()
	itemState.MessageID = opcode.InventoryItemUpdate
	itemState.WriteByte(slot)
	itemState.WriteByte(64) // ItemUpdateFlag.State
	itemState.WriteByte(byte(model.COSItemActive))
	player.SendToSelf(&itemState)
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
