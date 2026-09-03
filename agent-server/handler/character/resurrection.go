package character

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	resurrectAtReturnPoint  byte = 1
	resurrectAtPresentPoint byte = 2
)

type ResurrectionHandler struct {
	players    resurrectionPlayerLookup
	repository model.CharacterVitalsRepository
	items      model.ResurrectionItemRepository
	teleport   resurrectionReturnTeleport
}

type resurrectionPlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type resurrectionVisibilityReconciler interface {
	ReconcileCharacterVisibility(model.ICharacter) []service.VisibilityDelta
}

type resurrectionReturnTeleport interface {
	PrepareItemReturn(*server.Session, *model.Player) error
	AbortItemReturn(*server.Session)
	CompleteItemReturn(*server.Session, *model.Player, model.TeleportDestination) error
}

func RegisterResurrectionHandler(target *server.Server, repository model.CharacterVitalsRepository, teleport resurrectionReturnTeleport, items ...model.ResurrectionItemRepository) {
	if target == nil || repository == nil {
		return
	}
	handler := &ResurrectionHandler{players: service.GetWorldServiceInstance(), repository: repository, teleport: teleport}
	if len(items) > 0 {
		handler.items = items[0]
	}
	target.RegisterHandler(opcode.CharacterResurrectRequest, handler.handlePacket)
}

func (h *ResurrectionHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.repository == nil || packet.Remaining() != 1 {
		return fmt.Errorf("invalid resurrection request")
	}
	mode, err := packet.ReadByte()
	if err != nil || (mode != resurrectAtReturnPoint && mode != resurrectAtPresentPoint) || packet.Remaining() != 0 {
		return fmt.Errorf("invalid resurrection mode")
	}
	resolver := &resurrectionPlayerResolver{players: h.players}
	player, err := resolver.Resolve(session)
	if err != nil {
		return err
	}
	if player.GetLifeState() != model.Dead {
		return fmt.Errorf("character is not dead")
	}
	hp, mp := player.MaxVitals()
	if mode == resurrectAtPresentPoint {
		if h.items == nil {
			return fmt.Errorf("present-point resurrection item service is unavailable")
		}
		_, currentMP := player.CurrentVitals()
		player.LockEconomy()
		result, useErr := h.items.UseResurrectionItem(ctx, int64(player.ID), model.ItemUseVitals{HP: 0, MP: currentMP, MaxHP: hp, MaxMP: mp})
		if useErr != nil {
			player.UnlockEconomy()
			return useErr
		}
		player.CommitInventory(result.Inventory)
		if !player.CommitResurrection(result.HP, result.MP) {
			player.UnlockEconomy()
			return fmt.Errorf("character item resurrection was superseded")
		}
		player.UnlockEconomy()
		used := buildResurrectionItemUseSuccess(result)
		if err = session.Send(used); err != nil {
			return err
		}
		return h.publishResurrection(player)
	}
	if returnPoints, ok := h.repository.(model.ReturnPointResurrectionRepository); ok && h.teleport != nil {
		if err = h.teleport.PrepareItemReturn(session, player); err != nil {
			return err
		}
		destination, updated, resurrectErr := returnPoints.ResurrectAtReturnPoint(ctx, player.ID, hp, mp)
		if resurrectErr != nil || !updated {
			h.teleport.AbortItemReturn(session)
			if resurrectErr != nil {
				return resurrectErr
			}
			return fmt.Errorf("character resurrection was superseded")
		}
		if !player.CommitResurrection(hp, mp) {
			h.teleport.AbortItemReturn(session)
			return fmt.Errorf("character resurrection was superseded")
		}
		if err = h.teleport.CompleteItemReturn(session, player, destination); err != nil {
			return err
		}
		return nil
	}
	updated, err := h.repository.Resurrect(ctx, player.ID, hp, mp)
	if err != nil {
		return err
	}
	if !updated || !player.CommitResurrection(hp, mp) {
		return fmt.Errorf("character resurrection was superseded")
	}
	return h.publishResurrection(player)
}

func (h *ResurrectionHandler) publishResurrection(player *model.Player) error {
	player.SendVitalsUpdate()
	state := model.BuildLifeStateUpdatePacket(player.GetUniqueID(), model.Alive)
	player.SendToSelfAndKnown(&state)
	if visibility, ok := h.players.(resurrectionVisibilityReconciler); ok {
		_ = visibility.ReconcileCharacterVisibility(player)
	}
	return nil
}

func buildResurrectionItemUseSuccess(result model.ItemUseResult) network.Packet {
	return vsro188.BuildItemUseSuccess(vsro188.ItemUseSuccess{
		SourceSlot: result.SourceSlot,
		NewAmount:  result.NewAmount,
		ItemType:   result.ItemType,
	})
}

type resurrectionPlayerResolver struct{ players resurrectionPlayerLookup }

func (r *resurrectionPlayerResolver) Resolve(session *server.Session) (*model.Player, error) {
	if session == nil || r == nil || r.players == nil || session.UserContext.UniqueID == 0 {
		return nil, fmt.Errorf("resurrection player is unavailable")
	}
	player, err := r.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil {
		return nil, fmt.Errorf("resurrection player lookup failed")
	}
	if player.Session != session || player.ID != session.UserContext.CharacterID {
		return nil, fmt.Errorf("resurrection session does not own character")
	}
	return player, nil
}
