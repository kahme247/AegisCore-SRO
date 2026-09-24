package service

import (
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

// MoveTo gives a COS the stock command-1 move order. The pet remains at the
// ordered point after arrival until command 9 explicitly restores following.
func (s *COSService) MoveTo(owner *model.Player, petUID uint32, destination navmeshv2.RtNavmeshPosition) error {
	pet, err := s.Owned(owner, petUID)
	if err != nil {
		return err
	}
	if pet.GetLifeState() == model.Dead {
		return model.ErrItemUseInvalidRequest
	}
	world, ok := s.world.(cosFollowerWorld)
	if !ok || destination.Region.ID == 0 || !world.CanMoveDirectly(pet.GetNavmeshPosition(), destination) {
		return model.ErrItemUseInvalidRequest
	}
	transportState := owner.COSState.Snapshot()
	if transportState.Mounted && transportState.TransportUniqueID == petUID && (pet.Kind == model.COSRideTransport || pet.Kind == model.COSJobTransport) {
		// Mounted clicks use 0x70C5 command 1, not the player's ordinary 0x7021
		// path. Keep the owner as the authoritative world mover so persistence,
		// collision, and visibility follow the rider, but identify the transport
		// in B021 because the client has attached the player model to it via B0CB.
		s.mu.Lock()
		if state := s.actions[petUID]; state.action == cosActionPickup {
			delete(s.pickupClaims, state.targetUID)
		}
		delete(s.actions, petUID)
		delete(s.attacks, petUID)
		delete(s.nextAttack, petUID)
		s.mu.Unlock()
		now := time.Now()
		start := owner.GetNavmeshPosition()
		movement := world.RegisterMovingCharacter(owner, model.MovementData{
			StartTime:      now,
			UpdateTime:     now,
			TargetPosition: destination,
			HasDestination: true,
			SpeedOverride:  pet.GetMovementSpeed(),
		})
		pet.SetMotionState(owner.GetMotionState())
		packet := model.BuildMovementUpdatePacket(pet, movement, start)
		owner.SendToSelfAndKnown(&packet)
		traceMovementFor(owner, "mounted-cos-command owner_uid=%d transport_uid=%d generation=%d from=%s target=%s speed=%g", owner.GetUniqueID(), petUID, movement.Generation, start.String(), destination.String(), movement.SpeedOverride)
		return nil
	}
	s.mu.Lock()
	if state := s.actions[petUID]; state.action == cosActionPickup {
		delete(s.pickupClaims, state.targetUID)
	}
	delete(s.attacks, petUID)
	delete(s.nextAttack, petUID)
	s.actions[petUID] = cosActionState{action: cosActionMove, destination: destination}
	s.mu.Unlock()
	return nil
}

// Follow restores the stock command-9 owner-follow action and interrupts any
// manual movement, pickup pursuit, or combat order.
func (s *COSService) Follow(owner *model.Player, petUID uint32) error {
	pet, err := s.Owned(owner, petUID)
	if err != nil {
		return err
	}
	if pet.GetLifeState() == model.Dead {
		return model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	if state := s.actions[petUID]; state.action == cosActionPickup {
		delete(s.pickupClaims, state.targetUID)
	}
	delete(s.attacks, petUID)
	delete(s.nextAttack, petUID)
	s.actions[petUID] = cosActionState{action: cosActionFollow}
	if ai := s.companionAI[petUID]; ai != nil {
		ai.nextWaypoint = time.Time{}
	}
	s.mu.Unlock()
	if world, ok := s.world.(cosFollowerWorld); ok && pet.GetMotionState() != model.NoMotion {
		world.StopMovingCharacter(pet, pet.GetNavmeshPosition(), "cos-follow-command", false)
	}
	return nil
}

func (s *COSService) updateCommandedMovement(world cosFollowerWorld, pet *model.COS, owner *model.Player, now time.Time) bool {
	s.mu.Lock()
	state, exists := s.actions[pet.GetUniqueID()]
	s.mu.Unlock()
	if !exists || state.action == cosActionFollow || state.action == cosActionAttack {
		return false
	}
	if state.action == cosActionHold {
		return true
	}
	if state.action != cosActionMove {
		return true
	}
	petPos := pet.GetNavmeshPosition()
	if petPos.Region.ID != state.destination.Region.ID || navmeshv2.HorizontalDistance(petPos, owner.GetNavmeshPosition()) > companionRelocationRadius(pet.Kind, s.currentPolicy().Settings) {
		_ = s.Follow(owner, pet.GetUniqueID())
		return false
	}
	if navmeshv2.HorizontalDistance(petPos, state.destination) <= s.currentPolicy().Settings.ArrivalRadius {
		world.StopMovingCharacter(pet, petPos, "cos-command-arrival", false)
		s.mu.Lock()
		s.actions[pet.GetUniqueID()] = cosActionState{action: cosActionHold, destination: petPos}
		s.mu.Unlock()
		return true
	}
	if movement := pet.GetMovementData(); movement != nil && movement.HasDestination && navmeshv2.HorizontalDistance(movement.TargetPosition, state.destination) <= s.currentPolicy().Settings.DestinationMatchRange {
		return true
	}
	movement := world.RegisterMovingCharacter(pet, model.MovementData{StartTime: now, UpdateTime: now, TargetPosition: state.destination, HasDestination: true})
	packet := model.BuildMovementUpdatePacket(pet, movement, petPos)
	pet.SendToKnown(&packet)
	return true
}
