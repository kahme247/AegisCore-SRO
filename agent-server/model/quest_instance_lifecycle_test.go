package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type questInstanceLifecycleRepositoryFake struct {
	saved   []QuestInstanceMemberState
	deleted [][2]int
	err     error
}

func (f *questInstanceLifecycleRepositoryFake) SaveQuestInstanceMember(_ context.Context, state QuestInstanceMemberState) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, state)
	return nil
}
func (f *questInstanceLifecycleRepositoryFake) LoadQuestInstanceMember(context.Context, int, uint32) (QuestInstanceMemberState, error) {
	return QuestInstanceMemberState{}, ErrQuestInstanceMissing
}
func (f *questInstanceLifecycleRepositoryFake) LoadQuestInstanceMemberByWorld(_ context.Context, characterID int, worldID uint16) (QuestInstanceMemberState, error) {
	for _, state := range f.saved {
		if state.CharacterID == characterID && state.Scope.WorldID() == worldID {
			return state, nil
		}
	}
	return QuestInstanceMemberState{}, ErrQuestInstanceMissing
}
func (f *questInstanceLifecycleRepositoryFake) DeleteQuestInstanceMember(_ context.Context, characterID int, worldID uint16) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, [2]int{characterID, int(worldID)})
	return nil
}

type questInstanceAllocatorFake struct {
	scope               GameWorldScope
	allocated, released int
	err                 error
}

func (f *questInstanceAllocatorFake) AllocateQuestInstanceScope(context.Context, int, uint32) (GameWorldScope, error) {
	f.allocated++
	return f.scope, f.err
}
func (f *questInstanceAllocatorFake) ReleaseQuestInstanceScope(context.Context, GameWorldScope) error {
	f.released++
	return nil
}

func questInstanceLifecyclePlayer(uid uint32, characterID int) *Player {
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(10, 20, 30)}
	player := &Player{ID: characterID, SRObject: SRObject{UniqueID: uid, RtNavmeshPosition: position}}
	player.KnownObjectList = NewKnownObjectList(player)
	return player
}

func TestQuestInstanceLifecyclePersistsOwnerAfterScopeCreation(t *testing.T) {
	scope, _ := NewGameWorldScope(41, 2)
	repository := &questInstanceLifecycleRepositoryFake{}
	allocator := &questInstanceAllocatorFake{scope: scope}
	lifecycle, _ := NewQuestInstanceLifecycle(NewQuestInstanceRegistry(), repository, allocator)
	owner := questInstanceLifecyclePlayer(100, 77)
	opened := time.Unix(1000, 0)
	instance, err := lifecycle.Create(context.Background(), 141, 9001, owner, opened, true)
	if err != nil {
		t.Fatal(err)
	}
	if allocator.allocated != 1 || allocator.released != 0 || instance.Scope != scope || owner.GetGameWorldID() != uint32(scope) || len(repository.saved) != 1 {
		t.Fatalf("instance=%+v allocated=%d released=%d scope=%X saves=%#v", instance, allocator.allocated, allocator.released, owner.GetGameWorldID(), repository.saved)
	}
	state := repository.saved[0]
	if state.CharacterID != 77 || state.DungeonKey != 9001 || state.RegionID != 25000 || state.X != 10 || state.Y != 20 || state.Z != 30 || state.EnterCount != 1 || !state.Activated {
		t.Fatalf("state=%+v", state)
	}
}

func TestQuestInstanceLifecycleRollsBackScopeWhenOwnerPersistenceFails(t *testing.T) {
	scope, _ := NewGameWorldScope(41, 2)
	wantErr := errors.New("write failed")
	repository := &questInstanceLifecycleRepositoryFake{err: wantErr}
	allocator := &questInstanceAllocatorFake{scope: scope}
	registry := NewQuestInstanceRegistry()
	lifecycle, _ := NewQuestInstanceLifecycle(registry, repository, allocator)
	owner := questInstanceLifecyclePlayer(100, 77)
	if _, err := lifecycle.Create(context.Background(), 141, 9001, owner, time.Unix(1000, 0), true); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if allocator.released != 1 || owner.GetGameWorldID() != 0 {
		t.Fatalf("released=%d scope=%X", allocator.released, owner.GetGameWorldID())
	}
	if _, err := registry.Instance(scope); !errors.Is(err, ErrQuestInstanceMissing) {
		t.Fatalf("registry error=%v", err)
	}
}

func TestQuestInstanceLifecycleJoinAndLeaveMirrorDurableMembership(t *testing.T) {
	scope, _ := NewGameWorldScope(41, 2)
	repository := &questInstanceLifecycleRepositoryFake{}
	allocator := &questInstanceAllocatorFake{scope: scope}
	registry := NewQuestInstanceRegistry()
	lifecycle, _ := NewQuestInstanceLifecycle(registry, repository, allocator)
	owner := questInstanceLifecyclePlayer(100, 77)
	member := questInstanceLifecyclePlayer(200, 88)
	opened := time.Unix(1000, 0)
	_, _ = lifecycle.Create(context.Background(), 141, 9001, owner, opened, true)
	instance, err := lifecycle.Join(context.Background(), scope, 9001, member, opened, 2)
	if err != nil || len(instance.Members) != 2 || len(repository.saved) != 2 {
		t.Fatalf("instance=%+v saves=%d error=%v", instance, len(repository.saved), err)
	}
	instance, err = lifecycle.Leave(context.Background(), scope, member)
	if err != nil || len(instance.Members) != 1 || member.GetGameWorldID() != 0 || len(repository.deleted) != 1 || repository.deleted[0] != [2]int{88, 41} {
		t.Fatalf("instance=%+v deleted=%#v scope=%X error=%v", instance, repository.deleted, member.GetGameWorldID(), err)
	}
}

func TestQuestInstanceLifecyclePrepareExitReturnsPersistedOrigin(t *testing.T) {
	scope, _ := NewGameWorldScope(41, 2)
	repository := &questInstanceLifecycleRepositoryFake{}
	lifecycle, _ := NewQuestInstanceLifecycle(NewQuestInstanceRegistry(), repository, &questInstanceAllocatorFake{scope: scope})
	owner := questInstanceLifecyclePlayer(100, 77)
	_, _ = lifecycle.Create(context.Background(), 141, 9001, owner, time.Unix(1000, 0), true)
	state, err := lifecycle.PrepareExit(context.Background(), owner)
	if err != nil || state.Scope != scope || state.RegionID != 25000 || state.X != 10 || state.Y != 20 || state.Z != 30 {
		t.Fatalf("exit state=%+v error=%v", state, err)
	}
}
