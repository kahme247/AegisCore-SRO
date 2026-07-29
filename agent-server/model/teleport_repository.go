package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrTeleportRepositoryUnavailable = errors.New("teleport repository is unavailable")
	ErrTeleportLinkNotFound          = errors.New("teleport link does not exist")
	ErrTeleportCharacterNotFound     = errors.New("teleport character does not exist")
	ErrTeleportOwnerMismatch         = errors.New("teleport character owner mismatch")
	ErrTeleportInsufficientGold      = errors.New("insufficient gold for teleport")
	ErrTeleportOutcomeUnknown        = errors.New("teleport commit outcome is unknown")
)

type TeleportDestination struct {
	ID      uint32
	Region  int16
	X       float32
	Y       float32
	Z       float32
	WorldID int16
	Fee     uint64
}

type TeleportResult struct {
	Destination TeleportDestination
	Gold        uint64
}

type TeleportRepository interface {
	Quote(context.Context, uint32, uint32) (TeleportDestination, error)
	Commit(context.Context, int64, uint32, uint32, uint32) (TeleportResult, error)
}

type SQLTeleportRepository struct{ shard *sql.DB }

func NewTeleportRepository(shard *sql.DB) (*SQLTeleportRepository, error) {
	if shard == nil {
		return nil, ErrTeleportRepositoryUnavailable
	}
	return &SQLTeleportRepository{shard: shard}, nil
}

const selectTeleportLink = `SELECT target.ID,target.REGION_ID,target.POS_X,target.POS_Y,target.POS_Z,target.WORLD_ID,link.FEE
FROM TELEPORT_LINK link
JOIN TELEPORT_POINT owner ON owner.ID=link.OWNER_TELEPORT_ID AND owner.SERVICE=1
JOIN TELEPORT_POINT target ON target.ID=link.TARGET_TELEPORT_ID AND target.SERVICE=1
WHERE owner.NPC_REF_OBJ_ID=? AND target.ID=? AND link.SERVICE=1`

func (r *SQLTeleportRepository) Quote(ctx context.Context, sourceNPCRefID, targetTeleportID uint32) (TeleportDestination, error) {
	if r == nil || r.shard == nil {
		return TeleportDestination{}, ErrTeleportRepositoryUnavailable
	}
	return scanTeleportDestination(r.shard.QueryRowContext(normalizeTeleportContext(ctx), selectTeleportLink, sourceNPCRefID, targetTeleportID))
}

func (r *SQLTeleportRepository) Commit(ctx context.Context, characterID int64, userID, sourceNPCRefID, targetTeleportID uint32) (result TeleportResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrTeleportRepositoryUnavailable
	}
	ctx = normalizeTeleportContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var owner uint32
	var gold uint64
	if err = tx.QueryRowContext(ctx, "SELECT FK_USER,GOLD FROM `CHAR` WHERE ID=? FOR UPDATE", characterID).Scan(&owner, &gold); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrTeleportCharacterNotFound
		}
		return result, err
	}
	if owner != userID {
		return result, ErrTeleportOwnerMismatch
	}
	result.Destination, err = scanTeleportDestination(tx.QueryRowContext(ctx, selectTeleportLink, sourceNPCRefID, targetTeleportID))
	if err != nil {
		return result, err
	}
	if gold < result.Destination.Fee {
		return result, ErrTeleportInsufficientGold
	}
	result.Gold = gold - result.Destination.Fee
	update := "UPDATE `CHAR` SET GOLD=?,FK_REGION=?,POS_X=?,POS_Y=?,POS_Z=?,UTIME=CURRENT_TIMESTAMP WHERE ID=? AND FK_USER=?"
	write, execErr := tx.ExecContext(ctx, update, result.Gold, result.Destination.Region, result.Destination.X, result.Destination.Y, result.Destination.Z, characterID, userID)
	if execErr != nil {
		return TeleportResult{}, execErr
	}
	rows, rowsErr := write.RowsAffected()
	if rowsErr != nil {
		return TeleportResult{}, fmt.Errorf("read teleport update result: %w", rowsErr)
	}
	if rows != 1 {
		return TeleportResult{}, fmt.Errorf("teleport character update affected %d rows", rows)
	}
	if err = tx.Commit(); err != nil {
		return TeleportResult{}, errors.Join(ErrTeleportOutcomeUnknown, err)
	}
	committed = true
	return result, nil
}

type teleportRow interface{ Scan(...any) error }

func scanTeleportDestination(row teleportRow) (TeleportDestination, error) {
	var destination TeleportDestination
	err := row.Scan(&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID, &destination.Fee)
	if errors.Is(err, sql.ErrNoRows) {
		return TeleportDestination{}, ErrTeleportLinkNotFound
	}
	return destination, err
}

func normalizeTeleportContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
