package character

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type PremiumSilkHandler struct {
	repository  model.SilkHistoryRepository
	decorations model.PremiumDecorationRepository
	players     interface {
		GetPlayerByUniqueId(uint32) (*model.Player, error)
	}
	send func(*server.Session, network.Packet) error
}

func RegisterPremiumSilkHandler(target *server.Server, dependency any, decorations ...model.PremiumDecorationRepository) *PremiumSilkHandler {
	repository, ok := dependency.(model.SilkHistoryRepository)
	if target == nil || !ok || repository == nil {
		return nil
	}
	h := &PremiumSilkHandler{repository: repository, players: service.GetWorldServiceInstance()}
	if len(decorations) > 0 {
		h.decorations = decorations[0]
	}
	target.RegisterHandler(opcode.SilkHistoryRequest, h.handleSilkHistory)
	target.RegisterHandler(opcode.SilkUnknownRequest, h.handleSilkUnknown)
	target.RegisterHandler(opcode.PremiumDecoUseRequest, h.handlePremiumDecoration)
	target.RegisterHandler(opcode.PremiumPause, h.handlePremiumPause)
	return h
}

func (h *PremiumSilkHandler) handlePremiumPause(_ context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodePremiumPauseRequest(&packet)
	if err != nil || session == nil || session.UserContext.UniqueID == 0 {
		return err
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || !player.SetPremiumCategoryActive(request.Category, request.Active) {
		return model.ErrItemMallConflict
	}
	return nil
}

func (h *PremiumSilkHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if err := packet.WriteError(); err != nil {
		return err
	}
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func premiumSilkError(err error) uint16 {
	if err == nil {
		return 0
	}
	if errors.Is(err, model.ErrItemMallConflict) {
		return 1
	}
	return 2
}

func (h *PremiumSilkHandler) handleSilkHistory(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeSilkHistoryRequest(&packet)
	if err != nil || session == nil || session.UserContext.UserID == 0 {
		response, _ := vsro188.BuildSilkHistoryResponse(vsro188.SilkHistoryRequest{Mode: 1, Currency: 1}, nil, 1)
		return h.sendPacket(session, response)
	}
	entries, err := h.repository.SilkHistory(ctx, session.UserContext.UserID, model.ItemMallCurrency(request.Currency-1), vsro188.SilkHistoryLimit)
	if err != nil {
		response, _ := vsro188.BuildSilkHistoryResponse(request, nil, premiumSilkError(err))
		return h.sendPacket(session, response)
	}
	wire := make([]vsro188.SilkHistoryEntry, len(entries))
	for i, entry := range entries {
		wire[i] = vsro188.SilkHistoryEntry{Timestamp: utils.ToSilkroadTime(entry.Timestamp), Amount: entry.Amount}
	}
	response, err := vsro188.BuildSilkHistoryResponse(request, wire, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *PremiumSilkHandler) handleSilkUnknown(_ context.Context, session *server.Session, packet network.Packet) error {
	group, err := vsro188.DecodeSilkUnknownRequest(&packet)
	if err != nil {
		response, _ := vsro188.BuildSilkUnknownResponse(0, nil, 1)
		return h.sendPacket(session, response)
	}
	response, err := vsro188.BuildSilkUnknownResponse(group, nil, 0)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *PremiumSilkHandler) handlePremiumDecoration(ctx context.Context, session *server.Session, packet network.Packet) error {
	token, reference, err := vsro188.DecodePremiumDecorationUseRequest(&packet)
	if err != nil || token == 0 || reference == 0 || session == nil || session.UserContext.UniqueID == 0 {
		return h.sendPacket(session, vsro188.BuildPremiumDecorationUseResponse(0, 0, 1))
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session {
		return h.sendPacket(session, vsro188.BuildPremiumDecorationUseResponse(0, 0, 2))
	}
	previous := player.PremiumDecoration()
	if previous.Token != 0 {
		reset := vsro188.BuildPremiumDecorationReset(vsro188.PremiumDecoration{Token: previous.Token, Reference: previous.Reference, Value: previous.Value})
		if err = h.sendPacket(session, reset); err != nil {
			return err
		}
	}
	state := model.PremiumDecorationState{Token: token, Reference: reference, Value: 1}
	if h.decorations != nil {
		if err = h.decorations.SavePremiumDecoration(ctx, player.ID, state); err != nil {
			return h.sendPacket(session, vsro188.BuildPremiumDecorationUseResponse(0, 0, 2))
		}
	}
	player.SetPremiumDecoration(state)
	update := vsro188.BuildPremiumDecoration(vsro188.PremiumDecoration{Token: token, Reference: reference, Value: state.Value})
	if err = h.sendPacket(session, update); err != nil {
		return err
	}
	return h.sendPacket(session, vsro188.BuildPremiumDecorationUseResponse(token, reference, 0))
}
