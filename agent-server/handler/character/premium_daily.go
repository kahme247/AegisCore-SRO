package character

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

type premiumDailyPlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type premiumDailyHandler struct {
	players    premiumDailyPlayerLookup
	repository model.ItemServiceRepository
	now        func() time.Time
}

type premiumDailyUseRequest struct {
	EntryKey   uint32
	EntryValue uint32
	Mode       byte
	ExtraValue uint32
}

func RegisterPremiumDailyHandler(target *server.Server, repository model.ItemServiceRepository) {
	if target != nil {
		handler := &premiumDailyHandler{players: service.GetWorldServiceInstance(), repository: repository, now: func() time.Time { return time.Now().UTC() }}
		target.RegisterHandler(opcode.PremiumDailyUseRequest, handler.handleRequest)
	}
}

func (h *premiumDailyHandler) handleRequest(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := readPremiumDailyUseRequest(packet)
	if err != nil {
		return err
	}
	logrus.WithFields(logrus.Fields{
		"entry_key":   request.EntryKey,
		"entry_value": request.EntryValue,
		"mode":        request.Mode,
		"extra_value": request.ExtraValue,
	}).Info("received Premium daily action request")

	if h == nil || h.players == nil || h.repository == nil || session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return fmt.Errorf("Premium daily activation dependencies or session identity are unavailable")
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return fmt.Errorf("resolve Premium daily player: %w", err)
	}
	if player == nil || player.ID != session.UserContext.CharacterID || player.GetUniqueID() != session.UserContext.UniqueID || player.GetSession() != session {
		return fmt.Errorf("Premium daily session does not own the resolved player")
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now().UTC()
	}
	effect, exists := player.PremiumDailyState(now, request.EntryKey, request.EntryValue)
	if !exists || request.Mode != 0 || !premiumDailyAvailable(effect, now) {
		return nil
	}
	activeUntil, err := h.repository.ActivateItemServiceDaily(ctx, int64(player.ID), effect.Kind, now)
	if errors.Is(err, model.ErrItemServiceDailyUnavailable) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("activate Premium daily service: %w", err)
	}
	if !player.ActivateItemServiceDaily(effect.Kind, request.EntryValue, now, activeUntil) {
		return fmt.Errorf("publish Premium daily activation to runtime player")
	}
	response := model.BuildPremiumDailyUseSuccess(request.EntryKey, request.EntryValue)
	return session.Send(response)
}

func premiumDailyAvailable(effect model.ItemServiceEffect, now time.Time) bool {
	usedOn := effect.DailyUsedOn.UTC()
	now = now.UTC()
	return usedOn.IsZero() || usedOn.Year() != now.Year() || usedOn.YearDay() != now.YearDay()
}

func readPremiumDailyUseRequest(packet network.Packet) (premiumDailyUseRequest, error) {
	var request premiumDailyUseRequest
	if packet.Remaining() != 8 && packet.Remaining() != 9 && packet.Remaining() != 13 {
		return request, fmt.Errorf("Premium daily request payload length %d, want 8, 9, or 13", packet.Remaining())
	}
	var err error
	request.EntryKey, err = packet.ReadUInt32()
	if err != nil {
		return request, fmt.Errorf("read Premium daily entry key: %w", err)
	}
	request.EntryValue, err = packet.ReadUInt32()
	if err != nil {
		return request, fmt.Errorf("read Premium daily entry value: %w", err)
	}
	if packet.Remaining() == 0 {
		return request, nil
	}
	request.Mode, err = packet.ReadByte()
	if err != nil {
		return request, fmt.Errorf("read Premium daily action mode: %w", err)
	}
	if request.Mode == 0 {
		return request, fmt.Errorf("Premium daily request includes zero action mode")
	}
	if request.Mode == 7 {
		if packet.Remaining() != 4 {
			return request, fmt.Errorf("Premium daily mode 7 trailing payload length %d, want 4", packet.Remaining())
		}
		request.ExtraValue, err = packet.ReadUInt32()
		if err != nil {
			return request, fmt.Errorf("read Premium daily extra value: %w", err)
		}
	}
	if packet.Remaining() != 0 {
		return request, fmt.Errorf("Premium daily request has %d trailing bytes", packet.Remaining())
	}
	return request, nil
}
