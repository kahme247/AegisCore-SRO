package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrOperatorRepositoryUnavailable = errors.New("operator repository is unavailable")
	ErrOperatorAccessDenied          = errors.New("operator access denied")
	ErrOperatorTargetNotFound        = errors.New("operator target not found")
)

type OperatorPunishmentResult struct {
	TargetCharacterID int
	TargetAccountID   int
	Active            bool
	ChangedAt         time.Time
}

type OperatorPunishmentRepository interface {
	ToggleOperatorPunishment(context.Context, int, int, time.Time) (OperatorPunishmentResult, error)
}

type SQLOperatorPunishmentRepository struct{ shard *sql.DB }

func NewOperatorPunishmentRepository(shard *sql.DB) (*SQLOperatorPunishmentRepository, error) {
	if shard == nil {
		return nil, ErrOperatorRepositoryUnavailable
	}
	return &SQLOperatorPunishmentRepository{shard: shard}, nil
}

func (r *SQLOperatorPunishmentRepository) ToggleOperatorPunishment(ctx context.Context, actorCharacterID, targetCharacterID int, at time.Time) (result OperatorPunishmentResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrOperatorRepositoryUnavailable
	}
	if actorCharacterID <= 0 || targetCharacterID <= 0 || actorCharacterID == targetCharacterID || at.IsZero() {
		return result, ErrSocialInvalidRequest
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin operator punishment: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var actorAccountID int
	var isGM int
	if err = tx.QueryRowContext(ctx, "SELECT c.FK_USER,u.IS_GM FROM `CHAR` c JOIN `SRO_ACCOUNT`.`USER` u ON u.id=c.FK_USER WHERE c.id=? FOR UPDATE", actorCharacterID).Scan(&actorAccountID, &isGM); err != nil || isGM == 0 {
		return result, ErrOperatorAccessDenied
	}
	var oldStatus int
	if err = tx.QueryRowContext(ctx, "SELECT c.FK_USER,COALESCE(u.STATUS,0) FROM `CHAR` c JOIN `SRO_ACCOUNT`.`USER` u ON u.id=c.FK_USER WHERE c.id=? FOR UPDATE", targetCharacterID).Scan(&result.TargetAccountID, &oldStatus); err != nil {
		return result, ErrOperatorTargetNotFound
	}
	newStatus := 0
	if oldStatus != 1 {
		newStatus = 1
	}
	updated, execErr := tx.ExecContext(ctx, "UPDATE `SRO_ACCOUNT`.`USER` SET STATUS=?,UTIME=? WHERE id=? AND STATUS<=>?", newStatus, at.UTC(), result.TargetAccountID, oldStatus)
	if execErr != nil {
		return result, fmt.Errorf("update operator punishment: %w", execErr)
	}
	if affected, affectedErr := updated.RowsAffected(); affectedErr != nil || affected != 1 {
		return result, ErrSocialConflict
	}
	if _, execErr = tx.ExecContext(ctx, "INSERT INTO OPERATOR_PUNISHMENT_AUDIT (ACTOR_CHAR,ACTOR_ACCOUNT,TARGET_CHAR,TARGET_ACCOUNT,PREVIOUS_STATUS,NEW_STATUS,CREATED_AT) VALUES (?,?,?,?,?,?,?)", actorCharacterID, actorAccountID, targetCharacterID, result.TargetAccountID, oldStatus, newStatus, at.UTC()); execErr != nil {
		return result, fmt.Errorf("audit operator punishment: %w", execErr)
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit operator punishment: %w", err)
	}
	result.TargetCharacterID = targetCharacterID
	result.Active = newStatus != 1
	result.ChangedAt = at.UTC()
	return result, nil
}

var _ OperatorPunishmentRepository = (*SQLOperatorPunishmentRepository)(nil)
