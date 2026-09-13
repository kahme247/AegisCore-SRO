package model

import (
	"context"
	"errors"
	"time"
)

type QuestInstanceScopeAllocator interface {
	AllocateQuestInstanceScope(context.Context, int, uint32) (GameWorldScope, error)
	ReleaseQuestInstanceScope(context.Context, GameWorldScope) error
}

type QuestInstanceLifecycle struct {
	registry   *QuestInstanceRegistry
	repository QuestInstanceMemberRepository
	allocator  QuestInstanceScopeAllocator
}

type questInstanceExitRepository interface {
	LoadQuestInstanceMemberByWorld(context.Context, int, uint16) (QuestInstanceMemberState, error)
}

func NewQuestInstanceLifecycle(registry *QuestInstanceRegistry, repository QuestInstanceMemberRepository, allocator QuestInstanceScopeAllocator) (*QuestInstanceLifecycle, error) {
	if registry == nil || repository == nil || allocator == nil {
		return nil, ErrQuestInstanceInvalid
	}
	return &QuestInstanceLifecycle{registry: registry, repository: repository, allocator: allocator}, nil
}

// Create allocates the GameWorld before inserting its owner membership. If
// the recovered character-instance row cannot be persisted, both in-memory
// membership and the allocated scope are explicitly rolled back.
func (l *QuestInstanceLifecycle) Create(ctx context.Context, questID int, dungeonKey uint32, owner *Player, openedAt time.Time, activated bool) (QuestInstance, error) {
	if l == nil || owner == nil || owner.ID <= 0 || questID <= 0 || dungeonKey == 0 || openedAt.IsZero() {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	scope, err := l.allocator.AllocateQuestInstanceScope(ctx, questID, dungeonKey)
	if err != nil {
		return QuestInstance{}, err
	}
	instance, err := l.registry.Create(scope, questID, owner)
	if err != nil {
		return QuestInstance{}, errors.Join(err, l.allocator.ReleaseQuestInstanceScope(ctx, scope))
	}
	memberState, err := questInstanceMemberState(owner, dungeonKey, scope, openedAt, activated, 1)
	if err == nil {
		err = l.repository.SaveQuestInstanceMember(ctx, memberState)
	}
	if err != nil {
		_, destroyErr := l.registry.Destroy(scope, map[uint32]ISRObject{owner.GetUniqueID(): owner})
		releaseErr := l.allocator.ReleaseQuestInstanceScope(ctx, scope)
		return QuestInstance{}, errors.Join(err, destroyErr, releaseErr)
	}
	return instance, nil
}

func (l *QuestInstanceLifecycle) Join(ctx context.Context, scope GameWorldScope, dungeonKey uint32, member *Player, openedAt time.Time, enterCount uint32) (QuestInstance, error) {
	if l == nil || member == nil || member.ID <= 0 || dungeonKey == 0 || openedAt.IsZero() || enterCount == 0 {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	instance, err := l.registry.Join(scope, member)
	if err != nil {
		return QuestInstance{}, err
	}
	memberState, err := questInstanceMemberState(member, dungeonKey, scope, openedAt, true, enterCount)
	if err == nil {
		err = l.repository.SaveQuestInstanceMember(ctx, memberState)
	}
	if err != nil {
		_, leaveErr := l.registry.Leave(scope, member)
		return QuestInstance{}, errors.Join(err, leaveErr)
	}
	return instance, nil
}

func (l *QuestInstanceLifecycle) Leave(ctx context.Context, scope GameWorldScope, member *Player) (QuestInstance, error) {
	if l == nil || member == nil || member.ID <= 0 {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	instance, err := l.registry.Leave(scope, member)
	if err != nil {
		return QuestInstance{}, err
	}
	if err = l.repository.DeleteQuestInstanceMember(ctx, member.ID, scope.WorldID()); err != nil {
		_, joinErr := l.registry.Join(scope, member)
		return QuestInstance{}, errors.Join(err, joinErr)
	}
	return instance, nil
}

func (l *QuestInstanceLifecycle) PrepareExit(ctx context.Context, member *Player) (QuestInstanceMemberState, error) {
	if l == nil || member == nil || member.ID <= 0 {
		return QuestInstanceMemberState{}, ErrQuestInstanceInvalid
	}
	scope := GameWorldScope(member.GetGameWorldID())
	if !scope.ValidInstance() {
		return QuestInstanceMemberState{}, ErrQuestInstanceNotMember
	}
	repository, ok := l.repository.(questInstanceExitRepository)
	if !ok {
		return QuestInstanceMemberState{}, ErrQuestInstanceInvalid
	}
	state, err := repository.LoadQuestInstanceMemberByWorld(ctx, member.ID, scope.WorldID())
	if err != nil {
		return QuestInstanceMemberState{}, err
	}
	if state.Scope != scope {
		return QuestInstanceMemberState{}, ErrQuestInstanceNotMember
	}
	return state, nil
}

func (l *QuestInstanceLifecycle) Instance(scope GameWorldScope) (QuestInstance, error) {
	if l == nil || l.registry == nil {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	return l.registry.Instance(scope)
}

func (l *QuestInstanceLifecycle) Exit(ctx context.Context, scope GameWorldScope, member *Player) (QuestInstance, error) {
	instance, err := l.Leave(ctx, scope, member)
	if err != nil || len(instance.Members) != 0 {
		return instance, err
	}
	if _, err = l.registry.Destroy(scope, map[uint32]ISRObject{}); err != nil {
		return QuestInstance{}, err
	}
	if err = l.allocator.ReleaseQuestInstanceScope(ctx, scope); err != nil {
		return QuestInstance{}, err
	}
	return instance, nil
}

func (l *QuestInstanceLifecycle) Destroy(ctx context.Context, scope GameWorldScope, members []*Player) error {
	if l == nil || !scope.ValidInstance() || len(members) == 0 {
		return ErrQuestInstanceInvalid
	}
	objects := make(map[uint32]ISRObject, len(members))
	for _, member := range members {
		if member == nil || member.ID <= 0 || member.GetGameWorldID() != uint32(scope) {
			return ErrQuestInstanceNotMember
		}
		if err := l.repository.DeleteQuestInstanceMember(ctx, member.ID, scope.WorldID()); err != nil {
			return err
		}
		objects[member.GetUniqueID()] = member
	}
	if _, err := l.registry.Destroy(scope, objects); err != nil {
		return err
	}
	return l.allocator.ReleaseQuestInstanceScope(ctx, scope)
}

func questInstanceMemberState(player *Player, dungeonKey uint32, scope GameWorldScope, openedAt time.Time, activated bool, enterCount uint32) (QuestInstanceMemberState, error) {
	position := player.GetNavmeshPosition()
	if position.Region.ID == 0 || position.Offset == nil {
		return QuestInstanceMemberState{}, ErrQuestInstanceInvalid
	}
	lastEnter := openedAt
	return QuestInstanceMemberState{
		CharacterID: player.ID, DungeonKey: dungeonKey, Scope: scope, OpenedAt: openedAt,
		RegionID: position.Region.ID, X: position.Offset.X, Y: position.Offset.Y, Z: position.Offset.Z,
		Activated: activated, EnterCount: enterCount, LastEnterAt: &lastEnter,
	}, nil
}
