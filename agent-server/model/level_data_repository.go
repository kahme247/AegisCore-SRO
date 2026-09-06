package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var ErrLevelDataRepositoryUnavailable = errors.New("level data repository is unavailable")

// LevelTableRepository loads the server-authoritative progression table from
// the long-lived shard database connection. Agent startup does not read the
// extracted Media reference tree.
type LevelTableRepository interface {
	Load(ctx context.Context) (*LevelTable, error)
}

type LevelTableRepositoryError struct {
	Operation string
	Err       error
}

func (e *LevelTableRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("level data repository %s: %v", e.Operation, e.Err)
}

func (e *LevelTableRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type SQLLevelTableRepository struct {
	shard frameworkdb.Queryer
}

var _ LevelTableRepository = (*SQLLevelTableRepository)(nil)

func NewLevelTableRepository(shard frameworkdb.Queryer) (*SQLLevelTableRepository, error) {
	if shard == nil {
		return nil, &LevelTableRepositoryError{Operation: "configure", Err: ErrLevelDataRepositoryUnavailable}
	}
	return &SQLLevelTableRepository{shard: shard}, nil
}

func (r *SQLLevelTableRepository) Load(ctx context.Context) (*LevelTable, error) {
	if r == nil || r.shard == nil {
		return nil, &LevelTableRepositoryError{Operation: "load", Err: ErrLevelDataRepositoryUnavailable}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, &LevelTableRepositoryError{Operation: "load", Err: err}
	}

	rows, err := r.shard.QueryContext(ctx, selectAllLevelData)
	if err != nil {
		return nil, &LevelTableRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	levels := make([]LevelDataRow, 0)
	for rows.Next() {
		var row LevelDataRow
		if err := rows.Scan(&row.Level, &row.RequiredExperience, &row.RequiredSkillPoints, &row.DropGoldMin, &row.DropGoldMax); err != nil {
			return nil, &LevelTableRepositoryError{Operation: "scan", Err: err}
		}
		levels = append(levels, row)
	}
	if err := rows.Err(); err != nil {
		return nil, &LevelTableRepositoryError{Operation: "iterate", Err: err}
	}
	table, err := newLevelTable(levels)
	if err != nil {
		return nil, &LevelTableRepositoryError{Operation: "validate", Err: err}
	}
	return table, nil
}

// DROP_GOLD_* is populated by the optional client-reference import. Existing
// progression rows legitimately remain NULL until that maintenance import is
// run, so expose zero to the runtime (which disables restoration pricing for
// that level) instead of making Agent startup fail.
const selectAllLevelData = "SELECT `LEVEL`,`REQUIRED_EXPERIENCE`,`REQUIRED_SKILL_POINTS`,COALESCE(`DROP_GOLD_MIN`,0),COALESCE(`DROP_GOLD_MAX`,0) FROM `SRO_SHARD`.`REF_LEVEL_DATA` ORDER BY `LEVEL`"
