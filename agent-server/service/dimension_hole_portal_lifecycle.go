package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

var (
	ErrDimensionHolePortalLifecycleUnavailable = errors.New("Dimension Hole portal lifecycle is unavailable")
	ErrDimensionHolePortalPreparationMissing   = errors.New("Dimension Hole portal preparation is missing")
)

type PreparedDimensionHolePortal struct {
	Definition model.DimensionHoleDefinition
	owner      *model.Player
	position   navmeshv2.RtNavmeshPosition
	lease      model.DimensionHoleScopeLease
}

type DimensionHolePortalInstance struct {
	Portal    *model.DimensionHolePortal
	Lease     model.DimensionHoleScopeLease
	ExpiresAt time.Time
}

// DimensionHolePortalLifecycle couples world publication to a direct-world
// lease and expires both at the pinned REF_GAME_WORLD duration. Admission and
// player transfer remain separate authorities, so expiry never mutates a
// player's inventory or position.
type DimensionHolePortalLifecycle struct {
	mu       sync.RWMutex
	portals  *DimensionHolePortalService
	scopes   model.DimensionHoleScopeAllocator
	nextKey  func() uint32
	byUID    map[uint32]DimensionHolePortalInstance
	prepared map[uint32]*PreparedDimensionHolePortal
	now      func() time.Time
	lifetime time.Duration
	schedule func(time.Duration, func()) func() bool
	timers   map[uint32]func() bool
}

func NewDimensionHolePortalLifecycle(portals *DimensionHolePortalService, scopes model.DimensionHoleScopeAllocator, nextKey func() uint32) (*DimensionHolePortalLifecycle, error) {
	if portals == nil || scopes == nil || nextKey == nil {
		return nil, ErrDimensionHolePortalLifecycleUnavailable
	}
	return &DimensionHolePortalLifecycle{
		portals:  portals,
		scopes:   scopes,
		nextKey:  nextKey,
		byUID:    make(map[uint32]DimensionHolePortalInstance),
		prepared: make(map[uint32]*PreparedDimensionHolePortal),
		now:      time.Now,
		lifetime: time.Duration(model.DimensionHoleWorldDurationMS) * time.Millisecond,
		schedule: func(delay time.Duration, callback func()) func() bool {
			timer := time.AfterFunc(delay, callback)
			return timer.Stop
		},
		timers: make(map[uint32]func() bool),
	}, nil
}

func (l *DimensionHolePortalLifecycle) Prepare(ctx context.Context, definition model.DimensionHoleDefinition, owner *model.Player, position navmeshv2.RtNavmeshPosition) (*PreparedDimensionHolePortal, error) {
	if l == nil || l.portals == nil || l.scopes == nil || l.nextKey == nil {
		return nil, ErrDimensionHolePortalLifecycleUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := model.NewDimensionHolePortal(definition, owner, position); err != nil {
		return nil, err
	}
	lease, err := l.scopes.ReserveDimensionHoleScope(ctx, definition, l.nextKey())
	if err != nil {
		return nil, err
	}
	prepared := &PreparedDimensionHolePortal{Definition: definition, owner: owner, position: position, lease: lease}
	l.mu.Lock()
	if _, exists := l.prepared[lease.ReservationKey]; exists {
		l.mu.Unlock()
		return nil, errors.Join(
			ErrDimensionHolePortalLifecycleUnavailable,
			l.scopes.ReleaseDimensionHoleScope(context.WithoutCancel(ctx), lease),
		)
	}
	l.prepared[lease.ReservationKey] = prepared
	l.mu.Unlock()
	return prepared, nil
}

func (l *DimensionHolePortalLifecycle) Publish(ctx context.Context, prepared *PreparedDimensionHolePortal) (DimensionHolePortalInstance, error) {
	if l == nil || l.portals == nil || l.scopes == nil || prepared == nil {
		return DimensionHolePortalInstance{}, ErrDimensionHolePortalLifecycleUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.takePreparation(prepared) {
		return DimensionHolePortalInstance{}, ErrDimensionHolePortalPreparationMissing
	}
	rollback := func(cause error) error {
		return errors.Join(cause, l.scopes.ReleaseDimensionHoleScope(context.WithoutCancel(ctx), prepared.lease))
	}
	if err := ctx.Err(); err != nil {
		return DimensionHolePortalInstance{}, rollback(err)
	}
	portal, err := l.portals.Publish(prepared.Definition, prepared.owner, prepared.position)
	if err != nil {
		return DimensionHolePortalInstance{}, rollback(err)
	}
	now := l.now()
	instance := DimensionHolePortalInstance{Portal: portal, Lease: prepared.lease, ExpiresAt: now.Add(l.lifetime)}
	uid := portal.GetUniqueID()
	l.mu.Lock()
	if _, exists := l.byUID[uid]; exists {
		l.mu.Unlock()
		return DimensionHolePortalInstance{}, errors.Join(
			ErrDimensionHolePortalLifecycleUnavailable,
			l.portals.Destroy(uid),
			l.scopes.ReleaseDimensionHoleScope(context.WithoutCancel(ctx), prepared.lease),
		)
	}
	l.byUID[uid] = instance
	l.mu.Unlock()
	l.scheduleExpiration(uid, l.lifetime)
	return instance, nil
}

func (l *DimensionHolePortalLifecycle) scheduleExpiration(uid uint32, delay time.Duration) {
	if l == nil || uid == 0 || l.schedule == nil {
		return
	}
	cancel := l.schedule(delay, func() {
		_ = l.Destroy(context.Background(), uid)
	})
	l.mu.Lock()
	if _, exists := l.byUID[uid]; exists {
		l.timers[uid] = cancel
		cancel = nil
	}
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (l *DimensionHolePortalLifecycle) Abort(ctx context.Context, prepared *PreparedDimensionHolePortal) error {
	if l == nil || l.scopes == nil || prepared == nil {
		return ErrDimensionHolePortalLifecycleUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.takePreparation(prepared) {
		return ErrDimensionHolePortalPreparationMissing
	}
	return l.scopes.ReleaseDimensionHoleScope(context.WithoutCancel(ctx), prepared.lease)
}

func (l *DimensionHolePortalLifecycle) Open(ctx context.Context, definition model.DimensionHoleDefinition, owner *model.Player, position navmeshv2.RtNavmeshPosition) (DimensionHolePortalInstance, error) {
	prepared, err := l.Prepare(ctx, definition, owner, position)
	if err != nil {
		return DimensionHolePortalInstance{}, err
	}
	return l.Publish(ctx, prepared)
}

func (l *DimensionHolePortalLifecycle) takePreparation(prepared *PreparedDimensionHolePortal) bool {
	if l == nil || prepared == nil || prepared.lease.ReservationKey == 0 {
		return false
	}
	l.mu.Lock()
	owned, ok := l.prepared[prepared.lease.ReservationKey]
	if ok && owned == prepared {
		delete(l.prepared, prepared.lease.ReservationKey)
	} else {
		ok = false
	}
	l.mu.Unlock()
	return ok
}

func (l *DimensionHolePortalLifecycle) Instance(uid uint32) (DimensionHolePortalInstance, bool) {
	if l == nil || uid == 0 {
		return DimensionHolePortalInstance{}, false
	}
	l.mu.RLock()
	instance, ok := l.byUID[uid]
	l.mu.RUnlock()
	return instance, ok
}

func (l *DimensionHolePortalLifecycle) Destroy(ctx context.Context, uid uint32) error {
	if l == nil || l.portals == nil || l.scopes == nil || uid == 0 {
		return ErrDimensionHolePortalLifecycleUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	instance, ok := l.byUID[uid]
	if ok {
		delete(l.byUID, uid)
		if cancel := l.timers[uid]; cancel != nil {
			cancel()
		}
		delete(l.timers, uid)
	}
	l.mu.Unlock()
	if !ok {
		return ErrDimensionHolePortalMissing
	}

	destroyErr := l.portals.Destroy(uid)
	releaseErr := l.scopes.ReleaseDimensionHoleScope(context.WithoutCancel(ctx), instance.Lease)
	if releaseErr != nil {
		reschedule := false
		delay := time.Duration(0)
		l.mu.Lock()
		if _, exists := l.byUID[uid]; !exists {
			l.byUID[uid] = instance
			delay = instance.ExpiresAt.Sub(l.now())
			if delay < 0 {
				delay = 0
			}
			reschedule = true
		}
		l.mu.Unlock()
		if reschedule {
			l.scheduleExpiration(uid, delay)
		}
	}
	return errors.Join(destroyErr, releaseErr)
}

// Close destroys every published portal and releases every direct-world
// reservation before Agent closes its shard pool.
func (l *DimensionHolePortalLifecycle) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.RLock()
	uids := make([]uint32, 0, len(l.byUID))
	for uid := range l.byUID {
		uids = append(uids, uid)
	}
	prepared := make([]*PreparedDimensionHolePortal, 0, len(l.prepared))
	for _, reservation := range l.prepared {
		prepared = append(prepared, reservation)
	}
	l.mu.RUnlock()
	var closeErr error
	for _, uid := range uids {
		closeErr = errors.Join(closeErr, l.Destroy(context.WithoutCancel(ctx), uid))
	}
	for _, reservation := range prepared {
		closeErr = errors.Join(closeErr, l.Abort(context.WithoutCancel(ctx), reservation))
	}
	return closeErr
}
