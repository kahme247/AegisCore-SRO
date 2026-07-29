package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultPositionPersistenceMaxPending bounds the number of distinct players
	// whose most recent position is waiting to be saved. A player may update an
	// existing pending entry without consuming another slot.
	DefaultPositionPersistenceMaxPending = 4096

	// DefaultPositionPersistenceRetryInitialBackoff is the delay before the
	// first checkpoint retry after a repository failure.
	DefaultPositionPersistenceRetryInitialBackoff = time.Second

	// DefaultPositionPersistenceRetryMaxBackoff caps exponential retry delays so
	// a transient outage can recover without leaving an entry deferred forever.
	DefaultPositionPersistenceRetryMaxBackoff = 30 * time.Second
)

var (
	ErrPositionPersistencePendingFull   = errors.New("position persistence pending capacity reached")
	ErrPositionPersistenceInvalidPlayer = errors.New("invalid player for position persistence")
	ErrPositionPersistenceNoRepository  = errors.New("position persistence repository is not configured")
)

type CharacterPositionRepository interface {
	UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error
}

// ContextCharacterPositionRepository is an optional, backward-compatible
// extension for repositories that can cancel an in-flight write. Existing
// repositories continue to work through CharacterPositionRepository; for
// those repositories Drain observes cancellation between writes.
type ContextCharacterPositionRepository interface {
	UpdateCharacterPositionContext(ctx context.Context, characterID int, regionID int16, x, y, z float32) error
}

// ModelCharacterPositionRepositoryAdapter exposes a context-aware model
// repository to the bounded position queue without changing its legacy
// repository interface. New startup code can inject this adapter around a
// model.SQLCharacterPositionRepository backed by a long-lived shard pool.
// The adapter owns neither the repository nor its database dependency.
type ModelCharacterPositionRepositoryAdapter struct {
	repository model.CharacterPositionRepository
}

var _ CharacterPositionRepository = (*ModelCharacterPositionRepositoryAdapter)(nil)
var _ ContextCharacterPositionRepository = (*ModelCharacterPositionRepositoryAdapter)(nil)

// NewModelCharacterPositionRepositoryAdapter adapts an injected model
// repository for PositionPersistenceService. A nil dependency is retained as
// an explicit repository-unavailable error at write time rather than causing
// a constructor panic.
func NewModelCharacterPositionRepositoryAdapter(repository model.CharacterPositionRepository) *ModelCharacterPositionRepositoryAdapter {
	return &ModelCharacterPositionRepositoryAdapter{repository: repository}
}

// UpdateCharacterPosition preserves the legacy queue interface for callers
// that do not pass a context. PositionPersistenceService itself prefers the
// context-aware method below whenever this adapter is installed.
func (r *ModelCharacterPositionRepositoryAdapter) UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error {
	return r.UpdateCharacterPositionContext(context.Background(), characterID, regionID, x, y, z)
}

// UpdateCharacterPositionContext forwards cancellation and the complete
// snapshot to the injected repository. No request-scoped database connection
// is opened here.
func (r *ModelCharacterPositionRepositoryAdapter) UpdateCharacterPositionContext(ctx context.Context, characterID int, regionID int16, x, y, z float32) error {
	if r == nil || r.repository == nil {
		return model.ErrCharacterPositionRepositoryUnavailable
	}
	return r.repository.UpdateCharacterPosition(ctx, characterID, regionID, x, y, z)
}

type databaseCharacterPositionRepository struct{}

func (databaseCharacterPositionRepository) UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error {
	return model.UpdateCharacterPosition(characterID, regionID, x, y, z)
}

// PositionPersistenceOptions tunes the bounded queue. It is intentionally an
// optional constructor argument so existing WorldService startup remains
// source-compatible while tests and future configuration plumbing can choose
// tighter limits.
type PositionPersistenceOptions struct {
	MaxPending          int
	RetryInitialBackoff time.Duration
	RetryMaxBackoff     time.Duration

	// Now is primarily useful for deterministic tests. Nil uses time.Now.
	Now func() time.Time
}

// PositionPersistenceStats is a value-only snapshot. It deliberately exposes
// no map, player, or repository references, so callers can safely observe it
// from diagnostics and metrics code without retaining mutable service state.
type PositionPersistenceStats struct {
	Pending      int
	MaxPending   int
	InFlight     int
	RetryPending int

	OverloadDrops  uint64
	FlushAttempts  uint64
	FlushSucceeded uint64
	FlushFailed    uint64
	RetryScheduled uint64
	FlushCanceled  uint64

	// LastFailure is a coarse class, never raw repository or driver text.
	LastFailure   string
	LastFailureAt time.Time
}

type dirtyPosition struct {
	playerUID   uint32
	characterID int
	position    navmeshv2.RtNavmeshPosition
	version     uint64

	retryAt       time.Time
	retryAttempts uint
	flushing      bool
	flushDone     chan struct{}
}

type PositionPersistenceService struct {
	mutex              sync.Mutex
	repository         CharacterPositionRepository
	dirty              map[uint32]dirtyPosition
	nextVersion        uint64
	checkpointInterval time.Duration
	lastCheckpoint     time.Time

	maxPending          int
	retryInitialBackoff time.Duration
	retryMaxBackoff     time.Duration
	now                 func() time.Time

	overloadDrops  uint64
	flushAttempts  uint64
	flushSucceeded uint64
	flushFailed    uint64
	retryScheduled uint64
	flushCanceled  uint64
	inFlight       int
	lastFailure    string
	lastFailureAt  time.Time
}

func NewPositionPersistenceService(repository CharacterPositionRepository, checkpointInterval time.Duration) *PositionPersistenceService {
	return NewPositionPersistenceServiceWithOptions(repository, checkpointInterval, PositionPersistenceOptions{})
}

// NewPositionPersistenceServiceWithOptions creates a bounded, coalescing
// position queue. A non-positive option uses the conservative default rather
// than creating an unbounded or tight retry loop.
func NewPositionPersistenceServiceWithOptions(repository CharacterPositionRepository, checkpointInterval time.Duration, options PositionPersistenceOptions) *PositionPersistenceService {
	if checkpointInterval <= 0 {
		checkpointInterval = 30 * time.Second
	}
	if options.MaxPending <= 0 {
		options.MaxPending = DefaultPositionPersistenceMaxPending
	}
	if options.RetryInitialBackoff <= 0 {
		options.RetryInitialBackoff = DefaultPositionPersistenceRetryInitialBackoff
	}
	if options.RetryMaxBackoff <= 0 {
		options.RetryMaxBackoff = DefaultPositionPersistenceRetryMaxBackoff
	}
	if options.RetryMaxBackoff < options.RetryInitialBackoff {
		options.RetryMaxBackoff = options.RetryInitialBackoff
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	return &PositionPersistenceService{
		repository:          repository,
		dirty:               make(map[uint32]dirtyPosition),
		checkpointInterval:  checkpointInterval,
		lastCheckpoint:      options.Now(),
		maxPending:          options.MaxPending,
		retryInitialBackoff: options.RetryInitialBackoff,
		retryMaxBackoff:     options.RetryMaxBackoff,
		now:                 options.Now,
	}
}

// MarkDirty preserves the original fire-and-forget caller contract. If the
// bounded pending set is full, it records and logs a deliberate overload drop
// instead of growing the map without limit. Callers that need to surface this
// condition may use TryMarkDirty.
func (p *PositionPersistenceService) MarkDirty(player *model.Player) {
	if err := p.TryMarkDirty(player); err != nil {
		if errors.Is(err, ErrPositionPersistencePendingFull) {
			logrus.Warnf("position persistence queue full; dropping dirty position for player %d", player.GetUniqueID())
			return
		}
		logrus.Warnf("failed to mark player position dirty: %v", err)
	}
}

// TryMarkDirty records the latest position for a player. It coalesces updates
// already in the queue even while at capacity, but rejects a new player when
// accepting it would exceed MaxPending.
func (p *PositionPersistenceService) TryMarkDirty(player *model.Player) error {
	if player == nil {
		return ErrPositionPersistenceInvalidPlayer
	}

	position := player.GetNavmeshPosition()
	playerUID := player.GetUniqueID()
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if _, exists := p.dirty[playerUID]; !exists && len(p.dirty) >= p.maxPending {
		p.overloadDrops++
		return ErrPositionPersistencePendingFull
	}

	p.nextVersion++
	updated := dirtyPosition{
		playerUID:   playerUID,
		characterID: player.ID,
		position:    position,
		version:     p.nextVersion,
	}
	if existing, exists := p.dirty[playerUID]; exists && existing.flushing {
		// Keep one writer per player. Letting a newer snapshot start while the
		// old database write is still running can persist positions out of order
		// if the old write completes last. The old completion releases this
		// carried claim, after which the newest coalesced snapshot can run.
		updated.flushing = true
		updated.flushDone = existing.flushDone
	}
	p.dirty[playerUID] = updated
	return nil
}

// FlushDue checkpoints snapshots whose retry deadline has elapsed. It does
// not block the simulation waiting for another caller's in-flight write.
func (p *PositionPersistenceService) FlushDue(now time.Time) {
	p.mutex.Lock()
	if now.Sub(p.lastCheckpoint) < p.checkpointInterval {
		p.mutex.Unlock()
		return
	}
	p.lastCheckpoint = now
	snapshots := p.claimDueLocked(now)
	p.mutex.Unlock()

	for _, snapshot := range snapshots {
		_ = p.flushClaimedSnapshot(context.Background(), snapshot)
	}
}

// FlushPlayer performs one immediate persistence attempt for a player. It
// deliberately bypasses checkpoint retry delay: callers use it on important
// boundaries such as region changes and disconnect. A concurrent flush for
// the same snapshot is awaited, then any newer dirty snapshot is attempted.
func (p *PositionPersistenceService) FlushPlayer(playerUID uint32) error {
	for {
		snapshot, waitFor, exists := p.claimPlayer(playerUID)
		if !exists {
			return nil
		}
		if waitFor != nil {
			<-waitFor
			continue
		}
		return p.flushClaimedSnapshot(context.Background(), snapshot)
	}
}

// Drain synchronously attempts every dirty position until the queue is empty
// or a write/context error occurs. It is intended to be called after new game
// traffic has stopped during ordered shutdown. A failed entry remains queued
// with its normal retry state, allowing a caller to inspect stats or retry
// Drain with a later context. Context-aware repositories can cancel a blocked
// write; legacy repositories observe context cancellation between writes.
func (p *PositionPersistenceService) Drain(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		snapshot, waitFor, pending := p.claimNextForDrain(p.now())
		if !pending {
			return nil
		}
		if waitFor != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-waitFor:
				continue
			}
		}

		if err := p.flushClaimedSnapshot(ctx, snapshot); err != nil {
			return err
		}
	}
}

// Stats returns a safe point-in-time view of bounded-queue health.
func (p *PositionPersistenceService) Stats() PositionPersistenceStats {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	stats := PositionPersistenceStats{
		Pending:        len(p.dirty),
		MaxPending:     p.maxPending,
		OverloadDrops:  p.overloadDrops,
		FlushAttempts:  p.flushAttempts,
		FlushSucceeded: p.flushSucceeded,
		FlushFailed:    p.flushFailed,
		RetryScheduled: p.retryScheduled,
		FlushCanceled:  p.flushCanceled,
		LastFailure:    p.lastFailure,
		LastFailureAt:  p.lastFailureAt,
	}
	stats.InFlight = p.inFlight
	for _, snapshot := range p.dirty {
		if !snapshot.retryAt.IsZero() {
			stats.RetryPending++
		}
	}
	return stats
}

func (p *PositionPersistenceService) claimDueLocked(now time.Time) []dirtyPosition {
	snapshots := make([]dirtyPosition, 0, len(p.dirty))
	for playerUID, snapshot := range p.dirty {
		if snapshot.flushing || (!snapshot.retryAt.IsZero() && now.Before(snapshot.retryAt)) {
			continue
		}
		snapshot.flushing = true
		snapshot.flushDone = make(chan struct{})
		p.dirty[playerUID] = snapshot
		p.inFlight++
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

// claimPlayer returns either a claimed snapshot or a channel to wait for a
// concurrent write. The bool reports whether a dirty position exists.
func (p *PositionPersistenceService) claimPlayer(playerUID uint32) (dirtyPosition, <-chan struct{}, bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	snapshot, exists := p.dirty[playerUID]
	if !exists {
		return dirtyPosition{}, nil, false
	}
	if snapshot.flushing {
		return dirtyPosition{}, snapshot.flushDone, true
	}
	snapshot.flushing = true
	snapshot.flushDone = make(chan struct{})
	p.dirty[playerUID] = snapshot
	p.inFlight++
	return snapshot, nil, true
}

// claimNextForDrain chooses a stable UID order so shutdown behavior and tests
// do not depend on Go map iteration. Drain intentionally ignores retryAt: a
// shutdown flush is an explicit immediate attempt, not a periodic checkpoint.
func (p *PositionPersistenceService) claimNextForDrain(_ time.Time) (dirtyPosition, <-chan struct{}, bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var (
		selectedUID uint32
		selected    dirtyPosition
		found       bool
		waitFor     <-chan struct{}
	)
	for playerUID, snapshot := range p.dirty {
		if !found || playerUID < selectedUID {
			selectedUID = playerUID
			selected = snapshot
			found = true
		}
	}
	if !found {
		return dirtyPosition{}, nil, false
	}
	if selected.flushing {
		waitFor = selected.flushDone
		return dirtyPosition{}, waitFor, true
	}
	selected.flushing = true
	selected.flushDone = make(chan struct{})
	p.dirty[selectedUID] = selected
	p.inFlight++
	return selected, nil, true
}

// flushSnapshot is kept for package-local compatibility with existing tests
// and callers. Normal operation should claim first, but this function still
// preserves version-aware coalescing if invoked directly.
func (p *PositionPersistenceService) flushSnapshot(snapshot dirtyPosition) error {
	return p.flushClaimedSnapshot(context.Background(), snapshot)
}

func (p *PositionPersistenceService) flushClaimedSnapshot(ctx context.Context, snapshot dirtyPosition) error {
	p.mutex.Lock()
	p.flushAttempts++
	p.mutex.Unlock()

	err := p.persist(ctx, snapshot)
	completedAt := p.now()
	if err != nil {
		p.recordFailedSnapshot(snapshot, err, completedAt)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		logrus.Warnf("failed to persist position for player %d: %v", snapshot.playerUID, err)
		return err
	}

	p.recordSuccessfulSnapshot(snapshot)
	traceMovementf("persist uid=%d character=%d position=%s", snapshot.playerUID, snapshot.characterID, snapshot.position.String())
	return nil
}

func (p *PositionPersistenceService) persist(ctx context.Context, snapshot dirtyPosition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.repository == nil {
		return ErrPositionPersistenceNoRepository
	}
	position := snapshot.position
	if repository, ok := p.repository.(ContextCharacterPositionRepository); ok {
		return repository.UpdateCharacterPositionContext(
			ctx,
			snapshot.characterID,
			position.Region.ID,
			position.Offset.X,
			position.Offset.Y,
			position.Offset.Z,
		)
	}
	return p.repository.UpdateCharacterPosition(
		snapshot.characterID,
		position.Region.ID,
		position.Offset.X,
		position.Offset.Y,
		position.Offset.Z,
	)
}

func (p *PositionPersistenceService) recordSuccessfulSnapshot(snapshot dirtyPosition) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.flushSucceeded++
	current, exists := p.dirty[snapshot.playerUID]
	if exists && current.version == snapshot.version {
		delete(p.dirty, snapshot.playerUID)
	} else if p.isCarriedClaim(current, exists, snapshot) {
		current.flushing = false
		current.flushDone = nil
		p.dirty[snapshot.playerUID] = current
	}
	p.completeClaimLocked(snapshot)
}

func (p *PositionPersistenceService) recordFailedSnapshot(snapshot dirtyPosition, err error, completedAt time.Time) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.flushFailed++
	p.lastFailure = classifyPositionPersistenceError(err)
	p.lastFailureAt = completedAt
	current, exists := p.dirty[snapshot.playerUID]
	if !exists || current.version != snapshot.version {
		if p.isCarriedClaim(current, exists, snapshot) {
			// A newer movement superseded this write. Do not apply the old
			// failure's retry delay to that newer state; simply release it for a
			// fresh attempt.
			current.flushing = false
			current.flushDone = nil
			p.dirty[snapshot.playerUID] = current
		}
		p.completeClaimLocked(snapshot)
		return
	}

	current.flushing = false
	p.completeClaimLocked(snapshot)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		p.flushCanceled++
		current.flushDone = nil
		p.dirty[snapshot.playerUID] = current
		return
	}

	current.retryAttempts++
	current.retryAt = completedAt.Add(p.retryDelay(current.retryAttempts))
	current.flushDone = nil
	p.dirty[snapshot.playerUID] = current
	p.retryScheduled++
}

func (p *PositionPersistenceService) completeClaimLocked(snapshot dirtyPosition) {
	if snapshot.flushDone != nil {
		close(snapshot.flushDone)
		if p.inFlight > 0 {
			p.inFlight--
		}
	}
}

func (p *PositionPersistenceService) isCarriedClaim(current dirtyPosition, exists bool, snapshot dirtyPosition) bool {
	return exists && snapshot.flushDone != nil && current.flushing && current.flushDone == snapshot.flushDone
}

func (p *PositionPersistenceService) retryDelay(attempt uint) time.Duration {
	delay := p.retryInitialBackoff
	for i := uint(1); i < attempt && delay < p.retryMaxBackoff; i++ {
		if delay > p.retryMaxBackoff/2 {
			return p.retryMaxBackoff
		}
		delay *= 2
	}
	if delay > p.retryMaxBackoff {
		return p.retryMaxBackoff
	}
	return delay
}

func classifyPositionPersistenceError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline-exceeded"
	case errors.Is(err, ErrPositionPersistenceNoRepository):
		return "repository-not-configured"
	default:
		// Database drivers can include DSNs, SQL, or other operational detail in
		// their errors. Stats are often exported more broadly than logs, so keep
		// the observable classification intentionally coarse.
		return "repository-error"
	}
}
