package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type PremiumDecorationRepository interface {
	LoadPremiumDecoration(context.Context, int) (PremiumDecorationState, error)
	SavePremiumDecoration(context.Context, int, PremiumDecorationState) error
}

type SQLPremiumDecorationRepository struct{ shard *sql.DB }

func NewPremiumDecorationRepository(shard *sql.DB) (PremiumDecorationRepository, error) {
	if shard == nil {
		return nil, errors.New("premium decoration database is required")
	}
	return &SQLPremiumDecorationRepository{shard: shard}, nil
}

func (r *SQLPremiumDecorationRepository) LoadPremiumDecoration(ctx context.Context, characterID int) (state PremiumDecorationState, err error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return state, errors.New("invalid premium decoration character")
	}
	err = r.shard.QueryRowContext(ctx, "SELECT `TOKEN`,`REFERENCE`,`VALUE` FROM `CHAR_PREMIUM_DECORATION` WHERE `CHAR_ID`=?", characterID).Scan(&state.Token, &state.Reference, &state.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return PremiumDecorationState{}, nil
	}
	if err != nil {
		return PremiumDecorationState{}, fmt.Errorf("load premium decoration: %w", err)
	}
	return state, nil
}

func (r *SQLPremiumDecorationRepository) SavePremiumDecoration(ctx context.Context, characterID int, state PremiumDecorationState) error {
	if r == nil || r.shard == nil || characterID <= 0 || state.Token == 0 || state.Reference == 0 || state.Value == 0 {
		return errors.New("invalid premium decoration state")
	}
	_, err := r.shard.ExecContext(ctx, "INSERT INTO `CHAR_PREMIUM_DECORATION` (`CHAR_ID`,`TOKEN`,`REFERENCE`,`VALUE`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `TOKEN`=VALUES(`TOKEN`),`REFERENCE`=VALUES(`REFERENCE`),`VALUE`=VALUES(`VALUE`),`UPDATED_AT`=CURRENT_TIMESTAMP(6)", characterID, state.Token, state.Reference, state.Value)
	if err != nil {
		return fmt.Errorf("save premium decoration: %w", err)
	}
	return nil
}
