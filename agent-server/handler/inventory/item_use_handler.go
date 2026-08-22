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

type ReturnTeleportCoordinator interface {
	PrepareItemReturn(*server.Session, *model.Player) error
	AbortItemReturn(*server.Session)
	CompleteItemReturn(*server.Session, *model.Player, model.TeleportDestination) error
}

type ItemUseHandler struct {
	players           PlayerLookup
	useCase           service.ItemUseCase
	send              func(*server.Session, network.Packet) error
	publish           func(*model.Player, *network.Packet)
	schedule          func(time.Duration, func())
	timedBuffDuration bool
	returnTeleport    ReturnTeleportCoordinator
	devilSpirit       model.AvatarInventoryRepository
	cos               interface {
		Summon(context.Context, *model.Player, uint64) (*model.COS, error)
	}
}

func (h *ItemUseHandler) ConfigureDevilSpirit(repository model.AvatarInventoryRepository) {
	if h != nil {
		h.devilSpirit = repository
	}
}

const devilSpiritUseType uint16 = 0x0F2D

func (h *ItemUseHandler) ConfigureCOS(runtime interface {
	Summon(context.Context, *model.Player, uint64) (*model.COS, error)
}) {
	if h != nil {
		h.cos = runtime
	}
}

func NewItemUseHandler(players PlayerLookup, items model.ItemUseRepository) *ItemUseHandler {
	return &ItemUseHandler{players: players, useCase: service.NewItemUseService(items)}
}

func RegisterItemUseHandler(target *server.Server, items model.ItemUseRepository, returnTeleport ...ReturnTeleportCoordinator) *ItemUseHandler {
	if target == nil {
		return nil
	}
	h := NewItemUseHandler(service.GetWorldServiceInstance(), items)
	if len(returnTeleport) > 0 {
		h.returnTeleport = returnTeleport[0]
	}
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
	if itemType == devilSpiritUseType {
		return h.activateDevilSpirit(ctx, session, player, slot)
	}
	preparedReturn := false
	if h.returnTeleport != nil {
		inventory := player.GetInventory()
		if item, exists := inventory.Items[slot]; exists {
			if ref, exists := model.RefItems[item.GetRefObjectID()]; exists && model.IsTeleportScroll(ref) {
				if err = h.returnTeleport.PrepareItemReturn(session, player); err != nil {
					logrus.WithError(err).Debug("rejected return-scroll preparation")
					return h.sendFailure(session)
				}
				preparedReturn = true
			}
		}
	}
	result, err := h.useCase.UseItem(ctx, player, slot, itemType)
	if err != nil {
		if preparedReturn {
			h.returnTeleport.AbortItemReturn(session)
		}
		if errors.Is(err, model.ErrItemUseOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected inventory item use")
		return h.sendFailure(session)
	}
	if result.COSPersistentID != 0 {
		if h.cos == nil {
			return fmt.Errorf("committed COS item use has no configured runtime")
		}
		summoned, summonErr := h.cos.Summon(ctx, player, result.COSPersistentID)
		if summonErr != nil {
			logrus.WithError(summonErr).Debug("rejected COS summon")
			return h.sendFailure(session)
		}
		data := model.BuildCOSData(summoned)
		player.SendToSelf(&data)
		state := buildCOSItemStateUpdate(result.SourceSlot, model.COSItemSummoned)
		player.SendToSelf(&state)
	}
	if result.MaskRefCharID != 0 {
		character, exists := model.RefChars[result.MaskRefCharID]
		if !exists {
			return fmt.Errorf("committed monster mask references unavailable character %d", result.MaskRefCharID)
		}
		duration := result.MaskDuration
		if duration <= 0 {
			duration = 3 * time.Minute
		}
		mask := model.CharacterMask{
			RefCharID: character.ID,
			Scale:     byte(max(0, min(character.Scale, 255))),
			Skills:    characterMaskSkills(character),
			ExpiresAt: time.Now().Add(duration),
			Token:     model.NextActionUID(),
		}
		player.ApplyCharacterMask(mask)
		changed := model.BuildCharacterMaskPacket(player.GetUniqueID(), mask)
		h.publishPacket(player, &changed)
		expire := func() {
			if !player.RemoveCharacterMask(mask.Token) {
				return
			}
			removed := model.BuildCharacterMaskPacket(player.GetUniqueID(), model.CharacterMask{})
			h.publishPacket(player, &removed)
		}
		if h.schedule != nil {
			h.schedule(duration, expire)
		} else {
			time.AfterFunc(duration, expire)
		}
	}
	if err = h.sendPacket(session, buildItemUseSuccess(result.SourceSlot, result.ItemType, result.NewAmount)); err != nil {
		if preparedReturn && result.ReturnDestination == nil {
			h.returnTeleport.AbortItemReturn(session)
		}
		return err
	}
	if result.ReturnDestination != nil {
		if !preparedReturn || h.returnTeleport == nil {
			return fmt.Errorf("committed return-scroll use was not prepared for teleport")
		}
		destination := *result.ReturnDestination
		complete := func() {
			if completeErr := h.returnTeleport.CompleteItemReturn(session, player, destination); completeErr != nil {
				logrus.WithError(completeErr).Warn("failed to complete committed return-scroll teleport")
			}
		}
		if result.ReturnDelay <= 0 {
			complete()
		} else if h.schedule != nil {
			h.schedule(result.ReturnDelay, complete)
		} else {
			time.AfterFunc(result.ReturnDelay, complete)
		}
	} else if preparedReturn {
		h.returnTeleport.AbortItemReturn(session)
		return fmt.Errorf("prepared return-scroll produced no destination")
	} else if result.EffectSkillID > 0 && result.SpeedPercent > 0 && result.EffectDuration > 0 {
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
	} else if result.EffectSkillID > 0 && result.EffectDuration > 0 && result.BuffModifiers != (model.BuffModifiers{}) {
		token := model.NextActionUID()
		duration := time.Duration(result.EffectDuration) * time.Millisecond
		replacedToken, committed := player.CommitActiveBuff(result.EffectSkillID, token, duration, result.BuffModifiers)
		if !committed {
			return fmt.Errorf("commit item buff skill %d", result.EffectSkillID)
		}
		if replacedToken != 0 {
			removed := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &removed)
		}
		added := model.BuildBuffAdd(player.GetUniqueID(), result.EffectSkillID, token)
		if h.timedBuffDuration {
			added = model.BuildTimedBuffAdd(player.GetUniqueID(), result.EffectSkillID, token, uint32(result.EffectDuration))
		}
		h.publishPacket(player, &added)
		stats := player.BuildStatsUpdatePacket()
		player.SendToSelf(&stats)
		schedule := h.schedule
		if schedule == nil {
			schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
		}
		schedule(duration, func() {
			if player.RemoveActiveBuff(token) {
				removed := model.BuildBuffRemove(token)
				h.publishPacket(player, &removed)
				stats := player.BuildStatsUpdatePacket()
				player.SendToSelf(&stats)
			}
		})
	} else if result.ItemService != nil {
		h.publishItemService(player, *result.ItemService, result.ItemServiceToken, result.ReplacedItemServiceToken)
	} else {
		player.SendVitalsUpdate()
	}
	return nil
}

func (h *ItemUseHandler) activateDevilSpirit(ctx context.Context, session *server.Session, player *model.Player, slot byte) error {
	if player == nil || h == nil || h.devilSpirit == nil || slot < 13 || slot >= model.BaseInventorySize {
		return h.sendFailure(session)
	}
	activation, err := h.devilSpirit.ActivateDevilSpirit(ctx, int64(player.ID), slot)
	if err != nil {
		logrus.WithError(err).Debug("rejected devil spirit activation")
		return h.sendFailure(session)
	}
	item := activation.Item
	player.CommitInventory(activation.Inventory)
	amount := item.StackSize
	if amount < 1 {
		amount = 1
	}
	if amount > int(^uint16(0)) {
		amount = int(^uint16(0))
	}
	if err := h.sendPacket(session, buildItemUseSuccess(slot, devilSpiritUseType, uint16(amount))); err != nil {
		return err
	}
	used := network.EmptyPacket()
	used.MessageID = opcode.EntityItemUsed
	used.WriteUInt32(player.GetUniqueID())
	used.WriteUInt32(item.GetRefObjectID())
	h.publishPacket(player, &used)
	remaining := time.Until(activation.ExpiresAt)
	if remaining <= 0 {
		remaining = time.Millisecond
	}
	expire := func() {
		result, expireErr := h.devilSpirit.ExpireDevilSpirit(context.Background(), int64(player.ID), item.ID, time.Now())
		if expireErr != nil {
			logrus.WithError(expireErr).Warn("failed to expire devil spirit")
			return
		}
		if !result.Expired {
			return
		}
		player.CommitInventoryAndAvatar(result.Inventory, result.AvatarInventory)
		if result.WasEquipped {
			request := moveItemRequest{sourceSlot: result.AvatarSlot, targetSlot: result.InventorySlot}
			sendAvatarStateUpdate(player, UnequipAvatarItemOperation, request, result.AvatarTransferResult)
			if sendErr := sendAvatarTransferSuccess(session, UnequipAvatarItemOperation, request); sendErr != nil {
				logrus.WithError(sendErr).Debug("failed to publish expired devil spirit transfer")
			}
			player.SendStatsUpdate()
		}
	}
	if h.schedule != nil {
		h.schedule(remaining, expire)
	} else {
		time.AfterFunc(remaining, expire)
	}
	return nil
}

func buildCOSItemStateUpdate(slot byte, state model.COSItemState) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUpdate
	p.WriteByte(slot)
	p.WriteByte(64) // ItemUpdateFlag.State
	p.WriteByte(byte(state))
	return p
}

func characterMaskSkills(character model.RefChar) []uint32 {
	values := [...]int{
		character.DefaultSkill1, character.DefaultSkill2, character.DefaultSkill3, character.DefaultSkill4, character.DefaultSkill5,
		character.DefaultSkill6, character.DefaultSkill7, character.DefaultSkill8, character.DefaultSkill9, character.DefaultSkill10,
	}
	skills := make([]uint32, 0, len(values))
	for _, skillID := range values {
		if skillID > 0 {
			skills = append(skills, uint32(skillID))
		}
	}
	return skills
}

func (h *ItemUseHandler) publishItemService(player *model.Player, effect model.ItemServiceEffect, token, replacedToken uint32) {
	if player == nil || token == 0 {
		return
	}
	if replacedToken != 0 {
		if model.ItemServiceUsesItemPerk(effect) {
			removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, replacedToken)
			h.publishPacket(player, &removed)
		}
		if premiumRemoved, exists := model.BuildPremiumDailyRemove(replacedToken, effect); exists {
			player.SendToSelf(&premiumRemoved)
		}
		if model.ItemServiceBuffSkillID(effect) != 0 {
			buffRemoved := model.BuildBuffRemove(replacedToken)
			h.publishPacket(player, &buffRemoved)
		}
	}
	remaining := time.Until(effect.ExpiresAt)
	if remaining <= 0 {
		return
	}
	if model.ItemServiceUsesItemPerk(effect) {
		added := model.BuildItemPerkAdd(player.GetUniqueID(), effect.SourceRefItemID, token, model.ItemServicePerkValue(effect), remaining)
		h.publishPacket(player, &added)
	}
	if premiumAdded, exists := model.BuildPremiumDailyAdd(token, effect, remaining); exists {
		player.SendToSelf(&premiumAdded)
	}
	if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), token, effect, remaining); exists {
		h.publishPacket(player, &buffAdded)
	}
	schedule := h.schedule
	if schedule == nil {
		schedule = func(delay time.Duration, callback func()) { time.AfterFunc(delay, callback) }
	}
	schedule(remaining, func() {
		if player.ClearItemService(effect.Kind, token) {
			if model.ItemServiceUsesItemPerk(effect) {
				removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, token)
				h.publishPacket(player, &removed)
			}
			if premiumRemoved, exists := model.BuildPremiumDailyRemove(token, effect); exists {
				player.SendToSelf(&premiumRemoved)
			}
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
