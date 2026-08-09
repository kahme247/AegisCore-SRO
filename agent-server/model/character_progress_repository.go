package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const UpdateCharacterProgress = "UPDATE `SRO_SHARD`.`CHAR` SET CURRENT_LEVEL=?, EXP=?, SKILL_EXP=?, SKILL_POINTS=?, STAT_POINTS=?, MAX_LEVEL=? WHERE ID=?"

var ErrCharacterProgressRepositoryUnavailable = errors.New("character progress repository is unavailable")

type CharacterProgressRepository interface {
	SaveProgress(context.Context, int, ProgressionResult) error
}

type SQLCharacterProgressRepository struct{ shard frameworkdb.Execer }

func NewCharacterProgressRepository(shard frameworkdb.Execer) (*SQLCharacterProgressRepository, error) {
	if shard == nil {
		return nil, ErrCharacterProgressRepositoryUnavailable
	}
	return &SQLCharacterProgressRepository{shard: shard}, nil
}

func (r *SQLCharacterProgressRepository) SaveProgress(ctx context.Context, characterID int, progress ProgressionResult) error {
	if r == nil || r.shard == nil {
		return ErrCharacterProgressRepositoryUnavailable
	}
	if characterID <= 0 || progress.Level <= 0 {
		return fmt.Errorf("invalid character progress identity")
	}
	result, err := r.shard.ExecContext(ctx, UpdateCharacterProgress,
		progress.Level, progress.Experience, progress.SkillExperience,
		progress.SkillPoints, progress.StatPoints, progress.Level, characterID)
	if err != nil {
		return fmt.Errorf("save character progress: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read saved character progress result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("save character progress affected %d rows, want 1", affected)
	}
	return nil
}
