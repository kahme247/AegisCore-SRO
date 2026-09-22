package character

import (
	"context"
	"errors"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	freePVPUnavailable uint16 = 0x8006
	freePVPJobState    uint16 = 0x8007
)

type FreePVPHandler struct {
	players interface {
		GetPlayerByUniqueId(uint32) (*model.Player, error)
	}
	send        func(*server.Session, network.Packet) error
	notify      func(*model.Player, network.Packet)
	competitive *service.CompetitiveRuntime
}

func RegisterFreePVPHandler(target *server.Server, runtimes ...*service.CompetitiveRuntime) *FreePVPHandler {
	if target == nil {
		return nil
	}
	h := &FreePVPHandler{players: service.GetWorldServiceInstance()}
	if len(runtimes) > 0 {
		h.competitive = runtimes[0]
		h.competitive.ConfigureObserver(h)
	}
	target.RegisterHandler(opcode.FRPVPUpdateRequest, h.handleUpdate)
	target.RegisterHandler(opcode.FlagWarRegisterRequest, h.handleFlagWarRegister)
	target.RegisterHandler(opcode.BattleArenaRequest, h.handleBattleArenaRequest)
	return h
}

func (h *FreePVPHandler) CompetitiveMatchStarted(match service.CompetitiveMatch) {
	if match.Event == service.CompetitiveFlagWar {
		packet, err := vsro188.BuildFlagWarUpdate(vsro188.FlagWarBegin)
		if err == nil {
			h.sendToMatch(match, packet)
		}
		return
	}
	packet, err := vsro188.BuildBattleArenaOperation(vsro188.BattleArenaOperation{Operation: 8, MaxTime: uint32(match.EndsAt.Sub(match.StartedAt) / time.Millisecond)})
	if err == nil {
		h.sendToMatch(match, packet)
	}
	h.CompetitiveScoreUpdated(match)
}

func (h *FreePVPHandler) CompetitiveScoreUpdated(match service.CompetitiveMatch) {
	if match.Event != service.CompetitiveBattleArena {
		return
	}
	ranks := make([]vsro188.BattleArenaRank, 0, len(match.Teams[0])+len(match.Teams[1]))
	for team, entries := range match.Teams {
		for _, entry := range entries {
			ranks = append(ranks, vsro188.BattleArenaRank{Team: byte(team), Name: entry.Name, Points: match.Scores[team]})
		}
	}
	packet, err := vsro188.BuildBattleArenaOperation(vsro188.BattleArenaOperation{Operation: 0xff, Update: 0x41, RedPoints: match.Scores[0], BluePoints: match.Scores[1], Ranks: ranks})
	if err == nil {
		h.sendToMatch(match, packet)
	}
}

func (h *FreePVPHandler) CompetitiveMatchFinished(match service.CompetitiveMatch) {
	if match.Event == service.CompetitiveFlagWar {
		packet, err := vsro188.BuildFlagWarUpdate(vsro188.FlagWarEnded)
		if err == nil {
			h.sendToMatch(match, packet)
		}
		return
	}
	for team, entries := range match.Teams {
		update := byte(0x80)
		if match.Scores[team] > match.Scores[1-team] {
			update = 0x81
		}
		packet, err := vsro188.BuildBattleArenaOperation(vsro188.BattleArenaOperation{Operation: 0xff, Update: update})
		if err == nil {
			h.sendToEntries(entries, packet)
		}
	}
}

func (h *FreePVPHandler) sendToMatch(match service.CompetitiveMatch, packet network.Packet) {
	for _, entries := range match.Teams {
		h.sendToEntries(entries, packet)
	}
}

func (h *FreePVPHandler) sendToEntries(entries []service.CompetitiveEntry, packet network.Packet) {
	for _, entry := range entries {
		player, err := h.players.GetPlayerByUniqueId(entry.UniqueID)
		if err == nil && player != nil && player.Session != nil {
			_ = h.sendPacket(player.Session, packet)
		}
	}
}

func (h *FreePVPHandler) handleFlagWarRegister(_ context.Context, session *server.Session, packet network.Packet) error {
	if err := vsro188.DecodeFlagWarRegisterRequest(&packet); err != nil {
		return err
	}
	if h.competitive != nil {
		player, err := h.sessionPlayer(session)
		if err == nil {
			err = h.competitive.Register(service.CompetitiveFlagWar, competitiveEntry(player))
		}
		if err == nil {
			return h.sendPacket(session, vsro188.BuildFlagWarOperationResult(0))
		}
	}
	return h.sendPacket(session, vsro188.BuildFlagWarUnavailable())
}

func (h *FreePVPHandler) handleBattleArenaRequest(_ context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeBattleArenaRequest(&packet)
	if err != nil {
		return err
	}
	if h.competitive != nil {
		player, playerErr := h.sessionPlayer(session)
		if playerErr == nil {
			if request.Register {
				playerErr = h.competitive.Register(service.CompetitiveBattleArena, service.CompetitiveEntry{CharacterID: player.ID, UniqueID: player.GetUniqueID(), Name: player.GetName(), GameType: request.GameType, MatchType: request.MatchType})
			} else {
				playerErr = h.competitive.Cancel(service.CompetitiveBattleArena, player.ID)
			}
		}
		if playerErr == nil {
			response, buildErr := vsro188.BuildBattleArenaOperation(vsro188.BattleArenaOperation{Operation: 0xff, Update: 0, RequestType: map[bool]byte{true: 1, false: 2}[request.Register], GameType: request.GameType})
			if buildErr != nil {
				return buildErr
			}
			return h.sendPacket(session, response)
		}
	}
	response, err := vsro188.BuildBattleArenaUnavailable()
	if err != nil {
		return err
	}
	return h.sendPacket(session, response)
}

func (h *FreePVPHandler) sessionPlayer(session *server.Session) (*model.Player, error) {
	if h == nil || h.players == nil || session == nil || session.UserContext.UniqueID == 0 {
		return nil, errors.New("competitive session is unavailable")
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID <= 0 {
		return nil, errors.New("competitive player is unavailable")
	}
	return player, nil
}

func competitiveEntry(player *model.Player) service.CompetitiveEntry {
	return service.CompetitiveEntry{CharacterID: player.ID, UniqueID: player.GetUniqueID(), Name: player.GetName()}
}

func (h *FreePVPHandler) handleUpdate(_ context.Context, session *server.Session, packet network.Packet) error {
	mode, err := vsro188.DecodeFRPVPUpdateRequest(&packet)
	if err != nil || session == nil || session.UserContext.UniqueID == 0 {
		return h.sendPacket(session, vsro188.BuildFRPVPUpdateFailure(freePVPUnavailable))
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session {
		return h.sendPacket(session, vsro188.BuildFRPVPUpdateFailure(freePVPUnavailable))
	}
	if model.HasEquippedJobSuit(player) {
		return h.sendPacket(session, vsro188.BuildFRPVPUpdateFailure(freePVPJobState))
	}
	if !player.SetPVPCape(model.PVPCape(mode)) {
		return h.sendPacket(session, vsro188.BuildFRPVPUpdateFailure(freePVPUnavailable))
	}
	response, err := vsro188.BuildFRPVPUpdateSuccess(player.GetUniqueID(), mode)
	if err != nil {
		return err
	}
	if h.notify != nil {
		h.notify(player, response)
	} else {
		player.SendToSelfAndKnown(&response)
	}
	return nil
}

func (h *FreePVPHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return nil
	}
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
