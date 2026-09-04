package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type cosRepositoryFake struct {
	record   model.COSRecord
	saved    []model.COSRecord
	saveErr  error
	stored   []int
	services []model.COSTimedService
}

type cosGroundItemLifecycleFake struct{ canceled *model.Item }

type fixedCOSPolicyProvider struct{ policy model.COSPolicySnapshot }

func (p fixedCOSPolicyProvider) Current() model.COSPolicySnapshot { return p.policy }

func (f *cosGroundItemLifecycleFake) Cancel(item *model.Item) { f.canceled = item }

func (f *cosRepositoryFake) LoadOwned(_ context.Context, owner int, id uint64) (model.COSRecord, error) {
	if f.record.OwnerCharID != owner || f.record.ID != id {
		return model.COSRecord{}, model.ErrCOSNotFound
	}
	return f.record, nil
}
func (f *cosRepositoryFake) Save(_ context.Context, record model.COSRecord) error {
	f.saved = append(f.saved, record)
	return f.saveErr
}
func (f *cosRepositoryFake) Rename(context.Context, int, uint64, string) error { return nil }
func (f *cosRepositoryFake) LoadInventory(context.Context, int, uint64) (model.Inventory, error) {
	return model.NewInventory(nil), nil
}
func (f *cosRepositoryFake) LoadCOSTimedServices(context.Context, int, uint64, time.Time) ([]model.COSTimedService, error) {
	return append([]model.COSTimedService(nil), f.services...), nil
}
func (f *cosRepositoryFake) StoreItem(_ context.Context, _ int, _ uint64, _ byte, itemID int) error {
	f.stored = append(f.stored, itemID)
	return nil
}

type cosWorldFake struct {
	next           uint32
	added, removed []model.ISRObject
	players        map[uint32]*model.Player
	objects        map[uint32]model.ISRObject
	registered     []model.ICharacter
	movements      []model.MovementData
	stopped        []model.ICharacter
}

func (w *cosWorldFake) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	player := w.players[uid]
	if player == nil {
		return nil, model.ErrCOSNotFound
	}
	return player, nil
}
func (w *cosWorldFake) GetObjectByUniqueId(uid uint32) (model.ISRObject, error) {
	object := w.objects[uid]
	if object == nil {
		return nil, model.ErrCOSNotFound
	}
	return object, nil
}
func (w *cosWorldFake) GetKnownObjectsAroundObject(model.ISRObject) map[uint32]model.ISRObject {
	return w.objects
}
func (w *cosWorldFake) NewPosFromGlobalCoordinates(global *math32.Vector3) (navmeshv2.RtNavmeshPosition, error) {
	return navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: global.Clone()}, nil
}
func (w *cosWorldFake) CanMoveDirectly(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) bool {
	return true
}
func (w *cosWorldFake) RegisterMovingCharacter(character model.ICharacter, movement model.MovementData) model.MovementData {
	registered := character.BeginMovement(movement)
	character.SetMotionState(model.Running)
	w.registered = append(w.registered, character)
	w.movements = append(w.movements, registered)
	return registered
}
func (w *cosWorldFake) StopMovingCharacter(character model.ICharacter, position navmeshv2.RtNavmeshPosition, reason string, _ bool) bool {
	character.CancelMovement(position, reason)
	character.SetMotionState(model.NoMotion)
	w.stopped = append(w.stopped, character)
	return true
}

func (w *cosWorldFake) AddVisibleObject(object model.ISRObject) {
	w.next++
	object.SetUniqueID(w.next)
	w.added = append(w.added, object)
}

func TestCOSServiceFollowsAndRelocatesToOwner(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9002: {RefObject: model.RefObject{ID: 9002, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}, Speed1: 20, Speed2: 60}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 16}}
	owner := &model.Player{ID: 6, CharName: "Bob", LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{16: {Data: 78, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}}}
	world := &cosWorldFake{next: 200, players: map[uint32]*model.Player{10: owner}}
	runtime := NewCOSService(repository, world)
	pet, err := runtime.Summon(context.Background(), owner, 78)
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	owner.RtNavmeshPosition.Offset = math32.NewVector3(20, 0, 0)
	owner.SetRunSpeed(45)
	now := time.Now()
	runtime.UpdateFollowers(now)
	if len(world.registered) != 1 || pet.GetMotionState() != model.Running {
		t.Fatalf("follow registration = %d motion=%d", len(world.registered), pet.GetMotionState())
	}
	if got := pet.GetMovementSpeed(); got != owner.GetMovementSpeed() {
		t.Fatalf("growth pet speed = %g, want owner speed %g", got, owner.GetMovementSpeed())
	}
	owner.SetRunSpeed(75)
	runtime.UpdateFollowers(now.Add(250 * time.Millisecond))
	if got := pet.GetMovementSpeed(); got != 75 {
		t.Fatalf("dynamic growth pet speed = %g, want 75", got)
	}
	targetDistance := navmeshv2.HorizontalDistance(world.movements[0].TargetPosition, owner.GetNavmeshPosition())
	if targetDistance < 38 || targetDistance > 59 {
		t.Fatalf("companion target is %.2fm from owner, want bounded orbit distance", targetDistance)
	}
	firstTarget := world.movements[0].TargetPosition
	firstGeneration := pet.GetMovementGeneration()
	runtime.UpdateFollowers(now.Add(time.Second))
	if len(world.movements) != 1 || pet.GetMovementGeneration() != firstGeneration {
		t.Fatalf("unchanged orbit target restarted movement: moves=%d generation=%d want=%d", len(world.movements), pet.GetMovementGeneration(), firstGeneration)
	}
	pet.CancelMovement(firstTarget, "test-arrival")
	pet.SetMotionState(model.NoMotion)
	runtime.UpdateFollowers(now.Add(5 * time.Second))
	if len(world.movements) != 2 {
		t.Fatalf("idle companion moves = %d, want a second orbit waypoint", len(world.movements))
	}
	if moved := navmeshv2.HorizontalDistance(firstTarget, world.movements[1].TargetPosition); moved < 25 {
		t.Fatalf("orbit waypoint changed only %.2f world units", moved)
	}
	secondDistance := navmeshv2.HorizontalDistance(world.movements[1].TargetPosition, owner.GetNavmeshPosition())
	if secondDistance < 38 || secondDistance > 59 {
		t.Fatalf("second orbit target is %.2fm from owner", secondDistance)
	}
	owner.RtNavmeshPosition.Offset = math32.NewVector3(200, 0, 0)
	runtime.UpdateFollowers(now.Add(10 * time.Second))
	ownerDistance := navmeshv2.HorizontalDistance(pet.GetNavmeshPosition(), owner.GetNavmeshPosition())
	if len(world.stopped) != 1 || ownerDistance < 38 || ownerDistance > 59 {
		t.Fatalf("far relocation stops=%d pet=%v owner=%v", len(world.stopped), pet.GetNavmeshPosition(), owner.GetNavmeshPosition())
	}
}

func TestCOSServiceAppliesDatabaseBackedSpeedPolicyByKind(t *testing.T) {
	settings := model.DefaultCOSPolicySnapshot()
	settings.Kinds[model.COSGrowthPet].InheritOwnerSpeed = false
	settings.Kinds[model.COSAbilityPet].InheritOwnerSpeed = true
	provider := fixedCOSPolicyProvider{policy: settings}
	owner := &model.Player{RunSpeed: 75, MotionState: model.Running}
	growth := &model.COS{NPC: model.NPC{Mutex: new(sync.Mutex), RunSpeed: 40}, Kind: model.COSGrowthPet}
	ability := &model.COS{NPC: model.NPC{Mutex: new(sync.Mutex), RunSpeed: 50}, Kind: model.COSAbilityPet}
	runtime := NewCOSService(nil, nil)
	runtime.ConfigurePolicy(provider)
	policy := runtime.currentPolicy()
	growth.SetOwnerMovementSpeed(owner.GetMovementSpeed(), policy.ForKind(growth.Kind).InheritOwnerSpeed)
	ability.SetOwnerMovementSpeed(owner.GetMovementSpeed(), policy.ForKind(ability.Kind).InheritOwnerSpeed)
	if got := growth.GetMovementSpeed(); got != 40 {
		t.Fatalf("database-disabled growth owner speed = %g, want native 40", got)
	}
	if got := ability.GetMovementSpeed(); got != 75 {
		t.Fatalf("database-enabled ability owner speed = %g, want owner 75", got)
	}
}

func TestCOSServiceResolvesOwnedRuntimeTargetAndAppliesCommittedResources(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9002: {RefObject: model.RefObject{ID: 9002, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, Level: 1, Health: 50, MaxHealth: 100, HGP: 20, InventorySlot: 16}}
	owner := &model.Player{ID: 6, LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{16: {Data: 78, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10}}
	runtime := NewCOSService(repository, &cosWorldFake{next: 200})
	pet, err := runtime.Summon(context.Background(), owner, 78)
	if err != nil {
		t.Fatal(err)
	}
	if persistentID, ok := runtime.ResolvePersistentTarget(owner, pet.GetUniqueID()); !ok || persistentID != 78 {
		t.Fatalf("resolved target = %d, %v", persistentID, ok)
	}
	stranger := &model.Player{ID: 7, SRObject: model.SRObject{UniqueID: 11}}
	if _, ok := runtime.ResolvePersistentTarget(stranger, pet.GetUniqueID()); ok {
		t.Fatal("runtime target resolved for a non-owner")
	}
	updated, err := runtime.ApplyPersistedResources(owner, 78, 90, 30)
	if err != nil || updated != pet || pet.Health != 90 || pet.HGP != 30 {
		t.Fatalf("applied resources pet=%p err=%v health=%d hgp=%d", updated, err, pet.Health, pet.HGP)
	}
}

func TestCOSManualMoveHoldsUntilFollowCommand(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9002: {RefObject: model.RefObject{ID: 9002, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}, Speed1: 20, Speed2: 60}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 16}}
	owner := &model.Player{ID: 6, LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{16: {Data: 78, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}}}
	world := &cosWorldFake{next: 200, players: map[uint32]*model.Player{10: owner}}
	runtime := NewCOSService(repository, world)
	pet, err := runtime.Summon(context.Background(), owner, 78)
	if err != nil {
		t.Fatal(err)
	}
	destination := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(60, 0, 0)}
	if err = runtime.MoveTo(owner, pet.GetUniqueID(), destination); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	runtime.UpdateFollowers(now)
	if len(world.movements) != 1 || navmeshv2.HorizontalDistance(world.movements[0].TargetPosition, destination) > 0.1 {
		t.Fatalf("manual movements = %+v", world.movements)
	}
	pet.CancelMovement(destination, "arrived")
	pet.SetNavmeshPosition(destination)
	pet.SetMotionState(model.NoMotion)
	runtime.UpdateFollowers(now.Add(time.Second))
	runtime.UpdateFollowers(now.Add(5 * time.Second))
	if len(world.movements) != 1 {
		t.Fatalf("held pet resumed autonomous movement: %d", len(world.movements))
	}
	if err = runtime.Follow(owner, pet.GetUniqueID()); err != nil {
		t.Fatal(err)
	}
	runtime.UpdateFollowers(now.Add(6 * time.Second))
	if len(world.movements) != 2 {
		t.Fatalf("follow did not restore owner movement: %d", len(world.movements))
	}
}

func TestCOSAbilityPetIdlesCloseWhileRetainingLargeCollectionRange(t *testing.T) {
	owner := &model.Player{LifeState: model.Alive, SRObject: model.SRObject{
		UniqueID:          10,
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)},
	}}
	pet := &model.COS{
		NPC:          model.NPC{SRObject: model.SRObject{UniqueID: 201, RefObjectID: 9003}, Mutex: &sync.Mutex{}},
		PersistentID: 79, Kind: model.COSAbilityPet,
	}
	world := &cosWorldFake{}
	runtime := NewCOSService(nil, world)
	first, ok := runtime.companionTarget(world, pet, owner, time.Now(), true)
	if !ok {
		t.Fatal("ability pet collection target was unavailable")
	}
	distance := navmeshv2.HorizontalDistance(first, owner.GetNavmeshPosition())
	settings := model.DefaultCOSPolicySnapshot().Settings
	if distance < settings.PickupIdleMinRadius || distance > settings.PickupIdleMinRadius+float32(settings.PickupIdleVariance-1)/100 {
		t.Fatalf("ability pet idle target distance = %.2f", distance)
	}
	if companionRelocationRadius(model.COSAbilityPet, settings) <= settings.PickupCollectionRange {
		t.Fatalf("ability pet relocation radius %.2f does not contain collection range %.2f", companionRelocationRadius(model.COSAbilityPet, settings), settings.PickupCollectionRange)
	}
	if companionRelocationRadius(model.COSGrowthPet, settings) >= settings.PickupCollectionRange {
		t.Fatal("growth-pet leash was widened to the pickup collection range")
	}
}

func TestCOSAbilityPetPursuesAndStoresGroundItem(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9003: {
		RefObject:     model.RefObject{ID: 9003, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 4}, Speed1: 20, Speed2: 60},
		Level:         1,
		MaxHP:         100,
		InventorySize: 28,
	}}
	expires := time.Now().UTC().Add(time.Hour)
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 79, OwnerCharID: 6, RefObjectID: 9003, Kind: model.COSAbilityPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 17, RentEndsAt: &expires}}
	owner := &model.Player{ID: 6, LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{17: {Data: 79, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}}}
	item := &model.Item{ID: 55, SRObject: model.SRObject{UniqueID: 300, RefObjectID: 500, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3}, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(3, 0, 0)}}, StackSize: 1, OwnerUniqueID: 10}
	world := &cosWorldFake{next: 200, players: map[uint32]*model.Player{10: owner}, objects: map[uint32]model.ISRObject{300: item}}
	runtime := NewCOSService(repository, world)
	lifecycle := &cosGroundItemLifecycleFake{}
	runtime.ConfigureGroundItemLifecycle(lifecycle)
	pet, err := runtime.Summon(context.Background(), owner, 79)
	if err != nil {
		t.Fatal(err)
	}
	runtime.UpdateFollowers(time.Now())
	if len(repository.stored) != 1 || repository.stored[0] != 55 || len(world.removed) != 1 {
		t.Fatalf("pickup stored=%v removed=%d", repository.stored, len(world.removed))
	}
	if lifecycle.canceled != item {
		t.Fatalf("pickup lifecycle canceled=%v, want item", lifecycle.canceled)
	}
	stored := pet.Inventory.Snapshot()
	if got, exists := stored.Items[0]; !exists || got.ID != 55 {
		t.Fatalf("pet inventory = %+v", stored.Items)
	}
}

func TestCOSServiceGrowthPetAssistsSharesOwnerKillEXPAndKeepsPursuit(t *testing.T) {
	oldChars, oldSkills := model.RefChars, model.RefSkills
	defer func() { model.RefChars, model.RefSkills = oldChars, oldSkills }()
	model.RefChars = map[uint32]model.RefChar{
		9002: {
			RefObject: model.RefObject{ID: 9002, OrgObjCodeName: "COS_WOLF", TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}, Speed1: 20, Speed2: 60},
			Level:     1, MaxHP: 100, DefaultSkill1: 3824,
		},
		9100: {RefObject: model.RefObject{ID: 9100, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1}}, Level: 1, MaxHP: 100},
	}
	model.RefSkills = map[uint32]model.RefSkill{3824: {ID: 3824, AttackType: 5, ActionRange: 10, AttackMin: 5, AttackMax: 7, CoolTime: 1000}}
	levels, err := model.LoadLevelTable(strings.NewReader("1 100 0 0 0 0\n2 200 0 0 0 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	owner := &model.Player{
		ID: 6, CharName: "Bob", LifeState: model.Alive,
		Inventory: model.NewInventory(map[byte]model.Item{16: {Data: 78, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}),
		SRObject:  model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}},
	}
	target := &model.NPC{
		SRObject: model.SRObject{UniqueID: 300, RefObjectID: 9100, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1}, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(60, 0, 0)}},
		Type:     "NPC", Mutex: &sync.Mutex{},
	}
	target.ConfigureCombat(100, 40)
	target.SetLifeState(model.Alive)
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 16}}
	world := &cosWorldFake{next: 200, players: map[uint32]*model.Player{10: owner}, objects: map[uint32]model.ISRObject{300: target}}
	runtime := NewCOSService(repository, world)
	runtime.ConfigureCombat(nil, levels)
	pet, err := runtime.Summon(context.Background(), owner, 78)
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	if err := runtime.EngageOwnerTarget(owner, target); err != nil {
		t.Fatalf("EngageOwnerTarget: %v", err)
	}
	now := time.Now()
	runtime.UpdateFollowers(now)
	if len(world.movements) != 1 {
		t.Fatalf("pet pursuit movements = %d, want 1", len(world.movements))
	}
	generation := pet.GetMovementGeneration()
	runtime.UpdateFollowers(now.Add(time.Second))
	if len(world.movements) != 1 || pet.GetMovementGeneration() != generation {
		t.Fatalf("unchanged pursuit restarted: moves=%d generation=%d want=%d", len(world.movements), pet.GetMovementGeneration(), generation)
	}
	if err := runtime.AwardOwnerKillExperience(context.Background(), owner, target); err != nil {
		t.Fatalf("AwardOwnerKillExperience: %v", err)
	}
	if pet.Experience != 40 || len(repository.saved) != 2 {
		t.Fatalf("shared pet EXP/persistence = %d/%d, want 40/2", pet.Experience, len(repository.saved))
	}
	target.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(model.DefaultCOSPolicySnapshot().Settings.CombatLeashRange+1, 0, 0)})
	if err := runtime.SetAttackTarget(owner, pet.GetUniqueID(), target.GetUniqueID(), 0); !errors.Is(err, model.ErrItemUseInvalidRequest) {
		t.Fatalf("out-of-leash attack error = %v", err)
	}
	runtime.UpdateFollowers(now.Add(2 * time.Second))
	runtime.mu.Lock()
	_, attacking := runtime.attacks[pet.GetUniqueID()]
	runtime.mu.Unlock()
	if attacking {
		t.Fatal("pet retained target outside owner combat leash")
	}
}
func (w *cosWorldFake) RemoveVisibleObject(object model.ISRObject) {
	w.removed = append(w.removed, object)
}

func TestCOSServiceSummonAndUnsummonOwnsWorldAndPersistence(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9001: {RefObject: model.RefObject{ID: 9001, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 1}, Speed1: 20, Speed2: 60}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 77, OwnerCharID: 5, RefObjectID: 9001, Kind: model.COSRideTransport, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 15}}
	world := &cosWorldFake{next: 100}
	runtime := NewCOSService(repository, world)
	owner := &model.Player{ID: 5, CharName: "Alice", Inventory: model.NewInventory(map[byte]model.Item{15: {Data: 77, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 9, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1, 2, 3)}}}
	cos, err := runtime.Summon(context.Background(), owner, 77)
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	if cos.GetUniqueID() != 101 || len(world.added) != 1 || owner.COSState.Snapshot().TransportUniqueID != 101 {
		t.Fatalf("summon state = uid %d world %d player %#v", cos.GetUniqueID(), len(world.added), owner.COSState.Snapshot())
	}
	if err = runtime.Unsummon(context.Background(), owner, cos.GetUniqueID()); err != nil {
		t.Fatalf("Unsummon: %v", err)
	}
	if len(repository.saved) != 2 || repository.saved[0].State != model.COSItemSummoned || repository.saved[1].State != model.COSItemActive || len(world.removed) != 1 || owner.COSState.Snapshot().TransportUniqueID != 0 {
		t.Fatalf("unsummon persistence/world/player = %d/%d/%#v", len(repository.saved), len(world.removed), owner.COSState.Snapshot())
	}
}

func TestCOSServiceSummonRestoresWatchInventoryService(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9003: {RefObject: model.RefObject{ID: 9003, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 4}}, Level: 1, MaxHP: 100, InventorySize: 5}}
	expires := time.Now().UTC().Add(time.Hour)
	repository := &cosRepositoryFake{
		record:   model.COSRecord{ID: 79, OwnerCharID: 7, RefObjectID: 9003, Kind: model.COSAbilityPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 17, RentEndsAt: &expires},
		services: []model.COSTimedService{{Category: 5, JobID: 7002, Data1: 28, ExpiresAt: expires}},
	}
	owner := &model.Player{ID: 7, LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{17: {Data: 79, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}
	pet, err := NewCOSService(repository, &cosWorldFake{next: 200}).Summon(context.Background(), owner, 79)
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	if pet.BaseInventorySize != 5 || pet.InventorySize != 33 || len(pet.TimedServices) != 1 {
		t.Fatalf("restored watch service = base %d active %d jobs %#v", pet.BaseInventorySize, pet.InventorySize, pet.TimedServices)
	}
}

func TestCOSServiceTemporaryRideConsumesNoPersistentInstrument(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9001: {RefObject: model.RefObject{ID: 9001, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 2}, Speed1: 20, Speed2: 60}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{}
	world := &cosWorldFake{next: 100}
	runtime := NewCOSService(repository, world)
	owner := &model.Player{ID: 5, CharName: "Alice", LifeState: model.Alive, Inventory: model.NewInventory(nil), SRObject: model.SRObject{UniqueID: 9, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1, 2, 3)}}}
	world.players = map[uint32]*model.Player{owner.GetUniqueID(): owner}

	cos, err := runtime.ActivateTemporaryRide(owner, 9001)
	if err != nil {
		t.Fatalf("ActivateTemporaryRide: %v", err)
	}
	if cos.PersistentID != 0 || cos.GetUniqueID() != 101 || owner.COSState.Snapshot().TransportUniqueID != 101 || len(repository.saved) != 0 {
		t.Fatalf("temporary ride state = persistent %d uid %d transport %d saves %d", cos.PersistentID, cos.GetUniqueID(), owner.COSState.Snapshot().TransportUniqueID, len(repository.saved))
	}
	if mounted, ok := runtime.ResolveMountedTransport(owner); !ok || mounted != cos {
		t.Fatalf("mounted movement transport = %p/%t, want %p/true", mounted, ok, cos)
	}
	if _, err = owner.COSState.SetMounted(cos.GetUniqueID(), false); err != nil {
		t.Fatalf("dismount temporary ride: %v", err)
	}
	if mounted, ok := runtime.ResolveMountedTransport(owner); ok || mounted != nil {
		t.Fatalf("dismounted movement transport = %p/%t, want nil/false", mounted, ok)
	}
	if _, err = owner.COSState.SetMounted(cos.GetUniqueID(), true); err != nil {
		t.Fatalf("remount temporary ride: %v", err)
	}
	destination := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(40, 2, 30)}
	if err = runtime.MoveTo(owner, cos.GetUniqueID(), destination); err != nil {
		t.Fatalf("move mounted temporary ride: %v", err)
	}
	if len(world.registered) != 1 || world.registered[0] != owner || world.movements[0].SpeedOverride != cos.GetMovementSpeed() {
		t.Fatalf("mounted movement authority = registered:%v movement:%+v, want owner with transport speed %g", world.registered, world.movements, cos.GetMovementSpeed())
	}
	runtime.UpdateFollowers(time.Now())
	if len(world.registered) != 1 || len(world.stopped) != 0 {
		t.Fatalf("mounted transport received companion AI movement: registered=%d stopped=%d", len(world.registered), len(world.stopped))
	}
	if err = runtime.Unsummon(context.Background(), owner, cos.GetUniqueID()); err != nil {
		t.Fatalf("Unsummon temporary ride: %v", err)
	}
	if len(repository.saved) != 0 || len(world.removed) != 1 || owner.COSState.Snapshot().TransportUniqueID != 0 {
		t.Fatalf("temporary unsummon persistence/world/player = %d/%d/%#v", len(repository.saved), len(world.removed), owner.COSState.Snapshot())
	}
}

func TestCOSServiceRejectsDeadPetUntilRevival(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9002: {RefObject: model.RefObject{ID: 9002, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, State: model.COSItemDead, Health: 0, MaxHealth: 100, InventorySlot: 16}}
	owner := &model.Player{ID: 6, LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{16: {Data: 78, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}
	world := &cosWorldFake{next: 200, players: map[uint32]*model.Player{10: owner}}
	_, err := NewCOSService(repository, world).Summon(context.Background(), owner, 78)
	if !errors.Is(err, ErrCOSDead) {
		t.Fatalf("Summon() error = %v, want ErrCOSDead", err)
	}
	if len(world.added) != 0 || len(repository.saved) != 0 {
		t.Fatalf("dead summon mutated world/persistence: added=%d saved=%d", len(world.added), len(repository.saved))
	}
}

func TestCOSServiceExpiresSummonedAbilityPetRental(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9003: {RefObject: model.RefObject{ID: 9003, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 4}, Speed1: 20, Speed2: 60}, Level: 1, MaxHP: 100}}
	expires := time.Now().UTC().Add(time.Minute)
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 79, OwnerCharID: 7, RefObjectID: 9003, Kind: model.COSAbilityPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 17, RentEndsAt: &expires}}
	owner := &model.Player{ID: 7, CharName: "Carol", LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{17: {Data: 79, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 11, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1, 2, 3)}}}
	world := &cosWorldFake{next: 300, players: map[uint32]*model.Player{11: owner}}
	runtime := NewCOSService(repository, world)
	pet, err := runtime.Summon(context.Background(), owner, 79)
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	runtime.UpdateFollowers(expires)
	if len(repository.saved) != 2 || repository.saved[1].State != model.COSItemActive || len(world.removed) != 1 {
		t.Fatalf("rental expiry persistence/world removal = %d/%d, want 2/1", len(repository.saved), len(world.removed))
	}
	if _, err := runtime.Owned(owner, pet.GetUniqueID()); !errors.Is(err, model.ErrCOSNotOwned) {
		t.Fatalf("expired COS remains owned: %v", err)
	}
	item := owner.Inventory.Items[17]
	if !item.COS.Present || item.COS.State != model.COSItemActive {
		t.Fatalf("expired COS item state = %+v, want active/expired rental metadata", item.COS)
	}
}

func TestCOSServiceRejectsAbilityPetWithoutActiveRental(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9003: {RefObject: model.RefObject{ID: 9003, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 4}}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 79, OwnerCharID: 7, RefObjectID: 9003, Kind: model.COSAbilityPet, InventorySlot: 17}}
	owner := &model.Player{ID: 7, Inventory: model.NewInventory(map[byte]model.Item{17: {Data: 79, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}})}
	if _, err := NewCOSService(repository, &cosWorldFake{}).Summon(context.Background(), owner, 79); !errors.Is(err, model.ErrCOSNotFound) {
		t.Fatalf("Summon() error = %v, want expired/missing rental rejection", err)
	}
}

func TestCOSServiceThrottlesFailedRentalExpirySave(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9003: {RefObject: model.RefObject{ID: 9003, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 4}}, Level: 1, MaxHP: 100}}
	expires := time.Now().UTC().Add(time.Minute)
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 79, OwnerCharID: 7, RefObjectID: 9003, Kind: model.COSAbilityPet, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 17, RentEndsAt: &expires}}
	owner := &model.Player{ID: 7, LifeState: model.Alive, Inventory: model.NewInventory(map[byte]model.Item{17: {Data: 79, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 11}}
	world := &cosWorldFake{players: map[uint32]*model.Player{11: owner}}
	runtime := NewCOSService(repository, world)
	pet, err := runtime.Summon(context.Background(), owner, 79)
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	repository.saveErr = errors.New("database unavailable")
	runtime.UpdateFollowers(expires)
	runtime.UpdateFollowers(expires.Add(time.Second))
	if len(repository.saved) != 2 || len(world.removed) != 0 {
		t.Fatalf("throttled expiry saves/removals = %d/%d, want 2/0", len(repository.saved), len(world.removed))
	}
	if _, err := runtime.Owned(owner, pet.GetUniqueID()); err != nil {
		t.Fatalf("failed save detached COS: %v", err)
	}
	runtime.UpdateFollowers(expires.Add(5 * time.Second))
	if len(repository.saved) != 3 {
		t.Fatalf("expiry save retries = %d, want 3 after retry deadline", len(repository.saved))
	}
}

func TestCOSServiceRehydratesOnlyPersistedSummonedPets(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9002: {RefObject: model.RefObject{ID: 9002, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}, Speed1: 20, Speed2: 60}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, State: model.COSItemSummoned, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 16}}
	owner := &model.Player{
		ID: 6, CharName: "Bob", LifeState: model.Alive,
		Inventory: model.NewInventory(map[byte]model.Item{
			16: {Data: 78, COS: model.COSItemInfo{Present: true, Kind: model.COSGrowthPet, State: model.COSItemSummoned, RefObjectID: 9002, InventorySlot: 16}, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}},
			17: {Data: 79, COS: model.COSItemInfo{Present: true, Kind: model.COSAbilityPet, State: model.COSItemActive, RefObjectID: 9003, InventorySlot: 17}, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}},
		}),
		SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1, 2, 3)}},
	}
	world := &cosWorldFake{next: 200, players: map[uint32]*model.Player{10: owner}}
	runtime := NewCOSService(repository, world)
	if err := runtime.RehydratePlayer(context.Background(), owner); err != nil {
		t.Fatalf("RehydratePlayer: %v", err)
	}
	if len(world.added) != 1 || len(repository.saved) != 1 || repository.saved[0].State != model.COSItemSummoned {
		t.Fatalf("restored world/saves = %d/%+v, want one summoned pet", len(world.added), repository.saved)
	}
	if records := runtime.SnapshotOwned(owner); len(records) != 1 || records[0].ID != 78 || records[0].State != model.COSItemSummoned {
		t.Fatalf("restored snapshot = %+v", records)
	}
}

func TestCOSServiceSummonSaveFailureRollsBackRuntimeAndItemState(t *testing.T) {
	old := model.RefChars
	defer func() { model.RefChars = old }()
	model.RefChars = map[uint32]model.RefChar{9002: {RefObject: model.RefObject{ID: 9002, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}}, Level: 1, MaxHP: 100}}
	repository := &cosRepositoryFake{record: model.COSRecord{ID: 78, OwnerCharID: 6, RefObjectID: 9002, Kind: model.COSGrowthPet, State: model.COSItemActive, Level: 1, Health: 100, MaxHealth: 100, InventorySlot: 16}, saveErr: errors.New("database unavailable")}
	owner := &model.Player{ID: 6, Inventory: model.NewInventory(map[byte]model.Item{16: {Data: 78, COS: model.COSItemInfo{Present: true, State: model.COSItemActive}, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}}}), SRObject: model.SRObject{UniqueID: 10}}
	world := &cosWorldFake{next: 200}
	runtime := NewCOSService(repository, world)
	if _, err := runtime.Summon(context.Background(), owner, 78); err == nil {
		t.Fatal("Summon succeeded despite persistence failure")
	}
	if len(world.added) != 1 || len(world.removed) != 1 || len(runtime.SnapshotOwned(owner)) != 0 {
		t.Fatalf("rollback world/owned = %d/%d/%+v", len(world.added), len(world.removed), runtime.SnapshotOwned(owner))
	}
	if state := owner.GetInventory().Items[16].COS.State; state != model.COSItemActive {
		t.Fatalf("rollback item state = %d, want active", state)
	}
}
