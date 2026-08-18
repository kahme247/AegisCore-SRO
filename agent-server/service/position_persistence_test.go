package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type fakePositionRepository struct {
	fail  bool
	calls int
	last  dirtyPosition
}

type scriptedPositionRepository struct {
	mutex     sync.Mutex
	results   []error
	calls     []dirtyPosition
	callIndex int
}

func (r *scriptedPositionRepository) UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.calls = append(r.calls, dirtyPosition{
		characterID: characterID,
		position: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.Region{ID: regionID},
			Offset: math32.NewVector3(x, y, z),
		},
	})
	if r.callIndex >= len(r.results) {
		return nil
	}
	err := r.results[r.callIndex]
	r.callIndex++
	return err
}

func (r *scriptedPositionRepository) callCount() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.calls)
}

func (r *scriptedPositionRepository) snapshots() []dirtyPosition {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]dirtyPosition(nil), r.calls...)
}

type blockingPositionRepository struct {
	started chan struct{}
	release chan struct{}

	mutex        sync.Mutex
	callsStarted int
	calls        []dirtyPosition
}

func (r *blockingPositionRepository) UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error {
	r.mutex.Lock()
	callNumber := r.callsStarted
	r.callsStarted++
	r.mutex.Unlock()
	if callNumber == 0 {
		close(r.started)
		<-r.release
	}

	r.mutex.Lock()
	r.calls = append(r.calls, dirtyPosition{
		characterID: characterID,
		position: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.Region{ID: regionID},
			Offset: math32.NewVector3(x, y, z),
		},
	})
	r.mutex.Unlock()
	return nil
}

func (r *blockingPositionRepository) snapshots() []dirtyPosition {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]dirtyPosition(nil), r.calls...)
}

type contextBlockingPositionRepository struct {
	started chan struct{}
}

func (r *contextBlockingPositionRepository) UpdateCharacterPosition(int, int16, float32, float32, float32) error {
	return errors.New("legacy write path must not be used")
}

func (r *contextBlockingPositionRepository) UpdateCharacterPositionContext(ctx context.Context, _ int, _ int16, _, _, _ float32) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

type contextAwareModelPositionRepository struct {
	contextValue any
	calls        []dirtyPosition
}

func (r *contextAwareModelPositionRepository) UpdateCharacterPosition(ctx context.Context, characterID int, regionID int16, x, y, z float32) error {
	r.contextValue = ctx.Value(positionRepositoryContextKey{})
	r.calls = append(r.calls, dirtyPosition{
		characterID: characterID,
		position: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.Region{ID: regionID},
			Offset: math32.NewVector3(x, y, z),
		},
	})
	return nil
}

type positionRepositoryContextKey struct{}

func (f *fakePositionRepository) UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error {
	f.calls++
	f.last = dirtyPosition{characterID: characterID, position: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: regionID}, Offset: math32.NewVector3(x, y, z)}}
	if f.fail {
		return errors.New("database unavailable")
	}
	return nil
}

func persistenceTestPlayer(uid uint32, characterID int, x float32) *model.Player {
	return &model.Player{
		ID: characterID,
		SRObject: model.SRObject{
			UniqueID: uid,
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
				Region: navmeshv2.Region{ID: 25000},
				Offset: math32.NewVector3(x, 5, 7),
			},
		},
	}
}

func TestPositionPersistenceRetriesAndDeduplicates(t *testing.T) {
	repository := &fakePositionRepository{fail: true}
	service := NewPositionPersistenceService(repository, time.Second)
	player := persistenceTestPlayer(7, 99, 10)
	service.MarkDirty(player)
	if err := service.FlushPlayer(7); err == nil {
		t.Fatal("expected failed persistence attempt")
	}
	if len(service.dirty) != 1 {
		t.Fatal("failed snapshot was removed instead of retained")
	}

	repository.fail = false
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25001}, Offset: math32.NewVector3(20, 6, 8)})
	service.MarkDirty(player)
	if err := service.FlushPlayer(7); err != nil {
		t.Fatalf("unexpected persistence failure: %v", err)
	}
	if len(service.dirty) != 0 {
		t.Fatal("successful current snapshot was not cleared")
	}
	if repository.last.characterID != 99 || repository.last.position.Region.ID != 25001 || repository.last.position.Offset.X != 20 {
		t.Fatalf("unexpected persisted snapshot: %+v", repository.last)
	}
}

func TestStalePersistenceCompletionKeepsNewerSnapshot(t *testing.T) {
	repository := &fakePositionRepository{}
	service := NewPositionPersistenceService(repository, time.Second)
	player := persistenceTestPlayer(8, 100, 10)
	service.MarkDirty(player)
	stale := service.dirty[8]
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(30, 5, 7)})
	service.MarkDirty(player)
	if err := service.flushSnapshot(stale); err != nil {
		t.Fatalf("unexpected stale flush error: %v", err)
	}
	if current, exists := service.dirty[8]; !exists || current.position.Offset.X != 30 {
		t.Fatal("stale flush removed the newer dirty snapshot")
	}
}

func TestPositionPersistenceCheckpointInterval(t *testing.T) {
	repository := &fakePositionRepository{}
	service := NewPositionPersistenceService(repository, 30*time.Second)
	service.lastCheckpoint = time.Unix(100, 0)
	service.MarkDirty(persistenceTestPlayer(9, 101, 40))
	service.FlushDue(time.Unix(129, 0))
	if repository.calls != 0 {
		t.Fatalf("checkpoint flushed early: %d writes", repository.calls)
	}
	service.FlushDue(time.Unix(130, 0))
	if repository.calls != 1 || len(service.dirty) != 0 {
		t.Fatalf("checkpoint did not flush once: writes=%d dirty=%d", repository.calls, len(service.dirty))
	}
}

func TestPositionPersistenceRetriesWithBoundedBackoff(t *testing.T) {
	start := time.Unix(1000, 0)
	now := start
	repository := &scriptedPositionRepository{results: []error{
		errors.New("mysql://credential@database.example:3306 unavailable"),
		errors.New("mysql://credential@database.example:3306 unavailable"),
		nil,
	}}
	service := NewPositionPersistenceServiceWithOptions(repository, time.Nanosecond, PositionPersistenceOptions{
		MaxPending:          2,
		RetryInitialBackoff: time.Second,
		RetryMaxBackoff:     4 * time.Second,
		Now:                 func() time.Time { return now },
	})
	service.lastCheckpoint = start.Add(-time.Nanosecond)
	service.MarkDirty(persistenceTestPlayer(11, 111, 10))

	service.FlushDue(now)
	if got := repository.callCount(); got != 1 {
		t.Fatalf("first checkpoint calls=%d, want 1", got)
	}
	stats := service.Stats()
	if stats.Pending != 1 || stats.RetryPending != 1 || stats.FlushFailed != 1 || stats.RetryScheduled != 1 {
		t.Fatalf("unexpected first-failure stats: %+v", stats)
	}
	if stats.LastFailure != "repository-error" {
		t.Fatalf("stats exposed repository detail: %q", stats.LastFailure)
	}
	if !stats.LastFailureAt.Equal(start) {
		t.Fatalf("failure timestamp=%v, want %v", stats.LastFailureAt, start)
	}

	now = start.Add(500 * time.Millisecond)
	service.FlushDue(now)
	if got := repository.callCount(); got != 1 {
		t.Fatalf("retry ran before initial backoff: calls=%d", got)
	}

	now = start.Add(time.Second)
	service.FlushDue(now)
	if got := repository.callCount(); got != 2 {
		t.Fatalf("first retry calls=%d, want 2", got)
	}
	service.mutex.Lock()
	retryAt := service.dirty[11].retryAt
	service.mutex.Unlock()
	if want := start.Add(3 * time.Second); !retryAt.Equal(want) {
		t.Fatalf("second retry due at %v, want %v", retryAt, want)
	}

	now = start.Add(2 * time.Second)
	service.FlushDue(now)
	if got := repository.callCount(); got != 2 {
		t.Fatalf("exponential retry ran early: calls=%d", got)
	}

	now = start.Add(3 * time.Second)
	service.FlushDue(now)
	stats = service.Stats()
	if got := repository.callCount(); got != 3 {
		t.Fatalf("second retry calls=%d, want 3", got)
	}
	if stats.Pending != 0 || stats.FlushAttempts != 3 || stats.FlushSucceeded != 1 || stats.FlushFailed != 2 || stats.RetryScheduled != 2 {
		t.Fatalf("unexpected recovered stats: %+v", stats)
	}
}

func TestPositionPersistenceBoundsPendingEntriesWhileCoalescing(t *testing.T) {
	service := NewPositionPersistenceServiceWithOptions(&fakePositionRepository{}, time.Second, PositionPersistenceOptions{MaxPending: 2})
	first := persistenceTestPlayer(21, 201, 10)
	second := persistenceTestPlayer(22, 202, 20)
	third := persistenceTestPlayer(23, 203, 30)
	if err := service.TryMarkDirty(first); err != nil {
		t.Fatalf("mark first: %v", err)
	}
	if err := service.TryMarkDirty(second); err != nil {
		t.Fatalf("mark second: %v", err)
	}
	if err := service.TryMarkDirty(third); !errors.Is(err, ErrPositionPersistencePendingFull) {
		t.Fatalf("third player error=%v, want capacity error", err)
	}

	first.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(99, 5, 7)})
	if err := service.TryMarkDirty(first); err != nil {
		t.Fatalf("coalesced update was rejected at capacity: %v", err)
	}
	service.MarkDirty(third)

	stats := service.Stats()
	if stats.Pending != 2 || stats.MaxPending != 2 || stats.OverloadDrops != 2 {
		t.Fatalf("unexpected bounded queue stats: %+v", stats)
	}
	service.mutex.Lock()
	updated := service.dirty[first.GetUniqueID()]
	_, rejectedPresent := service.dirty[third.GetUniqueID()]
	service.mutex.Unlock()
	if updated.position.Offset.X != 99 {
		t.Fatalf("coalesced position=%v, want latest x=99", updated.position.Offset.X)
	}
	if rejectedPresent {
		t.Fatal("rejected player was added despite bounded capacity")
	}
}

func TestPositionPersistenceSerializesSupersededFlushes(t *testing.T) {
	repository := &blockingPositionRepository{started: make(chan struct{}), release: make(chan struct{})}
	service := NewPositionPersistenceServiceWithOptions(repository, time.Second, PositionPersistenceOptions{MaxPending: 2})
	player := persistenceTestPlayer(31, 301, 10)
	service.MarkDirty(player)
	firstResult := make(chan error, 1)
	go func() { firstResult <- service.FlushPlayer(player.GetUniqueID()) }()
	<-repository.started

	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(20, 5, 7)})
	service.MarkDirty(player)
	_, waitFor, exists := service.claimPlayer(player.GetUniqueID())
	if !exists || waitFor == nil {
		t.Fatal("newer snapshot was not held behind the in-flight write")
	}
	if stats := service.Stats(); stats.InFlight != 1 || stats.Pending != 1 {
		t.Fatalf("unexpected in-flight state after coalescing: %+v", stats)
	}

	close(repository.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first flush failed: %v", err)
	}
	select {
	case <-waitFor:
	case <-time.After(time.Second):
		t.Fatal("superseded flush never released waiting callers")
	}
	if err := service.FlushPlayer(player.GetUniqueID()); err != nil {
		t.Fatalf("newer flush failed: %v", err)
	}

	writes := repository.snapshots()
	if len(writes) != 2 || writes[0].position.Offset.X != 10 || writes[1].position.Offset.X != 20 {
		t.Fatalf("writes were not serialized oldest-to-latest: %+v", writes)
	}
	if stats := service.Stats(); stats.Pending != 0 || stats.InFlight != 0 {
		t.Fatalf("queue did not settle after serialized writes: %+v", stats)
	}
}

func TestPositionPersistenceDrainSuccessAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repository := &scriptedPositionRepository{}
		service := NewPositionPersistenceServiceWithOptions(repository, time.Second, PositionPersistenceOptions{MaxPending: 3})
		service.MarkDirty(persistenceTestPlayer(42, 402, 42))
		service.MarkDirty(persistenceTestPlayer(41, 401, 41))
		if err := service.Drain(context.Background()); err != nil {
			t.Fatalf("drain: %v", err)
		}
		writes := repository.snapshots()
		if len(writes) != 2 || writes[0].characterID != 401 || writes[1].characterID != 402 {
			t.Fatalf("drain did not use stable UID order: %+v", writes)
		}
		if stats := service.Stats(); stats.Pending != 0 || stats.FlushSucceeded != 2 {
			t.Fatalf("unexpected successful drain stats: %+v", stats)
		}
	})

	t.Run("failure retains state", func(t *testing.T) {
		failure := errors.New("database write refused")
		repository := &scriptedPositionRepository{results: []error{failure, nil}}
		service := NewPositionPersistenceServiceWithOptions(repository, time.Second, PositionPersistenceOptions{MaxPending: 1})
		service.MarkDirty(persistenceTestPlayer(43, 403, 43))
		if err := service.Drain(context.Background()); !errors.Is(err, failure) {
			t.Fatalf("drain error=%v, want repository failure", err)
		}
		if stats := service.Stats(); stats.Pending != 1 || stats.RetryPending != 1 || stats.LastFailure != "repository-error" {
			t.Fatalf("failed drain did not retain/reclassify state: %+v", stats)
		}
		if err := service.Drain(context.Background()); err != nil {
			t.Fatalf("retry drain: %v", err)
		}
		if stats := service.Stats(); stats.Pending != 0 || stats.FlushSucceeded != 1 {
			t.Fatalf("successful retry did not drain: %+v", stats)
		}
	})
}

func TestPositionPersistenceDrainCancellation(t *testing.T) {
	repository := &contextBlockingPositionRepository{started: make(chan struct{})}
	service := NewPositionPersistenceServiceWithOptions(repository, time.Second, PositionPersistenceOptions{MaxPending: 1})
	service.MarkDirty(persistenceTestPlayer(51, 501, 51))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.Drain(ctx) }()
	<-repository.started
	if stats := service.Stats(); stats.Pending != 1 || stats.InFlight != 1 {
		t.Fatalf("unexpected in-flight drain stats: %+v", stats)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("drain cancellation error=%v, want context canceled", err)
	}
	if stats := service.Stats(); stats.Pending != 1 || stats.InFlight != 0 || stats.FlushCanceled != 1 || stats.LastFailure != "canceled" {
		t.Fatalf("unexpected cancelled drain stats: %+v", stats)
	}
}

func TestModelCharacterPositionRepositoryAdapterUsesDrainContext(t *testing.T) {
	repository := &contextAwareModelPositionRepository{}
	adapter := NewModelCharacterPositionRepositoryAdapter(repository)
	service := NewPositionPersistenceServiceWithOptions(adapter, time.Second, PositionPersistenceOptions{MaxPending: 1})
	service.MarkDirty(persistenceTestPlayer(61, 601, 61))

	ctx := context.WithValue(context.Background(), positionRepositoryContextKey{}, "shutdown-drain")
	if err := service.Drain(ctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if repository.contextValue != "shutdown-drain" {
		t.Fatalf("repository context value = %#v, want shutdown-drain", repository.contextValue)
	}
	if len(repository.calls) != 1 || repository.calls[0].characterID != 601 || repository.calls[0].position.Offset.X != 61 {
		t.Fatalf("repository calls = %#v, want one full position snapshot", repository.calls)
	}
}

func TestCompletedRegionCrossingMovesMembershipAndQueuesPersistence(t *testing.T) {
	repository := &fakePositionRepository{}
	persistence := NewPositionPersistenceService(repository, 30*time.Second)
	oldPosition := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1919, 5, 10)}
	newPosition := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25001}, Offset: math32.NewVector3(1, 5, 10)}
	player := &model.Player{ID: 102, SRObject: model.SRObject{UniqueID: 10, RtNavmeshPosition: oldPosition}}
	world := &WorldService{
		movingObjects:          map[uint32]model.ICharacter{10: player},
		visibleObjectsByRegion: map[int16][]model.ISRObject{25000: {player}},
		mutex:                  &sync.Mutex{},
		positionPersistence:    persistence,
	}
	movement := player.BeginMovement(model.MovementData{HasDestination: true, TargetPosition: newPosition})
	if !world.commitMovementStep(player, movement.Generation, newPosition, time.Now(), true, "arrived", false) {
		t.Fatal("region crossing did not finalize")
	}
	if len(world.visibleObjectsByRegion[25000]) != 0 || len(world.visibleObjectsByRegion[25001]) != 1 {
		t.Fatalf("incorrect region membership: old=%d new=%d", len(world.visibleObjectsByRegion[25000]), len(world.visibleObjectsByRegion[25001]))
	}
	if _, moving := world.movingObjects[10]; moving {
		t.Fatal("completed player remained in moving-object index")
	}
	if repository.calls != 0 {
		t.Fatalf("region transition blocked movement on %d synchronous writes", repository.calls)
	}
	if stats := persistence.Stats(); stats.Pending != 1 {
		t.Fatalf("region transition pending persistence = %d, want 1", stats.Pending)
	}
	if err := persistence.FlushPlayer(player.GetUniqueID()); err != nil {
		t.Fatalf("flush queued region transition: %v", err)
	}
	if repository.calls != 1 {
		t.Fatalf("queued region transition writes = %d, want 1", repository.calls)
	}
}
