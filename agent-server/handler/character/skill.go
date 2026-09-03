package character

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

const (
	skillErrorInvalid      = uint16(0x0934)
	skillErrorMastery      = uint16(0x0234)
	skillErrorStrength     = uint16(0x0334)
	skillErrorIntellect    = uint16(0x0434)
	skillErrorAlreadyKnown = uint16(0x0C34)
	skillErrorPrerequisite = uint16(0x0734)
	skillErrorRace         = uint16(0x0534)
	skillErrorLevelChain   = uint16(0x0634)
	skillErrorSP           = uint16(0x0A34)
)

type SkillHandler struct {
	mu            sync.Mutex
	repository    model.SkillLearningRepository
	useCase       service.SkillUseCase
	referenceData *model.ReferenceDataCatalog
	world         masteryWorld
	send          func(*server.Session, network.Packet) error
}

func RegisterSkillHandler(target *server.Server, repository model.SkillLearningRepository) (*SkillHandler, error) {
	if target == nil || repository == nil {
		return nil, model.ErrCharacterSkillRepositoryUnavailable
	}
	handler := &SkillHandler{repository: repository, world: service.GetWorldServiceInstance(), send: sendMasteryPacket}
	target.RegisterHandler(opcode.SkillLearnRequest, handler.handlePacket)
	return handler, nil
}

func (h *SkillHandler) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) error {
	if h == nil || catalog == nil {
		return model.ErrReferenceDataRepositoryUnavailable
	}
	h.mu.Lock()
	h.useCase = service.NewSkillService(h.repository, catalog)
	h.referenceData = catalog
	h.mu.Unlock()
	return nil
}

func (h *SkillHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if packet.Remaining() != 4 {
		return fmt.Errorf("skill learn payload length %d, want 4", packet.Remaining())
	}
	skillID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	if h.useCase == nil {
		return h.sendPacket(session, buildSkillFailure(skillErrorInvalid))
	}
	player, err := (&MasteryHandler{world: h.world}).playerForSession(session)
	if err != nil {
		return h.sendPacket(session, buildSkillFailure(skillErrorInvalid))
	}
	result, err := h.useCase.LearnSkill(ctx, player, skillID)
	if err != nil {
		return h.sendPacket(session, buildSkillFailure(skillErrorCode(err)))
	}
	if err := h.sendPacket(session, buildSkillSuccess(result.Skill.ID)); err != nil {
		return err
	}
	if err := h.sendPacket(session, buildSkillPointBalance(result.SkillPoints)); err != nil {
		return err
	}
	if skill, exists := h.referenceData.Skill(result.Skill.ID); exists && skill.IsPassive() {
		return h.sendPacket(session, player.BuildStatsUpdatePacket())
	}
	return nil
}

func skillErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrSkillMasteryRequired):
		return skillErrorMastery
	case errors.Is(err, model.ErrSkillStrengthRequired):
		return skillErrorStrength
	case errors.Is(err, model.ErrSkillIntellectRequired):
		return skillErrorIntellect
	case errors.Is(err, model.ErrSkillRaceMismatch):
		return skillErrorRace
	case errors.Is(err, model.ErrSkillLevelChainInvalid):
		return skillErrorLevelChain
	case errors.Is(err, model.ErrSkillAlreadyLearned):
		return skillErrorAlreadyKnown
	case errors.Is(err, model.ErrSkillPrerequisiteRequired):
		return skillErrorPrerequisite
	case errors.Is(err, model.ErrSkillInsufficientSP):
		return skillErrorSP
	default:
		return skillErrorInvalid
	}
}

func buildSkillSuccess(skillID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SkillLearnResponse
	packet.WriteByte(1)
	packet.WriteUInt32(skillID)
	return packet
}

func buildSkillFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SkillLearnResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return packet
}

func (h *SkillHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h == nil || h.send == nil || session == nil {
		return model.ErrSkillConflict
	}
	return h.send(session, packet)
}
