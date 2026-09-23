package inventory

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

type alchemyAvatarRequest struct {
	cancel bool
	slots  []byte
}

type alchemyAvatarGrantRequest struct {
	targetSlot byte
	optionCode string
}

func parseAlchemyAvatarRequest(packet *network.Packet) (alchemyAvatarRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, err := packet.ReadByte()
	if err != nil {
		return alchemyAvatarRequest{}, err
	}
	if action != alchemyActionFuse {
		if packet.Remaining() != 0 {
			return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
		}
		return alchemyAvatarRequest{cancel: true}, nil
	}
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > 2 || packet.Remaining() != int(count) {
		return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
	}
	request := alchemyAvatarRequest{slots: make([]byte, count)}
	seen := make(map[byte]struct{}, count)
	for index := range request.slots {
		slot, readErr := packet.ReadByte()
		if readErr != nil || slot < model.InventoryEquipmentSlots || slot > 0x6f {
			return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
		}
		if _, duplicate := seen[slot]; duplicate {
			return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
		}
		seen[slot] = struct{}{}
		request.slots[index] = slot
	}
	return request, nil
}

func parseAlchemyAvatarGrantRequest(packet *network.Packet) (alchemyAvatarGrantRequest, error) {
	if packet == nil || packet.Remaining() < 3 {
		return alchemyAvatarGrantRequest{}, model.ErrAlchemyInvalidRequest
	}
	slot, err := packet.ReadByte()
	if err != nil || slot < model.InventoryEquipmentSlots || slot > 0x6f {
		return alchemyAvatarGrantRequest{}, model.ErrAlchemyInvalidRequest
	}
	optionCode, err := packet.ReadString()
	if err != nil || optionCode == "" || packet.Remaining() != 0 {
		return alchemyAvatarGrantRequest{}, model.ErrAlchemyInvalidRequest
	}
	return alchemyAvatarGrantRequest{targetSlot: slot, optionCode: optionCode}, nil
}

func buildAlchemyAvatarResult(mutated bool) network.Packet {
	return vsro188.BuildAlchemyAvatarResult(mutated)
}

func buildAlchemyAvatarGrantResult(result model.AlchemyResult) network.Packet {
	return vsro188.BuildAlchemyAvatarGrantResult(result.Success, result.TargetSlot, alchemyTargetPayload(result.Target))
}

func (h *AlchemyHandler) handleAvatarPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyAvatarRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, vsro188.BuildAlchemyFailure(opcode.AlchemyAvatarResponse, 0))
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	if request.cancel {
		h.lifecycle().cancel(player.ID)
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyAvatarResponse))
	}
	if !h.beginDelayed(player, alchemyTypeAvatar) {
		return h.sendPacket(session, vsro188.BuildAlchemyFailure(opcode.AlchemyAvatarResponse, 0))
	}
	defer h.lifecycle().finish(player.ID)
	wake := h.awaitPhaseThree(ctx, player)
	if !wake.execute {
		if wake.suppressResponse {
			return nil
		}
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyAvatarResponse))
	}
	var stoneSlot *byte
	if len(request.slots) == 2 {
		stoneSlot = &request.slots[1]
	}
	result, err := h.repo.EnchantAvatarItem(ctx, int64(player.ID), session.UserContext.UserID, request.slots[0], stoneSlot)
	buildResult := func(result model.AlchemyResult) network.Packet { return buildAlchemyAvatarResult(result.Success) }
	buildFailure := func() network.Packet { return vsro188.BuildAlchemyFailure(opcode.AlchemyAvatarResponse, 0) }
	return h.finishExtendedAlchemy(session, player, buildResult, buildFailure, result, err)
}

func (h *AlchemyHandler) handleAvatarGrantPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyAvatarGrantRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return nil
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.repo.BlessAvatarItem(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.optionCode)
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected avatar NPC blessing")
		return nil
	}
	player.CommitInventory(result.Inventory)
	return h.sendPacket(session, buildAlchemyAvatarGrantResult(result))
}
