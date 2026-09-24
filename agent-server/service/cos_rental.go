package service

import (
	"context"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

// expireAbilityPetRental retires a summoned fixed-period pet through the same
// persistence and world teardown path as a client-requested unsummon. A failed
// save leaves the pet indexed for retry but suppresses movement and combat once
// the authoritative expiry instant has passed.
func (s *COSService) expireAbilityPetRental(owner *model.Player, pet *model.COS, now time.Time) bool {
	if pet == nil || pet.Kind != model.COSAbilityPet || pet.RentEndsAt.IsZero() || pet.RentEndsAt.After(now.UTC()) {
		return false
	}
	uid, slot := pet.GetUniqueID(), pet.InventorySlot
	retryInterval := s.currentPolicy().Settings.RentalRetryInterval
	s.mu.Lock()
	retryAt := s.rentalRetry[uid]
	if now.Before(retryAt) {
		s.mu.Unlock()
		return true
	}
	s.rentalRetry[uid] = now.Add(retryInterval)
	s.mu.Unlock()
	if err := s.Unsummon(context.Background(), owner, uid); err != nil {
		return true
	}
	terminated := model.BuildCOSTerminatedUpdate(uid)
	owner.SendToSelfAndKnown(&terminated)
	itemState := model.BuildCOSItemStateUpdate(slot, model.COSItemActive)
	owner.SendToSelf(&itemState)
	return true
}
