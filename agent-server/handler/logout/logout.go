package logout

import (
	"context"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/sirupsen/logrus"
)

type LogoutHandler struct {
	// Logout and cancel were multiplexed by one legacy worker. Keep that
	// serialization while callbacks are dispatched from the session worker.
	mu        sync.Mutex
	countdown time.Duration
	pending   map[string]*logoutCountdown
	world     interface {
		GetPlayerByUniqueId(uint32) (*model.Player, error)
	}
}

type logoutCountdown struct {
	timer *time.Timer
	done  chan struct{}
}

func RegisterLogoutHandler(target *server.Server) {
	handler := newLogoutHandler()
	target.RegisterHandler(opcode.AgentLogoutRequest, handler.handleLogoutPacket)
	target.RegisterHandler(opcode.AgentLogoutCancelRequest, handler.handleCancelPacket)
}

func newLogoutHandler() *LogoutHandler {
	return &LogoutHandler{
		countdown: time.Second * time.Duration(CountdownTime),
		pending:   make(map[string]*logoutCountdown),
		world:     service.GetWorldServiceInstance(),
	}
}

const (
	LogoutModeExit byte = iota + 1
	LogoutModeRestart
	CountdownTime byte = 5
)

const (
	LogoutErrorInBattleState   uint16 = 0x0801
	LogoutErrorInTeleportState uint16 = 0x0802
)

func (h *LogoutHandler) handleLogoutPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.doLogout(session, packet)
}

func (h *LogoutHandler) handleCancelPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.doCancelLogout(session, packet)
}

func (h *LogoutHandler) doLogout(session *server.Session, packet network.Packet) error {
	if session == nil {
		return context.Canceled
	}
	logoutMode, err := packet.ReadByte()
	if err != nil {
		return err
	}
	if logoutMode != LogoutModeExit && logoutMode != LogoutModeRestart {
		return h.sendFailure(session, logoutMode, LogoutErrorInBattleState)
	}
	if session.LifecyclePhase() != server.SessionInWorld {
		return h.sendFailure(session, logoutMode, LogoutErrorInTeleportState)
	}
	player, lookupErr := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if lookupErr != nil || player == nil {
		return h.sendFailure(session, logoutMode, LogoutErrorInTeleportState)
	}
	if player.GetBattleState() == model.BattleBattle {
		return h.sendFailure(session, logoutMode, LogoutErrorInBattleState)
	}

	logrus.Debug("Logging out char")
	p := network.EmptyPacket()
	p.MessageID = opcode.AgentLogoutResponse
	// TODO: Evaluate the result based on current Player state
	var result byte = 1
	p.WriteByte(result)
	if result == 1 {
		p.WriteByte(CountdownTime) // Countdown in seconds
		p.WriteByte(logoutMode)
	} else if result == 2 {
		// TODO: Determine Error code
		p.WriteUInt16(LogoutErrorInBattleState)
	}
	if err := session.Send(p); err != nil {
		return err
	}

	h.cancelLocked(session.ID)
	uid := session.UserContext.UniqueID
	countdown := &logoutCountdown{done: make(chan struct{})}
	delay := h.countdown
	if delay <= 0 {
		delay = time.Second * time.Duration(CountdownTime)
	}
	countdown.timer = time.AfterFunc(delay, func() {
		// Claim the countdown under the same lock as cancellation. A late timer
		// cannot send a success after an explicit cancel or a session close.
		h.mu.Lock()
		if h.pending[session.ID] != countdown {
			h.mu.Unlock()
			return
		}
		delete(h.pending, session.ID)
		close(countdown.done)
		h.mu.Unlock()

		select {
		case <-session.Done():
			return
		default:
		}
		if uid != 0 {
			if err := service.GetWorldServiceInstance().FlushPlayerPosition(uid); err != nil {
				logrus.Warnf("failed to flush player position during logout: %v", err)
			}
		}
		p1 := network.EmptyPacket()
		p1.MessageID = opcode.AgentLogoutSuccess
		if err := session.Send(p1); err != nil {
			logrus.Debugf("failed to send logout success: %v", err)
		}
	})
	h.pending[session.ID] = countdown
	go h.cancelOnSessionClose(session, countdown)
	return nil
}

func (h *LogoutHandler) sendFailure(session *server.Session, mode byte, code uint16) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.AgentLogoutResponse
	p.WriteByte(2)
	p.WriteUInt16(code)
	if mode != 0 {
		logrus.Debugf("rejected logout mode %d with code 0x%04X", mode, code)
	}
	return session.Send(p)
}

func (h *LogoutHandler) doCancelLogout(session *server.Session, _ network.Packet) error {
	if session == nil {
		return context.Canceled
	}
	h.cancelLocked(session.ID)
	p := network.EmptyPacket()
	p.MessageID = opcode.AgentLogoutCancelResponse
	// TODO: Evaluate the result based on current Player state
	var result byte = 1
	p.WriteByte(result)
	if result == 2 {
		// TODO: Determine Error code
		p.WriteUInt16(LogoutErrorInBattleState)
	}
	return session.Send(p)
}

func (h *LogoutHandler) cancelOnSessionClose(session *server.Session, countdown *logoutCountdown) {
	select {
	case <-session.Done():
		h.mu.Lock()
		if h.pending[session.ID] == countdown {
			h.cancelLocked(session.ID)
		}
		h.mu.Unlock()
	case <-countdown.done:
	}
}

// cancelLocked must be called with h.mu held.
func (h *LogoutHandler) cancelLocked(sessionID string) {
	countdown := h.pending[sessionID]
	if countdown == nil {
		return
	}
	delete(h.pending, sessionID)
	if countdown.timer != nil {
		countdown.timer.Stop()
	}
	close(countdown.done)
}
