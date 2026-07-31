package character

import (
	"context"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

const (
	operatorPositionLookup   uint16 = 1
	operatorToTown           uint16 = 3
	operatorStatistics       uint16 = 4
	operatorPlayerStatistics uint16 = 5
	operatorRecallUser       uint16 = 17
	operatorPunishToggle     uint16 = 0x37
	operatorDenied           uint16 = 0x1401
	operatorTargetMissing    uint16 = 0x1402
	operatorUnsupported      uint16 = 0x1406
)

type operatorWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetPlayerByCharName(string) (*model.Player, error)
	OperatorStatistics() (uint32, uint32, uint32)
}

type operatorRecall interface {
	RecallPlayer(*model.Player, *model.Player) error
}

type operatorTownTeleport interface {
	TeleportPlayerTo(*model.Player, model.TeleportDestination) error
}

type OperatorHandler struct {
	world        operatorWorld
	repository   model.OperatorPunishmentRepository
	recall       operatorRecall
	returnTown   model.ReturnTeleportDestinationRepository
	townTeleport operatorTownTeleport
	send         func(*server.Session, network.Packet) error
	now          func() time.Time
}

func RegisterOperatorHandler(target *server.Server, world operatorWorld, repository model.OperatorPunishmentRepository) (*OperatorHandler, error) {
	if target == nil || world == nil || repository == nil {
		return nil, fmt.Errorf("configure operator handler: dependencies are required")
	}
	handler := &OperatorHandler{world: world, repository: repository, now: time.Now}
	target.RegisterHandler(opcode.OperatorRequest, handler.handlePacket)
	return handler, nil
}

func (h *OperatorHandler) ConfigureRecall(recall operatorRecall) {
	if h != nil {
		h.recall = recall
	}
}

func (h *OperatorHandler) ConfigureToTown(returnTown model.ReturnTeleportDestinationRepository, teleport operatorTownTeleport) {
	if h != nil {
		h.returnTown = returnTown
		h.townTeleport = teleport
	}
}

func (h *OperatorHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeOperatorRequest(&packet)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	actor, err := h.authorizedPlayer(session)
	if err != nil || !actor.IsGm {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorDenied))
	}
	switch request.Command {
	case operatorPositionLookup:
		return h.positionLookup(session, request)
	case operatorToTown:
		return h.toTown(ctx, session, request)
	case operatorPlayerStatistics:
		return h.playerStatistics(session, request)
	case operatorStatistics:
		if len(request.Payload) != 0 {
			return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
		}
		players, npcs, dropped := h.world.OperatorStatistics()
		body := network.EmptyPacket()
		body.WriteUInt32(players)
		body.WriteUInt32(npcs)
		body.WriteUInt32(dropped)
		return h.sendSuccess(session, request.Command, body.Payload())
	case operatorPunishToggle:
		return h.punishToggle(ctx, session, actor, request)
	case operatorRecallUser:
		return h.recallUser(session, actor, request)
	default:
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
}

func (h *OperatorHandler) punishToggle(ctx context.Context, session *server.Session, actor *model.Player, request vsro188.OperatorRequest) error {
	name, err := decodeOperatorString(request.Payload)
	if err != nil || h.repository == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	target, err := h.world.GetPlayerByCharName(name)
	if err != nil || target == nil || target.Session == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorTargetMissing))
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now().UTC()
	}
	result, err := h.repository.ToggleOperatorPunishment(ctx, actor.ID, target.ID, now)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorDenied))
	}
	state := byte(0)
	if result.Active {
		state = 1
	}
	notification, err := vsro188.BuildOperatorPunishment(state, vsro188.PackSRODateMinute(result.ChangedAt))
	if err != nil {
		return err
	}
	if err = h.sendPacket(target.Session, notification); err != nil {
		return err
	}
	return h.sendSuccess(session, request.Command, nil)
}

func (h *OperatorHandler) recallUser(session *server.Session, actor *model.Player, request vsro188.OperatorRequest) error {
	name, err := decodeOperatorString(request.Payload)
	if err != nil || h.recall == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	target, err := h.world.GetPlayerByCharName(name)
	if err != nil || target == nil || target.Session == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorTargetMissing))
	}
	if err = h.recall.RecallPlayer(actor, target); err != nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	return h.sendSuccess(session, request.Command, nil)
}

func (h *OperatorHandler) toTown(ctx context.Context, session *server.Session, request vsro188.OperatorRequest) error {
	name, err := decodeOperatorString(request.Payload)
	if err != nil || h.returnTown == nil || h.townTeleport == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	target, err := h.world.GetPlayerByCharName(name)
	if err != nil || target == nil || target.Session == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorTargetMissing))
	}
	destination, err := h.returnTown.ReturnTeleportDestination(ctx, target.ID)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"gm_uid": session.UserContext.UniqueID, "target": name, "target_id": target.ID,
		}).Warn("GM ToTown could not resolve target return point")
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorTargetMissing))
	}
	if err = h.townTeleport.TeleportPlayerTo(target, destination); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"gm_uid": session.UserContext.UniqueID, "target": name, "target_id": target.ID,
			"region": destination.Region, "world": destination.WorldID,
		}).Warn("GM ToTown could not stage target teleport")
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	logrus.WithFields(logrus.Fields{
		"gm_uid": session.UserContext.UniqueID, "target": name, "target_id": target.ID,
		"region": destination.Region, "world": destination.WorldID,
	}).Info("GM ToTown staged target teleport")
	return h.sendSuccess(session, request.Command, nil)
}

func (h *OperatorHandler) playerStatistics(session *server.Session, request vsro188.OperatorRequest) error {
	name, err := decodeOperatorString(request.Payload)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	target, err := h.world.GetPlayerByCharName(name)
	if err != nil || target == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorTargetMissing))
	}
	stats := target.DerivedStats()
	hp, mp := target.CurrentVitals()
	target.Mutex.Lock()
	level := target.Level
	target.Mutex.Unlock()
	logrus.Infof(
		"GM Stat of %s >> Level: %d, Str: %d, Int: %d, HP: %d/%d MP: %d/%d",
		target.CharName, level, stats.Strength, stats.Intellect, hp, stats.MaxHP, mp, stats.MaxMP,
	)
	// SR_GameServer.exe +0x11DE90 (case 5) sends only byte success=1 and the
	// u16 selector. The formatted statistics are server-side diagnostics;
	// appending text makes the stock client read an invalid response layout.
	return h.sendSuccess(session, request.Command, nil)
}

func (h *OperatorHandler) positionLookup(session *server.Session, request vsro188.OperatorRequest) error {
	name, err := decodeOperatorString(request.Payload)
	if err != nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorUnsupported))
	}
	target, err := h.world.GetPlayerByCharName(name)
	if err != nil || target == nil {
		return h.sendPacket(session, vsro188.BuildOperatorFailure(operatorTargetMissing))
	}
	position := target.GetPosition()
	message := fmt.Sprintf("Pos of %s: %d (%.2f, %.2f, %.2f)", target.CharName, position.Region.ID, position.X, position.Y, position.Z)
	body := network.EmptyPacket()
	if err = body.WriteString(message); err != nil {
		return err
	}
	return h.sendSuccess(session, request.Command, body.Payload())
}

func (h *OperatorHandler) authorizedPlayer(session *server.Session) (*model.Player, error) {
	if session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return nil, fmt.Errorf("invalid operator session")
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return nil, fmt.Errorf("resolve operator player: %w", err)
	}
	return player, nil
}

func decodeOperatorString(payload []byte) (string, error) {
	packet := network.EmptyClientPacket()
	packet.WriteBytes(payload)
	value, err := packet.ReadString()
	if err != nil || value == "" || packet.Remaining() != 0 {
		return "", fmt.Errorf("invalid operator string")
	}
	return value, nil
}

func (h *OperatorHandler) sendSuccess(session *server.Session, command uint16, payload []byte) error {
	response, err := vsro188.BuildOperatorSuccess(command, payload)
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *OperatorHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
