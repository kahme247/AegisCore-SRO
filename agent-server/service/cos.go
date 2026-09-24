package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

var (
	ErrCOSAlreadySummoned = errors.New("a COS of this class is already summoned")
	ErrCOSNotSummoned     = errors.New("COS is not summoned")
	ErrCOSDead            = errors.New("dead COS must be revived before summoning")
)

type COSWorld interface {
	AddVisibleObject(model.ISRObject)
	RemoveVisibleObject(model.ISRObject)
}

type cosFollowerWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	NewPosFromGlobalCoordinates(*math32.Vector3) (navmeshv2.RtNavmeshPosition, error)
	CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool
	RegisterMovingCharacter(model.ICharacter, model.MovementData) model.MovementData
	StopMovingCharacter(model.ICharacter, navmeshv2.RtNavmeshPosition, string, bool) bool
}

type cosCombatWorld interface {
	cosFollowerWorld
	GetObjectByUniqueId(uint32) (model.ISRObject, error)
	RemoveVisibleObject(model.ISRObject)
}

type COSCombatRewards interface {
	AwardCOSKill(context.Context, *model.Player, *model.NPC) error
}

type cosAttackIntent struct {
	targetUID uint32
	skillID   uint32
}

type cosAction byte

const (
	cosActionFollow cosAction = iota
	cosActionMove
	cosActionHold
	cosActionAttack
	cosActionPickup
)

type cosActionState struct {
	action      cosAction
	destination navmeshv2.RtNavmeshPosition
	targetUID   uint32
	storageSlot byte
}

type cosCompanionAI struct {
	seed           uint32
	followAngle    float32
	followRadius   float32
	orbitAngle     float32
	orbitRadius    float32
	orbitDirection float32
	nextWaypoint   time.Time
	ownerMoving    bool
}

// COSService owns the atomic owner/world/persistence lifecycle. Its indexes
// are authoritative only for the running Agent; durable pet state lives in
// CHAR_COS and is checkpointed before world removal.
type COSService struct {
	mu              sync.Mutex
	repository      model.COSRepository
	world           COSWorld
	byUID           map[uint32]*model.COS
	byOwnerKind     map[int]map[model.COSKind]*model.COS
	lastFollow      map[uint32]time.Time
	companionAI     map[uint32]*cosCompanionAI
	attacks         map[uint32]cosAttackIntent
	actions         map[uint32]cosActionState
	nextAttack      map[uint32]time.Time
	nextRetaliation map[uint32]time.Time
	rentalRetry     map[uint32]time.Time
	pickupClaims    map[uint32]uint32
	levels          *model.LevelTable
	rewards         COSCombatRewards
	policy          interface {
		Current() model.COSPolicySnapshot
	}
	groundItemLifecycle interface{ Cancel(*model.Item) }
	groundGold          model.GroundGoldPickupRepository
	questProgress       model.QuestRuntimeRepository
	cargoGround         interface {
		Spawn(*model.Item, uint32) error
	}
}

func NewCOSService(repository model.COSRepository, world COSWorld) *COSService {
	return &COSService{repository: repository, world: world, byUID: make(map[uint32]*model.COS), byOwnerKind: make(map[int]map[model.COSKind]*model.COS), lastFollow: make(map[uint32]time.Time), companionAI: make(map[uint32]*cosCompanionAI), attacks: make(map[uint32]cosAttackIntent), actions: make(map[uint32]cosActionState), nextAttack: make(map[uint32]time.Time), nextRetaliation: make(map[uint32]time.Time), rentalRetry: make(map[uint32]time.Time), pickupClaims: make(map[uint32]uint32)}
}

func (s *COSService) ConfigurePolicy(policy interface {
	Current() model.COSPolicySnapshot
}) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.policy = policy
	s.mu.Unlock()
}

func (s *COSService) currentPolicy() model.COSPolicySnapshot {
	if s == nil {
		return model.DefaultCOSPolicySnapshot()
	}
	s.mu.Lock()
	provider := s.policy
	s.mu.Unlock()
	if provider == nil {
		return model.DefaultCOSPolicySnapshot()
	}
	policy := provider.Current()
	if !policy.Valid() {
		return model.DefaultCOSPolicySnapshot()
	}
	return policy
}

func (s *COSService) ConfigureCombat(rewards COSCombatRewards, levels *model.LevelTable) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.rewards, s.levels = rewards, levels
	s.mu.Unlock()
}

// ConfigureGroundItemLifecycle connects successful COS collection to the
// same cancellable ownership/despawn timers used by player pickup.
func (s *COSService) ConfigureGroundItemLifecycle(lifecycle interface{ Cancel(*model.Item) }) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.groundItemLifecycle = lifecycle
	s.mu.Unlock()
}

func (s *COSService) ConfigureGroundGoldPickup(repository model.GroundGoldPickupRepository) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.groundGold = repository
	s.mu.Unlock()
}

// ConfigureQuestProgress connects companion collection to the same quest
// runtime accounting used by direct player pickup.
func (s *COSService) ConfigureQuestProgress(repository model.QuestRuntimeRepository) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.questProgress = repository
	s.mu.Unlock()
}

func (s *COSService) ConfigureCargoGroundItems(ground interface {
	Spawn(*model.Item, uint32) error
}) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cargoGround = ground
	s.mu.Unlock()
}

func (s *COSService) Summon(ctx context.Context, owner *model.Player, persistentID uint64) (*model.COS, error) {
	if s == nil || s.repository == nil || s.world == nil || owner == nil || owner.ID <= 0 || persistentID == 0 {
		return nil, model.ErrCOSRepositoryUnavailable
	}
	record, err := s.repository.LoadOwned(ctx, owner.ID, persistentID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if record.Kind == model.COSAbilityPet && (record.RentEndsAt == nil || !record.RentEndsAt.After(now)) {
		return nil, model.ErrCOSNotFound
	}
	if record.Health <= 0 || record.State == model.COSItemDead {
		return nil, ErrCOSDead
	}
	ref, exists := model.RefChars[record.RefObjectID]
	if !exists || !ref.TypeInfo.IsCOS() || model.COSKind(ref.TypeID4) != record.Kind {
		return nil, model.ErrInvalidCOSKind
	}
	cos, err := model.NewCOS(ref, owner)
	if err != nil {
		return nil, err
	}
	cos.PersistentID, cos.InventorySlot = record.ID, record.InventorySlot
	cos.Rename(record.Name)
	cos.Level, cos.Experience, cos.Health, cos.MaxHealth = record.Level, record.Experience, record.Health, record.MaxHealth
	cos.HGP, cos.Satiety, cos.Settings = record.HGP, record.Satiety, record.Settings
	if serviceRepository, ok := s.repository.(model.COSTimedServiceLoader); ok {
		services, loadErr := serviceRepository.LoadCOSTimedServices(ctx, owner.ID, record.ID, now)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, timedService := range services {
			if !cos.ApplyCommittedTimedService(timedService) {
				return nil, fmt.Errorf("restore duplicate COS timed service job %d", timedService.JobID)
			}
		}
	}
	if inventoryRepository, ok := s.repository.(model.COSInventoryRepository); ok && cos.InventorySize > 0 {
		inventory, loadErr := inventoryRepository.LoadInventory(ctx, owner.ID, record.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		cos.Inventory = inventory
	}
	if record.RentEndsAt != nil {
		cos.RentEndsAt = record.RentEndsAt.UTC()
	}
	originalState := record.State
	if originalState < model.COSItemInactive || originalState > model.COSItemDead {
		originalState = model.COSItemActive
	}
	itemState := model.COSItemSummoned
	policySettings := s.currentPolicy().Settings

	s.mu.Lock()
	if kinds := s.byOwnerKind[owner.ID]; kinds != nil && kinds[record.Kind] != nil {
		s.mu.Unlock()
		return nil, ErrCOSAlreadySummoned
	}
	s.world.AddVisibleObject(cos)
	if cos.GetUniqueID() == 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("world did not allocate COS identity")
	}
	if s.byOwnerKind[owner.ID] == nil {
		s.byOwnerKind[owner.ID] = make(map[model.COSKind]*model.COS)
	}
	s.byOwnerKind[owner.ID][record.Kind] = cos
	s.byUID[cos.GetUniqueID()] = cos
	s.companionAI[cos.GetUniqueID()] = newCOSCompanionAI(cos, policySettings)
	s.actions[cos.GetUniqueID()] = cosActionState{action: cosActionFollow}
	s.mu.Unlock()
	if record.Kind == model.COSRideTransport {
		if err = owner.COSState.ActivateTransport(cos.GetUniqueID(), cos.GetRefObjectID(), false); err != nil {
			s.detachRuntime(owner, cos, originalState)
			return nil, err
		}
	}
	if !owner.SetCOSItemState(record.InventorySlot, cos, itemState) {
		s.detachRuntime(owner, cos, originalState)
		return nil, fmt.Errorf("COS inventory item %d is no longer linked to persistent COS %d", record.InventorySlot, record.ID)
	}
	record = model.COSRecordFromSnapshot(cos.Snapshot())
	record.State = itemState
	if err = s.repository.Save(ctx, record); err != nil {
		s.detachRuntime(owner, cos, originalState)
		return nil, err
	}
	return cos, nil
}

// RevivePersisted applies an already-committed SQL revival to a summoned
// runtime pet. An inactive pet needs no runtime mutation; its next summon
// loads the new health from CHAR_COS.
// ResolvePersistentTarget converts the client-visible runtime COS object ID
// into the owned database identity used by transactional item mutations.
// ActivateTemporaryRide creates the consumable TypeID 3/3/3/2 transport
// directly in the runtime world. It deliberately has PersistentID zero: ride
// scrolls are consumed and must not acquire an ITEM.DATA pet-instrument link.
func (s *COSService) ActivateTemporaryRide(owner *model.Player, refObjectID uint32) (*model.COS, error) {
	if s == nil || s.world == nil || owner == nil || owner.ID <= 0 || refObjectID == 0 {
		return nil, model.ErrCOSRepositoryUnavailable
	}
	ref, exists := model.RefChars[refObjectID]
	if !exists || !ref.TypeInfo.IsCOS() || (ref.TypeID4 != 1 && ref.TypeID4 != 2) {
		return nil, model.ErrInvalidCOSKind
	}
	// Preserve the catalog TypeID4 in runtime state. C-family ride mounts are
	// type 1, while T-family trade transports are type 2; coercing either one
	// produces a spawn tail that does not match the client's reference lookup.
	cos, err := model.NewCOS(ref, owner)
	if err != nil {
		return nil, err
	}
	cos.PersistentID = 0

	s.mu.Lock()
	if kinds := s.byOwnerKind[owner.ID]; kinds != nil && (kinds[model.COSRideTransport] != nil || kinds[model.COSJobTransport] != nil) {
		s.mu.Unlock()
		return nil, ErrCOSAlreadySummoned
	}
	s.world.AddVisibleObject(cos)
	if cos.GetUniqueID() == 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("world did not allocate temporary ride COS identity")
	}
	if s.byOwnerKind[owner.ID] == nil {
		s.byOwnerKind[owner.ID] = make(map[model.COSKind]*model.COS)
	}
	s.byOwnerKind[owner.ID][cos.Kind] = cos
	s.byUID[cos.GetUniqueID()] = cos
	// Mounted transports are driven by their owner's movement requests. Giving
	// them companion follow AI concurrently emits independent B021 destinations
	// back toward the owner and continuously rewinds the mounted client.
	s.mu.Unlock()
	if err = owner.COSState.ActivateTransport(cos.GetUniqueID(), cos.GetRefObjectID(), true); err != nil {
		s.detachRuntime(owner, cos, model.COSItemActive)
		return nil, err
	}
	return cos, nil
}

// ResolveMountedTransport returns the runtime movement entity attached to the
// owner. The PlayerCOSState check prevents an active-but-dismounted transport
// from replacing ordinary player movement authority.
func (s *COSService) ResolveMountedTransport(owner *model.Player) (*model.COS, bool) {
	if s == nil || owner == nil {
		return nil, false
	}
	state := owner.COSState.Snapshot()
	if !state.Mounted || state.TransportUniqueID == 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	transport := s.byUID[state.TransportUniqueID]
	if transport == nil || transport.OwnerCharID != owner.ID || transport.OwnerUniqueID != owner.GetUniqueID() || (transport.Kind != model.COSRideTransport && transport.Kind != model.COSJobTransport) {
		return nil, false
	}
	return transport, true
}

func (s *COSService) ResolveActiveOwned(owner *model.Player, kind model.COSKind) (*model.COS, bool) {
	if s == nil || owner == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.byOwnerKind[owner.ID][kind]
	if candidate == nil || candidate.OwnerUniqueID != owner.GetUniqueID() {
		return nil, false
	}
	return candidate, true
}

func (s *COSService) ResolveActivePersistentTarget(owner *model.Player, kind model.COSKind) (uint64, bool) {
	if s == nil || owner == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.byOwnerKind[owner.ID][kind]
	if candidate == nil || candidate.PersistentID == 0 || candidate.OwnerUniqueID != owner.GetUniqueID() {
		return 0, false
	}
	return candidate.PersistentID, true
}

func (s *COSService) ResolvePersistentTarget(owner *model.Player, uniqueID uint32) (uint64, bool) {
	if s == nil || owner == nil || uniqueID == 0 {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, candidate := range s.byOwnerKind[owner.ID] {
		if candidate != nil && candidate.OwnerUniqueID == owner.GetUniqueID() && candidate.GetUniqueID() == uniqueID && candidate.PersistentID != 0 {
			return candidate.PersistentID, true
		}
	}
	return 0, false
}

// activePersistentCOS returns the runtime COS registered for an owner's persistent
// record. Callers retain their post-lookup ownership checks because those checks
// deliberately run after the service lock is released.
func (s *COSService) activePersistentCOS(ownerID int, persistentID uint64) *model.COS {
	s.mu.Lock()
	var pet *model.COS
	for _, candidate := range s.byOwnerKind[ownerID] {
		if candidate != nil && candidate.PersistentID == persistentID {
			pet = candidate
			break
		}
	}
	s.mu.Unlock()
	return pet
}

func (s *COSService) ApplyPersistedTimedService(owner *model.Player, persistentID uint64, service model.COSTimedService) (*model.COS, error) {
	if s == nil || owner == nil || persistentID == 0 {
		return nil, model.ErrItemUseInvalidRequest
	}
	pet := s.activePersistentCOS(owner.ID, persistentID)
	if pet == nil {
		return nil, nil
	}
	if pet.OwnerUniqueID != owner.GetUniqueID() || !pet.ApplyCommittedTimedService(service) {
		return nil, model.ErrCOSNotOwned
	}
	return pet, nil
}

func (s *COSService) ApplyPersistedRentEnd(owner *model.Player, persistentID uint64, rentEndsAt time.Time) (*model.COS, error) {
	if s == nil || owner == nil || persistentID == 0 || rentEndsAt.IsZero() {
		return nil, model.ErrItemUseInvalidRequest
	}
	pet := s.activePersistentCOS(owner.ID, persistentID)
	if pet == nil {
		return nil, nil
	}
	if pet.OwnerUniqueID != owner.GetUniqueID() || !pet.ApplyCommittedRentEnd(rentEndsAt) {
		return nil, model.ErrCOSNotOwned
	}
	return pet, nil
}

func (s *COSService) ApplyPersistedCure(owner *model.Player, persistentID uint64, effect model.ItemCureEffect, now time.Time) (*model.COS, model.ItemCureApplication, error) {
	if s == nil || owner == nil || persistentID == 0 {
		return nil, model.ItemCureApplication{}, model.ErrItemUseInvalidRequest
	}
	pet := s.activePersistentCOS(owner.ID, persistentID)
	if pet == nil {
		return nil, model.ItemCureApplication{}, nil
	}
	if pet.OwnerUniqueID != owner.GetUniqueID() {
		return nil, model.ItemCureApplication{}, model.ErrCOSNotOwned
	}
	return pet, pet.ApplyItemCure(effect, now), nil
}

func (s *COSService) ApplyPersistedResources(owner *model.Player, persistentID uint64, health int32, hgp int) (*model.COS, error) {
	if s == nil || owner == nil || persistentID == 0 {
		return nil, model.ErrItemUseInvalidRequest
	}
	pet := s.activePersistentCOS(owner.ID, persistentID)
	if pet == nil {
		return nil, nil
	}
	if pet.OwnerUniqueID != owner.GetUniqueID() || !pet.ApplyCommittedResources(health, hgp) {
		return nil, model.ErrCOSNotOwned
	}
	return pet, nil
}

func (s *COSService) RevivePersisted(owner *model.Player, persistentID uint64, health int32) (*model.COS, error) {
	if s == nil || owner == nil || persistentID == 0 || health <= 0 {
		return nil, model.ErrItemUseInvalidRequest
	}
	pet := s.activePersistentCOS(owner.ID, persistentID)
	if pet == nil {
		return nil, nil
	}
	if pet.OwnerUniqueID != owner.GetUniqueID() || !pet.Revive(health) {
		return nil, model.ErrCOSNotOwned
	}
	owner.SetCOSItemState(pet.InventorySlot, pet, model.COSItemSummoned)
	return pet, nil
}

// MarkDead persists and publishes the dead state for a summoned COS. Combat
// adapters can use this transition instead of mutating health and item state
// independently.
func (s *COSService) MarkDead(ctx context.Context, owner *model.Player, uid uint32) error {
	pet, err := s.Owned(owner, uid)
	if err != nil {
		return err
	}
	if !pet.MarkDead() {
		return model.ErrItemUseInvalidRequest
	}
	destroyed, releaseErr := s.releaseJobTransportCargo(ctx, pet)
	if releaseErr != nil && !destroyed {
		return releaseErr
	}
	if destroyed {
		owner.SetCOSItemState(pet.InventorySlot, pet, model.COSItemDead)
		state := model.BuildCOSItemStateUpdate(pet.InventorySlot, model.COSItemDead)
		owner.SendToSelf(&state)
		s.detachRuntime(owner, pet, model.COSItemDead)
		owner.ClearCOSItem(pet.InventorySlot, pet)
		return releaseErr
	}
	record := model.COSRecordFromSnapshot(pet.Snapshot())
	record.State = model.COSItemDead
	if err = s.repository.Save(ctx, record); err != nil {
		return err
	}
	owner.SetCOSItemState(pet.InventorySlot, pet, model.COSItemDead)
	state := model.BuildCOSItemStateUpdate(pet.InventorySlot, model.COSItemDead)
	owner.SendToSelf(&state)
	return nil
}

func (s *COSService) releaseJobTransportCargo(ctx context.Context, pet *model.COS) (bool, error) {
	if s == nil || pet == nil || pet.Kind != model.COSJobTransport || pet.PersistentID == 0 {
		return false, nil
	}
	repository, ok := s.repository.(model.COSCargoLossRepository)
	if !ok {
		return false, model.ErrCOSRepositoryUnavailable
	}
	s.mu.Lock()
	ground := s.cargoGround
	s.mu.Unlock()
	if ground == nil {
		return false, ErrGroundItemWorldUnavailable
	}
	items, err := repository.DestroyJobTransport(ctx, pet.OwnerCharID, pet.PersistentID)
	if err != nil {
		return false, err
	}
	position := pet.GetNavmeshPosition()
	for index := range items {
		item := &items[index]
		item.SetNavmeshPosition(position)
		if item.CreatorName != "" {
			item.OwnerUniqueID = 0
		}
		if err = ground.Spawn(item, 0); err != nil {
			return true, err
		}
	}
	pet.ReplaceInventory(model.NewInventory(nil))
	return true, nil
}

func (s *COSService) Owned(owner *model.Player, uid uint32) (*model.COS, error) {
	if s == nil || owner == nil || uid == 0 {
		return nil, model.ErrCOSNotOwned
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cos := s.byUID[uid]
	if cos == nil || cos.OwnerCharID != owner.ID || cos.OwnerUniqueID != owner.GetUniqueID() {
		return nil, model.ErrCOSNotOwned
	}
	return cos, nil
}

func (s *COSService) Unsummon(ctx context.Context, owner *model.Player, uid uint32) error {
	cos, err := s.Owned(owner, uid)
	if err != nil {
		return err
	}
	if cos.PersistentID != 0 {
		record := model.COSRecordFromSnapshot(cos.Snapshot())
		record.State = model.COSItemActive
		if err = s.repository.Save(ctx, record); err != nil {
			return err
		}
	}
	s.detachRuntime(owner, cos, model.COSItemActive)
	return nil
}

func (s *COSService) TerminateGuildMercenaries(ctx context.Context, owner *model.Player) (int, error) {
	if s == nil || owner == nil || owner.ID <= 0 {
		return 0, model.ErrCOSRepositoryUnavailable
	}
	s.mu.Lock()
	mercenary := s.byOwnerKind[owner.ID][model.COSGuild]
	if mercenary == nil {
		s.mu.Unlock()
		return 0, nil
	}
	if mercenary.OwnerUniqueID != owner.GetUniqueID() || mercenary.PersistentID == 0 {
		s.mu.Unlock()
		return 0, model.ErrCOSNotOwned
	}
	s.mu.Unlock()
	repository, ok := s.repository.(model.COSGuildReleaseRepository)
	if !ok {
		return 0, model.ErrCOSRepositoryUnavailable
	}
	if err := repository.ReleaseOwnedGuildCOS(ctx, owner.ID, mercenary.PersistentID); err != nil {
		return 0, err
	}
	s.detachRuntime(owner, mercenary, model.COSItemInactive)
	return 1, nil
}

func (s *COSService) detachRuntime(owner *model.Player, cos *model.COS, itemState model.COSItemState) {
	if s == nil || owner == nil || cos == nil {
		return
	}
	uid := cos.GetUniqueID()
	s.mu.Lock()
	if current := s.byUID[uid]; current != cos {
		s.mu.Unlock()
		return
	}
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
	if kinds := s.byOwnerKind[owner.ID]; kinds != nil {
		delete(kinds, cos.Kind)
		if len(kinds) == 0 {
			delete(s.byOwnerKind, owner.ID)
		}
	}
	s.mu.Unlock()
	if cos.Kind == model.COSRideTransport || cos.Kind == model.COSJobTransport {
		_ = owner.COSState.ClearTransport(uid)
	}
	s.world.RemoveVisibleObject(cos)
	if cos.PersistentID != 0 {
		owner.SetCOSItemState(cos.InventorySlot, cos, itemState)
	}
}

// UpdateFollowers keeps player-owned companions attached to their owner. A
// normal gap uses authoritative world movement; a large gap (including a
// teleport) is corrected immediately so the pet cannot remain in the old map.
func (s *COSService) UpdateFollowers(now time.Time) {
	if s == nil {
		return
	}
	world, ok := s.world.(cosFollowerWorld)
	if !ok {
		return
	}
	s.mu.Lock()
	pets := make([]*model.COS, 0, len(s.byUID))
	for _, pet := range s.byUID {
		pets = append(pets, pet)
	}
	s.mu.Unlock()
	for _, pet := range pets {
		// Timed service expiry is applied to the authoritative runtime object
		// on the same tick as follower maintenance. Durable cleanup remains
		// repository-owned; this prevents expired effects from remaining active.
		expiredServices := pet.ExpireTimedServices(now)
		if deleter, ok := s.repository.(model.COSTimedServiceRepository); ok {
			for _, expired := range expiredServices {
				_ = deleter.DeleteCOSTimedService(context.Background(), int64(pet.OwnerCharID), pet.PersistentID, expired.JobID)
			}
		}
		owner, err := world.GetPlayerByUniqueId(pet.OwnerUniqueID)
		if err != nil || owner == nil || owner.ID != pet.OwnerCharID || owner.GetLifeState() == model.Spawning || pet.GetLifeState() == model.Dead {
			continue
		}
		if s.expireAbilityPetRental(owner, pet, now) {
			continue
		}
		// Mounted ride/job transports are not companion followers. Their client
		// movement arrives through COS command 1 and is registered on the owner,
		// with the transport UID used on B021. Running follower maintenance here
		// compares the moving transport against a stale owner coordinate and
		// relocates it back to its summon point.
		transportState := owner.COSState.Snapshot()
		if transportState.Mounted && transportState.TransportUniqueID == pet.GetUniqueID() {
			continue
		}
		policy := s.currentPolicy()
		pet.SetOwnerMovementSpeed(owner.GetMovementSpeed(), policy.ForKind(pet.Kind).InheritOwnerSpeed)
		if s.updateGrowthPetCombat(world, pet, owner, now) {
			continue
		}
		if s.updateAbilityPetPickup(world, pet, owner, now) {
			continue
		}
		if s.updateCommandedMovement(world, pet, owner, now) {
			continue
		}
		petPos, ownerPos := pet.GetNavmeshPosition(), owner.GetNavmeshPosition()
		distance := navmeshv2.HorizontalDistance(petPos, ownerPos)
		farFromOwner := distance > companionRelocationRadius(pet.Kind, policy.Settings) || petPos.Region.ID != ownerPos.Region.ID
		target, targetOK := s.companionTarget(world, pet, owner, now, !farFromOwner)
		if farFromOwner {
			if !targetOK {
				target = ownerPos
			}
			world.StopMovingCharacter(pet, target, "cos-owner-relocation", true)
			continue
		}
		if !targetOK {
			continue
		}
		targetDistance := navmeshv2.HorizontalDistance(petPos, target)
		if targetDistance <= policy.Settings.ArrivalRadius {
			if pet.GetMotionState() != model.NoMotion {
				world.StopMovingCharacter(pet, petPos, "cos-owner-in-range", false)
			}
			continue
		}
		// UpdateFollowers runs more frequently than an orbit waypoint changes.
		// Re-registering an unchanged destination increments the authoritative
		// movement generation and makes the client repeatedly restart the same
		// run. Keep the active movement until the AI selects a real new target.
		if movement := pet.GetMovementData(); movement != nil && movement.HasDestination && navmeshv2.HorizontalDistance(movement.TargetPosition, target) <= policy.Settings.DestinationMatchRange {
			continue
		}
		s.mu.Lock()
		last := s.lastFollow[pet.GetUniqueID()]
		if now.Sub(last) < policy.Settings.FollowUpdateInterval {
			s.mu.Unlock()
			continue
		}
		s.lastFollow[pet.GetUniqueID()] = now
		s.mu.Unlock()
		movement := world.RegisterMovingCharacter(pet, model.MovementData{StartTime: now, UpdateTime: now, TargetPosition: target, HasDestination: true})
		packet := model.BuildMovementUpdatePacket(pet, movement, petPos)
		pet.SendToKnown(&packet)
	}
}

func (s *COSService) SetAttackTarget(owner *model.Player, petUID, targetUID, requestedSkillID uint32) error {
	pet, err := s.Owned(owner, petUID)
	if err != nil {
		return fmt.Errorf("pet ownership uid=%d: %w", petUID, err)
	}
	if pet.Kind != model.COSGrowthPet || targetUID == 0 {
		return fmt.Errorf("pet kind=%d target=%d: %w", pet.Kind, targetUID, model.ErrInvalidCOSKind)
	}
	if pet.GetLifeState() == model.Dead {
		return fmt.Errorf("pet is dead: %w", model.ErrItemUseInvalidRequest)
	}
	ref, exists := model.RefChars[pet.GetRefObjectID()]
	if !exists {
		return fmt.Errorf("pet ref=%d missing: %w", pet.GetRefObjectID(), model.ErrInvalidCOSKind)
	}
	allowed := [...]int{ref.DefaultSkill1, ref.DefaultSkill2, ref.DefaultSkill3, ref.DefaultSkill4, ref.DefaultSkill5, ref.DefaultSkill6, ref.DefaultSkill7, ref.DefaultSkill8, ref.DefaultSkill9, ref.DefaultSkill10}
	skillID := requestedSkillID
	if skillID == 0 {
		skillID = uint32(max(allowed[0], 0))
	}
	valid := false
	for _, candidate := range allowed {
		if candidate > 0 && uint32(candidate) == skillID {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("pet ref=%d requested skill=%d not in defaults=%v: %w", pet.GetRefObjectID(), skillID, allowed, model.ErrItemUseInvalidRequest)
	}
	world, ok := s.world.(cosCombatWorld)
	if !ok {
		return fmt.Errorf("COS combat world unavailable: %w", model.ErrCOSRepositoryUnavailable)
	}
	object, err := world.GetObjectByUniqueId(targetUID)
	target, validTarget := object.(*model.NPC)
	if err != nil || !validTarget || target == nil || !target.GetTypeInfo().IsNPCMob() || target.GetLifeState() != model.Alive {
		return fmt.Errorf("target uid=%d lookup=%v npc=%t mob=%t state=%v: %w", targetUID, err, validTarget && target != nil, validTarget && target != nil && target.GetTypeInfo().IsNPCMob(), func() model.LifeState {
			if target != nil {
				return target.GetLifeState()
			}
			return model.Dead
		}(), model.ErrItemUseInvalidRequest)
	}
	ownerPos, targetPos := owner.GetNavmeshPosition(), target.GetNavmeshPosition()
	if ownerPos.Region.ID != targetPos.Region.ID || navmeshv2.HorizontalDistance(ownerPos, targetPos) > s.currentPolicy().Settings.CombatLeashRange {
		return fmt.Errorf("target uid=%d outside leash owner-region=%d target-region=%d distance=%.2f limit=%.2f: %w", targetUID, ownerPos.Region.ID, targetPos.Region.ID, navmeshv2.HorizontalDistance(ownerPos, targetPos), s.currentPolicy().Settings.CombatLeashRange, model.ErrItemUseInvalidRequest)
	}
	s.mu.Lock()
	if state := s.actions[petUID]; state.action == cosActionPickup {
		delete(s.pickupClaims, state.targetUID)
	}
	previous := s.attacks[petUID]
	s.attacks[petUID] = cosAttackIntent{targetUID: targetUID, skillID: skillID}
	s.actions[petUID] = cosActionState{action: cosActionAttack, targetUID: targetUID}
	if previous.targetUID != targetUID || previous.skillID != skillID {
		delete(s.nextAttack, petUID)
	}
	s.mu.Unlock()
	return nil
}

// EngageOwnerTarget makes the active growth pet assist its owner with the
// same target. Explicit COS commands and owner attacks therefore use one
// validation, skill-selection, and leash path.
func (s *COSService) EngageOwnerTarget(owner *model.Player, target *model.NPC) error {
	if s == nil || owner == nil || target == nil {
		return model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	pet := s.byOwnerKind[owner.ID][model.COSGrowthPet]
	s.mu.Unlock()
	if pet == nil {
		return model.ErrCOSNotOwned
	}
	return s.SetAttackTarget(owner, pet.GetUniqueID(), target.GetUniqueID(), 0)
}

// AwardOwnerKillExperience gives a summoned growth pet the monster EXP when
// its owner lands the killing blow. The pet update is persisted before the
// vSRO 0x30C9 update is published to the owner.
func (s *COSService) AwardOwnerKillExperience(ctx context.Context, owner *model.Player, target *model.NPC) error {
	if s == nil || owner == nil || target == nil {
		return model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	pet := s.byOwnerKind[owner.ID][model.COSGrowthPet]
	levels := s.levels
	s.mu.Unlock()
	gained := target.ExperienceReward()
	if pet == nil || levels == nil || gained == 0 {
		return nil
	}
	_, refChanged := pet.AwardExperience(levels, gained)
	if err := s.repository.Save(ctx, model.COSRecordFromSnapshot(pet.Snapshot())); err != nil {
		return err
	}
	experience := model.BuildCOSExperienceUpdate(pet.GetUniqueID(), gained, target.GetUniqueID())
	owner.SendToSelf(&experience)
	if refChanged {
		changed := model.BuildCOSRefObjectUpdate(pet.GetUniqueID(), pet.GetRefObjectID())
		owner.SendToSelfAndKnown(&changed)
	}
	return nil
}

func (s *COSService) ClearAttackTarget(owner *model.Player, petUID uint32) error {
	if _, err := s.Owned(owner, petUID); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.attacks, petUID)
	delete(s.nextAttack, petUID)
	if state := s.actions[petUID]; state.action == cosActionAttack {
		s.actions[petUID] = cosActionState{action: cosActionFollow}
	}
	s.mu.Unlock()
	return nil
}

func (s *COSService) updateGrowthPetCombat(followerWorld cosFollowerWorld, pet *model.COS, owner *model.Player, now time.Time) bool {
	world, ok := followerWorld.(cosCombatWorld)
	if !ok || pet == nil || pet.Kind != model.COSGrowthPet {
		return false
	}
	s.mu.Lock()
	intent, active := s.attacks[pet.GetUniqueID()]
	next := s.nextAttack[pet.GetUniqueID()]
	rewards, levels := s.rewards, s.levels
	s.mu.Unlock()
	if !active {
		return false
	}
	object, err := world.GetObjectByUniqueId(intent.targetUID)
	target, valid := object.(*model.NPC)
	if err != nil || !valid || target == nil || target.GetLifeState() != model.Alive {
		_ = s.ClearAttackTarget(owner, pet.GetUniqueID())
		return false
	}
	ownerPos, targetPos := owner.GetNavmeshPosition(), target.GetNavmeshPosition()
	if ownerPos.Region.ID != targetPos.Region.ID || navmeshv2.HorizontalDistance(ownerPos, targetPos) > s.currentPolicy().Settings.CombatLeashRange {
		_ = s.ClearAttackTarget(owner, pet.GetUniqueID())
		return false
	}
	skill, exists := model.RefSkills[intent.skillID]
	if !exists || (skill.ExecutionKind() != model.SkillExecutionPhysicalAttack && skill.ExecutionKind() != model.SkillExecutionMagicalAttack) {
		_ = s.ClearAttackTarget(owner, pet.GetUniqueID())
		return false
	}
	petRef, petRefOK := model.RefChars[pet.GetRefObjectID()]
	targetRef, targetRefOK := model.RefChars[target.GetRefObjectID()]
	reach := float32(max(skill.ActionRange, 10) + 4)
	if petRefOK {
		reach += float32(max(petRef.BCRadius, 0))
	}
	if targetRefOK {
		reach += float32(max(targetRef.BCRadius, 0))
	}
	petPos := pet.GetNavmeshPosition()
	if navmeshv2.HorizontalDistance(petPos, targetPos) > reach {
		if movement := pet.GetMovementData(); movement != nil && movement.HasDestination && navmeshv2.HorizontalDistance(movement.TargetPosition, targetPos) <= s.currentPolicy().Settings.DestinationMatchRange {
			return true
		}
		movement := world.RegisterMovingCharacter(pet, model.MovementData{StartTime: now, UpdateTime: now, TargetPosition: targetPos, HasDestination: true})
		packet := model.BuildMovementUpdatePacket(pet, movement, petPos)
		pet.SendToKnown(&packet)
		return true
	}
	if pet.GetMotionState() != model.NoMotion {
		world.StopMovingCharacter(pet, petPos, "cos-attack-in-range", false)
	}
	if now.Before(next) {
		return true
	}
	cooldown := time.Duration(max(skill.CoolTime, skill.ReuseDelay)) * time.Millisecond
	if cooldown <= 0 {
		cooldown = s.currentPolicy().Settings.DefaultAttackCooldown
	}
	s.mu.Lock()
	s.nextAttack[pet.GetUniqueID()] = now.Add(cooldown)
	s.mu.Unlock()
	damage := max((skill.AttackMin+skill.AttackMax)/2, int(pet.Level)*2)
	if targetRefOK {
		if skill.IsMagicalAttack() {
			damage -= targetRef.MagDef
		} else {
			damage -= targetRef.PhyDef
		}
	}
	damage = max(damage, 1)
	applied, killed := target.ApplyDamage(damage)
	if applied == 0 {
		return true
	}
	target.AddAggro(pet.GetUniqueID(), applied)
	record := model.CombatDamageRecord{Damage: uint32(applied), DamageState: model.DamageStateNormal}
	if killed {
		record.AttackState = model.DamageAttackStateDead
	}
	castUID := model.NextActionUID()
	attack := model.BuildAttackBeginWithSkillDamageRecords(intent.skillID, pet.GetUniqueID(), target.GetUniqueID(), castUID, []model.CombatDamageRecord{record})
	pet.SendToKnown(&attack)
	if !killed {
		s.applyGrowthPetRetaliation(owner, pet, target, now)
	}
	if !killed {
		return true
	}
	world.StopMovingCharacter(target, targetPos, "cos-killed-mob", false)
	target.QueueRespawn()
	time.AfterFunc(target.DecayDuration(), func() { world.RemoveVisibleObject(target) })
	_ = s.ClearAttackTarget(owner, pet.GetUniqueID())
	if rewards != nil {
		_ = rewards.AwardCOSKill(context.Background(), owner, target)
	}
	gained := target.ExperienceReward()
	if gained > 0 && levels != nil {
		_, refChanged := pet.AwardExperience(levels, gained)
		_ = s.repository.Save(context.Background(), model.COSRecordFromSnapshot(pet.Snapshot()))
		experience := model.BuildCOSExperienceUpdate(pet.GetUniqueID(), gained, target.GetUniqueID())
		owner.SendToSelf(&experience)
		if refChanged {
			changed := model.BuildCOSRefObjectUpdate(pet.GetUniqueID(), pet.GetRefObjectID())
			owner.SendToSelfAndKnown(&changed)
		}
	}
	return true
}

func (s *COSService) applyGrowthPetRetaliation(owner *model.Player, pet *model.COS, attacker *model.NPC, now time.Time) {
	if s == nil || owner == nil || pet == nil || attacker == nil || pet.GetLifeState() != model.Alive || attacker.GetLifeState() != model.Alive || attacker.AggroValue(pet.GetUniqueID()) <= 0 {
		return
	}
	skillID := attacker.AttackSkillID()
	skill, exists := model.RefSkills[skillID]
	if !exists || skillID == 0 {
		return
	}
	reach := float32(max(skill.ActionRange, 10) + 4)
	if ref, ok := model.RefChars[attacker.GetRefObjectID()]; ok {
		reach += float32(max(ref.BCRadius, 0))
	}
	if ref, ok := model.RefChars[pet.GetRefObjectID()]; ok {
		reach += float32(max(ref.BCRadius, 0))
	}
	if navmeshv2.HorizontalDistance(attacker.GetNavmeshPosition(), pet.GetNavmeshPosition()) > reach {
		return
	}
	cooldown := time.Duration(max(skill.CoolTime, skill.ReuseDelay)) * time.Millisecond
	if cooldown <= 0 {
		cooldown = s.currentPolicy().Settings.DefaultAttackCooldown
	}
	s.mu.Lock()
	readyAt := s.nextRetaliation[attacker.GetUniqueID()]
	if now.Before(readyAt) {
		s.mu.Unlock()
		return
	}
	s.nextRetaliation[attacker.GetUniqueID()] = now.Add(cooldown)
	s.mu.Unlock()
	damage := max((skill.AttackMin+skill.AttackMax)/2, 1)
	if petRef, ok := model.RefChars[pet.GetRefObjectID()]; ok {
		if skill.IsMagicalAttack() {
			damage -= petRef.MagDef
		} else {
			damage -= petRef.PhyDef
		}
	}
	damage = max(damage, 1)
	applied, killed := pet.ApplyDamage(damage)
	if applied == 0 {
		return
	}
	record := model.CombatDamageRecord{DamageState: model.DamageStateNormal, Damage: uint32(applied)}
	if killed {
		record.AttackState = model.DamageAttackStateDead
	}
	attack := model.BuildAttackBeginWithSkillDamageRecords(skillID, attacker.GetUniqueID(), pet.GetUniqueID(), model.NextActionUID(), []model.CombatDamageRecord{record})
	// Broadcast from the owner/pet side rather than relying on the attacker's
	// known-object list. Visibility lists are updated independently, and the
	// owner must receive the lethal damage record (including its dead bit) for
	// the client to transition the COS from zero HP into the death animation.
	owner.SendToSelfAndKnown(&attack)
	vitals := model.BuildVitalsUpdatePacket(pet.GetUniqueID(), int(pet.Snapshot().Health), 0)
	owner.SendToSelfAndKnown(&vitals)
	recordSnapshot := model.COSRecordFromSnapshot(pet.Snapshot())
	destroyed := false
	if killed {
		var releaseErr error
		destroyed, releaseErr = s.releaseJobTransportCargo(context.Background(), pet)
		if releaseErr != nil {
			logrus.WithError(releaseErr).WithField("cos_id", pet.PersistentID).Error("failed to release dead job-transport cargo")
		}
		recordSnapshot.State = model.COSItemDead
		owner.SetCOSItemState(pet.InventorySlot, pet, model.COSItemDead)
		itemState := model.BuildCOSItemStateUpdate(pet.InventorySlot, model.COSItemDead)
		owner.SendToSelf(&itemState)
		if owner.COSState.Snapshot().TransportUniqueID == pet.GetUniqueID() {
			// The lethal COS packets do not release the client's mounted owner
			// controller by themselves. Publish the stock unmount transition
			// before removing the dead transport from the world.
			unmount := network.EmptyPacket()
			unmount.MessageID = opcode.COSUpdateMountStateResponse
			unmount.WriteByte(1)
			unmount.WriteUInt32(owner.GetUniqueID())
			unmount.WriteBool(false)
			unmount.WriteUInt32(pet.GetUniqueID())
			owner.SendToSelfAndKnown(&unmount)
		}
		life := model.BuildLifeStateUpdatePacket(pet.GetUniqueID(), model.Dead)
		owner.SendToSelfAndKnown(&life)
		attacker.ClearAggroTarget(pet.GetUniqueID())
		_ = s.ClearAttackTarget(owner, pet.GetUniqueID())
	}
	if !destroyed {
		_ = s.repository.Save(context.Background(), recordSnapshot)
	}
	if killed {
		// A dead growth COS must leave the active world registry. Keeping its
		// corpse as the owner's active companion leaves the v188 client action
		// controller bound to a dead target and can lock owner movement/attacks.
		s.detachRuntime(owner, pet, model.COSItemDead)
		if destroyed {
			owner.ClearCOSItem(pet.InventorySlot, pet)
		}
	}
}
