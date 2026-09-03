package character

import (
	"context"
	"errors"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	restorationErrorInvalid    uint16 = 0x7801
	restorationErrorState      uint16 = 0x7803
	restorationErrorGold       uint16 = 0x7804
	restorationErrorDependency uint16 = 0x7806
)

type SkillRestorationHandler struct {
	mu         sync.Mutex
	repository model.SkillRestorationRepository
	levels     *model.LevelTable
	useCase    *service.SkillRestorationService
	catalog    *model.ReferenceDataCatalog
	world      masteryWorld
	send       func(*server.Session, network.Packet) error
}

func RegisterSkillRestorationHandler(target *server.Server, repository model.SkillRestorationRepository, levels *model.LevelTable) (*SkillRestorationHandler, error) {
	if target == nil || repository == nil || levels == nil {
		return nil, model.ErrSkillRestorationRepositoryAbsent
	}
	handler := &SkillRestorationHandler{repository: repository, levels: levels, world: service.GetWorldServiceInstance(), send: sendMasteryPacket}
	target.RegisterHandler(opcode.SkillWithdrawRequest, handler.handleSkillPacket)
	target.RegisterHandler(opcode.MasteryWithdrawRequest, handler.handleMasteryPacket)
	return handler, nil
}

func (h *SkillRestorationHandler) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) error {
	if h == nil || catalog == nil {
		return model.ErrReferenceDataRepositoryUnavailable
	}
	h.mu.Lock()
	h.catalog = catalog
	h.useCase = service.NewSkillRestorationService(h.repository, catalog, h.levels)
	h.mu.Unlock()
	return nil
}

func (h *SkillRestorationHandler) handleSkillPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	request, err := vsro188.DecodeSkillRestorationRequest(&packet)
	if err != nil {
		return err
	}
	player, err := (&MasteryHandler{world: h.world}).playerForSession(session)
	if err != nil || h.useCase == nil {
		return h.sendPacket(session, vsro188.BuildSkillRestorationFailure(opcode.SkillWithdrawResponse, restorationErrorInvalid))
	}
	current, currentExists := h.catalog.Skill(request.TargetID)
	result, err := h.useCase.WithdrawSkill(ctx, player, request.ItemRefID, request.TargetID, request.TargetLevel)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildSkillRestorationFailure(opcode.SkillWithdrawResponse, restorationErrorCode(err)))
	}
	if err := h.sendPacket(session, vsro188.BuildSkillWithdrawSuccess(result.ResultingSkillID)); err != nil {
		return err
	}
	if err := h.sendPacket(session, buildRestorationItemQuantity(result.ItemSlot, result.ItemRemaining)); err != nil {
		return err
	}
	if err := h.sendPacket(session, buildSkillPointBalance(result.SkillPoints)); err != nil {
		return err
	}
	if result.GoldCharged != 0 {
		if err := h.sendPacket(session, buildRestorationGoldUpdate(result.Gold)); err != nil {
			return err
		}
	}
	resulting, resultingExists := h.catalog.Skill(result.ResultingSkillID)
	if currentExists && current.IsPassive() || resultingExists && resulting.IsPassive() {
		return h.sendPacket(session, player.BuildStatsUpdatePacket())
	}
	return nil
}

func (h *SkillRestorationHandler) handleMasteryPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	request, err := vsro188.DecodeSkillRestorationRequest(&packet)
	if err != nil {
		return err
	}
	player, err := (&MasteryHandler{world: h.world}).playerForSession(session)
	if err != nil || h.useCase == nil {
		return h.sendPacket(session, vsro188.BuildSkillRestorationFailure(opcode.MasteryWithdrawResponse, restorationErrorInvalid))
	}
	result, err := h.useCase.WithdrawMastery(ctx, player, request.ItemRefID, request.TargetID, request.TargetLevel)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildSkillRestorationFailure(opcode.MasteryWithdrawResponse, restorationErrorCode(err)))
	}
	if err := h.sendPacket(session, vsro188.BuildMasteryWithdrawSuccess(result.Mastery.ID, result.Mastery.Level)); err != nil {
		return err
	}
	if err := h.sendPacket(session, buildRestorationItemQuantity(result.ItemSlot, result.ItemRemaining)); err != nil {
		return err
	}
	if err := h.sendPacket(session, buildSkillPointBalance(result.SkillPoints)); err != nil {
		return err
	}
	if result.GoldCharged != 0 {
		return h.sendPacket(session, buildRestorationGoldUpdate(result.Gold))
	}
	return nil
}

func restorationErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrSkillRestorationDependency):
		return restorationErrorDependency
	case errors.Is(err, model.ErrSkillRestorationInvalid):
		return restorationErrorInvalid
	case errors.Is(err, model.ErrSkillRestorationGold):
		return restorationErrorGold
	default:
		return restorationErrorState
	}
}

func buildRestorationGoldUpdate(gold uint64) network.Packet {
	return vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold})
}

func buildRestorationItemQuantity(slot byte, remaining uint16) network.Packet {
	return vsro188.BuildInventoryItemQuantityUpdate(slot, remaining)
}

func (h *SkillRestorationHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h == nil || h.send == nil || session == nil {
		return model.ErrSkillRestorationConflict
	}
	return h.send(session, packet)
}
