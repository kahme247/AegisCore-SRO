package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrMasteryRepositoryUnavailable = errors.New("mastery repository is unavailable")
	ErrMasteryInvalid               = errors.New("mastery is invalid for this character")
	ErrMasteryLevelLimit            = errors.New("mastery cannot exceed character level")
	ErrMasteryTotalLimit            = errors.New("total mastery limit reached")
	ErrMasteryInsufficientSP        = errors.New("insufficient skill points")
	ErrMasteryConflict              = errors.New("mastery state conflicts with the request")
	ErrMasteryOutcomeUnknown        = errors.New("mastery transaction outcome is unknown")
)

type MasteryProgress struct {
	ID    uint32
	Level byte
}

type MasteryIncreaseResult struct {
	Mastery     MasteryProgress
	SkillPoints uint32
}

type MasteryRepository interface {
	LoadMasteries(context.Context, int) (map[uint32]byte, error)
	IncreaseMastery(context.Context, int, uint32, byte, byte, int, uint64) (MasteryIncreaseResult, error)
}

type SQLMasteryRepository struct{ shard *sql.DB }

func NewMasteryRepository(shard *sql.DB) (*SQLMasteryRepository, error) {
	if shard == nil {
		return nil, ErrMasteryRepositoryUnavailable
	}
	return &SQLMasteryRepository{shard: shard}, nil
}

func (r *SQLMasteryRepository) LoadMasteries(ctx context.Context, characterID int) (map[uint32]byte, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMasteryRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, ErrMasteryConflict
	}
	rows, err := r.shard.QueryContext(ctx,
		"SELECT m.`MASTERY_ID`,m.`LEVEL` FROM `SRO_SHARD`.`CHAR_MASTERY` m JOIN `SRO_SHARD`.`CHAR` c ON c.`ID`=m.`CHAR_ID` JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` JOIN `SRO_SHARD`.`REF_CHAR_DEFAULT_MASTERY` defaults ON defaults.`MASTERY_ID`=m.`MASTERY_ID` AND defaults.`RACE` IN (r.`Country`,3) WHERE m.`CHAR_ID`=? ORDER BY m.`MASTERY_ID`", characterID)
	if err != nil {
		return nil, fmt.Errorf("load character masteries: %w", err)
	}
	defer rows.Close()
	masteries := make(map[uint32]byte)
	for rows.Next() {
		var id uint32
		var level byte
		if err := rows.Scan(&id, &level); err != nil {
			return nil, fmt.Errorf("scan character mastery: %w", err)
		}
		masteries[id] = level
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read character masteries: %w", err)
	}
	return masteries, nil
}

// IncreaseMastery commits the SP debit and mastery level as one serializable
// transaction. expectedLevel protects the in-memory player snapshot from a
// replay or a second Agent process operating on stale state.
func (r *SQLMasteryRepository) IncreaseMastery(ctx context.Context, characterID int, masteryID uint32, expectedLevel, nextLevel byte, totalLimit int, cost uint64) (result MasteryIncreaseResult, resultErr error) {
	if r == nil || r.shard == nil {
		return result, ErrMasteryRepositoryUnavailable
	}
	if characterID <= 0 || masteryID == 0 || nextLevel == 0 || nextLevel != expectedLevel+1 || totalLimit <= 0 {
		return result, ErrMasteryConflict
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin mastery transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, rollbackErr)
		}
	}()

	var characterLevel, country int
	var skillPoints uint64
	if err := tx.QueryRowContext(ctx,
		"SELECT c.`CURRENT_LEVEL`,c.`SKILL_POINTS`,r.`Country` FROM `SRO_SHARD`.`CHAR` c JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` WHERE c.`ID`=? FOR UPDATE", characterID,
	).Scan(&characterLevel, &skillPoints, &country); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrMasteryConflict
		}
		return result, fmt.Errorf("lock mastery character: %w", err)
	}
	if int(nextLevel) > characterLevel {
		return result, ErrMasteryLevelLimit
	}
	var allowed int
	if err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM `SRO_SHARD`.`REF_CHAR_DEFAULT_MASTERY` WHERE `MASTERY_ID`=? AND `RACE` IN (?,3) LIMIT 1", masteryID, country,
	).Scan(&allowed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrMasteryInvalid
		}
		return result, fmt.Errorf("validate mastery reference: %w", err)
	}

	rows, err := tx.QueryContext(ctx,
		"SELECT `MASTERY_ID`,`LEVEL` FROM `SRO_SHARD`.`CHAR_MASTERY` WHERE `CHAR_ID`=? ORDER BY `MASTERY_ID` FOR UPDATE", characterID)
	if err != nil {
		return result, fmt.Errorf("lock character masteries: %w", err)
	}
	currentLevel := byte(0)
	total := 0
	for rows.Next() {
		var id uint32
		var level byte
		if err := rows.Scan(&id, &level); err != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan locked mastery: %w", err)
		}
		total += int(level)
		if id == masteryID {
			currentLevel = level
		}
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return result, fmt.Errorf("read locked masteries: %w", rowsErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("close locked masteries: %w", closeErr)
	}
	if currentLevel != expectedLevel {
		return result, ErrMasteryConflict
	}
	if total+1 > totalLimit {
		return result, ErrMasteryTotalLimit
	}
	if skillPoints < cost {
		return result, ErrMasteryInsufficientSP
	}
	skillPoints -= cost
	if _, err := tx.ExecContext(ctx,
		"UPDATE `SRO_SHARD`.`CHAR` SET `SKILL_POINTS`=? WHERE `ID`=?", skillPoints, characterID,
	); err != nil {
		return result, fmt.Errorf("debit mastery skill points: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO `SRO_SHARD`.`CHAR_MASTERY` (`CHAR_ID`,`MASTERY_ID`,`LEVEL`) VALUES (?,?,?) ON DUPLICATE KEY UPDATE `LEVEL`=VALUES(`LEVEL`)",
		characterID, masteryID, nextLevel,
	); err != nil {
		return result, fmt.Errorf("save character mastery: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, errors.Join(ErrMasteryOutcomeUnknown, err)
	}
	committed = true
	if skillPoints > uint64(^uint32(0)) {
		return result, ErrMasteryOutcomeUnknown
	}
	return MasteryIncreaseResult{
		Mastery:     MasteryProgress{ID: masteryID, Level: nextLevel},
		SkillPoints: uint32(skillPoints),
	}, nil
}
