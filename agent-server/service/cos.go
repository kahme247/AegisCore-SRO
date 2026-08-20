package service

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	seed            uint32
	formationAngle  float32
	formationRadius float32
	roamAngle       float32
	roamRadius      float32
	nextRoam        time.Time
}

const (
	cosFormationBaseRadius = float32(28)
	cosFormationRadiusStep = float32(5)
	cosRoamBaseRadius      = float32(6)
	cosRoamRadiusVariance  = uint32(501) // 0.00 through 5.00 world units
	cosArrivalRadius       = float32(3)
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
	levels      *model.LevelTable
	rewards     COSCombatRewards
}

func NewCOSService(repository model.COSRepository, world COSWorld) *COSService {
	return &COSService{repository: repository, world: world, byUID: make(map[uint32]*model.COS), byOwnerKind: make(map[int]map[model.COSKind]*model.COS), lastFollow: make(map[uint32]time.Time), companionAI: make(map[uint32]*cosCompanionAI), attacks: make(map[uint32]cosAttackIntent), nextAttack: make(map[uint32]time.Time)}
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
	if record.RentEndsAt != nil && !record.RentEndsAt.After(time.Now().UTC()) {
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
			_ = s.Unsummon(ctx, owner, cos.GetUniqueID())
			return nil, err
		}
	}
	if !owner.SetCOSItemState(record.InventorySlot, cos, model.COSItemSummoned) {
		_ = s.Unsummon(ctx, owner, cos.GetUniqueID())
		return nil, fmt.Errorf("COS inventory item %d is no longer linked to persistent COS %d", record.InventorySlot, record.ID)
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
	if err = s.repository.Save(ctx, model.COSRecordFromSnapshot(cos.Snapshot())); err != nil {
		return err
	}
	s.mu.Lock()
	if current := s.byUID[uid]; current != cos {
		s.mu.Unlock()
		return ErrCOSNotSummoned
	}
	delete(s.byUID, uid)
	delete(s.lastFollow, uid)
	delete(s.companionAI, uid)
	delete(s.attacks, uid)
	delete(s.nextAttack, uid)
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
	owner.SetCOSItemState(cos.InventorySlot, cos, model.COSItemActive)
	s.world.RemoveVisibleObject(cos)
	return nil
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
		if s.updateGrowthPetCombat(world, pet, owner, now) {
			continue
		}
		petPos, ownerPos := pet.GetNavmeshPosition(), owner.GetNavmeshPosition()
		distance := navmeshv2.HorizontalDistance(petPos, ownerPos)
		farFromOwner := distance > 80 || petPos.Region.ID != ownerPos.Region.ID
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
	s.mu.Lock()
	s.attacks[petUID] = cosAttackIntent{targetUID: targetUID, skillID: skillID}
	delete(s.nextAttack, petUID)
	s.mu.Unlock()
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
	petPos, targetPos := pet.GetNavmeshPosition(), target.GetNavmeshPosition()
	if navmeshv2.HorizontalDistance(petPos, targetPos) > reach {
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
	// Stable slots spread multiple companions around the owner. Stock COS rows
	// commonly use an 11-unit collision radius, so these distances keep the pet
	// outside the combined owner/pet footprint while remaining nearby.
	angle := float32(seed%360) * (2 * math.Pi / 360)
	radius := cosFormationBaseRadius + float32((seed>>9)%4)*cosFormationRadiusStep
	return &cosCompanionAI{seed: seed, formationAngle: angle, formationRadius: radius}
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
	if owner.GetMotionState() != model.NoMotion {
		ai.roamRadius = 0
		ai.nextRoam = now.Add(3 * time.Second)
	} else if ai.nextRoam.IsZero() || !now.Before(ai.nextRoam) {
		ai.seed = ai.seed*1664525 + 1013904223
		ai.roamAngle = float32(ai.seed%360) * (2 * math.Pi / 360)
		ai.seed = ai.seed*1664525 + 1013904223
		ai.roamRadius = cosRoamBaseRadius + float32(ai.seed%cosRoamRadiusVariance)/100
		ai.nextRoam = now.Add(time.Duration(4+ai.seed%5) * time.Second)
	}
	formationAngle, formationRadius := ai.formationAngle, ai.formationRadius
	roamAngle, roamRadius := ai.roamAngle, ai.roamRadius
	s.mu.Unlock()

	ownerGlobal := owner.GetNavmeshPosition().GetGlobalCoordinates()
	targetGlobal := math32.NewVector3(
		ownerGlobal.X+formationRadius*float32(math.Cos(float64(formationAngle)))+roamRadius*float32(math.Cos(float64(roamAngle))),
		ownerGlobal.Y,
		ownerGlobal.Z+formationRadius*float32(math.Sin(float64(formationAngle)))+roamRadius*float32(math.Sin(float64(roamAngle))),
	)
	target, err := world.NewPosFromGlobalCoordinates(targetGlobal)
	if err != nil || (requireDirect && !world.CanMoveDirectly(pet.GetNavmeshPosition(), target)) {
		// A roaming offset may cross a wall while the stable formation slot is
		// still valid. Retry the slot before giving up and retaining position.
		targetGlobal = math32.NewVector3(
			ownerGlobal.X+formationRadius*float32(math.Cos(float64(formationAngle))),
			ownerGlobal.Y,
			ownerGlobal.Z+formationRadius*float32(math.Sin(float64(formationAngle))),
		)
		target, err = world.NewPosFromGlobalCoordinates(targetGlobal)
		if err != nil || (requireDirect && !world.CanMoveDirectly(pet.GetNavmeshPosition(), target)) {
			return navmeshv2.RtNavmeshPosition{}, false
		}
	}
	return target, true
}

// RehydratePlayer relocates and republishes active pets after the stock client
// clears its object table during teleport loading.
func (s *COSService) RehydratePlayer(_ context.Context, owner *model.Player) error {
	if s == nil || owner == nil {
		return nil
	}
	world, ok := s.world.(cosFollowerWorld)
	if !ok {
		return nil
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
