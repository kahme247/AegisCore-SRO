package service

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type cosRepositoryFake struct {
	record model.COSRecord
	saved  []model.COSRecord
}

func (f *cosRepositoryFake) LoadOwned(_ context.Context, owner int, id uint64) (model.COSRecord, error) {
	if f.record.OwnerCharID != owner || f.record.ID != id {
		return model.COSRecord{}, model.ErrCOSNotFound
	}
	return f.record, nil
}
func (f *cosRepositoryFake) Save(_ context.Context, record model.COSRecord) error {
	f.saved = append(f.saved, record)
	return nil
}
func (f *cosRepositoryFake) Rename(context.Context, int, uint64, string) error { return nil }

type cosWorldFake struct {
	next           uint32
	added, removed []model.ISRObject
	players        map[uint32]*model.Player
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
	runtime.UpdateFollowers(time.Now())
	if len(world.registered) != 1 || pet.GetMotionState() != model.Running {
		t.Fatalf("follow registration = %d motion=%d", len(world.registered), pet.GetMotionState())
	}
	targetDistance := navmeshv2.HorizontalDistance(world.movements[0].TargetPosition, owner.GetNavmeshPosition())
	if targetDistance < 17 || targetDistance > 54 {
		t.Fatalf("companion target is %.2fm from owner, want bounded formation distance", targetDistance)
	}
	firstTarget := world.movements[0].TargetPosition
	pet.CancelMovement(firstTarget, "test-arrival")
	pet.SetMotionState(model.NoMotion)
	runtime.UpdateFollowers(time.Now().Add(15 * time.Second))
	if len(world.movements) != 2 {
		t.Fatalf("idle companion moves = %d, want a second roaming target", len(world.movements))
	}
	if moved := navmeshv2.HorizontalDistance(firstTarget, world.movements[1].TargetPosition); moved < 3 {
		t.Fatalf("idle roaming target changed only %.2f world units", moved)
	}
	owner.RtNavmeshPosition.Offset = math32.NewVector3(200, 0, 0)
	runtime.UpdateFollowers(time.Now().Add(30 * time.Second))
	ownerDistance := navmeshv2.HorizontalDistance(pet.GetNavmeshPosition(), owner.GetNavmeshPosition())
	if len(world.stopped) != 1 || ownerDistance < 17 || ownerDistance > 54 {
		t.Fatalf("far relocation stops=%d pet=%v owner=%v", len(world.stopped), pet.GetNavmeshPosition(), owner.GetNavmeshPosition())
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
	if len(repository.saved) != 1 || len(world.removed) != 1 || owner.COSState.Snapshot().TransportUniqueID != 0 {
		t.Fatalf("unsummon persistence/world/player = %d/%d/%#v", len(repository.saved), len(world.removed), owner.COSState.Snapshot())
	}
}
