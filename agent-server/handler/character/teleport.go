package character

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/handler/lobby"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

const normalTeleportType = byte(2)

type teleportWorld interface {
	selectEntityWorld
	GetRegion(int16) (navmeshv2.RtNavmeshTerrain, error)
	StopMovingCharacter(model.ICharacter, navmeshv2.RtNavmeshPosition, string, bool) bool
	FlushPlayerPosition(uint32) error
	ResetPlayerVisibilityForTeleport(*model.Player)
}

type TeleportHandler struct {
	world      teleportWorld
	repository model.TeleportRepository
	send       func(*server.Session, network.Packet) error
	bootstrap  func(*server.Session, *model.Player) error
	roll       func() int
	instances  *model.QuestInstanceLifecycle
	mu         sync.Mutex
	pending    map[*server.Session]*model.Player
}

func (h *TeleportHandler) ConfigureQuestInstanceExit(lifecycle *model.QuestInstanceLifecycle) {
	if h != nil {
		h.instances = lifecycle
	}
}

func (h *TeleportHandler) SubmitPartyQuestInstanceExit(ctx context.Context, player *model.Player) error {
	if h == nil || h.world == nil || h.instances == nil || player == nil || player.Session == nil || player.Session.LifecyclePhase() != server.SessionInWorld {
		return model.ErrQuestInstanceInvalid
	}
	state, err := h.instances.PrepareExit(ctx, player)
	if err != nil {
		if errors.Is(err, model.ErrQuestInstanceNotMember) || errors.Is(err, model.ErrQuestInstanceMissing) {
			return nil
		}
		return err
	}
	instance, err := h.instances.Instance(state.Scope)
	if err != nil {
		return err
	}
	if _, handled := model.NativePartyInstanceHookForQuest(instance.QuestID); !handled {
		return nil
	}
	region, err := h.world.GetRegion(state.RegionID)
	if err != nil {
		return err
	}
	offset := math32.NewVector3(state.X, state.Y, state.Z)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		return err
	}
	destination := navmeshv2.RtNavmeshPosition{Offset: offset, Region: region.Region, Cell: &cell}
	h.mu.Lock()
	pending := h.pending[player.Session] != nil
	h.mu.Unlock()
	if pending || !player.Session.BeginTeleportLoading() {
		return model.ErrQuestInstanceInvalid
	}
	notice, err := vsro188.BuildQuestNotification("UIIT_MSG_QUEST_INS_CONDITION")
	if err != nil {
		player.Session.AbortTeleportLoading()
		return err
	}
	if err = h.sendPacket(player.Session, notice); err != nil {
		player.Session.AbortTeleportLoading()
		return err
	}
	if _, err = h.instances.Exit(ctx, state.Scope, player); err != nil {
		player.Session.AbortTeleportLoading()
		return err
	}
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, destination, "quest-instance-party-exit", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	h.pending[player.Session] = player
	h.mu.Unlock()
	return h.sendPacket(player.Session, buildTeleportReadyRequest(state.RegionID))
}

// SubmitForgottenWorldRecall joins one authenticated party member to the
// inviter's active quest-instance scope and stages the stock teleport-loading
// exchange at the inviter's authoritative position.
func (h *TeleportHandler) SubmitForgottenWorldRecall(ctx context.Context, member, inviter *model.Player) error {
	if h == nil || h.world == nil || h.instances == nil || member == nil || inviter == nil || member == inviter ||
		member.Session == nil || inviter.Session == nil || member.Session.LifecyclePhase() != server.SessionInWorld ||
		member.GetParty() == nil || member.GetParty() != inviter.GetParty() {
		return model.ErrQuestInstanceInvalid
	}
	scope := model.GameWorldScope(inviter.GetGameWorldID())
	if !scope.ValidInstance() || member.GetGameWorldID() == uint32(scope) {
		return model.ErrQuestInstanceMember
	}
	ownerState, err := h.instances.PrepareExit(ctx, inviter)
	if err != nil || ownerState.Scope != scope {
		return errors.Join(model.ErrQuestInstanceNotMember, err)
	}
	position := inviter.GetNavmeshPosition()
	if position.Region.ID == 0 || position.Offset == nil {
		return model.ErrQuestInstanceInvalid
	}
	h.mu.Lock()
	pending := h.pending[member.Session] != nil
	h.mu.Unlock()
	if pending || !member.Session.BeginTeleportLoading() {
		return model.ErrQuestInstanceInvalid
	}
	if _, err = h.instances.Join(ctx, scope, ownerState.DungeonKey, member, time.Now().UTC(), 1); err != nil {
		member.Session.AbortTeleportLoading()
		return err
	}
	rollback := func(cause error) error {
		member.Session.AbortTeleportLoading()
		_, leaveErr := h.instances.Leave(ctx, scope, member)
		return errors.Join(cause, leaveErr)
	}
	member.SetLifeState(model.Spawning)
	member.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(member)
	if !h.world.StopMovingCharacter(member, position, "forgotten-world-recall", false) {
		return rollback(model.ErrQuestInstanceInvalid)
	}
	npcInteractions.Clear(member.GetUniqueID())
	h.mu.Lock()
	h.pending[member.Session] = member
	h.mu.Unlock()
	if err = h.sendPacket(member.Session, buildTeleportReadyRequest(position.Region.ID)); err != nil {
		h.mu.Lock()
		delete(h.pending, member.Session)
		h.mu.Unlock()
		return rollback(err)
	}
	return nil
}

// SubmitDimensionHoleEntry moves one admitted player into the exact direct
// GameWorld layer and authored destination selected by the Dimension Hole
// runtime. Admission owns capacity; this method owns only the reversible
// world/scope mutation and the stock teleport-loading exchange.
func (h *TeleportHandler) SubmitDimensionHoleEntry(ctx context.Context, player *model.Player, admission service.DimensionHoleAdmission, destination service.DimensionHoleEntryDestination) error {
	if h == nil || h.world == nil || player == nil || player.Session == nil ||
		player.Session.LifecyclePhase() != server.SessionInWorld ||
		admission.PlayerUID == 0 || admission.PlayerUID != player.GetUniqueID() ||
		!admission.Scope.ValidInstance() ||
		admission.Scope.WorldID() != admission.Definition.WorldID ||
		destination.WorldID != admission.Definition.WorldID {
		return service.ErrDimensionHoleEntryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := h.resolveDestination(model.TeleportDestination{
		WorldID: int16(destination.WorldID),
		Region:  destination.Region,
		X:       destination.X,
		Y:       destination.Y,
		Z:       destination.Z,
	})
	if err != nil {
		return fmt.Errorf("resolve Dimension Hole destination: %w", err)
	}

	session := player.Session
	h.mu.Lock()
	pending := h.pending[session] != nil
	h.mu.Unlock()
	if pending || !session.BeginTeleportLoading() {
		return fmt.Errorf("%w: loading is already pending", service.ErrDimensionHoleEntryUnavailable)
	}

	sourceScope := player.GetGameWorldID()
	sourcePosition := player.GetNavmeshPosition()
	sourceLifeState := player.GetLifeState()
	sourceMask, hadSourceMask := player.CharacterMask(time.Now())
	rollback := func(cause error) error {
		h.mu.Lock()
		delete(h.pending, session)
		h.mu.Unlock()
		player.SetGameWorldID(sourceScope)
		player.SetLifeState(sourceLifeState)
		if hadSourceMask {
			player.ApplyCharacterMask(sourceMask)
		}
		h.world.ResetPlayerVisibilityForTeleport(player)
		if !h.world.StopMovingCharacter(player, sourcePosition, "dimension-hole-entry-rollback", false) {
			cause = errors.Join(cause, fmt.Errorf("restore Dimension Hole source position"))
		}
		session.AbortTeleportLoading()
		return cause
	}

	player.SetGameWorldID(uint32(admission.Scope))
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	if !h.world.StopMovingCharacter(player, target, "dimension-hole-entry", false) {
		return rollback(service.ErrDimensionHoleEntryUnavailable)
	}
	h.mu.Lock()
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportReadyRequest(target.Region.ID)); err != nil {
		return rollback(err)
	}
	npcInteractions.Clear(player.GetUniqueID())
	return nil
}

func NewTeleportHandler(world teleportWorld, repository model.TeleportRepository) *TeleportHandler {
	return &TeleportHandler{world: world, repository: repository, pending: make(map[*server.Session]*model.Player), roll: func() int { return rand.Intn(32768) }}
}

// SubmitSamePositionReload performs the stock world-reset exchange without
// changing coordinates. Job-clothes equip and removal use it after the
// inventory commit so the client reloads its job-mode object state and
// respawns the character at the same authoritative position.
func (h *TeleportHandler) SubmitSamePositionReload(player *model.Player) error {
	if h == nil || h.world == nil || player == nil || player.Session == nil || player.Session.LifecyclePhase() != server.SessionInWorld {
		return fmt.Errorf("same-position teleport is unavailable")
	}
	session := player.Session
	h.mu.Lock()
	pending := h.pending[session] != nil
	h.mu.Unlock()
	if pending || !session.BeginTeleportLoading() {
		return fmt.Errorf("same-position teleport could not begin while session is %s", session.LifecyclePhase())
	}
	position := player.GetNavmeshPosition()
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, position, "job-suit-unequip", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	h.pending[session] = player
	h.mu.Unlock()
	if err := h.sendPacket(session, buildTeleportReadyRequest(position.Region.ID)); err != nil {
		h.mu.Lock()
		delete(h.pending, session)
		h.mu.Unlock()
		session.AbortTeleportLoading()
		return err
	}
	return nil
}

// SubmitDungeonEventZoneTeleport uses the ordinary stock loading exchange for
// a server-owned event-zone transition. No item or gold transaction is
// involved; the destination comes from the authoritative teleport catalog.
func (h *TeleportHandler) SubmitDungeonEventZoneTeleport(player *model.Player, destination model.TeleportDestination) error {
	if h == nil || h.world == nil || player == nil || player.Session == nil || player.Session.LifecyclePhase() != server.SessionInWorld {
		return fmt.Errorf("dungeon event-zone teleport is unavailable")
	}
	position, err := h.resolveDestination(destination)
	if err != nil {
		return fmt.Errorf("resolve dungeon event-zone destination: %w", err)
	}
	session := player.Session
	h.mu.Lock()
	pending := h.pending[session] != nil
	h.mu.Unlock()
	if pending || !session.BeginTeleportLoading() {
		return fmt.Errorf("dungeon event-zone teleport could not begin while session is %s", session.LifecyclePhase())
	}
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, position, "dungeon-event-zone", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportReadyRequest(position.Region.ID)); err != nil {
		h.mu.Lock()
		delete(h.pending, session)
		h.mu.Unlock()
		session.AbortTeleportLoading()
		return err
	}
	logrus.WithFields(logrus.Fields{
		"uid": player.GetUniqueID(), "region": position.Region.ID,
		"x": position.Offset.X, "y": position.Offset.Y, "z": position.Offset.Z,
	}).Info("staged dungeon event-zone teleport loading")
	return nil
}

// SubmitNativeQuestInstanceTeleport stages one shared destination for every
// current GameWorld member and starts the stock 0x34B5/0x34B6 reset exchange.
// The instance variables commit only after all reset requests are submitted.
func (h *TeleportHandler) SubmitNativeQuestInstanceTeleport(registry *model.QuestInstanceRegistry, plan model.NativeQuestInstanceDeathPlan, source *model.Player) error {
	if h == nil || h.world == nil || registry == nil || source == nil || source.Session == nil || source.GetGameWorldID() != uint32(plan.Scope) || len(plan.MemberUIDs) == 0 {
		return model.ErrQuestInstanceInvalid
	}
	position := source.GetNavmeshPosition()
	relocation, err := model.BuildNativeQuestInstanceRelocation(position.Region.ID, position.Offset.X, position.Offset.Y, position.Offset.Z, h.roll)
	if err != nil {
		return err
	}
	region, err := h.world.GetRegion(relocation.RegionID)
	if err != nil {
		return err
	}
	offset := math32.NewVector3(relocation.X, relocation.Y, relocation.Z)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		return err
	}
	destination := navmeshv2.RtNavmeshPosition{Offset: offset, Heading: relocation.Heading, Region: region.Region, Cell: &cell}
	members := make([]*model.Player, 0, len(plan.MemberUIDs))
	for _, uid := range plan.MemberUIDs {
		member, lookupErr := h.world.GetPlayerByUniqueId(uid)
		if lookupErr != nil || member == nil || member.Session == nil || member.GetGameWorldID() != uint32(plan.Scope) || member.Session.LifecyclePhase() != server.SessionInWorld {
			return model.ErrQuestInstanceNotMember
		}
		members = append(members, member)
	}
	h.mu.Lock()
	for _, member := range members {
		if h.pending[member.Session] != nil {
			h.mu.Unlock()
			return fmt.Errorf("quest instance member %d already has a pending teleport", member.GetUniqueID())
		}
	}
	h.mu.Unlock()
	notice, err := vsro188.BuildQuestNotification(plan.TalkToken)
	if err != nil {
		return err
	}
	if err = h.sendPacket(source.Session, notice); err != nil {
		return err
	}
	begun := make([]*server.Session, 0, len(members))
	for _, member := range members {
		if !member.Session.BeginTeleportLoading() {
			for _, session := range begun {
				session.AbortTeleportLoading()
			}
			return fmt.Errorf("quest instance member %d could not begin teleport loading", member.GetUniqueID())
		}
		begun = append(begun, member.Session)
	}
	for _, member := range members {
		member.SetLifeState(model.Spawning)
		member.RemoveCharacterMask(0)
		h.world.ResetPlayerVisibilityForTeleport(member)
		h.world.StopMovingCharacter(member, destination, "quest-instance-teleport", false)
		npcInteractions.Clear(member.GetUniqueID())
		h.mu.Lock()
		h.pending[member.Session] = member
		h.mu.Unlock()
		if err = h.sendPacket(member.Session, buildTeleportReadyRequest(relocation.RegionID)); err != nil {
			return err
		}
	}
	_, err = registry.CommitNativeQuestInstanceDeath(plan)
	return err
}

func (h *TeleportHandler) SubmitQuestInstanceEntry(instance model.QuestInstance, members []*model.Player) error {
	if h == nil || h.world == nil || !instance.Scope.ValidInstance() || len(members) == 0 {
		return model.ErrQuestInstanceInvalid
	}
	for _, member := range members {
		if member == nil || member.Session == nil || member.GetGameWorldID() != uint32(instance.Scope) || member.Session.LifecyclePhase() != server.SessionInWorld {
			return model.ErrQuestInstanceNotMember
		}
		h.mu.Lock()
		pending := h.pending[member.Session] != nil
		h.mu.Unlock()
		if pending {
			return fmt.Errorf("quest instance member %d already has a pending teleport", member.GetUniqueID())
		}
	}
	begun := make([]*server.Session, 0, len(members))
	for _, member := range members {
		if !member.Session.BeginTeleportLoading() {
			for _, session := range begun {
				session.AbortTeleportLoading()
			}
			return model.ErrQuestInstanceInvalid
		}
		begun = append(begun, member.Session)
	}
	for _, member := range members {
		position := member.GetNavmeshPosition()
		member.SetLifeState(model.Spawning)
		member.RemoveCharacterMask(0)
		h.world.ResetPlayerVisibilityForTeleport(member)
		h.world.StopMovingCharacter(member, position, "quest-instance-entry", false)
		npcInteractions.Clear(member.GetUniqueID())
		h.mu.Lock()
		h.pending[member.Session] = member
		h.mu.Unlock()
		if err := h.sendPacket(member.Session, buildTeleportReadyRequest(position.Region.ID)); err != nil {
			return err
		}
	}
	return nil
}

func RegisterTeleportHandler(target *server.Server, repository model.TeleportRepository) *TeleportHandler {
	if target == nil {
		return nil
	}
	handler := NewTeleportHandler(service.GetWorldServiceInstance(), repository)
	target.RegisterHandler(opcode.TeleportDesignateRequest, handler.handleDesignate)
	target.RegisterHandler(opcode.TeleportUseRequest, handler.handleUse)
	target.RegisterHandler(opcode.TeleportCancelRequest, handler.handleCancel)
	target.RegisterHandler(opcode.TeleportReadyResponse, handler.handleReady)
	return handler
}

// handleDesignate implements the stock 0x7059 bind-point request. The wire
// payload is exactly the selected world-object UID; the response carries only
// the one-byte success/failure status recovered from the GameServer/client
// family. The catalog write is intentionally separate from teleport use:
// designating a gate does not charge gold or move the player.
func (h *TeleportHandler) handleDesignate(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 4 {
		return fmt.Errorf("teleport designation payload length %d, want 4", packet.Remaining())
	}
	npcUID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read teleport designation target: %w", err)
	}
	if h == nil {
		return fmt.Errorf("teleport designation handler is unavailable")
	}
	if h.world == nil || h.repository == nil {
		return h.rejectDesignation(session, 1)
	}
	designation, ok := h.repository.(model.TeleportDesignationRepository)
	if !ok {
		return h.rejectDesignation(session, 1)
	}
	player, err := resolveSelectingPlayer(h.world, session)
	if err != nil {
		return err
	}
	interaction, ok := npcInteractions.Get(player.GetUniqueID())
	if !ok || interaction.NPCUniqueID != npcUID || interaction.Action != teleportTaskUse {
		return h.rejectDesignation(session, 1)
	}
	if err = validateNPCSelection(h.world, player, npcUID); err != nil {
		return h.rejectDesignation(session, 1)
	}
	object, err := h.world.GetObjectByUniqueId(npcUID)
	if err != nil || object == nil {
		return h.rejectDesignation(session, 1)
	}
	// The stock selection data advertises designation only for a teleport
	// building. Ferry NPCs share the use/link path but expose no bind-point
	// task, so a forged 0x7059 for a ferry must not change RETURN_TELEPORT_ID.
	if gate, ok := object.(*model.TeleportGate); !ok || !model.IsTeleportGateObject(gate) {
		return h.rejectDesignation(session, 1)
	}
	if _, err = designation.Designate(ctx, int64(player.ID), session.UserContext.UserID, interaction.NPCRefObjectID); err != nil {
		if errors.Is(err, model.ErrTeleportOutcomeUnknown) {
			return fmt.Errorf("teleport designation outcome is unknown: %w", err)
		}
		return h.rejectDesignation(session, 1)
	}
	return h.sendPacket(session, buildTeleportDesignationSuccess())
}

func (h *TeleportHandler) handleCancel(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 {
		return fmt.Errorf("teleport cancel contains %d trailing bytes", packet.Remaining())
	}
	if h == nil || session == nil {
		return nil
	}
	// The stock client may send 0x705B after a server-side destination failure
	// has already unwound CharacterLoading. Treat that acknowledgement as
	// idempotent; turning it into a dispatcher error closes an otherwise healthy
	// session precisely when teleport recovery is needed.
	if session.LifecyclePhase() != server.SessionCharacterLoading {
		return nil
	}
	h.mu.Lock()
	_, alreadyCommitted := h.pending[session]
	h.mu.Unlock()
	if alreadyCommitted {
		return fmt.Errorf("teleport cancel arrived after teleport commit")
	}
	session.AbortTeleportLoading()
	return nil
}

// PrepareItemReturn reserves the same loading lifecycle used by gate
// teleports, then drains the last movement checkpoint before the item-use
// transaction writes the bound-city position.
func (h *TeleportHandler) PrepareItemReturn(session *server.Session, player *model.Player) error {
	if h == nil || h.world == nil || session == nil || player == nil || player.Session != session {
		return fmt.Errorf("return-scroll teleport is unavailable")
	}
	h.mu.Lock()
	_, pending := h.pending[session]
	h.mu.Unlock()
	if pending || !session.BeginTeleportLoading() {
		return fmt.Errorf("return-scroll teleport could not begin while session is %s", session.LifecyclePhase())
	}
	h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "return-scroll-prepare", false)
	if err := h.world.FlushPlayerPosition(player.GetUniqueID()); err != nil {
		session.AbortTeleportLoading()
		return fmt.Errorf("flush pre-return-scroll position: %w", err)
	}
	return nil
}

func (h *TeleportHandler) AbortItemReturn(session *server.Session) {
	if session != nil {
		session.AbortTeleportLoading()
	}
}

// CompleteItemReturn stages the committed destination and starts the stock
// GAME_RESET/ready/bootstrap exchange. The item-use success packet is sent by
// the inventory handler before this method is called.
func (h *TeleportHandler) CompleteItemReturn(session *server.Session, player *model.Player, destination model.TeleportDestination) error {
	if h == nil || h.world == nil || session == nil || player == nil || session.LifecyclePhase() != server.SessionCharacterLoading {
		return fmt.Errorf("return-scroll teleport was not prepared")
	}
	position, err := h.resolveDestination(destination)
	if err != nil {
		session.AbortTeleportLoading()
		return fmt.Errorf("resolve return-scroll world position: %w", err)
	}
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, position, "return-scroll", false)
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	if h.pending[session] != nil {
		h.mu.Unlock()
		return fmt.Errorf("return-scroll teleport is already pending")
	}
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportReadyRequest(destination.Region)); err != nil {
		h.mu.Lock()
		delete(h.pending, session)
		h.mu.Unlock()
		return err
	}
	return nil
}

func (h *TeleportHandler) handleUse(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 9 {
		return fmt.Errorf("teleport request payload length %d, want 9", packet.Remaining())
	}
	npcUID, e1 := packet.ReadUInt32()
	teleportType, e2 := packet.ReadByte()
	targetID, e3 := packet.ReadUInt32()
	if err := errors.Join(e1, e2, e3); err != nil {
		return fmt.Errorf("read teleport request: %w", err)
	}
	if teleportType != normalTeleportType || targetID == 0 {
		return h.reject(session, 0x0001)
	}
	if h.world == nil || h.repository == nil {
		return h.reject(session, 0x0001)
	}
	h.mu.Lock()
	_, alreadyPending := h.pending[session]
	h.mu.Unlock()
	if alreadyPending {
		return h.reject(session, 0x0001)
	}
	player, err := resolveSelectingPlayer(h.world, session)
	if err != nil {
		return err
	}
	interaction, ok := npcInteractions.Get(player.GetUniqueID())
	if !ok || interaction.NPCUniqueID != npcUID {
		return h.reject(session, 0x0001)
	}
	if err = validateNPCSelection(h.world, player, npcUID); err != nil {
		logrus.WithError(err).Debug("rejected teleport from invalid NPC interaction")
		return h.reject(session, 0x0001)
	}
	object, err := h.world.GetObjectByUniqueId(npcUID)
	if err != nil || object == nil || object.GetRefObjectID() != interaction.NPCRefObjectID {
		return h.reject(session, 0x0001)
	}

	quote, err := h.repository.Quote(ctx, interaction.NPCRefObjectID, targetID)
	if err != nil {
		return h.reject(session, teleportErrorCode(err))
	}
	destination, err := h.resolveDestination(quote)
	if err != nil {
		logrus.WithError(err).Warn("rejected teleport to unavailable world position")
		return h.reject(session, 0x0001)
	}
	if !session.BeginTeleportLoading() {
		return fmt.Errorf("teleport could not begin loading while session is %s", session.LifecyclePhase())
	}
	teleportLoading := true
	defer func() {
		if teleportLoading {
			session.AbortTeleportLoading()
		}
	}()
	// Cancel movement and drain the last pre-teleport checkpoint before the
	// gold+position transaction. Otherwise an older queued movement snapshot
	// could commit after this transaction and move the character back.
	h.world.StopMovingCharacter(player, player.GetNavmeshPosition(), "teleport-prepare", false)
	if err = h.world.FlushPlayerPosition(player.GetUniqueID()); err != nil {
		return fmt.Errorf("flush pre-teleport position: %w", err)
	}
	result, err := h.repository.Commit(ctx, int64(player.ID), session.UserContext.UserID, interaction.NPCRefObjectID, targetID)
	if err != nil {
		if errors.Is(err, model.ErrTeleportOutcomeUnknown) {
			return fmt.Errorf("teleport commit outcome is unknown: %w", err)
		}
		return h.reject(session, teleportErrorCode(err))
	}
	if result.Destination != quote {
		return fmt.Errorf("teleport catalog changed during transaction")
	}

	player.SetGold(result.Gold)
	// GameReady activates only staged players. Re-enter that state before the
	// client and server both discard the old area's visibility state. The reset
	// must not queue ordinary despawns: 0x34B5 has already cleared those UIDs
	// from the client's global object table.
	player.SetLifeState(model.Spawning)
	player.RemoveCharacterMask(0)
	h.world.ResetPlayerVisibilityForTeleport(player)
	h.world.StopMovingCharacter(player, destination, "teleport", false)
	if navmeshv2.IsDungeonRegion(destination.Region.ID) {
		logrus.WithFields(logrus.Fields{
			"uid": player.GetUniqueID(), "target_id": targetID, "region": destination.Region.ID,
			"x": destination.Offset.X, "y": destination.Offset.Y, "z": destination.Offset.Z,
		}).Info("staged dungeon teleport")
	}
	npcInteractions.Clear(player.GetUniqueID())
	h.mu.Lock()
	h.pending[session] = player
	h.mu.Unlock()
	if err = h.sendPacket(session, buildTeleportSuccess()); err != nil {
		return err
	}
	ready := buildTeleportReadyRequest(quote.Region)
	if err = h.sendPacket(session, ready); err != nil {
		return err
	}
	teleportLoading = false
	return nil
}

func buildTeleportReadyRequest(regionID int16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportReadyRequest
	// SERVER_AGENT_GAME_RESET carries the destination region. The stock client
	// reads this uint16 before choosing its loading screen and resetting world
	// state, even though clientless bot handlers commonly ignore the payload.
	packet.WriteUInt16(uint16(regionID))
	return packet
}

func (h *TeleportHandler) handleReady(_ context.Context, session *server.Session, packet network.Packet) error {
	if _, err := (vsro188.TeleportReadyResponseCodec{}).Decode(packet); err != nil {
		return err
	}
	h.mu.Lock()
	player := h.pending[session]
	delete(h.pending, session)
	h.mu.Unlock()
	if player == nil || session == nil || player.Session != session || player.GetUniqueID() != session.UserContext.UniqueID {
		return fmt.Errorf("teleport ready response has no pending teleport")
	}
	position := player.GetNavmeshPosition()
	logrus.WithFields(logrus.Fields{
		"uid": player.GetUniqueID(), "region": position.Region.ID,
	}).Info("received teleport-ready response; sending character bootstrap")
	if h.bootstrap != nil {
		if err := h.bootstrap(session, player); err != nil {
			return err
		}
	} else if err := lobby.SendCharacterBootstrap(session, player); err != nil {
		return err
	}
	logrus.WithFields(logrus.Fields{
		"uid": player.GetUniqueID(), "region": position.Region.ID,
	}).Info("completed teleport character bootstrap")
	return nil
}

func (h *TeleportHandler) resolveDestination(destination model.TeleportDestination) (navmeshv2.RtNavmeshPosition, error) {
	region, err := h.world.GetRegion(destination.Region)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, err
	}
	offset := math32.NewVector3(destination.X, destination.Y, destination.Z)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		return navmeshv2.RtNavmeshPosition{}, err
	}
	return navmeshv2.RtNavmeshPosition{Offset: offset, Heading: 0, Region: region.Region, Cell: &cell}, nil
}

func (h *TeleportHandler) reject(session *server.Session, code uint16) error {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportUseResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return h.sendPacket(session, packet)
}

func (h *TeleportHandler) rejectDesignation(session *server.Session, code uint16) error {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportDesignateResponse
	packet.WriteByte(2)
	packet.WriteUInt16(code)
	return h.sendPacket(session, packet)
}

func (h *TeleportHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func buildTeleportSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportUseResponse
	packet.WriteByte(1)
	return packet
}

func buildTeleportDesignationSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.TeleportDesignateResponse
	packet.WriteByte(1)
	return packet
}

func teleportErrorCode(err error) uint16 {
	if errors.Is(err, model.ErrTeleportInsufficientGold) {
		return 0x0002
	}
	return 0x0001
}
