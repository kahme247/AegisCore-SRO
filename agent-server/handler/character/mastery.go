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
	masteryResultSuccess = byte(1)
	masteryResultFailure = byte(2)

	masteryErrorInvalid      = uint16(0x0138)
	masteryErrorInsufficient = uint16(0x0238)
	masteryErrorLevelLimit   = uint16(0x0438)
	masteryErrorTotalLimit   = uint16(0x0538)
)

type masteryWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type MasteryHandler struct {
	mu         sync.Mutex
	repository model.MasteryRepository
	levels     *model.LevelTable
	useCase    service.MasteryUseCase
	world      masteryWorld
	send       func(*server.Session, network.Packet) error
}

func RegisterMasteryHandler(target *server.Server, repository model.MasteryRepository, levels *model.LevelTable) error {
	if target == nil || repository == nil || levels == nil {
		return model.ErrMasteryRepositoryUnavailable
	}
	handler := &MasteryHandler{
		repository: repository,
		levels:     levels,
		useCase:    service.NewMasteryService(repository, levels),
		world:      service.GetWorldServiceInstance(),
		send:       sendMasteryPacket,
	}
	target.RegisterHandler(opcode.SkillMasteryLearnRequest, handler.handlePacket)
	return nil
}

func (h *MasteryHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if packet.Remaining() != 5 {
		return fmt.Errorf("mastery learn payload length %d, want 5", packet.Remaining())
	}
	masteryID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	increment, err := packet.ReadByte()
	if err != nil {
		return err
	}
	if increment != 1 {
		return h.sendFailure(session, masteryErrorInvalid)
	}
	player, err := h.playerForSession(session)
	if err != nil {
		return h.sendFailure(session, masteryErrorInvalid)
	}
	useCase := h.useCase
	if useCase == nil {
		// Compatibility for focused tests and older internal callers that build a
		// handler literal with the historical repository/level fields.
		useCase = service.NewMasteryService(h.repository, h.levels)
	}
	result, err := useCase.IncreaseMastery(ctx, player, masteryID)
	if err != nil {
		return h.sendFailure(session, masteryErrorCode(err))
	}
	if err := h.sendPacket(session, buildMasterySuccess(result.Mastery)); err != nil {
		return err
	}
	return h.sendPacket(session, buildSkillPointBalance(result.SkillPoints))
}

func (h *MasteryHandler) playerForSession(session *server.Session) (*model.Player, error) {
	if session == nil || h == nil || h.world == nil || session.UserContext.UniqueID == 0 {
		return nil, model.ErrMasteryConflict
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID <= 0 {
		return nil, model.ErrMasteryConflict
	}
	return player, nil
}

func masteryErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrMasteryInsufficientSP):
		return masteryErrorInsufficient
	case errors.Is(err, model.ErrMasteryLevelLimit):
		return masteryErrorLevelLimit
	case errors.Is(err, model.ErrMasteryTotalLimit):
		return masteryErrorTotalLimit
	case errors.Is(err, model.ErrMasteryInvalid):
		return masteryErrorInvalid
	default:
		return masteryErrorInvalid
	}
}

func buildMasterySuccess(mastery model.MasteryProgress) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SkillMasteryLearnResponse
	packet.WriteByte(masteryResultSuccess)
	packet.WriteUInt32(mastery.ID)
	packet.WriteByte(mastery.Level)
	return packet
}

func buildMasteryFailure(code uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.SkillMasteryLearnResponse
	packet.WriteByte(masteryResultFailure)
	packet.WriteUInt16(code)
	return packet
}

func buildSkillPointBalance(skillPoints uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdatePoints
	packet.WriteByte(2)
	packet.WriteUInt32(skillPoints)
	// vSRO 1.88 always terminates an SP update with the notification flag.
	// Mastery learning spends SP, so update the balance without displaying the
	// SP-recovery message.
	packet.WriteByte(0)
	return packet
}

func (h *MasteryHandler) sendFailure(session *server.Session, code uint16) error {
	return h.sendPacket(session, buildMasteryFailure(code))
}

func (h *MasteryHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h == nil || h.send == nil || session == nil {
		return model.ErrMasteryConflict
	}
	return h.send(session, packet)
}

func sendMasteryPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return model.ErrMasteryConflict
	}
	return session.Send(packet)
}
