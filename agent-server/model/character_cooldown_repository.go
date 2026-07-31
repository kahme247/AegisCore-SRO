package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrCharacterCooldownRepositoryUnavailable = errors.New("character cooldown repository is unavailable")
	ErrCharacterCooldownInvalidClaim          = errors.New("character cooldown claim is invalid")
	ErrCharacterCooldownActive                = errors.New("character cooldown is active")
)

// CharacterCooldownKind scopes a cooldown source to its gameplay family. A
// source ID is only meaningful inside its kind; it may therefore be an item
// reference, a skill ID, or zero for a character-wide cooldown.
type CharacterCooldownKind uint8

const (
	CharacterCooldownItem CharacterCooldownKind = iota + 1
	CharacterCooldownSkill
	CharacterCooldownSkillGlobal
)

type CharacterCooldownClaim struct {
	Kind     CharacterCooldownKind
	SourceID uint32
	Duration time.Duration
}

type CharacterCooldownState struct {
	SourceID  uint32
	Remaining time.Duration
}

type CharacterCooldownSnapshotRepository interface {
	ActiveCooldowns(context.Context, int, CharacterCooldownKind) ([]CharacterCooldownState, error)
}

// CharacterCooldownRepository is the durable authority for cooldowns that
// must survive an Agent restart or a character relog.
type CharacterCooldownRepository interface {
	CheckCooldowns(context.Context, int, []CharacterCooldownClaim) error
	ClaimCooldowns(context.Context, int, []CharacterCooldownClaim) error
}

type CharacterCooldownStore interface {
	CharacterCooldownRepository
	CharacterCooldownSnapshotRepository
}

// CharacterCooldownChecker is the read-only preflight boundary used before a
// runtime publisher creates a visible object.
type CharacterCooldownChecker interface {
	CheckCooldowns(context.Context, int, []CharacterCooldownClaim) error
}

type SQLCharacterCooldownRepository struct{ shard *sql.DB }

func NewCharacterCooldownRepository(shard *sql.DB) (*SQLCharacterCooldownRepository, error) {
	if shard == nil {
		return nil, ErrCharacterCooldownRepositoryUnavailable
	}
	return &SQLCharacterCooldownRepository{shard: shard}, nil
}

// CheckCooldowns reads the durable state before resource consumption. It is
// intentionally non-locking: ClaimCooldowns remains the post-spend atomic
// authority that resolves a concurrent cast between this check and the claim.
func (r *SQLCharacterCooldownRepository) CheckCooldowns(ctx context.Context, characterID int, claims []CharacterCooldownClaim) error {
	if r == nil || r.shard == nil {
		return ErrCharacterCooldownRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if characterID <= 0 || len(claims) == 0 {
		return ErrCharacterCooldownInvalidClaim
	}
	for _, claim := range claims {
		if claim.Kind == 0 || claim.Duration <= 0 {
			return ErrCharacterCooldownInvalidClaim
		}
		var active int
		err := r.shard.QueryRowContext(ctx, "SELECT 1 FROM `CHAR_COOLDOWN` WHERE `CHAR_ID`=? AND `COOLDOWN_KIND`=? AND `SOURCE_ID`=? AND `EXPIRES_AT`>UTC_TIMESTAMP(6) LIMIT 1", characterID, claim.Kind, claim.SourceID).Scan(&active)
		switch {
		case err == nil:
			return ErrCharacterCooldownActive
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("check character cooldown: %w", err)
		}
	}
	return nil
}

func (r *SQLCharacterCooldownRepository) ClaimCooldowns(ctx context.Context, characterID int, claims []CharacterCooldownClaim) (err error) {
	if r == nil || r.shard == nil {
		return ErrCharacterCooldownRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin character cooldown transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = claimCharacterCooldowns(ctx, tx, characterID, claims); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit character cooldown transaction: %w", err)
	}
	return nil
}

func (r *SQLCharacterCooldownRepository) ActiveCooldowns(ctx context.Context, characterID int, kind CharacterCooldownKind) ([]CharacterCooldownState, error) {
	if r == nil || r.shard == nil {
		return nil, ErrCharacterCooldownRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if characterID <= 0 || kind == 0 {
		return nil, ErrCharacterCooldownInvalidClaim
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT `SOURCE_ID`,TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),`EXPIRES_AT`) FROM `CHAR_COOLDOWN` WHERE `CHAR_ID`=? AND `COOLDOWN_KIND`=? AND `EXPIRES_AT`>UTC_TIMESTAMP(6) ORDER BY `SOURCE_ID`", characterID, kind)
	if err != nil {
		return nil, fmt.Errorf("load active character cooldowns: %w", err)
	}
	defer rows.Close()
	states := make([]CharacterCooldownState, 0)
	for rows.Next() {
		var sourceID uint32
		var remainingMicroseconds int64
		if err = rows.Scan(&sourceID, &remainingMicroseconds); err != nil {
			return nil, fmt.Errorf("scan active character cooldown: %w", err)
		}
		if sourceID != 0 && remainingMicroseconds > 0 {
			states = append(states, CharacterCooldownState{SourceID: sourceID, Remaining: time.Duration(remainingMicroseconds) * time.Microsecond})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active character cooldowns: %w", err)
	}
	return states, nil
}

// claimCharacterCooldowns serializes claims through the character row. The
// caller may compose it into a larger transaction (item consumption) without
// creating a second transaction boundary.
func claimCharacterCooldowns(ctx context.Context, tx *sql.Tx, characterID int, claims []CharacterCooldownClaim) error {
	if tx == nil || characterID <= 0 || len(claims) == 0 {
		return ErrCharacterCooldownInvalidClaim
	}
	for _, claim := range claims {
		if claim.Kind == 0 || claim.Duration <= 0 {
			return ErrCharacterCooldownInvalidClaim
		}
	}
	var lockedCharacterID int
	if err := tx.QueryRowContext(ctx, "SELECT `id` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&lockedCharacterID); err != nil {
		return fmt.Errorf("lock character cooldown owner: %w", err)
	}
	for _, claim := range claims {
		var expiresAt, now time.Time
		err := tx.QueryRowContext(ctx, "SELECT `EXPIRES_AT`,UTC_TIMESTAMP(6) FROM `CHAR_COOLDOWN` WHERE `CHAR_ID`=? AND `COOLDOWN_KIND`=? AND `SOURCE_ID`=? FOR UPDATE", characterID, claim.Kind, claim.SourceID).Scan(&expiresAt, &now)
		switch {
		case err == nil:
			if expiresAt.After(now) {
				return ErrCharacterCooldownActive
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("lock character cooldown: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO `CHAR_COOLDOWN` (`CHAR_ID`,`COOLDOWN_KIND`,`SOURCE_ID`,`EXPIRES_AT`) VALUES (?,?,?,DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)) ON DUPLICATE KEY UPDATE `EXPIRES_AT`=VALUES(`EXPIRES_AT`)", characterID, claim.Kind, claim.SourceID, claim.Duration.Microseconds()); err != nil {
			return fmt.Errorf("persist character cooldown: %w", err)
		}
	}
	return nil
}
