package service

import (
	"context"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/questwire"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type cosPickupWorld interface {
	cosCombatWorld
	GetKnownObjectsAroundObject(model.ISRObject) map[uint32]model.ISRObject
}

func (s *COSService) Pickup(owner *model.Player, petUID, itemUID uint32) error {
	pet, err := s.Owned(owner, petUID)
	if err != nil {
		return err
	}
	if pet.Kind != model.COSAbilityPet || itemUID == 0 {
		return model.ErrInvalidCOSKind
	}
	if pet.GetLifeState() == model.Dead {
		return model.ErrItemUseInvalidRequest
	}
	world, ok := s.world.(cosPickupWorld)
	repository, repositoryOK := s.repository.(model.COSInventoryRepository)
	if !ok || !repositoryOK || repository == nil {
		return model.ErrCOSRepositoryUnavailable
	}
	object, err := world.GetObjectByUniqueId(itemUID)
	item, valid := cosGroundItem(object)
	if err != nil || !valid || item == nil || item.ID <= 0 {
		return model.ErrItemUseInvalidRequest
	}
	itemPos, ownerPos := item.GetNavmeshPosition(), owner.GetNavmeshPosition()
	if navmeshv2.HorizontalDistance(itemPos, ownerPos) > s.currentPolicy().Settings.PickupCollectionRange || (item.GetOwner() != 0 && item.GetOwner() != owner.GetUniqueID()) {
		return model.ErrItemUseInvalidRequest
	}
	slot, available := byte(0), true
	if !cosGeneratedGoldItem(item) {
		slot, available = cosPickupDestination(pet, item)
	}
	if !available {
		return model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	if claimant := s.pickupClaims[itemUID]; claimant != 0 && claimant != petUID {
		s.mu.Unlock()
		return model.ErrItemUseInvalidRequest
	}
	if previous := s.actions[petUID]; previous.action == cosActionPickup && previous.targetUID != itemUID {
		delete(s.pickupClaims, previous.targetUID)
	}
	s.pickupClaims[itemUID] = petUID
	delete(s.attacks, petUID)
	delete(s.nextAttack, petUID)
	s.actions[petUID] = cosActionState{action: cosActionPickup, targetUID: itemUID, storageSlot: slot}
	s.mu.Unlock()
	return nil
}

func (s *COSService) updateAbilityPetPickup(followerWorld cosFollowerWorld, pet *model.COS, owner *model.Player, now time.Time) bool {
	world, ok := followerWorld.(cosPickupWorld)
	repository, repositoryOK := s.repository.(model.COSInventoryRepository)
	if !ok || !repositoryOK || pet == nil || pet.Kind != model.COSAbilityPet {
		return false
	}
	s.mu.Lock()
	state := s.actions[pet.GetUniqueID()]
	s.mu.Unlock()
	if state.action == cosActionFollow {
		var nearest model.ISRObject
		nearestDistance := s.currentPolicy().Settings.PickupCollectionRange + 1
		ownerPos := owner.GetNavmeshPosition()
		for _, object := range world.GetKnownObjectsAroundObject(owner) {
			item, itemOK := cosGroundItem(object)
			if !itemOK || item == nil || item.ID <= 0 || (item.GetOwner() != 0 && item.GetOwner() != owner.GetUniqueID()) {
				continue
			}
			itemPos := item.GetNavmeshPosition()
			distance := navmeshv2.HorizontalDistance(ownerPos, itemPos)
			if distance <= s.currentPolicy().Settings.PickupCollectionRange && distance < nearestDistance {
				nearest, nearestDistance = object, distance
			}
		}
		if nearest == nil || s.Pickup(owner, pet.GetUniqueID(), nearest.GetUniqueID()) != nil {
			return false
		}
		s.mu.Lock()
		state = s.actions[pet.GetUniqueID()]
		s.mu.Unlock()
	}
	if state.action != cosActionPickup {
		return false
	}
	object, err := world.GetObjectByUniqueId(state.targetUID)
	item, valid := cosGroundItem(object)
	if err != nil || !valid || item == nil {
		s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
		return false
	}
	petPos, itemPos := pet.GetNavmeshPosition(), item.GetNavmeshPosition()
	ownerPos := owner.GetNavmeshPosition()
	if navmeshv2.HorizontalDistance(itemPos, ownerPos) > s.currentPolicy().Settings.PickupCollectionRange {
		s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
		return false
	}
	if navmeshv2.HorizontalDistance(petPos, itemPos) > 5 {
		if movement := pet.GetMovementData(); movement != nil && movement.HasDestination && navmeshv2.HorizontalDistance(movement.TargetPosition, itemPos) <= s.currentPolicy().Settings.DestinationMatchRange {
			return true
		}
		movement := world.RegisterMovingCharacter(pet, model.MovementData{StartTime: now, UpdateTime: now, TargetPosition: itemPos, HasDestination: true})
		packet := model.BuildMovementUpdatePacket(pet, movement, petPos)
		pet.SendToKnown(&packet)
		return true
	}
	world.StopMovingCharacter(pet, petPos, "cos-pickup-in-range", false)
	if cosGeneratedGoldItem(item) {
		s.mu.Lock()
		goldRepository := s.groundGold
		lifecycle := s.groundItemLifecycle
		s.mu.Unlock()
		if goldRepository == nil {
			s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
			return false
		}
		gold, pickupErr := goldRepository.PickupGold(context.Background(), int64(owner.ID), int64(item.ID))
		if pickupErr != nil {
			s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
			return false
		}
		owner.SetGold(gold.Gold)
		if lifecycle != nil {
			lifecycle.Cancel(item)
		}
		pickup := vsro188.BuildEntityPickupAnimation(pet.GetUniqueID(), 0)
		owner.SendToSelfAndKnown(&pickup)
		world.RemoveVisibleObject(object)
		goldUpdate := vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: gold.Gold, TrailingFlag: 1})
		owner.SendToSelf(&goldUpdate)
		s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
		return true
	}
	existing, stacking := pet.Inventory.Snapshot().Items[state.storageSlot]
	if stacking {
		stackRepository, stackOK := s.repository.(model.COSInventoryStackRepository)
		if !stackOK || stackRepository == nil || existing.GetRefObjectID() != item.GetRefObjectID() || existing.StackSize+item.StackSize > model.RefItems[item.GetRefObjectID()].StackSize {
			s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
			return false
		}
		err = stackRepository.StackItem(context.Background(), owner.ID, pet.PersistentID, existing.ID, item.ID, item.StackSize)
	} else {
		err = repository.StoreItem(context.Background(), owner.ID, pet.PersistentID, state.storageSlot, item.ID)
	}
	if err != nil {
		s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
		return false
	}
	if (stacking && !pet.Inventory.AddToStack(state.storageSlot, item.StackSize)) || (!stacking && !pet.Inventory.Put(state.storageSlot, pet.InventorySize, *item)) {
		s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
		return false
	}
	s.mu.Lock()
	lifecycle := s.groundItemLifecycle
	s.mu.Unlock()
	if lifecycle != nil {
		lifecycle.Cancel(item)
	}
	// Client 0x0087BC1F accepts operation 0x11 as a ground-to-COS pickup.
	// Its occupied-slot path reports newQuantity-oldQuantity. Publish B034
	// before the authoritative 0x30C9 snapshot; sending 0x30C9 first makes both
	// values equal and produces the incorrect "0 pieces" activity entry.
	updatedItem, _ := pet.Inventory.Snapshot().Items[state.storageSlot]
	packets, packetCount := buildAbilityPetPickupClientPackets(pet, state.storageSlot, updatedItem)
	owner.SendToSelfAndKnown(&packets[0])
	for index := 1; index < packetCount; index++ {
		owner.SendToSelf(&packets[index])
	}
	if s.questProgress != nil {
		quantity := item.StackSize
		if quantity <= 0 {
			quantity = 1
		}
		if progress, progressErr := s.questProgress.RecordCollectedItem(context.Background(), owner.ID, item.GetRefObjectID(), uint32(quantity)); progressErr == nil {
			for _, result := range progress {
				if !result.Changed {
					continue
				}
				owner.CommitQuestMutation(result.Quest)
				if questUpdate, buildErr := questwire.BuildUpdate(result.Quest, vsro188.QuestUpdateDelta); buildErr == nil {
					owner.SendToSelf(&questUpdate)
				}
			}
		}
	}
	// Keep the ground object live through B034/0x11 so the client can resolve
	// the pickup source, then remove it after the inventory completion packets.
	world.RemoveVisibleObject(object)
	s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
	return true
}

func buildAbilityPetPickupClientPackets(pet *model.COS, slot byte, item model.Item) ([3]network.Packet, int) {
	packets := [3]network.Packet{
		vsro188.BuildEntityPickupAnimation(pet.GetUniqueID(), 0),
		model.BuildCOSPickupInventoryUpdate(pet.GetUniqueID(), slot, item),
	}
	count := 2
	if inventoryUpdate, supported := model.BuildCOSInventoryUpdate(pet); supported {
		packets[count] = inventoryUpdate
		count++
	}
	return packets, count
}

func cosPickupDestination(pet *model.COS, item *model.Item) (byte, bool) {
	if pet == nil || item == nil {
		return 0, false
	}
	if ref, ok := model.RefItems[item.GetRefObjectID()]; ok && ref.StackSize > 1 && item.StackSize > 0 {
		items := pet.Inventory.Snapshot().Items
		// SR_GameServer 0x004B2890 walks COS slots in ascending order and
		// selects the first compatible stack with sufficient remaining room;
		// only then does it ask the free-slot selector for a new destination.
		for slot := byte(0); slot < pet.InventorySize; slot++ {
			existing, occupied := items[slot]
			if occupied && existing.GetRefObjectID() == item.GetRefObjectID() && existing.StackSize+item.StackSize <= ref.StackSize {
				return slot, true
			}
		}
	}
	return pet.Inventory.FirstFreeSlot(pet.InventorySize)
}

func cosGeneratedGoldItem(item *model.Item) bool {
	if item == nil {
		return false
	}
	// The maintained monster-drop materializer uses the three stock vSRO gold
	// object references directly. Keep that identity check authoritative even
	// if a partially hydrated world object has not received its TypeInfo yet.
	if refID := item.GetRefObjectID(); refID >= 1 && refID <= 3 {
		return true
	}
	typeInfo := item.GetTypeInfo()
	return typeInfo.TypeID1 == model.TypeID1Item && typeInfo.TypeID2 == 3 && typeInfo.TypeID3 == 5 && typeInfo.TypeID4 != 1
}

func cosGroundItem(object model.ISRObject) (*model.Item, bool) {
	switch item := object.(type) {
	case *model.Item:
		return item, item != nil
	case *model.Equipment:
		if item == nil {
			return nil, false
		}
		return &item.Item, true
	default:
		return nil, false
	}
}

func (s *COSService) finishPickupAction(petUID, itemUID uint32) {
	s.mu.Lock()
	if s.pickupClaims[itemUID] == petUID {
		delete(s.pickupClaims, itemUID)
	}
	if state := s.actions[petUID]; state.action == cosActionPickup && state.targetUID == itemUID {
		s.actions[petUID] = cosActionState{action: cosActionFollow}
	}
	s.mu.Unlock()
}
