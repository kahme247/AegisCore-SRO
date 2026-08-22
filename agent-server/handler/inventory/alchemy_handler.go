package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/sirupsen/logrus"
)

const (
	alchemyActionCancel       = 1
	alchemyActionFuse         = 2
	alchemyTypeElixir         = 3
	alchemyTypeMagicStone     = 4
	alchemyTypeAttributeStone = 5
	alchemyTypeAdvancedElixir = 8
	alchemyTypeSocketInsert   = 9
)

type alchemyElixirRequest struct {
	cancel      bool
	alchemyType byte
	targetSlot  byte
	elixirSlot  byte
	powderSlot  *byte
}

type AlchemyHandler struct {
	players PlayerLookup
	repo    model.AlchemyRepository
	send    func(*server.Session, network.Packet) error
	wait    func(time.Duration)
}

func RegisterAlchemyHandler(target *server.Server, repo model.AlchemyRepository) {
	if target == nil {
		return
	}
	h := &AlchemyHandler{players: service.GetWorldServiceInstance(), repo: repo, wait: time.Sleep}
	target.RegisterHandler(opcode.AlchemyElixirRequest, h.handlePacket)
	target.RegisterHandler(opcode.AlchemyStoneRequest, h.handleStonePacket)
	target.RegisterHandler(opcode.AlchemySocketRequest, h.handleSocketPacket)
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
	var result model.AlchemyResult
	if request.alchemyType == alchemyTypeAdvancedElixir {
		result, err = h.repo.ApplyAdvancedElixir(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.elixirSlot)
	} else {
		result, err = h.repo.FuseElixir(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.elixirSlot, request.powderSlot)
	}
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected alchemy enhancement")
		return h.sendPacket(session, buildAlchemyFailure())
	}
	h.waitFor(3 * time.Second)
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
	if errors.Join(e1, e2, e3, e4) != nil || (alchemyType != alchemyTypeElixir && alchemyType != alchemyTypeAdvancedElixir) || (count != 2 && count != 3) || (alchemyType == alchemyTypeAdvancedElixir && count != 2) {
		return alchemyElixirRequest{}, model.ErrAlchemyUnsupported
	}
	request := alchemyElixirRequest{alchemyType: alchemyType, targetSlot: target, elixirSlot: elixir}
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
	return buildElixirAlchemyResult(result)
}

func buildElixirAlchemyResult(result model.AlchemyResult) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemyElixirResponse
	p.WriteByte(1)
	p.WriteByte(alchemyActionFuse)
	p.WriteBool(result.Success)
	p.WriteByte(result.TargetSlot)
	if !result.Success {
		p.WriteBool(result.Destroyed)
		if result.Destroyed {
			return p
		}
	}
	writeAlchemyTarget(&p, result.Target)
	return p
}

func buildStoneAlchemyResult(result model.AlchemyResult) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemyStoneResponse
	p.WriteByte(1)
	p.WriteByte(alchemyActionFuse)
	p.WriteBool(result.Success)
	p.WriteByte(result.TargetSlot)
	writeAlchemyTarget(&p, result.Target)
	return p
}

func buildSocketAlchemyResult(result model.AlchemyResult) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemySocketResponse
	p.WriteByte(1)
	p.WriteByte(alchemyActionFuse)
	p.WriteByte(result.TargetSlot)
	writeAlchemyTarget(&p, result.Target)
	return p
}

func writeAlchemyTarget(p *network.Packet, target model.Item) {
	model.WriteRentInfo(p, target)
	p.WriteUInt32(target.GetRefObjectID())
	model.WriteInventoryItem(p, target)
}

type alchemyPairRequest struct {
	cancel       bool
	alchemyType  byte
	targetSlot   byte
	materialSlot byte
}

func parseAlchemyPairRequest(packet *network.Packet) (alchemyPairRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, err := packet.ReadByte()
	if err != nil {
		return alchemyPairRequest{}, err
	}
	if action == alchemyActionCancel && packet.Remaining() == 0 {
		return alchemyPairRequest{cancel: true}, nil
	}
	alchemyType, e1 := packet.ReadByte()
	count, e2 := packet.ReadByte()
	target, e3 := packet.ReadByte()
	material, e4 := packet.ReadByte()
	if action != alchemyActionFuse || count != 2 || packet.Remaining() != 0 || errors.Join(e1, e2, e3, e4) != nil {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	if alchemyType != alchemyTypeMagicStone && alchemyType != alchemyTypeAttributeStone {
		return alchemyPairRequest{}, model.ErrAlchemyUnsupported
	}
	return alchemyPairRequest{alchemyType: alchemyType, targetSlot: target, materialSlot: material}, nil
}

func parseAlchemySocketRequest(packet *network.Packet) (alchemyPairRequest, error) {
	if packet == nil || packet.Remaining() != 4 {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, e1 := packet.ReadByte()
	alchemyType, e2 := packet.ReadByte()
	target, e3 := packet.ReadByte()
	stone, e4 := packet.ReadByte()
	if errors.Join(e1, e2, e3, e4) != nil || action != alchemyActionFuse || alchemyType != alchemyTypeSocketInsert {
		return alchemyPairRequest{}, model.ErrAlchemyUnsupported
	}
	return alchemyPairRequest{alchemyType: alchemyType, targetSlot: target, materialSlot: stone}, nil
}

func (h *AlchemyHandler) handleStonePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyPairRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildStoneAlchemyFailure())
	}
	if request.cancel {
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyStoneResponse))
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.repo.FuseStone(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.materialSlot, request.alchemyType)
	if err == nil {
		h.waitFor(4500 * time.Millisecond)
	}
	return h.finishExtendedAlchemy(session, player, buildStoneAlchemyResult, buildStoneAlchemyFailure, result, err)
}

func (h *AlchemyHandler) handleSocketPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemySocketRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildSocketAlchemyFailure())
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.repo.InsertSocket(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.materialSlot)
	if err == nil {
		h.waitFor(4500 * time.Millisecond)
	}
	return h.finishExtendedAlchemy(session, player, buildSocketAlchemyResult, buildSocketAlchemyFailure, result, err)
}

func (h *AlchemyHandler) waitFor(duration time.Duration) {
	if h != nil && h.wait != nil && duration > 0 {
		h.wait(duration)
	}
}

func (h *AlchemyHandler) finishExtendedAlchemy(session *server.Session, player *model.Player, buildResult func(model.AlchemyResult) network.Packet, buildFailure func() network.Packet, result model.AlchemyResult, err error) error {
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected extended alchemy operation")
		return h.sendPacket(session, buildFailure())
	}
	player.CommitInventory(result.Inventory)
	if err = h.sendPacket(session, buildResult(result)); err != nil {
		return err
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

func buildStoneAlchemyFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemyStoneResponse
	p.WriteByte(2)
	p.WriteUInt16(0)
	return p
}

func buildSocketAlchemyFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.AlchemySocketResponse
	p.WriteByte(2)
	p.WriteByte(alchemyActionFuse)
	p.WriteUInt16(0)
	return p
}

func buildAlchemyProtocolCancel(messageID uint16) network.Packet {
	p := buildAlchemyCancel()
	p.MessageID = messageID
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
