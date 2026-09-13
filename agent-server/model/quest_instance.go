package model

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrQuestInstanceInvalid   = errors.New("invalid quest instance")
	ErrQuestInstanceExists    = errors.New("quest instance already exists")
	ErrQuestInstanceMissing   = errors.New("quest instance does not exist")
	ErrQuestInstanceMember    = errors.New("object already belongs to a quest instance")
	ErrQuestInstanceNotMember = errors.New("object does not belong to the quest instance")
)

type QuestInstance struct {
	Scope    GameWorldScope
	QuestID  int
	OwnerUID uint32
	Members  []uint32
}

type QuestInstanceRegistry struct {
	mu       sync.Mutex
	byScope  map[GameWorldScope]*questInstanceState
	byMember map[uint32]GameWorldScope
}

type questInstanceState struct {
	scope     GameWorldScope
	questID   int
	ownerUID  uint32
	members   map[uint32]struct{}
	variables map[string]int64
}

func NewQuestInstanceRegistry() *QuestInstanceRegistry {
	return &QuestInstanceRegistry{
		byScope:  make(map[GameWorldScope]*questInstanceState),
		byMember: make(map[uint32]GameWorldScope),
	}
}

func (r *QuestInstanceRegistry) Create(scope GameWorldScope, questID int, owner ISRObject) (QuestInstance, error) {
	if r == nil || !scope.ValidInstance() || questID <= 0 || owner == nil || owner.GetUniqueID() == 0 {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byScope[scope]; exists {
		return QuestInstance{}, ErrQuestInstanceExists
	}
	ownerUID := owner.GetUniqueID()
	if _, exists := r.byMember[ownerUID]; exists {
		return QuestInstance{}, ErrQuestInstanceMember
	}
	state := &questInstanceState{
		scope: scope, questID: questID, ownerUID: ownerUID,
		members: map[uint32]struct{}{ownerUID: {}}, variables: make(map[string]int64),
	}
	r.byScope[scope] = state
	r.byMember[ownerUID] = scope
	owner.SetGameWorldID(uint32(scope))
	return snapshotQuestInstance(state), nil
}

type QuestInstanceVariableMutation struct {
	Set       map[string]int64
	Increment []string
}

// ApplyVariables mutates one GameWorld's named runtime variables under the
// same lock as membership. These values intentionally remain transient: the
// pinned GameServer persists member/location data separately and exposes no
// quest-variable SQL write.
func (r *QuestInstanceRegistry) ApplyVariables(scope GameWorldScope, mutation QuestInstanceVariableMutation) (map[string]int64, error) {
	if r == nil {
		return nil, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return nil, ErrQuestInstanceMissing
	}
	return applyQuestInstanceVariables(state, mutation)
}

func (r *QuestInstanceRegistry) Variables(scope GameWorldScope) (map[string]int64, error) {
	if r == nil {
		return nil, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return nil, ErrQuestInstanceMissing
	}
	return snapshotQuestInstanceVariables(state), nil
}

func (r *QuestInstanceRegistry) Instance(scope GameWorldScope) (QuestInstance, error) {
	if r == nil || !scope.ValidInstance() {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return QuestInstance{}, ErrQuestInstanceMissing
	}
	return snapshotQuestInstance(state), nil
}

func snapshotQuestInstanceVariables(state *questInstanceState) map[string]int64 {
	result := make(map[string]int64, len(state.variables))
	for key, value := range state.variables {
		result[key] = value
	}
	return result
}

func (r *QuestInstanceRegistry) Join(scope GameWorldScope, member ISRObject) (QuestInstance, error) {
	if r == nil || member == nil || member.GetUniqueID() == 0 {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return QuestInstance{}, ErrQuestInstanceMissing
	}
	uid := member.GetUniqueID()
	if _, exists := r.byMember[uid]; exists {
		return QuestInstance{}, ErrQuestInstanceMember
	}
	state.members[uid] = struct{}{}
	r.byMember[uid] = scope
	member.SetGameWorldID(uint32(scope))
	return snapshotQuestInstance(state), nil
}

// TransitionScope moves an intact authored dungeon stage to its next
// GameWorld scope while preserving owner, membership and transient variables.
// Durable member rows and scope reservations are coordinated by the lifecycle
// layer before this in-memory commit is used.
func (r *QuestInstanceRegistry) TransitionScope(from, to GameWorldScope, members map[uint32]ISRObject) (QuestInstance, error) {
	if r == nil || !from.ValidInstance() || !to.ValidInstance() || from == to {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[from]
	if state == nil {
		return QuestInstance{}, ErrQuestInstanceMissing
	}
	if _, exists := r.byScope[to]; exists {
		return QuestInstance{}, ErrQuestInstanceExists
	}
	for uid := range state.members {
		if r.byMember[uid] != from || members[uid] == nil {
			return QuestInstance{}, ErrQuestInstanceNotMember
		}
	}
	delete(r.byScope, from)
	state.scope = to
	r.byScope[to] = state
	for uid := range state.members {
		r.byMember[uid] = to
		members[uid].SetGameWorldID(uint32(to))
	}
	return snapshotQuestInstance(state), nil
}

func (r *QuestInstanceRegistry) Leave(scope GameWorldScope, member ISRObject) (QuestInstance, error) {
	if r == nil || member == nil || member.GetUniqueID() == 0 {
		return QuestInstance{}, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return QuestInstance{}, ErrQuestInstanceMissing
	}
	uid := member.GetUniqueID()
	if r.byMember[uid] != scope {
		return QuestInstance{}, ErrQuestInstanceNotMember
	}
	delete(state.members, uid)
	delete(r.byMember, uid)
	member.SetGameWorldID(0)
	return snapshotQuestInstance(state), nil
}

// Destroy is explicit, matching the GameServer destroy job. Empty membership
// alone does not infer teardown. The caller receives the sorted member IDs it
// must evacuate/reset in the live world before discarding instance content.
func (r *QuestInstanceRegistry) Destroy(scope GameWorldScope, members map[uint32]ISRObject) ([]uint32, error) {
	if r == nil {
		return nil, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return nil, ErrQuestInstanceMissing
	}
	ids := make([]uint32, 0, len(state.members))
	for uid := range state.members {
		ids = append(ids, uid)
		delete(r.byMember, uid)
		if member := members[uid]; member != nil {
			member.SetGameWorldID(0)
		}
	}
	delete(r.byScope, scope)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func snapshotQuestInstance(state *questInstanceState) QuestInstance {
	result := QuestInstance{Scope: state.scope, QuestID: state.questID, OwnerUID: state.ownerUID}
	result.Members = make([]uint32, 0, len(state.members))
	for uid := range state.members {
		result.Members = append(result.Members, uid)
	}
	sort.Slice(result.Members, func(i, j int) bool { return result.Members[i] < result.Members[j] })
	return result
}
