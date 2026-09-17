package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type dimensionHoleScopeAllocatorFake struct {
	mu           sync.Mutex
	reserveCalls int
	releaseCalls int
	reserveErr   error
	releaseErr   error
}

func (a *dimensionHoleScopeAllocatorFake) ReserveDimensionHoleScope(ctx context.Context, definition model.DimensionHoleDefinition, key uint32) (model.DimensionHoleScopeLease, error) {
	if err := ctx.Err(); err != nil {
		return model.DimensionHoleScopeLease{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reserveCalls++
	if a.reserveErr != nil {
		return model.DimensionHoleScopeLease{}, a.reserveErr
	}
	scope, _ := model.NewGameWorldScope(definition.WorldID, 1)
	return model.DimensionHoleScopeLease{Scope: scope, ReservationKey: key, ItemRefID: definition.ItemRefID}, nil
}

func (a *dimensionHoleScopeAllocatorFake) ReleaseDimensionHoleScope(ctx context.Context, _ model.DimensionHoleScopeLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	a.releaseCalls++
	err := a.releaseErr
	a.mu.Unlock()
	return err
}

func (*dimensionHoleScopeAllocatorFake) DimensionHoleLeaseForScope(model.GameWorldScope) (model.DimensionHoleScopeLease, bool) {
	return model.DimensionHoleScopeLease{}, false
}

func (a *dimensionHoleScopeAllocatorFake) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reserveCalls, a.releaseCalls
}

func TestDimensionHolePortalLifecycleOpensAndDestroysLeaseBackedPortal(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: 100}
	portals, _ := NewDimensionHolePortalService(world)
	scopes := &dimensionHoleScopeAllocatorFake{}
	lifecycle, err := NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	if err != nil {
		t.Fatal(err)
	}
	definition := model.DimensionHoleCatalog()[0]
	owner := dimensionHolePortalTestOwner(7, 900, "Owner")
	instance, err := lifecycle.Open(context.Background(), definition, owner, dimensionHolePortalTestPosition())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if instance.Portal == nil || instance.Portal.GetUniqueID() != 101 || instance.Lease.Scope.WorldID() != definition.WorldID || instance.Lease.ReservationKey != 700 {
		t.Fatalf("instance = %#v", instance)
	}
	if found, ok := lifecycle.Instance(101); !ok || found != instance {
		t.Fatalf("Instance(101) = %#v, %t", found, ok)
	}
	if err = lifecycle.Destroy(context.Background(), 101); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, ok := lifecycle.Instance(101); ok || len(world.removed) != 1 {
		t.Fatal("destroyed portal remained registered or visible")
	}
	if reserves, releases := scopes.counts(); reserves != 1 || releases != 1 {
		t.Fatalf("scope calls = reserve:%d release:%d", reserves, releases)
	}
	if err = lifecycle.Destroy(context.Background(), 101); !errors.Is(err, ErrDimensionHolePortalMissing) {
		t.Fatalf("second Destroy() error = %v", err)
	}
	if _, releases := scopes.counts(); releases != 1 {
		t.Fatalf("second Destroy() released scope again: %d", releases)
	}
}

func TestDimensionHolePortalLifecycleRollsBackLeaseWhenPublicationFails(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: ^uint32(0)}
	portals, _ := NewDimensionHolePortalService(world)
	scopes := &dimensionHoleScopeAllocatorFake{}
	lifecycle, _ := NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	owner := dimensionHolePortalTestOwner(7, 900, "Owner")
	instance, err := lifecycle.Open(context.Background(), model.DimensionHoleCatalog()[0], owner, dimensionHolePortalTestPosition())
	if instance.Portal != nil || !errors.Is(err, ErrDimensionHolePortalUnavailable) {
		t.Fatalf("Open() = %#v, %v", instance, err)
	}
	if reserves, releases := scopes.counts(); reserves != 1 || releases != 1 {
		t.Fatalf("scope calls = reserve:%d release:%d", reserves, releases)
	}
	if len(world.added) != 1 || len(world.removed) != 1 {
		t.Fatalf("world publication rollback = added:%d removed:%d", len(world.added), len(world.removed))
	}
}

func TestDimensionHolePortalLifecycleExpiresAtPinnedWorldDuration(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: 100}
	portals, _ := NewDimensionHolePortalService(world)
	scopes := &dimensionHoleScopeAllocatorFake{}
	lifecycle, _ := NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	var delay time.Duration
	var expire func()
	lifecycle.schedule = func(got time.Duration, callback func()) func() bool {
		delay, expire = got, callback
		return func() bool { return true }
	}
	instance, err := lifecycle.Open(context.Background(), model.DimensionHoleCatalog()[0], dimensionHolePortalTestOwner(7, 900, "Owner"), dimensionHolePortalTestPosition())
	if err != nil {
		t.Fatal(err)
	}
	wantLifetime := time.Duration(model.DimensionHoleWorldDurationMS) * time.Millisecond
	if delay != wantLifetime || !instance.ExpiresAt.Equal(now.Add(wantLifetime)) || expire == nil {
		t.Fatalf("expiry = delay:%s at:%s", delay, instance.ExpiresAt)
	}
	expire()
	if _, ok := lifecycle.Instance(instance.Portal.GetUniqueID()); ok || len(world.removed) != 1 {
		t.Fatal("expired portal remained registered or visible")
	}
	if _, releases := scopes.counts(); releases != 1 {
		t.Fatalf("expired portal releases = %d, want 1", releases)
	}
}

func TestDimensionHolePortalLifecycleCloseDrainsPublishedScopes(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: 100}
	portals, _ := NewDimensionHolePortalService(world)
	scopes := &dimensionHoleScopeAllocatorFake{}
	lifecycle, _ := NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	owner := dimensionHolePortalTestOwner(7, 900, "Owner")
	for _, definition := range model.DimensionHoleCatalog()[:2] {
		if _, err := lifecycle.Open(context.Background(), definition, owner, dimensionHolePortalTestPosition()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lifecycle.Prepare(context.Background(), model.DimensionHoleCatalog()[2], owner, dimensionHolePortalTestPosition()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(world.removed) != 2 {
		t.Fatalf("Close removed %d portals, want 2", len(world.removed))
	}
	if reserves, releases := scopes.counts(); reserves != 3 || releases != 3 {
		t.Fatalf("Close scope calls = reserve:%d release:%d", reserves, releases)
	}
}

func TestDimensionHolePortalLifecycleAbortsPreparationExactlyOnce(t *testing.T) {
	world := &dimensionHolePortalWorldFake{}
	portals, _ := NewDimensionHolePortalService(world)
	scopes := &dimensionHoleScopeAllocatorFake{}
	lifecycle, _ := NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	prepared, err := lifecycle.Prepare(
		context.Background(), model.DimensionHoleCatalog()[0],
		dimensionHolePortalTestOwner(7, 900, "Owner"), dimensionHolePortalTestPosition(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = lifecycle.Abort(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if err = lifecycle.Abort(context.Background(), prepared); !errors.Is(err, ErrDimensionHolePortalPreparationMissing) {
		t.Fatalf("second Abort() error = %v", err)
	}
	if _, err = lifecycle.Publish(context.Background(), prepared); !errors.Is(err, ErrDimensionHolePortalPreparationMissing) {
		t.Fatalf("Publish(aborted) error = %v", err)
	}
	if reserves, releases := scopes.counts(); reserves != 1 || releases != 1 || len(world.added) != 0 {
		t.Fatalf("aborted preparation = reserve:%d release:%d world:%d", reserves, releases, len(world.added))
	}
}

func TestDimensionHolePortalLifecycleHonorsCancellationBeforeMutation(t *testing.T) {
	world := &dimensionHolePortalWorldFake{}
	portals, _ := NewDimensionHolePortalService(world)
	scopes := &dimensionHoleScopeAllocatorFake{}
	lifecycle, _ := NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lifecycle.Open(ctx, model.DimensionHoleCatalog()[0], dimensionHolePortalTestOwner(7, 900, "Owner"), dimensionHolePortalTestPosition())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open(canceled) error = %v", err)
	}
	if reserves, releases := scopes.counts(); reserves != 0 || releases != 0 || len(world.added) != 0 {
		t.Fatalf("canceled open mutated state: reserve:%d release:%d world:%d", reserves, releases, len(world.added))
	}
}

func TestDimensionHolePortalLifecycleRejectsMissingDependencies(t *testing.T) {
	if lifecycle, err := NewDimensionHolePortalLifecycle(nil, nil, nil); lifecycle != nil || !errors.Is(err, ErrDimensionHolePortalLifecycleUnavailable) {
		t.Fatalf("NewDimensionHolePortalLifecycle(nil) = %#v, %v", lifecycle, err)
	}
}
