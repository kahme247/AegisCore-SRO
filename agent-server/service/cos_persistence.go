package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

// RehydratePlayer restores persisted summoned pets on a fresh login, then
// relocates and republishes all active pets after the stock client clears its
// object table during login or teleport loading.
func (s *COSService) RehydratePlayer(ctx context.Context, owner *model.Player) error {
	if s == nil || owner == nil {
		return nil
	}
	world, ok := s.world.(cosFollowerWorld)
	if !ok {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	restored := make(map[uint64]struct{})
	for _, pet := range s.byOwnerKind[owner.ID] {
		if pet != nil && pet.OwnerUniqueID == owner.GetUniqueID() {
			restored[pet.PersistentID] = struct{}{}
		}
	}
	s.mu.Unlock()
	type persistedSummon struct {
		slot byte
		id   uint64
	}
	candidates := make([]persistedSummon, 0)
	for slot, item := range owner.GetInventory().Items {
		if !item.COS.Present || item.COS.State != model.COSItemSummoned || item.Data == 0 {
			continue
		}
		id := uint64(item.Data)
		if _, exists := restored[id]; !exists {
			candidates = append(candidates, persistedSummon{slot: slot, id: id})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].slot < candidates[j].slot })
	for _, candidate := range candidates {
		if _, err := s.Summon(ctx, owner, candidate.id); err != nil {
			return fmt.Errorf("restore summoned COS %d from inventory slot %d: %w", candidate.id, candidate.slot, err)
		}
	}

	s.mu.Lock()
	kinds := s.byOwnerKind[owner.ID]
	pets := make([]*model.COS, 0, len(kinds))
	for _, pet := range kinds {
		if pet != nil && pet.OwnerUniqueID == owner.GetUniqueID() {
			pets = append(pets, pet)
		}
	}
	s.mu.Unlock()
	sort.Slice(pets, func(i, j int) bool { return pets[i].PersistentID < pets[j].PersistentID })
	for _, pet := range pets {
		target, ok := s.companionTarget(world, pet, owner, time.Now(), false)
		if !ok {
			target = owner.GetNavmeshPosition()
		}
		world.StopMovingCharacter(pet, target, "cos-teleport-rehydrate", false)
		if data, supported := model.BuildCOSData(pet); supported {
			owner.SendToSelf(&data)
		}
		itemState := model.BuildCOSItemStateUpdate(pet.InventorySlot, model.COSItemSummoned)
		owner.SendToSelf(&itemState)
	}
	return nil
}

func (s *COSService) Rename(ctx context.Context, owner *model.Player, uid uint32, name string) error {
	cos, err := s.Owned(owner, uid)
	if err != nil {
		return err
	}
	if err = s.repository.Rename(ctx, owner.ID, cos.PersistentID, name); err != nil {
		return err
	}
	cos.Rename(name)
	return nil
}

func (s *COSService) UpdateSettings(ctx context.Context, owner *model.Player, uid, settings uint32) error {
	cos, err := s.Owned(owner, uid)
	if err != nil {
		return err
	}
	cos.UpdateSettings(settings)
	return s.repository.Save(ctx, model.COSRecordFromSnapshot(cos.Snapshot()))
}

func (s *COSService) UnsummonAll(ctx context.Context, owner *model.Player) error {
	if s == nil || owner == nil {
		return nil
	}
	s.mu.Lock()
	kinds := s.byOwnerKind[owner.ID]
	uids := make([]uint32, 0, len(kinds))
	for _, cos := range kinds {
		if cos != nil {
			uids = append(uids, cos.GetUniqueID())
		}
	}
	s.mu.Unlock()
	var result error
	for _, uid := range uids {
		result = errors.Join(result, s.Unsummon(ctx, owner, uid))
	}
	return result
}

// SnapshotOwned returns a stable persistent snapshot without changing world
// membership. The terminal save writes these records in its caller-owned
// transaction before DetachAll performs in-memory teardown.
func (s *COSService) SnapshotOwned(owner *model.Player) []model.COSRecord {
	if s == nil || owner == nil {
		return nil
	}
	s.mu.Lock()
	kinds := s.byOwnerKind[owner.ID]
	pets := make([]*model.COS, 0, len(kinds))
	for _, cos := range kinds {
		if cos != nil {
			pets = append(pets, cos)
		}
	}
	s.mu.Unlock()
	records := make([]model.COSRecord, 0, len(pets))
	for _, cos := range pets {
		records = append(records, model.COSRecordFromSnapshot(cos.Snapshot()))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records
}

// CheckpointAndDetachAll preserves the summoned state during the non-atomic
// disconnect fallback, then removes runtime objects even when a save fails so
// a disconnected owner cannot leave ghost pets in the world.
func (s *COSService) CheckpointAndDetachAll(ctx context.Context, owner *model.Player) error {
	if s == nil || owner == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var result error
	for _, record := range s.SnapshotOwned(owner) {
		result = errors.Join(result, s.repository.Save(ctx, record))
	}
	s.DetachAll(owner)
	return result
}

// DetachAll removes a character's COS runtime state without performing SQL.
// It is called only after the atomic terminal save attempt has completed.
func (s *COSService) DetachAll(owner *model.Player) {
	if s == nil || owner == nil {
		return
	}
	s.mu.Lock()
	kinds := s.byOwnerKind[owner.ID]
	pets := make([]*model.COS, 0, len(kinds))
	for _, cos := range kinds {
		if cos == nil {
			continue
		}
		pets = append(pets, cos)
		uid := cos.GetUniqueID()
		delete(s.byUID, uid)
		delete(s.lastFollow, uid)
		delete(s.companionAI, uid)
		delete(s.attacks, uid)
		if state := s.actions[uid]; state.action == cosActionPickup {
			delete(s.pickupClaims, state.targetUID)
		}
		delete(s.actions, uid)
		delete(s.nextAttack, uid)
		delete(s.rentalRetry, uid)
	}
	delete(s.byOwnerKind, owner.ID)
	s.mu.Unlock()
	for _, cos := range pets {
		uid := cos.GetUniqueID()
		if cos.Kind == model.COSRideTransport {
			_ = owner.COSState.ClearTransport(uid)
		}
		owner.SetCOSItemState(cos.InventorySlot, cos, model.COSItemActive)
		if s.world != nil {
			s.world.RemoveVisibleObject(cos)
		}
	}
}
