package character

import (
	"context"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

// gameReadyPlayerLookup is intentionally limited to the lookup GameReady
// needs. It keeps the lifecycle transition testable without adding another
// world mutation path outside the authoritative world service.
type gameReadyPlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

// gameReadyVisibilityActivator keeps GameReady dependent on the world-domain
// transition rather than on a concrete WorldService. Visibility changes enter
// each observer's known-object queue and SpawnManager delivers them.
type gameReadyVisibilityActivator interface {
	ActivatePlayer(*model.Player) []service.VisibilityDelta
}

// GameReadyHandler confirms the client has received the character bootstrap.
// A staged player remains Spawning until this one-way confirmation succeeds.
type GameReadyHandler struct {
	players gameReadyPlayerLookup
	mall    model.ItemMallRepository
	cos     interface {
		RehydratePlayer(context.Context, *model.Player) error
	}
	social interface {
		PreparePlayer(context.Context, *model.Player) error
		Bootstrap(context.Context, *server.Session) error
	}
	countdowns interface {
		Rehydrate(context.Context, *server.Session, *model.Player) error
	}
}

func (h *GameReadyHandler) ConfigureCOS(runtime interface {
	RehydratePlayer(context.Context, *model.Player) error
}) {
	h.cos = runtime
}

func (h *GameReadyHandler) ConfigureSocial(runtime interface {
	PreparePlayer(context.Context, *model.Player) error
	Bootstrap(context.Context, *server.Session) error
}) {
	h.social = runtime
}

func (h *GameReadyHandler) ConfigureQuestCountdowns(runtime interface {
	Rehydrate(context.Context, *server.Session, *model.Player) error
}) {
	h.countdowns = runtime
}

func NewGameReadyHandler(players gameReadyPlayerLookup, mall ...model.ItemMallRepository) *GameReadyHandler {
	handler := &GameReadyHandler{players: players}
	if len(mall) > 0 {
		handler.mall = mall[0]
	}
	return handler
}

// RegisterGameReadyHandler installs GameReady directly on the Agent server's
// per-session registry. The old PacketChannel path was best-effort and left
// the opcode unregistered, so a client could never complete the intended
// loading lifecycle deterministically.
func RegisterGameReadyHandler(target *server.Server, mall ...model.ItemMallRepository) *GameReadyHandler {
	if target == nil {
		return nil
	}
	handler := NewGameReadyHandler(nil, mall...)
	target.RegisterHandler(opcode.GameReadyRequest, handler.handleSessionPacket)
	return handler
}

func (h *GameReadyHandler) handleSessionPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if session == nil {
		return fmt.Errorf("GameReady request has no session")
	}
	if session.LifecyclePhase() != server.SessionCharacterLoading {
		return fmt.Errorf("GameReady request arrived while session is %s", session.LifecyclePhase())
	}
	// No sanitized capture establishes a payload structure beyond the opcode.
	// The legacy implementation read no fields; accept the documented empty
	// request only rather than guessing at optional fields.
	if _, err := (vsro188.GameReadyRequestCodec{}).Decode(packet); err != nil {
		return err
	}
	if session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 || session.UserContext.CharName == "" {
		return fmt.Errorf("GameReady request has no staged character")
	}

	world := h.playerLookup()
	player, err := world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return fmt.Errorf("GameReady player lookup: %w", err)
	}
	if player == nil || player.Session != session || player.ID != session.UserContext.CharacterID || player.CharName != session.UserContext.CharName {
		return fmt.Errorf("GameReady session does not own staged character")
	}
	if player.GetLifeState() != model.Spawning {
		return fmt.Errorf("GameReady staged player is not spawning")
	}
	var balances model.ItemMallBalances
	if h.mall != nil {
		balances, err = h.mall.Balances(ctx, session.UserContext.UserID)
		if err != nil {
			return fmt.Errorf("load item mall balances: %w", err)
		}
	}
	// Durable state that participates in the player's spawn encoding must be
	// loaded before visibility activation. Otherwise observers can receive an
	// entering job character as no-job until the later social bootstrap.
	if h.social != nil {
		if err := h.social.PreparePlayer(ctx, player); err != nil {
			return fmt.Errorf("prepare social state before GameReady: %w", err)
		}
	}
	if !session.CompleteCharacterLoading() {
		return fmt.Errorf("GameReady could not complete character loading")
	}

	if activator, ok := world.(gameReadyVisibilityActivator); ok {
		_ = activator.ActivatePlayer(player)
	} else {
		// Focused handler tests and small integrations may supply a lookup-only
		// fake. Preserve the lifecycle invariant there without bypassing the
		// real world's serialized activation path in production.
		if player.CurrentHP() == 0 {
			player.SetLifeState(model.Dead)
		} else {
			player.SetLifeState(model.Alive)
		}
	}
	// Character data is sent while the player is staged as Spawning. Publish
	// the completed state for living characters too: the stock client keeps
	// that staged state locally and its auto-potion gate requires the low alive
	// bit before it will activate a configured quick slot.
	lifeState := player.GetLifeState()
	if lifeState != model.Alive && lifeState != model.Dead {
		return fmt.Errorf("GameReady activation left player in life state %d", lifeState)
	}
	state := model.BuildLifeStateUpdatePacket(player.GetUniqueID(), lifeState)
	if err := session.Send(state); err != nil {
		return fmt.Errorf("send loaded life state: %w", err)
	}
	// The bootstrap carries current HP/MP, while the stock client initializes
	// the maxima used by auto-potion from 0x303D. Send the authoritative derived
	// stats after activation so both threshold values are calculated before the
	// first vitality update; otherwise MP can remain at a zero threshold until
	// an unrelated stat-changing action refreshes it.
	stats := player.BuildStatsUpdatePacket()
	if err := session.Send(stats); err != nil {
		return fmt.Errorf("send loaded character stats: %w", err)
	}
	if h.mall != nil {
		balancePacket := vsro188.BuildBalanceStateUpdate(vsro188.BalanceStateUpdate{
			First:  balances.Silk,
			Second: balances.GiftSilk,
			Third:  balances.Point,
		})
		if err := session.Send(balancePacket); err != nil {
			return fmt.Errorf("send item mall balances: %w", err)
		}
	}
	if h.cos != nil {
		if err := h.cos.RehydratePlayer(ctx, player); err != nil {
			return fmt.Errorf("rehydrate COS after GameReady: %w", err)
		}
	}
	if h.social != nil {
		if err := h.social.Bootstrap(ctx, session); err != nil {
			return fmt.Errorf("bootstrap social systems after GameReady: %w", err)
		}
	}
	if h.countdowns != nil {
		if err := h.countdowns.Rehydrate(ctx, session, player); err != nil {
			return fmt.Errorf("rehydrate quest countdowns after GameReady: %w", err)
		}
	}
	publishRestoredItemServices(player)
	publishRestoredActiveBuffs(player)
	logrus.Debugf("player %s's client is ready", player.CharName)
	return nil
}

func publishRestoredActiveBuffs(player *model.Player) {
	if player == nil {
		return
	}
	for _, buff := range player.ActiveBuffSnapshot() {
		if buff.Token == 0 {
			continue
		}
		if buff.ExpiresAt.IsZero() {
			added := model.BuildBuffAdd(player.GetUniqueID(), buff.SkillID, buff.Token)
			player.SendToSelfAndKnown(&added)
			continue
		}
		remaining := time.Until(buff.ExpiresAt)
		if remaining <= 0 {
			continue
		}
		added := model.BuildTimedBuffAdd(player.GetUniqueID(), buff.SkillID, buff.Token, uint32(remaining.Milliseconds()))
		player.SendToSelfAndKnown(&added)
		token := buff.Token
		time.AfterFunc(remaining, func() {
			if player.RemoveActiveBuff(token) {
				removed := model.BuildBuffRemove(token)
				player.SendToSelfAndKnown(&removed)
				stats := player.BuildStatsUpdatePacket()
				player.SendToSelf(&stats)
			}
		})
	}
}

func publishRestoredItemServices(_ *model.Player) {
	// The stock vSRO 1.188 client currently rejects the restored premium
	// service sequence emitted here (0x3159 followed by 0xB0BD) during
	// GameReady and enters its deliberate access-violation fail-fast path.
	// Keep the persisted effects loaded for server-side accounting, but do
	// not publish the unverified restoration packets on login.
}

func (h *GameReadyHandler) playerLookup() gameReadyPlayerLookup {
	if h != nil && h.players != nil {
		return h.players
	}
	return service.GetWorldServiceInstance()
}

// Handle remains as a compatibility entry point for integrations that still
// construct PacketChannelData directly. Server startup uses the direct
// registry method above.
func (h *GameReadyHandler) Handle(data server.PacketChannelData) {
	if err := h.handleSessionPacket(context.Background(), data.Session, data.Packet); err != nil {
		logrus.Debugf("rejecting GameReady request: %v", err)
	}
}
