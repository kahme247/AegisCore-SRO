package service

import (
	"errors"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

var ErrGroundItemLifecycleUnavailable = errors.New("ground-item lifecycle is unavailable")

// groundItemRemover is deliberately narrower than WorldService. Pickup and
// expiry both use the same idempotent removal edge, while tests and alternate
// world implementations only need to provide this one method.
type groundItemRemover interface {
	RemoveVisibleObject(model.ISRObject)
}

// GroundItemLifecycle owns timers that are part of a ground object's runtime
// lifetime. Ownership release and final despawn are separate events in the
// recovered server: 0x304D clears the owner marker, while the later object
// removal produces the normal visibility leave path.
//
// Timers are cancellable and removal is idempotent. That matters because a
// successful player/COS pickup can remove an object before either timer fires.
type GroundItemLifecycle struct {
	world groundItemRemover

	mu      sync.Mutex
	tracked map[uint32]*groundItemTimer

	ownershipRelease func(*model.Item) error
}

type groundItemTimer struct {
	item      *model.Item
	ownership *time.Timer
	despawn   *time.Timer
}

// NewGroundItemLifecycle creates a scheduler around the live world removal
// boundary. Zero durations disable the corresponding timer, which is useful
// for focused tests and for callers that have not yet selected deployment
// policy values.
func NewGroundItemLifecycle(world groundItemRemover) *GroundItemLifecycle {
	lifecycle := &GroundItemLifecycle{world: world, tracked: make(map[uint32]*groundItemTimer)}
	lifecycle.ConfigureOwnershipRelease(func(item *model.Item) error {
		if item == nil || item.GetKnownObjectList() == nil {
			return nil
		}
		packet := model.BuildGroundItemOwnershipRelease(item.GetUniqueID())
		var firstErr error
		for _, object := range item.GetKnownObjectList().GetKnownObjects() {
			player, ok := object.(*model.Player)
			if !ok || player == nil || player.GetSession() == nil {
				continue
			}
			if err := player.GetSession().Send(packet); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	})
	return lifecycle
}

// ConfigureOwnershipRelease installs the packet/event boundary for 0x304D.
// The lifecycle package does not guess how a deployment broadcasts that
// packet; it only guarantees the callback runs once when ownership expires.
func (l *GroundItemLifecycle) ConfigureOwnershipRelease(callback func(*model.Item) error) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.ownershipRelease = callback
	l.mu.Unlock()
}

// Track registers timers after the item has entered the world and therefore
// has a runtime object UID. Existing timers for the same UID are cancelled so
// a respawned/reused object cannot retain an old callback.
func (l *GroundItemLifecycle) Track(item *model.Item, ownershipAfter, despawnAfter time.Duration) error {
	if l == nil || l.world == nil || item == nil || item.GetUniqueID() == 0 {
		return ErrGroundItemLifecycleUnavailable
	}
	uid := item.GetUniqueID()
	l.mu.Lock()
	if previous := l.tracked[uid]; previous != nil {
		stopGroundItemTimer(previous)
	}
	timer := &groundItemTimer{item: item}
	l.tracked[uid] = timer
	if ownershipAfter > 0 {
		timer.ownership = time.AfterFunc(ownershipAfter, func() {
			l.releaseOwnership(uid, item)
		})
	}
	if despawnAfter > 0 {
		timer.despawn = time.AfterFunc(despawnAfter, func() {
			l.despawn(uid, item)
		})
	}
	l.mu.Unlock()
	return nil
}

// Cancel stops future callbacks for an object. World removal itself remains
// the caller's responsibility because pickup and teardown own that boundary.
func (l *GroundItemLifecycle) Cancel(item *model.Item) {
	if l == nil || item == nil {
		return
	}
	uid := item.GetUniqueID()
	l.mu.Lock()
	timer := l.tracked[uid]
	delete(l.tracked, uid)
	stopGroundItemTimer(timer)
	l.mu.Unlock()
}

// Close cancels all outstanding callbacks during Agent shutdown. It does not
// remove world objects; shutdown owns world teardown and must decide whether
// those objects are persisted, discarded, or drained first.
func (l *GroundItemLifecycle) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	tracked := l.tracked
	l.tracked = make(map[uint32]*groundItemTimer)
	for _, timer := range tracked {
		stopGroundItemTimer(timer)
	}
	l.mu.Unlock()
}

func (l *GroundItemLifecycle) releaseOwnership(uid uint32, item *model.Item) {
	l.mu.Lock()
	timer := l.tracked[uid]
	if timer == nil || timer.item != item {
		l.mu.Unlock()
		return
	}
	callback := l.ownershipRelease
	// Ownership expiry is one-shot. Keep the despawn timer alive, but clear the
	// ownership handle so a repeated timer callback cannot notify twice.
	timer.ownership = nil
	item.OwnerUniqueID = 0
	l.mu.Unlock()
	if callback != nil {
		_ = callback(item)
	}
}

func (l *GroundItemLifecycle) despawn(uid uint32, item *model.Item) {
	l.mu.Lock()
	timer := l.tracked[uid]
	if timer == nil || timer.item != item {
		l.mu.Unlock()
		return
	}
	delete(l.tracked, uid)
	stopGroundItemTimer(timer)
	world := l.world
	l.mu.Unlock()
	world.RemoveVisibleObject(item)
}

func stopGroundItemTimer(timer *groundItemTimer) {
	if timer == nil {
		return
	}
	if timer.ownership != nil {
		timer.ownership.Stop()
	}
	if timer.despawn != nil {
		timer.despawn.Stop()
	}
}
