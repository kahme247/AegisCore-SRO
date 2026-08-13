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
func RegisterGameReadyHandler(target *server.Server, mall ...model.ItemMallRepository) {
	if target == nil {
		return
	}
	handler := NewGameReadyHandler(nil, mall...)
	target.RegisterHandler(opcode.GameReadyRequest, handler.handleSessionPacket)
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
	if packet.Remaining() != 0 {
		return fmt.Errorf("GameReady request contains trailing data")
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
	if player.GetLifeState() == model.Dead {
		state := model.BuildLifeStateUpdatePacket(player.GetUniqueID(), model.Dead)
		if err := session.Send(state); err != nil {
			return fmt.Errorf("send loaded death state: %w", err)
		}
	}
	if h.mall != nil {
		balancePacket := network.EmptyPacket()
		balancePacket.MessageID = opcode.ItemMallBalanceUpdate
		balancePacket.WriteUInt32(balances.Silk)
		balancePacket.WriteUInt32(balances.GiftSilk)
		balancePacket.WriteUInt32(balances.Point)
		if err := session.Send(balancePacket); err != nil {
			return fmt.Errorf("send item mall balances: %w", err)
		}
	}
	publishRestoredItemServices(player)
	logrus.Debugf("player %s's client is ready", player.CharName)
	return nil
}

func publishRestoredItemServices(player *model.Player) {
	if player == nil {
		return
	}
	for _, state := range player.ItemServiceStates(time.Now().UTC()) {
		effect := state.Effect
		token := state.Token
		remaining := time.Until(effect.ExpiresAt)
		if remaining <= 0 || token == 0 {
			continue
		}
		if model.ItemServiceUsesItemPerk(effect) {
			added := model.BuildItemPerkAdd(player.GetUniqueID(), effect.SourceRefItemID, token, effect.DailyBudget, remaining)
			player.SendToSelfAndKnown(&added)
		}
		if premiumAdded, exists := model.BuildPremiumDailyAdd(token, effect, remaining); exists {
			player.SendToSelf(&premiumAdded)
		}
		if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), token, effect, remaining); exists {
			player.SendToSelfAndKnown(&buffAdded)
		}
		time.AfterFunc(remaining, func() {
			if player.ClearItemService(effect.Kind, token) {
				if model.ItemServiceUsesItemPerk(effect) {
					removed := model.BuildItemPerkRemove(player.GetUniqueID(), effect.SourceRefItemID, token)
					player.SendToSelfAndKnown(&removed)
				}
				if premiumRemoved, exists := model.BuildPremiumDailyRemove(token, effect); exists {
					player.SendToSelf(&premiumRemoved)
				}
				if model.ItemServiceBuffSkillID(effect) != 0 {
					buffRemoved := model.BuildBuffRemove(token)
					player.SendToSelfAndKnown(&buffRemoved)
				}
			}
		})
	}
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
