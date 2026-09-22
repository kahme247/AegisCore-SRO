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
	ErrTeleportLevelRequired         = errors.New("character level is too low for teleport")
	ErrTeleportInsufficientGold      = errors.New("insufficient gold for teleport")
	ErrTeleportOutcomeUnknown        = errors.New("teleport commit outcome is unknown")
	ErrTeleportPointNotFound         = errors.New("teleport point does not exist")
)

type TeleportDestination struct {
	ID       uint32
	Region   int16
	X        float32
	Y        float32
	Z        float32
	WorldID  int16
	Fee      uint64
	MinLevel uint8
}

type DungeonTeleportPoint struct {
	ID       uint32
	CodeName string
	Region   int16
	X        float32
	Y        float32
	Z        float32
	Radius   float32
	WorldID  int16
}

type TeleportResult struct {
	Destination TeleportDestination
	Gold        uint64
}

type TeleportRepository interface {
	Quote(context.Context, uint32, uint32) (TeleportDestination, error)
	Commit(context.Context, int64, uint32, uint32, uint32) (TeleportResult, error)
}

// DungeonTeleportLinkRepository loads the directed event-zone graph used by
// database-backed dungeon transitions.
type DungeonTeleportLinkRepository interface {
	DungeonLinks(context.Context) (map[uint32]uint32, error)
}

type DungeonTeleportPointRepository interface {
	DungeonPoints(context.Context) (map[uint32]DungeonTeleportPoint, error)
}

// TeleportDesignationRepository is implemented separately so focused tests
// and alternate catalog providers can support normal travel without also
// pretending to persist a bind-point change.
type TeleportDesignationRepository interface {
	Designate(context.Context, int64, uint32, uint32) (uint32, error)
}

type SQLTeleportRepository struct{ shard *sql.DB }

func NewTeleportRepository(shard *sql.DB) (*SQLTeleportRepository, error) {
	if shard == nil {
		return nil, ErrTeleportRepositoryUnavailable
	}
	return &SQLTeleportRepository{shard: shard}, nil
}

const selectTeleportLink = `SELECT target.ID,target.REGION_ID,target.POS_X,target.POS_Y,target.POS_Z,target.WORLD_ID,link.FEE,link.MIN_LEVEL
FROM TELEPORT_LINK link
JOIN TELEPORT_POINT owner ON owner.ID=link.OWNER_TELEPORT_ID AND owner.SERVICE=1
JOIN TELEPORT_POINT target ON target.ID=link.TARGET_TELEPORT_ID AND target.SERVICE=1
WHERE owner.NPC_REF_OBJ_ID=? AND target.ID=? AND link.SERVICE=1`

const SelectDungeonTeleportLinks = `SELECT SOURCE_TELEPORT_ID,TARGET_TELEPORT_ID
FROM DUNGEON_TELEPORT_LINK
WHERE SERVICE=1
ORDER BY SOURCE_TELEPORT_ID`

const SelectDungeonTeleportPoints = `SELECT ID,CODE_NAME,REGION_ID,POS_X,POS_Y,POS_Z,RADIUS,WORLD_ID
FROM DUNGEON_TELEPORT_POINT
WHERE SERVICE=1
ORDER BY ID`

func (r *SQLTeleportRepository) Quote(ctx context.Context, sourceNPCRefID, targetTeleportID uint32) (TeleportDestination, error) {
	if r == nil || r.shard == nil {
		return TeleportDestination{}, ErrTeleportRepositoryUnavailable
	}
	return scanTeleportDestination(r.shard.QueryRowContext(normalizeTeleportContext(ctx), selectTeleportLink, sourceNPCRefID, targetTeleportID))
}

func (r *SQLTeleportRepository) DungeonLinks(ctx context.Context) (map[uint32]uint32, error) {
	if r == nil || r.shard == nil {
		return nil, ErrTeleportRepositoryUnavailable
	}
	rows, err := r.shard.QueryContext(normalizeTeleportContext(ctx), SelectDungeonTeleportLinks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make(map[uint32]uint32)
	for rows.Next() {
		var sourceID, targetID uint32
		if err := rows.Scan(&sourceID, &targetID); err != nil {
			return nil, err
		}
		links[sourceID] = targetID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

func (r *SQLTeleportRepository) DungeonPoints(ctx context.Context) (map[uint32]DungeonTeleportPoint, error) {
	if r == nil || r.shard == nil {
		return nil, ErrTeleportRepositoryUnavailable
	}
	rows, err := r.shard.QueryContext(normalizeTeleportContext(ctx), SelectDungeonTeleportPoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make(map[uint32]DungeonTeleportPoint)
	for rows.Next() {
		var point DungeonTeleportPoint
		if err := rows.Scan(&point.ID, &point.CodeName, &point.Region, &point.X, &point.Y, &point.Z, &point.Radius, &point.WorldID); err != nil {
			return nil, err
		}
		points[point.ID] = point
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return points, nil
}

// Designate persists the selected teleport building as the character's bind
// city. The caller has already authenticated the selected world object; the
// transaction repeats the owner and catalog checks so a stale or forged
// request cannot change another character's return point.
func (r *SQLTeleportRepository) Designate(ctx context.Context, characterID int64, userID, sourceNPCRefID uint32) (uint32, error) {
	if r == nil || r.shard == nil {
		return 0, ErrTeleportRepositoryUnavailable
	}
	if characterID <= 0 || userID == 0 || sourceNPCRefID == 0 {
		return 0, ErrTeleportCharacterNotFound
	}
	ctx = normalizeTeleportContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT FK_USER FROM `CHAR` WHERE ID=? FOR UPDATE", characterID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrTeleportCharacterNotFound
		}
		return 0, err
	}
	if owner != userID {
		return 0, ErrTeleportOwnerMismatch
	}
	var teleportID uint32
	if err = tx.QueryRowContext(ctx, "SELECT ID FROM TELEPORT_POINT WHERE NPC_REF_OBJ_ID=? AND SERVICE=1 LIMIT 1", sourceNPCRefID).Scan(&teleportID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrTeleportPointNotFound
		}
		return 0, err
	}
	write, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET RETURN_TELEPORT_ID=?,UTIME=CURRENT_TIMESTAMP WHERE ID=? AND FK_USER=?", teleportID, characterID, userID)
	if err != nil {
		return 0, err
	}
	rows, err := write.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, fmt.Errorf("teleport designation affected %d rows", rows)
	}
	if err = tx.Commit(); err != nil {
		return 0, errors.Join(ErrTeleportOutcomeUnknown, err)
	}
	committed = true
	return teleportID, nil
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
	var level uint8
	if err = tx.QueryRowContext(ctx, "SELECT FK_USER,GOLD,CURRENT_LEVEL FROM `CHAR` WHERE ID=? FOR UPDATE", characterID).Scan(&owner, &gold, &level); err != nil {
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
	if level < result.Destination.MinLevel {
		return result, ErrTeleportLevelRequired
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
	err := row.Scan(&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID, &destination.Fee, &destination.MinLevel)
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
