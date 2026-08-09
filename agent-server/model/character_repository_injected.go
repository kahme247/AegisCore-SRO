package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

// ErrCharacterRepositoryUnavailable identifies a missing injected shard or
// account data source. Network-facing code must treat it as an operational
// failure instead of falling back to a request-scoped database connection.
var ErrCharacterRepositoryUnavailable = errors.New("character repository is unavailable")

// CharacterRepositoryError identifies the repository operation that failed
// while preserving the underlying driver, context, or scan error for callers
// that need to determine whether a retry is appropriate.
type CharacterRepositoryError struct {
	Operation string
	Err       error
}

func (e *CharacterRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("character repository %s: %v", e.Operation, e.Err)
}

func (e *CharacterRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CharacterRepository is the owner-qualified persistence boundary used by
// character selection and deletion flows. Its methods accept a caller-owned
// context and never create or close database connections.
type CharacterRepository interface {
	GetCharacterForOwner(ctx context.Context, charName string, userID uint32, shardID uint16) (Char, error)
	GetCharactersForOwner(ctx context.Context, userID uint32, shardID uint16) ([]Char, error)
	SetCharacterDeletionForOwner(ctx context.Context, charName string, userID uint32, shardID uint16, deleting bool) (bool, error)
	CharacterNameExists(ctx context.Context, charName string) (bool, error)
}

// CharacterJoinRecoveryRepository atomically restores a dead character at its
// configured return city before any world or client bootstrap state exists.
type CharacterJoinRecoveryRepository interface {
	RecoverDeadCharacterForJoin(ctx context.Context, characterID int, userID uint32, shardID uint16, hp, mp int) (bool, error)
}

// SQLCharacterRepository uses an injected long-lived shard pool or transaction
// for character data. account is separately injected because GM state belongs
// to SRO_ACCOUNT rather than SRO_SHARD. It does not own either dependency.
type SQLCharacterRepository struct {
	shard   frameworkdb.DBTX
	account frameworkdb.RowQueryer
}

var _ CharacterRepository = (*SQLCharacterRepository)(nil)
var _ CharacterJoinRecoveryRepository = (*SQLCharacterRepository)(nil)

const recoverDeadCharacterForJoin = "UPDATE `SRO_SHARD`.`CHAR` AS c " +
	"JOIN `SRO_SHARD`.`TELEPORT_POINT` AS t " +
	"ON t.ID=c.RETURN_TELEPORT_ID AND t.SERVICE=1 " +
	"SET c.HP=?, c.MP=?, c.FK_REGION=t.REGION_ID, " +
	"c.POS_X=t.POS_X, c.POS_Y=t.POS_Y, c.POS_Z=t.POS_Z " +
	"WHERE c.ID=? AND c.FK_USER=? AND c.FK_SHARD=? AND c.HP=0"

// NewCharacterRepository constructs the production owner-qualified
// repository. shard is required for every operation. account may be nil only
// for list/delete-only uses; GetCharacterForOwner fails closed if it cannot
// obtain GM state from an injected account queryer.
func NewCharacterRepository(shard frameworkdb.DBTX, account frameworkdb.RowQueryer) (*SQLCharacterRepository, error) {
	if shard == nil {
		return nil, &CharacterRepositoryError{
			Operation: "configure shard database",
			Err:       ErrCharacterRepositoryUnavailable,
		}
	}
	return &SQLCharacterRepository{shard: shard, account: account}, nil
}

// GetCharacterForOwner resolves a character only when it belongs to the
// authenticated account on its admitted shard. A missing, foreign, or
// wrong-shard name returns the same ErrCharacterNotFound result so ownership
// cannot be inferred from the response.
func (r *SQLCharacterRepository) GetCharacterForOwner(ctx context.Context, charName string, userID uint32, shardID uint16) (Char, error) {
	if err := validateCharacterOwner(charName, userID, shardID); err != nil {
		return Char{}, err
	}
	if err := r.requireShard("query character for owner"); err != nil {
		return Char{}, err
	}
	ctx, err := repositoryContext(ctx, "query character for owner")
	if err != nil {
		return Char{}, err
	}

	char, err := scanCharacter(r.shard.QueryRowContext(ctx, SelectCharByNameAndOwner, charName, userID, shardID))
	if errors.Is(err, sql.ErrNoRows) {
		return Char{}, ErrCharacterNotFound
	}
	if err != nil {
		return Char{}, &CharacterRepositoryError{Operation: "scan character for owner", Err: err}
	}

	isGM, err := r.getCharacterGM(ctx, char.User)
	if err != nil {
		return Char{}, err
	}
	char.IsGm = isGM
	return char, nil
}

func (r *SQLCharacterRepository) RecoverDeadCharacterForJoin(ctx context.Context, characterID int, userID uint32, shardID uint16, hp, mp int) (bool, error) {
	if characterID <= 0 || hp <= 0 || mp < 0 {
		return false, &CharacterRepositoryError{Operation: "validate dead character recovery", Err: ErrCharacterNotFound}
	}
	if err := validateCharacterScope(userID, shardID); err != nil {
		return false, err
	}
	if err := r.requireShard("recover dead character for join"); err != nil {
		return false, err
	}
	ctx, err := repositoryContext(ctx, "recover dead character for join")
	if err != nil {
		return false, err
	}
	result, err := r.shard.ExecContext(ctx, recoverDeadCharacterForJoin, hp, mp, characterID, userID, shardID)
	if err != nil {
		return false, &CharacterRepositoryError{Operation: "recover dead character for join", Err: err}
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, &CharacterRepositoryError{Operation: "read dead character recovery count", Err: err}
	}
	if affected > 1 {
		return false, &CharacterRepositoryError{Operation: "read dead character recovery count", Err: fmt.Errorf("updated %d characters", affected)}
	}
	return affected == 1, nil
}

// GetCharactersForOwner returns only characters belonging to the requested
// account on its admitted shard. It checks each scan and the terminal rows
// error so an interrupted query cannot be mistaken for a partial character
// list.
func (r *SQLCharacterRepository) GetCharactersForOwner(ctx context.Context, userID uint32, shardID uint16) ([]Char, error) {
	if err := validateCharacterScope(userID, shardID); err != nil {
		return nil, err
	}
	if err := r.requireShard("query characters for owner"); err != nil {
		return nil, err
	}
	ctx, err := repositoryContext(ctx, "query characters for owner")
	if err != nil {
		return nil, err
	}

	rows, err := r.shard.QueryContext(ctx, SelectCharsByAccountAndShard, userID, shardID)
	if err != nil {
		return nil, &CharacterRepositoryError{Operation: "query characters for owner", Err: err}
	}
	defer rows.Close()

	chars := make([]Char, 0)
	for rows.Next() {
		char, err := scanOwnerCharacter(rows, userID)
		if err != nil {
			return nil, &CharacterRepositoryError{Operation: "scan character for owner", Err: err}
		}
		chars = append(chars, char)
	}
	if err := rows.Err(); err != nil {
		return nil, &CharacterRepositoryError{Operation: "iterate characters for owner", Err: err}
	}
	return chars, nil
}

// SetCharacterDeletionForOwner changes deletion state only for a character
// owned by userID on shardID. A false result with a nil error deliberately
// covers unknown, foreign, and wrong-shard names to avoid disclosure.
func (r *SQLCharacterRepository) SetCharacterDeletionForOwner(ctx context.Context, charName string, userID uint32, shardID uint16, deleting bool) (bool, error) {
	if err := validateCharacterOwner(charName, userID, shardID); err != nil {
		return false, err
	}
	if err := r.requireShard("update character deletion state for owner"); err != nil {
		return false, err
	}
	ctx, err := repositoryContext(ctx, "update character deletion state for owner")
	if err != nil {
		return false, err
	}

	deletingValue := 0
	if deleting {
		deletingValue = 1
	}

	result, err := r.shard.ExecContext(ctx, update_is_deleting_for_owner, deletingValue, charName, userID, shardID)
	if err != nil {
		return false, &CharacterRepositoryError{Operation: "update character deletion state for owner", Err: err}
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, &CharacterRepositoryError{Operation: "read character deletion update count", Err: err}
	}
	return affected > 0, nil
}

// CharacterNameExists is the injected preflight used by the lobby. The
// transactional creator remains authoritative for duplicate handling, but
// this avoids reopening a request-scoped connection for the convenience
// check.
func (r *SQLCharacterRepository) CharacterNameExists(ctx context.Context, charName string) (bool, error) {
	if strings.TrimSpace(charName) == "" {
		return false, nil
	}
	if err := r.requireShard("query character name"); err != nil {
		return false, err
	}
	ctx, err := repositoryContext(ctx, "query character name")
	if err != nil {
		return false, err
	}
	var value int
	err = r.shard.QueryRowContext(ctx, select_does_charname_exist, charName).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, &CharacterRepositoryError{Operation: "query character name", Err: err}
	}
	return value == 1, nil
}

func (r *SQLCharacterRepository) getCharacterGM(ctx context.Context, userID int) (bool, error) {
	if r == nil || r.account == nil {
		return false, &CharacterRepositoryError{
			Operation: "query character GM status",
			Err:       ErrCharacterRepositoryUnavailable,
		}
	}

	var isGM int
	err := r.account.QueryRowContext(ctx, SelectIsGm, userID).Scan(&isGM)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, &CharacterRepositoryError{Operation: "query character GM status", Err: err}
	}
	return isGM == 1, nil
}

func (r *SQLCharacterRepository) requireShard(operation string) error {
	if r == nil || r.shard == nil {
		return &CharacterRepositoryError{Operation: operation, Err: ErrCharacterRepositoryUnavailable}
	}
	return nil
}

func repositoryContext(ctx context.Context, operation string) (context.Context, error) {
	if ctx == nil {
		return context.Background(), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, &CharacterRepositoryError{Operation: operation, Err: err}
	}
	return ctx, nil
}

func scanOwnerCharacter(scanner characterScanner, userID uint32) (Char, error) {
	var char Char
	var deleting int
	var utime sql.NullTime
	if err := scanner.Scan(
		&char.ID,
		&char.RefObjID,
		&char.Shard,
		&char.Name,
		&char.Scale,
		&char.Level,
		&char.Exp,
		&char.SkillExp,
		&char.Str,
		&char.Int,
		&char.StatPoints,
		&char.HP,
		&char.MP,
		&deleting,
		&utime,
		&char.Region,
		&char.InventorySize,
	); err != nil {
		return Char{}, err
	}
	char.User = int(userID)
	char.IsDeleting = deleting != 0
	if utime.Valid {
		char.Utime = utime.Time
	}
	return char, nil
}
