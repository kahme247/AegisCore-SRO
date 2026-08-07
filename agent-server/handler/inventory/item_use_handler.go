package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/sirupsen/logrus"
)

type ItemUseHandler struct {
	players           PlayerLookup
	useCase           service.RecoveryItemUseCase
	send              func(*server.Session, network.Packet) error
	publish           func(*model.Player, *network.Packet)
	schedule          func(time.Duration, func())
	timedBuffDuration bool
}

func NewItemUseHandler(players PlayerLookup, items model.ItemUseRepository) *ItemUseHandler {
	return &ItemUseHandler{players: players, useCase: service.NewItemUseService(items)}
}

func RegisterItemUseHandler(target *server.Server, items model.ItemUseRepository) *ItemUseHandler {
	if target == nil {
		return nil
	}
	h := NewItemUseHandler(service.GetWorldServiceInstance(), items)
	target.RegisterHandler(opcode.InventoryItemUseRequest, h.handlePacket)
	return h
}

func (h *ItemUseHandler) ConfigureTimedBuffDuration(enabled bool) {
	if h != nil {
		h.timedBuffDuration = enabled
	}
}

func (h *ItemUseHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.useCase == nil || packet.Remaining() != 3 {
		return h.sendFailure(session)
	}
	slot, e1 := packet.ReadByte()
	itemType, e2 := packet.ReadUInt16()
	if errors.Join(e1, e2) != nil || packet.Remaining() != 0 {
		return h.sendFailure(session)
	}
	resolver := &InventoryHandler{players: h.players}
	player, err := resolver.resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.useCase.UseRecoveryItem(ctx, player, slot, itemType)
	if err != nil {
		if errors.Is(err, model.ErrItemUseOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected inventory item use")
		return h.sendFailure(session)
	}
	if err = h.sendPacket(session, buildItemUseSuccess(result.SourceSlot, result.ItemType, result.NewAmount)); err != nil {
		return err
	}
	if result.EffectSkillID > 0 && result.SpeedPercent > 0 && result.EffectDuration > 0 {
		token := model.NextActionUID()
		generation, replacedToken := player.ApplyMovementSpeedEffect(result.EffectSkillID, result.SpeedPercent, result.EffectExpiresAt, token)
		if replacedToken != 0 {
			removed := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &removed)
		}
		added := model.BuildBuffAdd(player.GetUniqueID(), result.EffectSkillID, token)
		if h.timedBuffDuration {
			added = model.BuildTimedBuffAdd(player.GetUniqueID(), result.EffectSkillID, token, uint32(result.EffectDuration))
		}
		h.publishPacket(player, &added)
		player.SendMovementSpeedUpdate()
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
		}
		schedule(time.Duration(result.EffectDuration)*time.Millisecond, func() {
			if expiredToken, cleared := player.ClearMovementSpeedEffect(generation); cleared {
				removed := model.BuildBuffRemove(expiredToken)
				h.publishPacket(player, &removed)
				player.SendMovementSpeedUpdate()
			}
		})
	} else if result.ItemService != nil {
		h.publishItemService(player, *result.ItemService, result.ItemServiceToken, result.ReplacedItemServiceToken)
	} else {
		player.SendVitalsUpdate()
	}
	return nil
}

func (h *ItemUseHandler) publishItemService(player *model.Player, effect model.ItemServiceEffect, token, replacedToken uint32) {
	if player == nil || token == 0 {
		return
	}
	if replacedToken != 0 {
		removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, replacedToken)
		h.publishPacket(player, &removed)
		if model.ItemServiceBuffSkillID(effect) != 0 {
			buffRemoved := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &buffRemoved)
		}
	}
	remaining := time.Until(effect.ExpiresAt)
	if remaining <= 0 {
		return
	}
	added := model.BuildItemPerkAdd(player.GetUniqueID(), effect.SourceRefItemID, token, effect.DailyBudget, remaining)
	h.publishPacket(player, &added)
	if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), token, effect, remaining); exists {
		h.publishPacket(player, &buffAdded)
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	schedule(remaining, func() {
		if player.ClearItemService(effect.Kind, token) {
			removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, token)
			h.publishPacket(player, &removed)
			if model.ItemServiceBuffSkillID(effect) != 0 {
				buffRemoved := model.BuildBuffRemove(token)
				h.publishPacket(player, &buffRemoved)
			}
		}
	})
}

func (h *ItemUseHandler) publishPacket(player *model.Player, packet *network.Packet) {
	if h != nil && h.publish != nil {
		h.publish(player, packet)
		return
	}
	player.SendToSelfAndKnown(packet)
}

func (h *ItemUseHandler) sendFailure(session *server.Session) error {
	return h.sendPacket(session, buildItemUseFailure())
}
func (h *ItemUseHandler) sendPacket(session *server.Session, p network.Packet) error {
	if session == nil {
		return fmt.Errorf("item-use session is nil")
	}
	if h != nil && h.send != nil {
		return h.send(session, p)
	}
	return session.Send(p)
}
func buildItemUseSuccess(sourceSlot byte, itemType, newAmount uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUseResponse
	p.WriteByte(1)
	p.WriteByte(sourceSlot)
	p.WriteUInt16(newAmount)
	p.WriteUInt16(itemType)
	return p
}

func buildItemUseFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUseResponse
	p.WriteByte(2)
	p.WriteUInt16(0)
	return p
}
