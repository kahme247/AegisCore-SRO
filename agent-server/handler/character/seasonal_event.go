package character

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type seasonalEventPlayers interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type SeasonalEventHandler struct {
	repository model.SeasonalEventRepository
	players    seasonalEventPlayers
	enabled    bool
	send       func(*server.Session, network.Packet) error
}

func RegisterSeasonalEventHandler(target *server.Server, repository model.SeasonalEventRepository, players seasonalEventPlayers, enabled bool) (*SeasonalEventHandler, error) {
	if target == nil || repository == nil || players == nil {
		return nil, fmt.Errorf("configure seasonal-event handler: dependencies are required")
	}
	h := &SeasonalEventHandler{repository: repository, players: players, enabled: enabled}
	target.RegisterHandler(opcode.ChristmasEventRequest, h.handleChristmas)
	return h, nil
}

func (h *SeasonalEventHandler) handleChristmas(ctx context.Context, session *server.Session, packet network.Packet) error {
	if err := vsro188.DecodeChristmasEventRequest(&packet); err != nil {
		return err
	}
	if !h.enabled {
		return h.sendPacket(session, vsro188.BuildChristmasEventFailure(3))
	}
	if session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return h.sendPacket(session, vsro188.BuildChristmasEventFailure(3))
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return h.sendPacket(session, vsro188.BuildChristmasEventFailure(3))
	}
	result, err := h.repository.ExchangeChristmasSocks(ctx, int64(player.ID))
	if err != nil {
		code := byte(3)
		if errors.Is(err, model.ErrGachaInventoryFull) {
			code = 4
		} else if !errors.Is(err, model.ErrChristmasSocksMissing) && !errors.Is(err, model.ErrChristmasPoolEmpty) {
			return err
		}
		return h.sendPacket(session, vsro188.BuildChristmasEventFailure(code))
	}
	player.CommitInventory(result.Inventory)
	if result.SocksRemaining == 0 {
		if err = h.sendPacket(session, vsro188.BuildItemOperationServerDeleteSuccess(result.SocksSlot)); err != nil {
			return err
		}
	} else if err = h.sendPacket(session, vsro188.BuildInventoryItemQuantityUpdate(result.SocksSlot, result.SocksRemaining)); err != nil {
		return err
	}
	if err = h.sendPacket(session, buildGachaItemAdded(result.RewardSlot, result.RewardItem)); err != nil {
		return err
	}
	return h.sendPacket(session, vsro188.BuildChristmasEventSuccess())
}

func (h *SeasonalEventHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
