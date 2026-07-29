package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

var ErrCharacterVitalsRepositoryUnavailable = errors.New("character vitals repository is unavailable")

type CharacterVitalsRepository interface {
	SaveVitals(context.Context, int, int, int) error
	Resurrect(context.Context, int, int, int) (bool, error)
	SpendMP(context.Context, int, int, int) (bool, error)
}

func (r *SQLCharacterVitalsRepository) SpendMP(ctx context.Context, characterID, expectedMP, cost int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || expectedMP < 0 || cost < 0 {
		return false, fmt.Errorf("invalid skill MP cost")
	}
	if cost == 0 {
		return true, nil
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `MP`=`MP`-? WHERE `ID`=? AND `MP`=? AND `MP`>=?", cost, characterID, expectedMP, cost)
	if err != nil {
		return false, fmt.Errorf("spend skill MP: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read skill MP result: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("spend skill MP affected %d rows, want at most 1", affected)
	}
	return affected == 1, nil
}

type SQLCharacterVitalsRepository struct{ shard frameworkdb.Execer }

func NewCharacterVitalsRepository(shard frameworkdb.Execer) (*SQLCharacterVitalsRepository, error) {
	if shard == nil {
		return nil, ErrCharacterVitalsRepositoryUnavailable
	}
	return &SQLCharacterVitalsRepository{shard: shard}, nil
}

func (r *SQLCharacterVitalsRepository) SaveVitals(ctx context.Context, characterID, hp, mp int) error {
	if r == nil || r.shard == nil {
		return ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || hp < 0 || mp < 0 {
		return fmt.Errorf("invalid character vitals")
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=?, `MP`=? WHERE `ID`=?", hp, mp, characterID)
	if err != nil {
		return fmt.Errorf("save character vitals: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read character vitals result: %w", err)
	}
	if affected > 1 {
		return fmt.Errorf("save character vitals affected %d rows, want at most 1", affected)
	}
	return nil
}

func (r *SQLCharacterVitalsRepository) Resurrect(ctx context.Context, characterID, hp, mp int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || hp <= 0 || mp < 0 {
		return false, fmt.Errorf("invalid resurrection vitals")
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=?, `MP`=? WHERE `ID`=? AND `HP`=0", hp, mp, characterID)
	if err != nil {
		return false, fmt.Errorf("resurrect character: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read resurrection result: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("resurrection affected %d rows, want at most 1", affected)
	}
	return affected == 1, nil
}
