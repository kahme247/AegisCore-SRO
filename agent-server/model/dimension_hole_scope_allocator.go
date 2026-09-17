package model

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrDimensionHoleScopeAllocatorUnavailable = errors.New("Dimension Hole scope allocator is unavailable")
	ErrDimensionHoleScopeInvalid              = errors.New("invalid Dimension Hole scope lease")
	ErrDimensionHoleScopeReservationExists    = errors.New("Dimension Hole reservation already exists")
	ErrDimensionHoleScopeExhausted            = errors.New("Dimension Hole world has no free instance layer")
	ErrDimensionHoleScopeMissing              = errors.New("Dimension Hole scope lease is missing")
	ErrDimensionHoleScopeOwnership            = errors.New("Dimension Hole scope lease ownership mismatch")
)

// DimensionHoleScopeLease is the ownership proof required to release one
// direct GameWorld layer. ItemRefID binds the lease back to the pinned world;
// ReservationKey binds it to the runtime action or instance that owns it.
type DimensionHoleScopeLease struct {
	Scope          GameWorldScope
	ReservationKey uint32
	ItemRefID      uint32
	token          uint64
}

func (l DimensionHoleScopeLease) Valid() bool {
	definition, ok := DimensionHoleDefinitionForItem(l.ItemRefID)
	return ok && l.Scope.ValidInstance() && l.ReservationKey != 0 && l.token != 0 && definition.WorldID == l.Scope.WorldID()
}

type DimensionHoleScopeAllocator interface {
	ReserveDimensionHoleScope(context.Context, DimensionHoleDefinition, uint32) (DimensionHoleScopeLease, error)
	ReleaseDimensionHoleScope(context.Context, DimensionHoleScopeLease) error
	DimensionHoleLeaseForScope(GameWorldScope) (DimensionHoleScopeLease, bool)
}

// LocalDimensionHoleScopeAllocator reserves direct world/layer pairs without
// inventing REF_QUEST_INSTANCE_WORLD_MAP rows or fake quest IDs. The current
// deployable topology has one Agent, so leases are process-local until the
// still-unproven portal lifetime/cooldown contract justifies durable schema.
type LocalDimensionHoleScopeAllocator struct {
	mu      sync.RWMutex
	catalog *DimensionHoleWorldCatalog
	byScope map[GameWorldScope]DimensionHoleScopeLease
	byKey   map[uint32]GameWorldScope
	next    uint64
}

var _ DimensionHoleScopeAllocator = (*LocalDimensionHoleScopeAllocator)(nil)

func NewLocalDimensionHoleScopeAllocator(catalog *DimensionHoleWorldCatalog) (*LocalDimensionHoleScopeAllocator, error) {
	if !validDimensionHoleWorldCatalog(catalog) {
		return nil, ErrDimensionHoleScopeAllocatorUnavailable
	}
	return &LocalDimensionHoleScopeAllocator{
		catalog: catalog,
		byScope: make(map[GameWorldScope]DimensionHoleScopeLease),
		byKey:   make(map[uint32]GameWorldScope),
	}, nil
}

func (a *LocalDimensionHoleScopeAllocator) ReserveDimensionHoleScope(ctx context.Context, definition DimensionHoleDefinition, reservationKey uint32) (DimensionHoleScopeLease, error) {
	if a == nil || a.catalog == nil {
		return DimensionHoleScopeLease{}, ErrDimensionHoleScopeAllocatorUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DimensionHoleScopeLease{}, err
	}
	pinned, ok := DimensionHoleDefinitionForItem(definition.ItemRefID)
	if !ok || pinned != definition || reservationKey == 0 {
		return DimensionHoleScopeLease{}, ErrDimensionHoleScopeInvalid
	}
	world, ok := a.catalog.World(definition.WorldID)
	if !ok || world.MaxInstanceCount == 0 {
		return DimensionHoleScopeLease{}, ErrDimensionHoleScopeAllocatorUnavailable
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DimensionHoleScopeLease{}, err
	}
	if _, exists := a.byKey[reservationKey]; exists {
		return DimensionHoleScopeLease{}, ErrDimensionHoleScopeReservationExists
	}
	for layer := uint32(1); layer <= uint32(world.MaxInstanceCount) && layer < 0xffff; layer++ {
		scope, err := NewGameWorldScope(world.WorldID, uint16(layer))
		if err != nil {
			return DimensionHoleScopeLease{}, errors.Join(ErrDimensionHoleScopeInvalid, err)
		}
		if _, reserved := a.byScope[scope]; reserved {
			continue
		}
		a.next++
		if a.next == 0 {
			a.next++
		}
		lease := DimensionHoleScopeLease{Scope: scope, ReservationKey: reservationKey, ItemRefID: definition.ItemRefID, token: a.next}
		a.byScope[scope] = lease
		a.byKey[reservationKey] = scope
		return lease, nil
	}
	return DimensionHoleScopeLease{}, ErrDimensionHoleScopeExhausted
}

func (a *LocalDimensionHoleScopeAllocator) ReleaseDimensionHoleScope(ctx context.Context, lease DimensionHoleScopeLease) error {
	if a == nil || a.catalog == nil {
		return ErrDimensionHoleScopeAllocatorUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !lease.Valid() {
		return ErrDimensionHoleScopeInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	owned, ok := a.byScope[lease.Scope]
	if !ok {
		return ErrDimensionHoleScopeMissing
	}
	if owned != lease || a.byKey[lease.ReservationKey] != lease.Scope {
		return ErrDimensionHoleScopeOwnership
	}
	delete(a.byScope, lease.Scope)
	delete(a.byKey, lease.ReservationKey)
	return nil
}

func (a *LocalDimensionHoleScopeAllocator) DimensionHoleLeaseForScope(scope GameWorldScope) (DimensionHoleScopeLease, bool) {
	if a == nil || !scope.ValidInstance() {
		return DimensionHoleScopeLease{}, false
	}
	a.mu.RLock()
	lease, ok := a.byScope[scope]
	a.mu.RUnlock()
	return lease, ok
}

func validDimensionHoleWorldCatalog(catalog *DimensionHoleWorldCatalog) bool {
	if catalog == nil || len(catalog.worlds) != len(dimensionHoleDefinitions) || len(catalog.byID) != len(dimensionHoleDefinitions) {
		return false
	}
	for index, definition := range dimensionHoleDefinitions {
		world := catalog.worlds[index]
		if world.WorldID != definition.WorldID || validateDimensionHoleWorldReference(world, definition) != nil || catalog.byID[world.WorldID] != world {
			return false
		}
	}
	return true
}
