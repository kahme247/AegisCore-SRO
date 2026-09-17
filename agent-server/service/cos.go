package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/questwire"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
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

type cosPickupWorld interface {
	cosCombatWorld
	GetKnownObjectsAroundObject(model.ISRObject) map[uint32]model.ISRObject
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

func (s *COSService) ApplyPersistedTimedService(owner *model.Player, persistentID uint64, service model.COSTimedService) (*model.COS, error) {
	if s == nil || owner == nil || persistentID == 0 {
		return nil, model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	var pet *model.COS
	for _, candidate := range s.byOwnerKind[owner.ID] {
		if candidate != nil && candidate.PersistentID == persistentID {
			pet = candidate
			break
		}
	}
	s.mu.Unlock()
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
	s.mu.Lock()
	var pet *model.COS
	for _, candidate := range s.byOwnerKind[owner.ID] {
		if candidate != nil && candidate.PersistentID == persistentID {
			pet = candidate
			break
		}
	}
	s.mu.Unlock()
	if pet == nil {
		return nil, nil
	}
	if pet.OwnerUniqueID != owner.GetUniqueID() || !pet.ApplyCommittedRentEnd(rentEndsAt) {
		return nil, model.ErrCOSNotOwned
	}
	return pet, nil
}

func (s *COSService) ApplyPersistedResources(owner *model.Player, persistentID uint64, health int32, hgp int) (*model.COS, error) {
	if s == nil || owner == nil || persistentID == 0 {
		return nil, model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	var pet *model.COS
	for _, candidate := range s.byOwnerKind[owner.ID] {
		if candidate != nil && candidate.PersistentID == persistentID {
			pet = candidate
			break
		}
	}
	s.mu.Unlock()
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
	s.mu.Lock()
	var pet *model.COS
	for _, candidate := range s.byOwnerKind[owner.ID] {
		if candidate != nil && candidate.PersistentID == persistentID {
			pet = candidate
			break
		}
	}
	s.mu.Unlock()
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
	// Send the post-merge slot state. Sending the ground item here leaves the
	// client with a stale quantity when a pet pickup merges into an existing
	// stack.
	updatedItem, _ := pet.Inventory.Snapshot().Items[state.storageSlot]
	update := model.BuildCOSPickupInventoryUpdate(pet.GetUniqueID(), state.storageSlot, updatedItem)
	owner.SendToSelf(&update)
	if inventoryUpdate, supported := model.BuildCOSInventoryUpdate(pet); supported {
		owner.SendToSelf(&inventoryUpdate)
	}
	// SR_GameServer.exe 0x004D4F60 commits/publishes the COS inventory change
	// through sub_4B29A0 before it builds 0x3036. Preserve that ordering: the
	// client resolves operation 0x17 into the pet slot first, then completes the
	// pickup action animation. Reversing them leaves the action controller and
	// visible COS inventory stale.
	pickup := vsro188.BuildEntityPickupAnimation(pet.GetUniqueID(), 0)
	owner.SendToSelfAndKnown(&pickup)
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
	// Stock SR_GameServer emits the B034/0x17 transfer completion before its
	// post-completion ground-object cleanup. The client resolves the item from
	// that still-live object when applying the COS slot update; despawning it
	// first commits SQL but leaves both client inventories visually unchanged.
	world.RemoveVisibleObject(object)
	s.finishPickupAction(pet.GetUniqueID(), state.targetUID)
	return true
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

func newCOSCompanionAI(pet *model.COS, settings model.COSGameplaySettings) *cosCompanionAI {
	seed := uint32(pet.PersistentID) ^ pet.GetRefObjectID() ^ uint32(pet.Kind)<<24
	if seed == 0 {
		seed = 1
	}
	// Stable follow slots spread companions while the player is travelling.
	// At rest the angle becomes only the starting point of a true orbit.
	angle := float32(seed%360) * (2 * math.Pi / 360)
	followRadius := settings.FollowBaseRadius + float32((seed>>9)%4)*settings.FollowRadiusStep
	direction := float32(1)
	if seed&1 != 0 {
		direction = -1
	}
	orbitMin, orbitVariance := companionOrbitRadius(pet.Kind, settings)
	return &cosCompanionAI{
		seed:           seed,
		followAngle:    angle,
		followRadius:   followRadius,
		orbitAngle:     angle,
		orbitRadius:    orbitMin + float32((seed>>12)%orbitVariance)/100,
		orbitDirection: direction,
	}
}

func companionOrbitRadius(kind model.COSKind, settings model.COSGameplaySettings) (float32, uint32) {
	if kind == model.COSAbilityPet {
		return settings.PickupIdleMinRadius, settings.PickupIdleVariance
	}
	return settings.OrbitMinRadius, settings.OrbitRadiusVariance
}

func companionRelocationRadius(kind model.COSKind, settings model.COSGameplaySettings) float32 {
	if kind == model.COSAbilityPet {
		return settings.PickupPetRelocateRange
	}
	return settings.CompanionRelocateRange
}

func usesCompanionFormation(kind model.COSKind) bool {
	switch kind {
	case model.COSGrowthPet, model.COSAbilityPet, model.COSCaptured, model.COSQuest, model.COSQuest2, model.COSFellowPet:
		return true
	default:
		return false
	}
}

func (s *COSService) companionTarget(world cosFollowerWorld, pet *model.COS, owner *model.Player, now time.Time, requireDirect bool) (navmeshv2.RtNavmeshPosition, bool) {
	if pet == nil || owner == nil {
		return navmeshv2.RtNavmeshPosition{}, false
	}
	if !usesCompanionFormation(pet.Kind) {
		return owner.GetNavmeshPosition(), true
	}
	settings := s.currentPolicy().Settings
	s.mu.Lock()
	ai := s.companionAI[pet.GetUniqueID()]
	if ai == nil {
		ai = newCOSCompanionAI(pet, settings)
		s.companionAI[pet.GetUniqueID()] = ai
	}
	ownerMoving := owner.GetMotionState() != model.NoMotion
	if ownerMoving {
		ai.ownerMoving = true
		ai.nextWaypoint = now.Add(settings.IdleWaypointMinInterval)
	} else if ai.ownerMoving {
		// Resume circling promptly after the owner stops, from the pet's stable
		// follow side rather than jumping to an unrelated point.
		ai.ownerMoving = false
		ai.orbitAngle = ai.followAngle
		ai.nextWaypoint = time.Time{}
	}
	if !ownerMoving && (ai.nextWaypoint.IsZero() || !now.Before(ai.nextWaypoint)) {
		ai.seed = ai.seed*1664525 + 1013904223
		stepDegrees := settings.OrbitMinAngleStep + float32(ai.seed%settings.OrbitAngleStepRange)
		ai.orbitAngle += ai.orbitDirection * stepDegrees * (2 * math.Pi / 360)
		ai.seed = ai.seed*1664525 + 1013904223
		orbitMin, orbitVariance := companionOrbitRadius(pet.Kind, settings)
		ai.orbitRadius = orbitMin + float32(ai.seed%orbitVariance)/100
		waypointDelay := settings.IdleWaypointMinInterval
		if settings.IdleWaypointVariance > 0 {
			waypointDelay += time.Duration(uint64(ai.seed) % uint64(settings.IdleWaypointVariance))
		}
		ai.nextWaypoint = now.Add(waypointDelay)
	}
	targetAngle, targetRadius := ai.orbitAngle, ai.orbitRadius
	if ownerMoving {
		targetAngle, targetRadius = ai.followAngle, ai.followRadius
	}
	followAngle, followRadius := ai.followAngle, ai.followRadius
	s.mu.Unlock()

	ownerGlobal := owner.GetNavmeshPosition().GetGlobalCoordinates()
	targetGlobal := math32.NewVector3(
		ownerGlobal.X+targetRadius*float32(math.Cos(float64(targetAngle))),
		ownerGlobal.Y,
		ownerGlobal.Z+targetRadius*float32(math.Sin(float64(targetAngle))),
	)
	target, err := world.NewPosFromGlobalCoordinates(targetGlobal)
	if err != nil || (requireDirect && !world.CanMoveDirectly(pet.GetNavmeshPosition(), target)) {
		// An orbit waypoint may cross a wall while the closer stable follow slot
		// is still valid. Retry that slot before retaining the current position.
		targetGlobal = math32.NewVector3(
			ownerGlobal.X+followRadius*float32(math.Cos(float64(followAngle))),
			ownerGlobal.Y,
			ownerGlobal.Z+followRadius*float32(math.Sin(float64(followAngle))),
		)
		target, err = world.NewPosFromGlobalCoordinates(targetGlobal)
		if err != nil || (requireDirect && !world.CanMoveDirectly(pet.GetNavmeshPosition(), target)) {
			return navmeshv2.RtNavmeshPosition{}, false
		}
	}
	return target, true
}

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
