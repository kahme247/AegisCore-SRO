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
	"github.com/g3n/engine/math32"
)

var (
	ErrCOSAlreadySummoned = errors.New("a COS of this class is already summoned")
	ErrCOSNotSummoned     = errors.New("COS is not summoned")
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

const (
	cosFollowBaseRadius      = float32(30)
	cosFollowRadiusStep      = float32(4)
	cosOrbitMinRadius        = float32(38)
	cosOrbitRadiusVariance   = uint32(2001) // 0.00 through 20.00 world units
	cosPickupOrbitMinRadius  = float32(85)
	cosPickupRadiusVariance  = uint32(7501) // 0.00 through 75.00 world units
	cosCompanionRelocate     = float32(80)
	cosPickupPetRelocate     = float32(190)
	cosOrbitMinAngleStep     = float32(45)
	cosOrbitAngleStepRange   = uint32(41) // 45 through 85 degrees per waypoint
	cosArrivalRadius         = float32(3)
	cosDestinationMatchRange = float32(2.5)
	cosCombatLeashRadius     = float32(120)
)

// COSService owns the atomic owner/world/persistence lifecycle. Its indexes
// are authoritative only for the running Agent; durable pet state lives in
// CHAR_COS and is checkpointed before world removal.
type COSService struct {
	mu          sync.Mutex
	repository  model.COSRepository
	world       COSWorld
	byUID       map[uint32]*model.COS
	byOwnerKind map[int]map[model.COSKind]*model.COS
	lastFollow  map[uint32]time.Time
	companionAI map[uint32]*cosCompanionAI
	attacks     map[uint32]cosAttackIntent
	nextAttack  map[uint32]time.Time
	rentalRetry map[uint32]time.Time
	levels      *model.LevelTable
	rewards     COSCombatRewards
}

func NewCOSService(repository model.COSRepository, world COSWorld) *COSService {
	return &COSService{repository: repository, world: world, byUID: make(map[uint32]*model.COS), byOwnerKind: make(map[int]map[model.COSKind]*model.COS), lastFollow: make(map[uint32]time.Time), companionAI: make(map[uint32]*cosCompanionAI), attacks: make(map[uint32]cosAttackIntent), nextAttack: make(map[uint32]time.Time), rentalRetry: make(map[uint32]time.Time)}
}

func (s *COSService) ConfigureCombat(rewards COSCombatRewards, levels *model.LevelTable) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.rewards, s.levels = rewards, levels
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
	if record.RentEndsAt != nil {
		cos.RentEndsAt = record.RentEndsAt.UTC()
	}
	originalState := record.State
	if originalState < model.COSItemInactive || originalState > model.COSItemDead {
		originalState = model.COSItemActive
	}

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
	s.companionAI[cos.GetUniqueID()] = newCOSCompanionAI(cos)
	s.mu.Unlock()
	if record.Kind == model.COSRideTransport {
		if err = owner.COSState.ActivateTransport(cos.GetUniqueID(), cos.GetRefObjectID(), false); err != nil {
			s.detachRuntime(owner, cos, originalState)
			return nil, err
		}
	}
	if !owner.SetCOSItemState(record.InventorySlot, cos, model.COSItemSummoned) {
		s.detachRuntime(owner, cos, originalState)
		return nil, fmt.Errorf("COS inventory item %d is no longer linked to persistent COS %d", record.InventorySlot, record.ID)
	}
	record = model.COSRecordFromSnapshot(cos.Snapshot())
	if err = s.repository.Save(ctx, record); err != nil {
		s.detachRuntime(owner, cos, originalState)
		return nil, err
	}
	return cos, nil
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
	record := model.COSRecordFromSnapshot(cos.Snapshot())
	record.State = model.COSItemActive
	if err = s.repository.Save(ctx, record); err != nil {
		return err
	}
	s.detachRuntime(owner, cos, model.COSItemActive)
	return nil
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
	delete(s.nextAttack, uid)
	delete(s.rentalRetry, uid)
	if kinds := s.byOwnerKind[owner.ID]; kinds != nil {
		delete(kinds, cos.Kind)
		if len(kinds) == 0 {
			delete(s.byOwnerKind, owner.ID)
		}
	}
	s.mu.Unlock()
	if cos.Kind == model.COSRideTransport {
		_ = owner.COSState.ClearTransport(uid)
	}
	s.world.RemoveVisibleObject(cos)
	owner.SetCOSItemState(cos.InventorySlot, cos, itemState)
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
		owner, err := world.GetPlayerByUniqueId(pet.OwnerUniqueID)
		if err != nil || owner == nil || owner.ID != pet.OwnerCharID || owner.GetLifeState() == model.Spawning {
			continue
		}
		if s.expireAbilityPetRental(owner, pet, now) {
			continue
		}
		if s.updateGrowthPetCombat(world, pet, owner, now) {
			continue
		}
		petPos, ownerPos := pet.GetNavmeshPosition(), owner.GetNavmeshPosition()
		distance := navmeshv2.HorizontalDistance(petPos, ownerPos)
		farFromOwner := distance > companionRelocationRadius(pet.Kind) || petPos.Region.ID != ownerPos.Region.ID
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
		if targetDistance <= cosArrivalRadius {
			if pet.GetMotionState() != model.NoMotion {
				world.StopMovingCharacter(pet, petPos, "cos-owner-in-range", false)
			}
			continue
		}
		// UpdateFollowers runs more frequently than an orbit waypoint changes.
		// Re-registering an unchanged destination increments the authoritative
		// movement generation and makes the client repeatedly restart the same
		// run. Keep the active movement until the AI selects a real new target.
		if movement := pet.GetMovementData(); movement != nil && movement.HasDestination && navmeshv2.HorizontalDistance(movement.TargetPosition, target) <= cosDestinationMatchRange {
			continue
		}
		s.mu.Lock()
		last := s.lastFollow[pet.GetUniqueID()]
		if now.Sub(last) < 500*time.Millisecond {
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

// expireAbilityPetRental retires a summoned fixed-period pet through the same
// persistence and world teardown path as a client-requested unsummon. A failed
// save leaves the pet indexed for retry but suppresses movement and combat once
// the authoritative expiry instant has passed.
func (s *COSService) expireAbilityPetRental(owner *model.Player, pet *model.COS, now time.Time) bool {
	if pet == nil || pet.Kind != model.COSAbilityPet || pet.RentEndsAt.IsZero() || pet.RentEndsAt.After(now.UTC()) {
		return false
	}
	uid, slot := pet.GetUniqueID(), pet.InventorySlot
	s.mu.Lock()
	retryAt := s.rentalRetry[uid]
	if now.Before(retryAt) {
		s.mu.Unlock()
		return true
	}
	s.rentalRetry[uid] = now.Add(5 * time.Second)
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
		return err
	}
	if pet.Kind != model.COSGrowthPet || targetUID == 0 {
		return model.ErrInvalidCOSKind
	}
	ref, exists := model.RefChars[pet.GetRefObjectID()]
	if !exists {
		return model.ErrInvalidCOSKind
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
		return model.ErrItemUseInvalidRequest
	}
	world, ok := s.world.(cosCombatWorld)
	if !ok {
		return model.ErrCOSRepositoryUnavailable
	}
	object, err := world.GetObjectByUniqueId(targetUID)
	target, validTarget := object.(*model.NPC)
	if err != nil || !validTarget || target == nil || !target.GetTypeInfo().IsNPCMob() || target.GetLifeState() != model.Alive {
		return model.ErrItemUseInvalidRequest
	}
	ownerPos, targetPos := owner.GetNavmeshPosition(), target.GetNavmeshPosition()
	if ownerPos.Region.ID != targetPos.Region.ID || navmeshv2.HorizontalDistance(ownerPos, targetPos) > cosCombatLeashRadius {
		return model.ErrItemUseInvalidRequest
	}
	s.mu.Lock()
	previous := s.attacks[petUID]
	s.attacks[petUID] = cosAttackIntent{targetUID: targetUID, skillID: skillID}
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
	if ownerPos.Region.ID != targetPos.Region.ID || navmeshv2.HorizontalDistance(ownerPos, targetPos) > cosCombatLeashRadius {
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
		if movement := pet.GetMovementData(); movement != nil && movement.HasDestination && navmeshv2.HorizontalDistance(movement.TargetPosition, targetPos) <= cosDestinationMatchRange {
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
		cooldown = 1500 * time.Millisecond
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

func newCOSCompanionAI(pet *model.COS) *cosCompanionAI {
	seed := uint32(pet.PersistentID) ^ pet.GetRefObjectID() ^ uint32(pet.Kind)<<24
	if seed == 0 {
		seed = 1
	}
	// Stable follow slots spread companions while the player is travelling.
	// At rest the angle becomes only the starting point of a true orbit.
	angle := float32(seed%360) * (2 * math.Pi / 360)
	followRadius := cosFollowBaseRadius + float32((seed>>9)%4)*cosFollowRadiusStep
	direction := float32(1)
	if seed&1 != 0 {
		direction = -1
	}
	orbitMin, orbitVariance := companionOrbitRadius(pet.Kind)
	return &cosCompanionAI{
		seed:           seed,
		followAngle:    angle,
		followRadius:   followRadius,
		orbitAngle:     angle,
		orbitRadius:    orbitMin + float32((seed>>12)%orbitVariance)/100,
		orbitDirection: direction,
	}
}

func companionOrbitRadius(kind model.COSKind) (float32, uint32) {
	if kind == model.COSAbilityPet {
		return cosPickupOrbitMinRadius, cosPickupRadiusVariance
	}
	return cosOrbitMinRadius, cosOrbitRadiusVariance
}

func companionRelocationRadius(kind model.COSKind) float32 {
	if kind == model.COSAbilityPet {
		return cosPickupPetRelocate
	}
	return cosCompanionRelocate
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
	s.mu.Lock()
	ai := s.companionAI[pet.GetUniqueID()]
	if ai == nil {
		ai = newCOSCompanionAI(pet)
		s.companionAI[pet.GetUniqueID()] = ai
	}
	ownerMoving := owner.GetMotionState() != model.NoMotion
	if ownerMoving {
		ai.ownerMoving = true
		ai.nextWaypoint = now.Add(2 * time.Second)
	} else if ai.ownerMoving {
		// Resume circling promptly after the owner stops, from the pet's stable
		// follow side rather than jumping to an unrelated point.
		ai.ownerMoving = false
		ai.orbitAngle = ai.followAngle
		ai.nextWaypoint = time.Time{}
	}
	if !ownerMoving && (ai.nextWaypoint.IsZero() || !now.Before(ai.nextWaypoint)) {
		ai.seed = ai.seed*1664525 + 1013904223
		stepDegrees := cosOrbitMinAngleStep + float32(ai.seed%cosOrbitAngleStepRange)
		ai.orbitAngle += ai.orbitDirection * stepDegrees * (2 * math.Pi / 360)
		ai.seed = ai.seed*1664525 + 1013904223
		orbitMin, orbitVariance := companionOrbitRadius(pet.Kind)
		ai.orbitRadius = orbitMin + float32(ai.seed%orbitVariance)/100
		ai.nextWaypoint = now.Add(time.Duration(2+ai.seed%3) * time.Second)
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
		data := model.BuildCOSData(pet)
		owner.SendToSelf(&data)
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
