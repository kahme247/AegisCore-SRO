package character

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const bodyStateErrorUnavailable uint16 = 0

type hwanWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type HwanHandler struct {
	mu         sync.Mutex
	world      hwanWorld
	repository model.HwanRepository
	now        func() time.Time
	schedule   func(time.Duration, func())
	send       func(*server.Session, network.Packet) error
}

func RegisterHwanHandler(target *server.Server, repository model.HwanRepository) (*HwanHandler, error) {
	if target == nil || repository == nil {
		return nil, model.ErrHwanRepositoryUnavailable
	}
	handler := &HwanHandler{
		world: service.GetWorldServiceInstance(), repository: repository, now: time.Now,
		schedule: func(delay time.Duration, fn func()) { time.AfterFunc(delay, fn) },
		send:     sendHwanPacket,
	}
	target.RegisterHandler(opcode.CharacterBodyStateRequest, handler.handleBodyState)
	return handler, nil
}

func (h *HwanHandler) handleBodyState(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	requested, err := packet.ReadByte()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("character body-state payload is invalid")
	}
	state, err := vsro188.TranslateClientBodyRequest(vsro188.ClientBodyRequest(requested))
	if err != nil || state != vsro188.BodyBerserker {
		return h.sendFailure(session, bodyStateErrorUnavailable)
	}
	player, ok := h.resolvePlayer(session)
	if !ok || player.GetLifeState() != model.Alive || player.GetBodyState() != model.Normal {
		return h.sendFailure(session, bodyStateErrorUnavailable)
	}
	snapshot, consumedPoints, err := h.repository.ConsumeBerserk(ctx, player.ID)
	if err != nil {
		return err
	}
	if consumedPoints == 0 {
		player.CommitHwanSnapshot(snapshot)
		return h.sendFailure(session, bodyStateErrorUnavailable)
	}
	player.CommitHwanSnapshot(snapshot)
	now := h.now()
	duration := player.BerserkDurationForPoints(consumedPoints, now)
	generation := player.ActivateBerserk(now.Add(duration))
	if generation == 0 {
		return fmt.Errorf("persisted Berserk activation could not be published")
	}

	response := network.EmptyPacket()
	response.MessageID = opcode.CharacterBodyStateResponse
	response.WriteByte(1)
	if err := h.sendPacket(session, response); err != nil {
		return err
	}
	body := model.BuildBodyStateUpdatePacket(player.GetUniqueID(), model.Berserker, model.BerserkEffectVariant(snapshot.Level))
	player.SendToSelfAndKnown(&body)
	player.SendMovementSpeedUpdate()
	points := model.BuildHwanPointUpdatePacket(snapshot.Points, player.GetUniqueID())
	if err := h.sendPacket(session, points); err != nil {
		return err
	}
	if h.schedule != nil {
		h.schedule(duration, func() { h.expire(player, generation) })
	}
	return nil
}

func (h *HwanHandler) expire(player *model.Player, generation uint64) {
	if player == nil || !player.ExpireBerserk(generation) {
		return
	}
	body := model.BuildBodyStateUpdatePacket(player.GetUniqueID(), model.Normal)
	player.SendToSelfAndKnown(&body)
	player.SendMovementSpeedUpdate()
}

// SetTitleLevel is the server-authoritative HWAN title mutation used by quest,
// item, and management integrations. The stock client has no unrestricted
// title-selection request; accepting a level directly from 0x70A7 would also
// allow clients to select privileged body modes.
func (h *HwanHandler) SetTitleLevel(ctx context.Context, player *model.Player, level byte) error {
	if h == nil || h.repository == nil {
		return model.ErrHwanRepositoryUnavailable
	}
	if player == nil || player.ID <= 0 || player.GetUniqueID() == 0 {
		return model.ErrHwanCharacterIDRequired
	}
	if err := h.repository.SetLevel(ctx, player.ID, level); err != nil {
		return err
	}
	player.CommitHwanLevel(level)
	packet := model.BuildHwanLevelUpdatePacket(player.GetUniqueID(), level)
	player.SendToSelfAndKnown(&packet)
	return nil
}

func (h *HwanHandler) resolvePlayer(session *server.Session) (*model.Player, bool) {
	if h == nil || h.world == nil || session == nil || session.UserContext.UniqueID == 0 {
		return nil, false
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	return player, err == nil && player != nil && player.Session == session && player.ID > 0
}

func (h *HwanHandler) sendFailure(session *server.Session, code uint16) error {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.CharacterBodyStateResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return h.sendPacket(session, packet)
}

func (h *HwanHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return sendHwanPacket(session, packet)
}

func sendHwanPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return fmt.Errorf("HWAN session is unavailable")
	}
	return session.Send(packet)
}
