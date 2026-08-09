package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var (
	// ErrCharacterPositionRepositoryUnavailable identifies a missing injected
	// shard pool or transaction. New movement persistence paths must surface
	// this failure instead of opening a database connection per checkpoint.
	ErrCharacterPositionRepositoryUnavailable = errors.New("character position repository is unavailable")
	ErrCharacterPositionIDRequired            = errors.New("character position character ID is required")
)

// CharacterPositionRepository is the context-aware persistence boundary for
// checkpointing a character position. It accepts an injected DBTX so a
// caller can use either a long-lived shard pool or a higher-level
// transaction without this repository opening or closing connections.
type CharacterPositionRepository interface {
	UpdateCharacterPosition(ctx context.Context, characterID int, regionID int16, x, y, z float32) error
}

// CharacterPositionRepositoryError preserves a machine-readable cause via
// errors.Is/errors.As while keeping Error deliberately free of database
// driver text. Position-persistence failures are logged and exported by
// diagnostics, where a DSN or query detail would be inappropriate.
type CharacterPositionRepositoryError struct {
	Operation string
	Err       error
}

func (e *CharacterPositionRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Operation == "" {
		return "character position repository failed"
	}
	return fmt.Sprintf("character position repository %s failed", e.Operation)
}

func (e *CharacterPositionRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SQLCharacterPositionRepository writes a position through an injected
// long-lived shard pool or transaction. It does not own the dependency.
type SQLCharacterPositionRepository struct {
	shard frameworkdb.DBTX
}

var _ CharacterPositionRepository = (*SQLCharacterPositionRepository)(nil)

// NewCharacterPositionRepository constructs a position repository over an
// injected shard database. The dependency is required rather than silently
// falling back to OpenConnShard.
func NewCharacterPositionRepository(shard frameworkdb.DBTX) (*SQLCharacterPositionRepository, error) {
	if shard == nil {
		return nil, &CharacterPositionRepositoryError{
			Operation: "configure shard database",
			Err:       ErrCharacterPositionRepositoryUnavailable,
		}
	}
	return &SQLCharacterPositionRepository{shard: shard}, nil
}

// UpdateCharacterPosition persists one complete coordinate snapshot. It uses
// the existing explicit, parameterized update so no position data is
// interpolated into SQL. The caller owns cancellation and transaction scope.
func (r *SQLCharacterPositionRepository) UpdateCharacterPosition(ctx context.Context, characterID int, regionID int16, x, y, z float32) error {
	if characterID <= 0 {
		return fmt.Errorf("%w: %d", ErrCharacterPositionIDRequired, characterID)
	}
	if r == nil || r.shard == nil {
		return &CharacterPositionRepositoryError{
			Operation: "update character position",
			Err:       ErrCharacterPositionRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return &CharacterPositionRepositoryError{
			Operation: "update character position",
			Err:       err,
		}
	}

	if _, err := r.shard.ExecContext(ctx, update_character_position, x, y, z, regionID, characterID); err != nil {
		return &CharacterPositionRepositoryError{
			Operation: "update character position",
			Err:       err,
		}
	}
	return nil
}
