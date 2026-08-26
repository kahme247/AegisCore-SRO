package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrCharacterPKRepositoryUnavailable = errors.New("character PK repository is unavailable")
	ErrCharacterPKNotFound              = errors.New("character PK target was not found")
)

type CharacterPKState struct {
	DailyPK        uint8
	TotalPK        int16
	PKPenaltyPoint int32
}

type CharacterPKRepository interface {
	LoadByName(context.Context, string) (CharacterPKState, error)
	EditByName(context.Context, string, CharacterPKState) error
}

type characterPKDatabase interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type SQLCharacterPKRepository struct{ shard characterPKDatabase }

func NewCharacterPKRepository(shard characterPKDatabase) (*SQLCharacterPKRepository, error) {
	if shard == nil {
		return nil, ErrCharacterPKRepositoryUnavailable
	}
	return &SQLCharacterPKRepository{shard: shard}, nil
}

func (r *SQLCharacterPKRepository) LoadByName(ctx context.Context, name string) (state CharacterPKState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrCharacterPKRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	name, err = validateCharacterPKName(name)
	if err != nil {
		return state, err
	}
	err = r.shard.QueryRowContext(ctx,
		"SELECT `DAILY_PK`,`TOTAL_PK`,`PK_PENALTY_POINT` FROM `SRO_SHARD`.`CHAR` WHERE `CHAR_NAME_NORMALIZED`=LOWER(?)",
		name,
	).Scan(&state.DailyPK, &state.TotalPK, &state.PKPenaltyPoint)
	if errors.Is(err, sql.ErrNoRows) {
		return CharacterPKState{}, ErrCharacterPKNotFound
	}
	if err != nil {
		return CharacterPKState{}, fmt.Errorf("load character PK state: %w", err)
	}
	return state, nil
}

// EditByName preserves the recovered _EditCharPK result contract: a missing
// character fails, while an existing character succeeds even when all three
// values are unchanged. The row lock closes the legacy lookup/update race.
func (r *SQLCharacterPKRepository) EditByName(ctx context.Context, name string, state CharacterPKState) (resultErr error) {
	if r == nil || r.shard == nil {
		return ErrCharacterPKRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	name, err := validateCharacterPKName(name)
	if err != nil {
		return err
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin character PK edit: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback character PK edit: %w", rollbackErr))
		}
	}()
	var characterID int
	err = tx.QueryRowContext(ctx,
		"SELECT `id` FROM `SRO_SHARD`.`CHAR` WHERE `CHAR_NAME_NORMALIZED`=LOWER(?) FOR UPDATE",
		name,
	).Scan(&characterID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCharacterPKNotFound
	}
	if err != nil {
		return fmt.Errorf("lock character PK target: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		"UPDATE `SRO_SHARD`.`CHAR` SET `DAILY_PK`=?,`TOTAL_PK`=?,`PK_PENALTY_POINT`=? WHERE `id`=?",
		state.DailyPK, state.TotalPK, state.PKPenaltyPoint, characterID,
	)
	if err != nil {
		return fmt.Errorf("persist character PK state: %w", err)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		if affectedErr != nil {
			return fmt.Errorf("read character PK edit result: %w", affectedErr)
		}
		return fmt.Errorf("character PK edit affected %d rows, want 1", affected)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit character PK edit: %w", err)
	}
	return nil
}

func validateCharacterPKName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 32 || strings.IndexByte(name, 0) >= 0 {
		return "", fmt.Errorf("invalid character PK target name")
	}
	return name, nil
}
