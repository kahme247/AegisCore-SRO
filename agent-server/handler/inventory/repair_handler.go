package inventory

import (
	"context"
	"errors"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/sirupsen/logrus"
)

const (
	repairSingle byte = 1
	repairAll    byte = 2
)

func (h *InventoryHandler) registerRepairHandler(target *server.Server) {
	if target != nil {
		target.RegisterHandler(opcode.ItemRepairRequest, h.handleRepairPacket)
	}
}

func (h *InventoryHandler) handleRepairPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	repository, ok := h.economy.(model.RepairRepository)
	if !ok || packet.Remaining() < 5 {
		return h.sendPacket(session, buildRepairFailure())
	}
	npcUID, err := packet.ReadUInt32()
	if err != nil {
		return h.sendPacket(session, buildRepairFailure())
	}
	repairType, err := packet.ReadByte()
	if err != nil {
		return h.sendPacket(session, buildRepairFailure())
	}
	var slot *byte
	switch repairType {
	case repairSingle:
		value, readErr := packet.ReadByte()
		if readErr != nil || packet.Remaining() != 0 {
			return h.sendPacket(session, buildRepairFailure())
		}
		slot = &value
	case repairAll:
		if packet.Remaining() != 0 {
			return h.sendPacket(session, buildRepairFailure())
		}
	default:
		return h.sendPacket(session, buildRepairFailure())
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	if h.exchange != nil && h.exchange.Busy(player.ID) {
		return h.sendPacket(session, buildRepairFailure())
	}
	npcCode, validateErr := h.validateInteractionNPC(player, npcUID, false)
	upperCode := strings.ToUpper(npcCode)
	if validateErr != nil || (!strings.Contains(upperCode, "SMITH") && !strings.Contains(upperCode, "ARMOR")) {
		return h.sendPacket(session, buildRepairFailure())
	}
	result, err := repository.Repair(ctx, int64(player.ID), session.UserContext.UserID, slot)
	if err != nil {
		if errors.Is(err, model.ErrRepairOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected item repair")
		return h.sendPacket(session, buildRepairFailure())
	}
	player.CommitEconomy(result.Gold, result.Inventory)
	if err = h.sendPacket(session, buildRepairSuccess()); err != nil {
		return err
	}
	for repairedSlot, durability := range result.Durability {
		update := network.EmptyPacket()
		update.MessageID = opcode.ItemDurabilityUpdate
		update.WriteByte(repairedSlot)
		update.WriteUInt32(durability)
		if err = h.sendPacket(session, update); err != nil {
			return err
		}
	}
	points := network.EmptyPacket()
	points.MessageID = opcode.EntityUpdatePoints
	points.WriteByte(1)
	points.WriteUInt64(result.Gold)
	points.WriteByte(0)
	return h.sendPacket(session, points)
}

func buildRepairSuccess() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemRepairResponse
	p.WriteByte(1)
	return p
}

func buildRepairFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemRepairResponse
	p.WriteByte(2)
	p.WriteUInt16(0)
	return p
}
