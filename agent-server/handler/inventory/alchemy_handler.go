package inventory

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
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

const (
	alchemyActionCancel       = 1
	alchemyActionFuse         = 2
	alchemyTypeElixir         = 3
	alchemyTypeMagicStone     = 4
	alchemyTypeAttributeStone = 5
	alchemyTypeAdvancedElixir = 8
	alchemyTypeSocketInsert   = 9
	alchemyTypeAvatar         = 7
)

type alchemyElixirRequest struct {
	cancel      bool
	alchemyType byte
	targetSlot  byte
	elixirSlot  byte
	powderSlot  *byte
}

type AlchemyHandler struct {
	players PlayerLookup
	repo    model.AlchemyRepository
	send    func(*server.Session, network.Packet) error
	states  *alchemyLifecycle
}

type alchemyWake struct {
	execute          bool
	suppressResponse bool
}

type alchemyRuntimeState struct {
	typeID byte
	phase  byte
	player *model.Player
	wake   chan alchemyWake
}

type alchemyLifecycle struct {
	mu     sync.Mutex
	active map[int]*alchemyRuntimeState
}

func newAlchemyLifecycle() *alchemyLifecycle {
	return &alchemyLifecycle{active: make(map[int]*alchemyRuntimeState)}
}

func (l *alchemyLifecycle) begin(player *model.Player, typeID byte) bool {
	if player == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.active[player.ID]; exists {
		return false
	}
	l.active[player.ID] = &alchemyRuntimeState{typeID: typeID, player: player, wake: make(chan alchemyWake, 1)}
	return true
}

func (l *alchemyLifecycle) cancel(characterID int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, exists := l.active[characterID]
	if exists && state.phase < 3 {
		state.phase = 3
		state.wake <- alchemyWake{suppressResponse: true}
	}
	return exists
}

func (l *alchemyLifecycle) tick() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, state := range l.active {
		if state == nil || state.phase >= 3 {
			continue
		}
		if state.player == nil || state.player.GetLifeState() == model.Dead || state.player.GetBodyState() == model.Untouchable {
			state.phase = 3
			state.wake <- alchemyWake{}
			continue
		}
		state.phase++
		if state.phase == 3 {
			state.wake <- alchemyWake{execute: true}
		}
	}
}

func (l *alchemyLifecycle) wait(ctx context.Context, characterID int) alchemyWake {
	l.mu.Lock()
	state := l.active[characterID]
	if state == nil {
		l.mu.Unlock()
		return alchemyWake{}
	}
	wake := state.wake
	l.mu.Unlock()
	select {
	case result := <-wake:
		return result
	case <-ctx.Done():
		return alchemyWake{suppressResponse: true}
	}
}

func (l *alchemyLifecycle) reset(characterID int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.active[characterID]
	if state == nil || state.phase != 3 {
		return false
	}
	state.phase = 0
	state.wake = make(chan alchemyWake, 1)
	return true
}

func (l *alchemyLifecycle) finish(characterID int) {
	l.mu.Lock()
	delete(l.active, characterID)
	l.mu.Unlock()
}

func RegisterAlchemyHandler(target *server.Server, repo model.AlchemyRepository) *AlchemyHandler {
	if target == nil {
		return nil
	}
	h := &AlchemyHandler{players: service.GetWorldServiceInstance(), repo: repo, states: newAlchemyLifecycle()}
	target.RegisterHandler(opcode.AlchemyElixirRequest, h.handlePacket)
	target.RegisterHandler(opcode.AlchemyStoneRequest, h.handleStonePacket)
	target.RegisterHandler(opcode.AlchemySocketRequest, h.handleSocketPacket)
	target.RegisterHandler(opcode.AlchemyMaterialRequest, h.handleMaterialPacket)
	target.RegisterHandler(opcode.AlchemyDismantleRequest, h.handleDismantlePacket)
	target.RegisterHandler(opcode.AlchemyAvatarRequest, h.handleAvatarPacket)
	target.RegisterHandler(opcode.AlchemyAvatarGrantRequest, h.handleAvatarGrantPacket)
	return h
}

// TickAlchemy advances active operations exactly once per authoritative game
// update. The binary performs this phase transition from the character update
// path rather than from a wall-clock timer owned by the packet handler.
func (h *AlchemyHandler) TickAlchemy() {
	if h != nil {
		h.lifecycle().tick()
	}
}

type alchemyMaterialRequest struct {
	cancel   bool
	selector byte
	slots    []byte
}

func parseAlchemyMaterialRequest(packet *network.Packet) (alchemyMaterialRequest, error) {
	wireRequest, err := vsro188.DecodeAlchemyStagedRequest(packet)
	if err != nil {
		return alchemyMaterialRequest{}, model.ErrAlchemyInvalidRequest
	}
	if wireRequest.Action != alchemyActionFuse {
		return alchemyMaterialRequest{cancel: true}, nil
	}
	if wireRequest.Type != 1 && wireRequest.Type != 2 {
		return alchemyMaterialRequest{}, model.ErrAlchemyInvalidRequest
	}
	request := alchemyMaterialRequest{selector: wireRequest.Type, slots: wireRequest.Slots}
	seen := make(map[byte]struct{}, len(request.slots))
	for _, slot := range request.slots {
		if slot < model.InventoryEquipmentSlots || slot > 0x6f {
			return alchemyMaterialRequest{}, model.ErrAlchemyInvalidRequest
		}
		if _, duplicate := seen[slot]; duplicate {
			return alchemyMaterialRequest{}, model.ErrAlchemyInvalidRequest
		}
		seen[slot] = struct{}{}
	}
	return request, nil
}

func parseAlchemyDismantleRequest(packet *network.Packet) ([]byte, error) {
	if packet == nil || packet.Remaining() < 2 {
		return nil, model.ErrAlchemyInvalidRequest
	}
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > 9 || packet.Remaining() != int(count) {
		return nil, model.ErrAlchemyInvalidRequest
	}
	slots := make([]byte, count)
	seen := make(map[byte]struct{}, count)
	for index := range slots {
		slot, readErr := packet.ReadByte()
		if readErr != nil || slot < model.InventoryEquipmentSlots || slot > 0x6f {
			return nil, model.ErrAlchemyInvalidRequest
		}
		if _, duplicate := seen[slot]; duplicate {
			return nil, model.ErrAlchemyInvalidRequest
		}
		seen[slot] = struct{}{}
		slots[index] = slot
	}
	return slots, nil
}

func (h *AlchemyHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyElixirRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildAlchemyFailure())
	}
	resolver := &InventoryHandler{players: h.players}
	player, err := resolver.resolvePlayer(session)
	if err != nil {
		return err
	}
	if request.cancel {
		h.lifecycle().cancel(player.ID)
		return h.sendPacket(session, buildAlchemyCancel())
	}
	if !h.beginDelayed(player, request.alchemyType) {
		return h.sendPacket(session, buildAlchemyFailure())
	}
	defer h.lifecycle().finish(player.ID)
	wake := h.awaitPhaseThree(ctx, player)
	if !wake.execute {
		if wake.suppressResponse {
			return nil
		}
		return h.sendPacket(session, buildAlchemyCancel())
	}
	var result model.AlchemyResult
	if request.alchemyType == alchemyTypeAdvancedElixir {
		result, err = h.repo.ApplyAdvancedElixir(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.elixirSlot)
	} else {
		result, err = h.repo.FuseElixir(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.elixirSlot, request.powderSlot)
	}
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected alchemy enhancement")
		return h.sendPacket(session, buildAlchemyFailure())
	}
	player.CommitInventory(result.Inventory)
	if err = h.sendPacket(session, buildAlchemySuccess(result)); err != nil {
		return err
	}
	if request.targetSlot <= model.SlotRightRing {
		if err = h.sendPacket(session, player.BuildStatsUpdatePacket()); err != nil {
			return err
		}
	}
	for _, material := range result.Materials {
		if material.Remaining == 0 {
			err = h.sendPacket(session, buildAlchemyMaterialRemoved(material.Slot))
		} else {
			err = h.sendPacket(session, buildAlchemyMaterialQuantity(material.Slot, material.Remaining))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func parseAlchemyElixirRequest(packet *network.Packet) (alchemyElixirRequest, error) {
	wireRequest, err := vsro188.DecodeAlchemyStagedRequest(packet)
	if err != nil {
		return alchemyElixirRequest{}, model.ErrAlchemyInvalidRequest
	}
	if wireRequest.Action != alchemyActionFuse {
		return alchemyElixirRequest{cancel: true}, nil
	}
	if (wireRequest.Type != alchemyTypeElixir && wireRequest.Type != alchemyTypeAdvancedElixir) || len(wireRequest.Slots) < 2 || len(wireRequest.Slots) > 3 || (wireRequest.Type == alchemyTypeAdvancedElixir && len(wireRequest.Slots) != 2) {
		return alchemyElixirRequest{}, model.ErrAlchemyUnsupported
	}
	request := alchemyElixirRequest{alchemyType: wireRequest.Type, targetSlot: wireRequest.Slots[0], elixirSlot: wireRequest.Slots[1]}
	if len(wireRequest.Slots) == 3 {
		powder := wireRequest.Slots[2]
		request.powderSlot = &powder
	}
	return request, nil
}

func buildAlchemyCancel() network.Packet {
	return vsro188.BuildAlchemyCancel(opcode.AlchemyElixirResponse)
}

func buildAlchemyFailure() network.Packet {
	return vsro188.BuildAlchemyFailure(opcode.AlchemyElixirResponse, 0)
}

func buildAlchemySuccess(result model.AlchemyResult) network.Packet {
	return buildElixirAlchemyResult(result)
}

func buildElixirAlchemyResult(result model.AlchemyResult) network.Packet {
	return vsro188.BuildAlchemyElixirResult(result.Success, result.TargetSlot, result.Destroyed, alchemyTargetPayload(result.Target))
}

func buildStoneAlchemyResult(result model.AlchemyResult) network.Packet {
	return vsro188.BuildAlchemyStoneResult(result.Success, result.TargetSlot, alchemyTargetPayload(result.Target))
}

func buildSocketAlchemyResult(result model.AlchemyResult) network.Packet {
	return vsro188.BuildAlchemySocketResult()
}

func alchemyTargetPayload(target model.Item) vsro188.AlchemyTargetPayload {
	packet := network.EmptyPacket()
	model.WriteRentInfo(&packet, target)
	packet.WriteUInt32(target.GetRefObjectID())
	model.WriteInventoryItem(&packet, target)
	return vsro188.AlchemyTargetPayload(packet.Payload())
}

type alchemyPairRequest struct {
	cancel       bool
	action       byte
	alchemyType  byte
	targetSlot   byte
	materialSlot byte
	index        byte
}

type alchemyAvatarRequest struct {
	cancel bool
	slots  []byte
}

type alchemyAvatarGrantRequest struct {
	targetSlot byte
	optionCode string
}

func parseAlchemyAvatarRequest(packet *network.Packet) (alchemyAvatarRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, err := packet.ReadByte()
	if err != nil {
		return alchemyAvatarRequest{}, err
	}
	if action != alchemyActionFuse {
		if packet.Remaining() != 0 {
			return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
		}
		return alchemyAvatarRequest{cancel: true}, nil
	}
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > 2 || packet.Remaining() != int(count) {
		return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
	}
	request := alchemyAvatarRequest{slots: make([]byte, count)}
	seen := make(map[byte]struct{}, count)
	for index := range request.slots {
		slot, readErr := packet.ReadByte()
		if readErr != nil || slot < model.InventoryEquipmentSlots || slot > 0x6f {
			return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
		}
		if _, duplicate := seen[slot]; duplicate {
			return alchemyAvatarRequest{}, model.ErrAlchemyInvalidRequest
		}
		seen[slot] = struct{}{}
		request.slots[index] = slot
	}
	return request, nil
}

func parseAlchemyAvatarGrantRequest(packet *network.Packet) (alchemyAvatarGrantRequest, error) {
	if packet == nil || packet.Remaining() < 3 {
		return alchemyAvatarGrantRequest{}, model.ErrAlchemyInvalidRequest
	}
	slot, err := packet.ReadByte()
	if err != nil || slot < model.InventoryEquipmentSlots || slot > 0x6f {
		return alchemyAvatarGrantRequest{}, model.ErrAlchemyInvalidRequest
	}
	optionCode, err := packet.ReadString()
	if err != nil || optionCode == "" || packet.Remaining() != 0 {
		return alchemyAvatarGrantRequest{}, model.ErrAlchemyInvalidRequest
	}
	return alchemyAvatarGrantRequest{targetSlot: slot, optionCode: optionCode}, nil
}

func buildAlchemyAvatarResult(mutated bool) network.Packet {
	return vsro188.BuildAlchemyAvatarResult(mutated)
}

func buildAlchemyAvatarGrantResult(mutated bool) network.Packet {
	return vsro188.BuildAlchemyAvatarGrantResult(mutated)
}

func buildAlchemyAvatarFailure() network.Packet {
	return vsro188.BuildAlchemyFailure(opcode.AlchemyAvatarResponse, 0)
}

func (h *AlchemyHandler) handleAvatarPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyAvatarRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildAlchemyAvatarFailure())
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	if request.cancel {
		h.lifecycle().cancel(player.ID)
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyAvatarResponse))
	}
	if !h.beginDelayed(player, alchemyTypeAvatar) {
		return h.sendPacket(session, buildAlchemyAvatarFailure())
	}
	defer h.lifecycle().finish(player.ID)
	wake := h.awaitPhaseThree(ctx, player)
	if !wake.execute {
		if wake.suppressResponse {
			return nil
		}
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyAvatarResponse))
	}
	var stoneSlot *byte
	if len(request.slots) == 2 {
		stoneSlot = &request.slots[1]
	}
	result, err := h.repo.EnchantAvatarItem(ctx, int64(player.ID), session.UserContext.UserID, request.slots[0], stoneSlot)
	buildResult := func(result model.AlchemyResult) network.Packet { return buildAlchemyAvatarResult(result.Success) }
	return h.finishExtendedAlchemy(session, player, buildResult, buildAlchemyAvatarFailure, result, err)
}

func (h *AlchemyHandler) handleAvatarGrantPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyAvatarGrantRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return nil
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.repo.BlessAvatarItem(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.optionCode)
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected avatar NPC blessing")
		return nil
	}
	player.CommitInventory(result.Inventory)
	return h.sendPacket(session, buildAlchemyAvatarGrantResult(result.Success))
}

func parseAlchemyPairRequest(packet *network.Packet) (alchemyPairRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, err := packet.ReadByte()
	if err != nil {
		return alchemyPairRequest{}, err
	}
	if action == alchemyActionCancel && packet.Remaining() == 0 {
		return alchemyPairRequest{cancel: true}, nil
	}
	alchemyType, e1 := packet.ReadByte()
	count, e2 := packet.ReadByte()
	target, e3 := packet.ReadByte()
	material, e4 := packet.ReadByte()
	if action != alchemyActionFuse || count != 2 || packet.Remaining() != 0 || errors.Join(e1, e2, e3, e4) != nil {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	if alchemyType != alchemyTypeMagicStone && alchemyType != alchemyTypeAttributeStone {
		return alchemyPairRequest{}, model.ErrAlchemyUnsupported
	}
	return alchemyPairRequest{alchemyType: alchemyType, targetSlot: target, materialSlot: material}, nil
}

func parseAlchemySocketRequest(packet *network.Packet) (alchemyPairRequest, error) {
	if packet == nil || packet.Remaining() < 2 {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	action, e1 := packet.ReadByte()
	subtype, e2 := packet.ReadByte()
	if errors.Join(e1, e2) != nil {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	if action == alchemyActionCancel && packet.Remaining() == 0 {
		return alchemyPairRequest{cancel: true, action: action, alchemyType: subtype}, nil
	}
	request := alchemyPairRequest{action: action, alchemyType: subtype}
	switch {
	case action == 2 && subtype == 9 && packet.Remaining() == 3:
		request.targetSlot, e1 = packet.ReadByte()
		request.materialSlot, e2 = packet.ReadByte()
		request.index, _ = packet.ReadByte()
	case action == 3 && subtype == 8 && packet.Remaining() == 2:
		request.targetSlot, e1 = packet.ReadByte()
		request.index, e2 = packet.ReadByte()
	case action == 4 && subtype == 10 && packet.Remaining() == 2:
		request.targetSlot, e1 = packet.ReadByte()
		request.index, e2 = packet.ReadByte()
	default:
		return alchemyPairRequest{}, model.ErrAlchemyUnsupported
	}
	if errors.Join(e1, e2) != nil || packet.Remaining() != 0 {
		return alchemyPairRequest{}, model.ErrAlchemyInvalidRequest
	}
	return request, nil
}

func (h *AlchemyHandler) handleStonePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyPairRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildStoneAlchemyFailure())
	}
	if request.cancel {
		player, resolveErr := (&InventoryHandler{players: h.players}).resolvePlayer(session)
		if resolveErr != nil {
			return resolveErr
		}
		h.lifecycle().cancel(player.ID)
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyStoneResponse))
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	if !h.beginDelayed(player, request.alchemyType) {
		return h.sendPacket(session, buildStoneAlchemyFailure())
	}
	defer h.lifecycle().finish(player.ID)
	wake := h.awaitPhaseThree(ctx, player)
	if !wake.execute {
		if wake.suppressResponse {
			return nil
		}
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyStoneResponse))
	}
	result, err := h.repo.FuseStone(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.materialSlot, request.alchemyType)
	return h.finishExtendedAlchemy(session, player, buildStoneAlchemyResult, buildStoneAlchemyFailure, result, err)
}

func (h *AlchemyHandler) handleSocketPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemySocketRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildSocketAlchemyFailure())
	}
	if request.cancel {
		player, resolveErr := (&InventoryHandler{players: h.players}).resolvePlayer(session)
		if resolveErr != nil {
			return resolveErr
		}
		h.lifecycle().cancel(player.ID)
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemySocketResponse))
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	var result model.AlchemyResult
	if request.action == 2 {
		if !h.beginDelayed(player, alchemyTypeSocketInsert) {
			return h.sendPacket(session, buildSocketAlchemyFailure())
		}
		defer h.lifecycle().finish(player.ID)
		wake := h.awaitPhaseThree(ctx, player)
		if !wake.execute {
			if wake.suppressResponse {
				return nil
			}
			return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemySocketResponse))
		}
	}
	switch request.action {
	case 2:
		result, err = h.repo.InsertSocket(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.materialSlot, request.index)
	case 3:
		result, err = h.repo.CreateSocket(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.index)
	case 4:
		result, err = h.repo.EraseSocket(ctx, int64(player.ID), session.UserContext.UserID, request.targetSlot, request.index)
	}
	buildResult := func(result model.AlchemyResult) network.Packet {
		return buildSocketAlchemyActionResult(request.action, result)
	}
	return h.finishExtendedAlchemy(session, player, buildResult, buildSocketAlchemyFailure, result, err)
}

func buildSocketAlchemyActionResult(action byte, result model.AlchemyResult) network.Packet {
	return vsro188.BuildAlchemySocketActionResult(action, result.TargetSlot, alchemyTargetPayload(result.Target))
}

func (h *AlchemyHandler) handleMaterialPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseAlchemyMaterialRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildAlchemyMaterialFailure())
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	if request.cancel {
		h.lifecycle().cancel(player.ID)
		return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyMaterialResponse))
	}
	if !h.beginDelayed(player, request.selector) {
		return h.sendPacket(session, buildAlchemyMaterialFailure())
	}
	defer h.lifecycle().finish(player.ID)
	for index, slot := range request.slots {
		wake := h.awaitPhaseThree(ctx, player)
		if !wake.execute {
			if wake.suppressResponse {
				return nil
			}
			return h.sendPacket(session, buildAlchemyProtocolCancel(opcode.AlchemyMaterialResponse))
		}
		var result model.AlchemyMakeResult
		if request.selector == 1 {
			result, err = h.repo.ResolveMaterial(ctx, int64(player.ID), session.UserContext.UserID, slot)
		} else {
			result, err = h.repo.CompoundMaterial(ctx, int64(player.ID), session.UserContext.UserID, request.slots)
		}
		if err != nil {
			return h.finishAlchemyMake(session, player, result, err)
		}
		player.CommitInventory(result.Inventory)
		if err = h.sendPacket(session, buildAlchemyMaterialSuccess(result)); err != nil {
			return err
		}
		if err = h.sendMaterialUpdates(session, result.Materials); err != nil {
			return err
		}
		if request.selector == 2 || index+1 == len(request.slots) {
			break
		}
		// Resolve-material repeats reset the phase in the binary.
		if !h.lifecycle().reset(player.ID) {
			return h.sendPacket(session, buildAlchemyMaterialFailure())
		}
	}
	return nil
}

func (h *AlchemyHandler) handleDismantlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	slots, err := parseAlchemyDismantleRequest(&packet)
	if err != nil || h == nil || h.repo == nil {
		return h.sendPacket(session, buildAlchemyDismantleFailure())
	}
	player, err := (&InventoryHandler{players: h.players}).resolvePlayer(session)
	if err != nil {
		return err
	}
	if !h.beginDelayed(player, 0x57) {
		return h.sendPacket(session, buildAlchemyDismantleFailure())
	}
	defer h.lifecycle().finish(player.ID)
	wake := h.awaitPhaseThree(ctx, player)
	if !wake.execute {
		if wake.suppressResponse {
			return nil
		}
		return h.sendPacket(session, buildAlchemyDismantleFailure())
	}
	result, err := h.repo.DismantleEquipment(ctx, int64(player.ID), session.UserContext.UserID, slots)
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		return h.sendPacket(session, buildAlchemyDismantleFailure())
	}
	player.CommitInventory(result.Inventory)
	if err = h.sendPacket(session, buildAlchemyDismantleSuccess()); err != nil {
		return err
	}
	return h.sendMaterialUpdates(session, result.Materials)
}

func buildAlchemyMaterialSuccess(result model.AlchemyMakeResult) network.Packet {
	return vsro188.BuildAlchemyMaterialResult(result.ResponseByte, result.ProducedValue)
}

func buildAlchemyMaterialFailure() network.Packet {
	return vsro188.BuildAlchemyFailure(opcode.AlchemyMaterialResponse, 0)
}

func buildAlchemyDismantleSuccess() network.Packet {
	return vsro188.BuildAlchemyDismantleSuccess()
}

func buildAlchemyDismantleFailure() network.Packet {
	return vsro188.BuildAlchemyFailure(opcode.AlchemyDismantleResponse, 0)
}

func (h *AlchemyHandler) sendMaterialUpdates(session *server.Session, materials []model.AlchemyMaterialUpdate) error {
	for _, material := range materials {
		packet := buildAlchemyMaterialRemoved(material.Slot)
		if material.Remaining != 0 {
			packet = buildAlchemyMaterialQuantity(material.Slot, material.Remaining)
		}
		if err := h.sendPacket(session, packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *AlchemyHandler) finishAlchemyMake(session *server.Session, player *model.Player, result model.AlchemyMakeResult, err error) error {
	if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
		session.Close()
		return err
	}
	return h.sendPacket(session, buildAlchemyMaterialFailure())
}

func (h *AlchemyHandler) lifecycle() *alchemyLifecycle {
	if h.states == nil {
		h.states = newAlchemyLifecycle()
	}
	return h.states
}

func (h *AlchemyHandler) beginDelayed(player *model.Player, typeID byte) bool {
	return player != nil && player.GetLifeState() != model.Dead && player.GetBodyState() != model.Untouchable && h.lifecycle().begin(player, typeID)
}

func (h *AlchemyHandler) awaitPhaseThree(ctx context.Context, player *model.Player) alchemyWake {
	if player == nil {
		return alchemyWake{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return h.lifecycle().wait(ctx, player.ID)
}

func (h *AlchemyHandler) finishExtendedAlchemy(session *server.Session, player *model.Player, buildResult func(model.AlchemyResult) network.Packet, buildFailure func() network.Packet, result model.AlchemyResult, err error) error {
	if err != nil {
		if errors.Is(err, model.ErrAlchemyOutcomeUnknown) {
			session.Close()
			return err
		}
		logrus.WithError(err).Debug("rejected extended alchemy operation")
		return h.sendPacket(session, buildFailure())
	}
	player.CommitInventory(result.Inventory)
	if err = h.sendPacket(session, buildResult(result)); err != nil {
		return err
	}
	for _, material := range result.Materials {
		if material.Remaining == 0 {
			err = h.sendPacket(session, buildAlchemyMaterialRemoved(material.Slot))
		} else {
			err = h.sendPacket(session, buildAlchemyMaterialQuantity(material.Slot, material.Remaining))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func buildStoneAlchemyFailure() network.Packet {
	return vsro188.BuildAlchemyFailure(opcode.AlchemyStoneResponse, 0)
}

func buildSocketAlchemyFailure() network.Packet {
	return vsro188.BuildAlchemySocketFailure(0)
}

func buildAlchemyProtocolCancel(messageID uint16) network.Packet {
	return vsro188.BuildAlchemyCancel(messageID)
}

func buildAlchemyMaterialQuantity(slot byte, remaining uint16) network.Packet {
	return vsro188.BuildInventoryItemQuantityUpdate(slot, remaining)
}

func buildAlchemyMaterialRemoved(slot byte) network.Packet {
	return vsro188.BuildItemOperationSlotSuccess(vsro188.ItemOperationServerDelete, slot)
}

func (h *AlchemyHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return fmt.Errorf("alchemy session is nil")
	}
	if h != nil && h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
