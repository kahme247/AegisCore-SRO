package character

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const gachaClientAnimationSettleDelay = 4500 * time.Millisecond

type GachaHandler struct {
	repository model.GachaRepository
	world      gachaWorld
	send       func(*server.Session, network.Packet) error
	settleDelay time.Duration
}

type gachaWorld interface {
	selectEntityWorld
	Broadcast(network.Packet)
}

func RegisterGachaHandler(target *server.Server, repository model.GachaRepository, world gachaWorld) (*GachaHandler, error) {
	if target == nil || repository == nil || world == nil {
		return nil, fmt.Errorf("configure Gacha handler: dependencies are required")
	}
	h := &GachaHandler{repository: repository, world: world, settleDelay: gachaClientAnimationSettleDelay}
	target.RegisterHandler(opcode.GachaPlayRequest, h.handlePlay)
	target.RegisterHandler(opcode.GachaExchangeRequest, h.handleExchange)
	return h, nil
}

func (h *GachaHandler) handlePlay(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeGachaPlayRequest(&packet)
	if err != nil {
		return err
	}
	if session == nil || session.UserContext.CharacterID <= 0 || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("invalid Gacha session")
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return fmt.Errorf("resolve Gacha player: %w", err)
	}
	if err = validateNPCSelection(h.world, player, request.NPCUniqueID); err != nil {
		return fmt.Errorf("validate Gacha NPC: %w", err)
	}
	npc, err := h.world.GetObjectByUniqueId(request.NPCUniqueID)
	if err != nil || npc == nil || !isGachaNPCCodeName(npc.GetName()) {
		return fmt.Errorf("validate Gacha NPC: unsupported operator")
	}
	result, err := h.repository.Play(ctx, model.GachaPlayCommand{
		CharacterID: int64(session.UserContext.CharacterID),
		EntryID:     request.EntryID,
		CardSlot:    request.CardSlot,
	})
	if err != nil {
		if errors.Is(err, model.ErrGachaCardNotFound) || errors.Is(err, model.ErrGachaPoolEmpty) || errors.Is(err, model.ErrGachaInventoryFull) {
			return h.sendPacket(session, vsro188.BuildGachaPlayResponse(2, 1))
		}
		return err
	}
	player.CommitInventory(result.Inventory)
	detail := byte(0)
	if result.Won {
		detail = 1
	}
	if err = h.sendPacket(session, vsro188.BuildGachaPlayResponse(1, detail)); err != nil {
		return err
	}
	if h.settleDelay > 0 {
		time.Sleep(h.settleDelay)
	}
	if result.CardRemaining == 0 {
		if err = h.sendPacket(session, vsro188.BuildItemOperationServerDeleteSuccess(result.CardSlot)); err != nil {
			return err
		}
	} else if err = h.sendPacket(session, vsro188.BuildInventoryItemQuantityUpdate(result.CardSlot, result.CardRemaining)); err != nil {
		return err
	}
	if err = h.sendPacket(session, buildGachaItemAdded(result.RewardSlot, result.RewardItem)); err != nil {
		return err
	}
	if !result.Won {
		return nil
	}
	announcement, err := vsro188.BuildGachaAnnouncement(result.RewardRefItemID, result.RewardQuantity)
	if err != nil {
		return err
	}
	h.world.Broadcast(announcement)
	return nil
}

func isGachaNPCCodeName(codeName string) bool {
	switch codeName {
	case "NPC_CH_GACHA_MACHINE", "NPC_CH_GACHA_OPERATOR":
		return true
	default:
		return false
	}
}

func (h *GachaHandler) handleExchange(ctx context.Context, session *server.Session, packet network.Packet) error {
	npcUniqueID, err := vsro188.DecodeGachaExchangeRequest(&packet)
	if err != nil {
		return err
	}
	if session == nil || session.UserContext.CharacterID <= 0 || session.UserContext.UniqueID == 0 {
		return fmt.Errorf("invalid Gacha exchange session")
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return fmt.Errorf("resolve Gacha exchange player: %w", err)
	}
	if err = validateNPCSelection(h.world, player, npcUniqueID); err != nil {
		return err
	}
	npc, err := h.world.GetObjectByUniqueId(npcUniqueID)
	if err != nil || npc == nil || npc.GetName() != "NPC_CH_GACHA_OPERATOR" {
		return fmt.Errorf("validate Gacha exchange NPC: unsupported operator")
	}
	command := model.GachaExchangeCommand{CharacterID: int64(session.UserContext.CharacterID)}
	results := make([]model.GachaExchangeResult, 0, 4)
	for len(results) < int(model.MaxInventoryWireSize) {
		result, exchangeErr := h.repository.Exchange(ctx, command)
		if exchangeErr == nil {
			results = append(results, result)
			continue
		}
		if len(results) > 0 {
			break
		}
		if errors.Is(exchangeErr, model.ErrGachaCardNotFound) || errors.Is(exchangeErr, model.ErrGachaInventoryFull) || errors.Is(exchangeErr, model.ErrGachaInvalidRequest) {
			return h.sendPacket(session, vsro188.BuildGachaExchangeResponse(2, 1))
		}
		return exchangeErr
	}
	player.CommitInventory(results[len(results)-1].Inventory)
	for _, result := range results {
		if result.CardRemaining == 0 {
			if err = h.sendPacket(session, vsro188.BuildItemOperationServerDeleteSuccess(result.CardSlot)); err != nil {
				return err
			}
		} else if err = h.sendPacket(session, vsro188.BuildInventoryItemQuantityUpdate(result.CardSlot, result.CardRemaining)); err != nil {
			return err
		}
		if err = h.sendPacket(session, buildGachaItemAdded(result.RewardSlot, result.RewardItem)); err != nil {
			return err
		}
	}
	if err = h.sendPacket(session, vsro188.BuildGachaExchangeResponse(1, 0)); err != nil {
		return err
	}
	return nil
}

func buildGachaItemAdded(slot byte, item model.Item) network.Packet {
	packet := vsro188.BuildItemOperationTwoByteSuccess(vsro188.ItemOperationServerAdd, slot, 0x3C)
	model.WriteRentInfo(&packet, item)
	packet.WriteUInt32(item.GetRefObjectID())
	model.WriteInventoryItem(&packet, item)
	return packet
}

func (h *GachaHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
