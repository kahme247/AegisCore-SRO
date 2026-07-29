package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

type CharacterAttribute byte

const (
	CharacterStrength CharacterAttribute = iota + 1
	CharacterIntellect
)

const spendCharacterStatPoint = "UPDATE `SRO_SHARD`.`CHAR` SET `STRENGTH`=`STRENGTH`+?, `INTELLECT`=`INTELLECT`+?, `STAT_POINTS`=`STAT_POINTS`-1 WHERE `ID`=? AND `STAT_POINTS`>0"

var ErrCharacterAttributeRepositoryUnavailable = errors.New("character attribute repository is unavailable")

type CharacterAttributeRepository interface {
	SpendStatPoint(context.Context, int, CharacterAttribute) (bool, error)
}

type SQLCharacterAttributeRepository struct{ shard frameworkdb.Execer }

func NewCharacterAttributeRepository(shard frameworkdb.Execer) (*SQLCharacterAttributeRepository, error) {
	if shard == nil {
		return nil, ErrCharacterAttributeRepositoryUnavailable
	}
	return &SQLCharacterAttributeRepository{shard: shard}, nil
}

func (r *SQLCharacterAttributeRepository) SpendStatPoint(ctx context.Context, characterID int, attribute CharacterAttribute) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterAttributeRepositoryUnavailable
	}
	if characterID <= 0 {
		return false, fmt.Errorf("invalid character attribute identity")
	}
	strength, intellect := 0, 0
	switch attribute {
	case CharacterStrength:
		strength = 1
	case CharacterIntellect:
		intellect = 1
	default:
		return false, fmt.Errorf("invalid character attribute %d", attribute)
	}
	result, err := r.shard.ExecContext(ctx, spendCharacterStatPoint, strength, intellect, characterID)
	if err != nil {
		return false, fmt.Errorf("spend character stat point: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read character stat allocation result: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("character stat allocation affected %d rows, want at most 1", affected)
	}
	return affected == 1, nil
}
