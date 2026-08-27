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
	record  model.COSRecord
	saved   []model.COSRecord
	saveErr error
}

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
	now := time.Now()
	runtime.UpdateFollowers(now)
	if len(world.registered) != 1 || pet.GetMotionState() != model.Running {
		t.Fatalf("follow registration = %d motion=%d", len(world.registered), pet.GetMotionState())
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

func TestCOSAbilityPetUsesLargeCollectionPatrolArea(t *testing.T) {
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
	if distance < cosPickupOrbitMinRadius || distance > cosPickupOrbitMinRadius+float32(cosPickupRadiusVariance-1)/100 {
		t.Fatalf("ability pet collection target distance = %.2f", distance)
	}
	if companionRelocationRadius(model.COSAbilityPet) <= distance {
		t.Fatalf("ability pet relocation radius %.2f does not contain patrol target %.2f", companionRelocationRadius(model.COSAbilityPet), distance)
	}
	if companionRelocationRadius(model.COSGrowthPet) >= cosPickupOrbitMinRadius {
		t.Fatal("growth-pet leash was widened with the pickup pet")
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
	target.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(cosCombatLeashRadius+1, 0, 0)})
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
