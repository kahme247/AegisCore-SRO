package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/sirupsen/logrus"
)

const (
	alchemyActionCancel = 1
	alchemyActionFuse   = 2
	alchemyTypeElixir   = 3
)

type alchemyElixirRequest struct {
	cancel     bool
	targetSlot byte
	elixirSlot byte
	powderSlot *byte
}

type AlchemyHandler struct {
	players PlayerLookup
	repo    model.AlchemyRepository
	send    func(*server.Session, network.Packet) error
}

func RegisterAlchemyHandler(target *server.Server, repo model.AlchemyRepository) {
	if target == nil {
		return
	}
	h := &AlchemyHandler{players: service.GetWorldServiceInstance(), repo: repo}
	target.RegisterHandler(opcode.AlchemyElixirRequest, h.handlePacket)
}

func (h *AlchemyHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyElixirRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildAlchemyFailure())
	}
	if request.cancel {
		return h.sendPacket(session, buildAlchemyCancel())
	}
	resolver := &InventoryHandler{players: h.players}
	player, err := resolver.resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.repo.FuseElixir(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.elixirSlot, request.powderSlot)
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected alchemy enhancement")
		return h.sendPacket(session, buildAlchemyFailure())
	}
	player.CommitInventory(result.Inventory)
	if err = h.sendPacket(session, buildAlchemySuccess(result)); err != nil {
		return err
	}
	if request.targetSlot <= model.SlotRightRing {
		if err = h.sendPacket(session, player.BuildStatsUpdatePacket()); err != nil {
			return err
		}
	}
	for _, material := range result.Materials {
		if material.Remaining == 0 {
			err = h.sendPacket(session, buildAlchemyMaterialRemoved(material.Slot))
		} else {
			err = h.sendPacket(session, buildAlchemyMaterialQuantity(material.Slot, material.Remaining))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func parseAlchemyElixirRequest(packet *network.Packet) (alchemyElixirRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return alchemyElixirRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, err := packet.ReadByte()
	if err != nil {
		return alchemyElixirRequest{}, err
	}
	if action == alchemyActionCancel {
		if packet.Remaining() != 0 {
			return alchemyElixirRequest{}, model.ErrAlchemyInvalidRequest
		}
		return alchemyElixirRequest{cancel: true}, nil
	}
	if action != alchemyActionFuse || packet.Remaining() < 4 {
		return alchemyElixirRequest{}, model.ErrAlchemyUnsupported
	}
	alchemyType, e1 := packet.ReadByte()
	count, e2 := packet.ReadByte()
	target, e3 := packet.ReadByte()
	elixir, e4 := packet.ReadByte()
	if errors.Join(e1, e2, e3, e4) != nil || alchemyType != alchemyTypeElixir || (count != 2 && count != 3) {
		return alchemyElixirRequest{}, model.ErrAlchemyUnsupported
	}
	request := alchemyElixirRequest{targetSlot: target, elixirSlot: elixir}
	if count == 3 {
		powder, readErr := packet.ReadByte()
		if readErr != nil {
			return alchemyElixirRequest{}, readErr
		}
		request.powderSlot = &powder
	}
	if packet.Remaining() != 0 {
		return alchemyElixirRequest{}, model.ErrAlchemyInvalidRequest
	}
	return request, nil
}

func buildAlchemyCancel() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemyElixirResponse
	p.WriteByte(1)
	p.WriteByte(alchemyActionCancel)
	return p
}

func buildAlchemyFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemyElixirResponse
	p.WriteByte(2)
	p.WriteUInt16(0)
	return p
}

func buildAlchemySuccess(result model.AlchemyResult) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemyElixirResponse
	p.WriteByte(1)
	p.WriteByte(alchemyActionFuse)
	p.WriteBool(result.Success)
	p.WriteByte(result.TargetSlot)
	model.WriteRentInfo(&p, result.Target)
	p.WriteUInt32(result.Target.GetRefObjectID())
	model.WriteInventoryItem(&p, result.Target)
	return p
}

func buildAlchemyMaterialQuantity(slot byte, remaining uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUpdate
	p.WriteByte(slot)
	p.WriteByte(8) // ItemUpdateFlag.Quantity
	p.WriteUInt16(remaining)
	return p
}

func buildAlchemyMaterialRemoved(slot byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(1)
	p.WriteByte(15) // SP_DEL_ITEM_BY_SERVER
	p.WriteByte(slot)
	return p
}

func (h *AlchemyHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return fmt.Errorf("alchemy session is nil")
	}
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
