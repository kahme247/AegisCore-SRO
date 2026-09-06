package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const AutoPotionSettingsSize = 7

var (
	ErrAutoPotionRepositoryUnavailable = errors.New("auto-potion settings repository is unavailable")
	ErrAutoPotionCharacterIDRequired   = errors.New("auto-potion settings character ID is required")
)

type AutoPotionSettings [AutoPotionSettingsSize]byte

type AutoPotionSettingsRepository interface {
	LoadAutoPotionSettings(context.Context, int) (AutoPotionSettings, bool, error)
	SaveAutoPotionSettings(context.Context, int, AutoPotionSettings) error
}

type autoPotionDatabase interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type SQLAutoPotionSettingsRepository struct{ shard autoPotionDatabase }

func NewAutoPotionSettingsRepository(shard autoPotionDatabase) (*SQLAutoPotionSettingsRepository, error) {
	if shard == nil {
		return nil, ErrAutoPotionRepositoryUnavailable
	}
	return &SQLAutoPotionSettingsRepository{shard: shard}, nil
}

func (r *SQLAutoPotionSettingsRepository) LoadAutoPotionSettings(ctx context.Context, characterID int) (AutoPotionSettings, bool, error) {
	var settings AutoPotionSettings
	if r == nil || r.shard == nil {
		return settings, false, ErrAutoPotionRepositoryUnavailable
	}
	if characterID <= 0 {
		return settings, false, fmt.Errorf("%w: %d", ErrAutoPotionCharacterIDRequired, characterID)
	}
	var payload []byte
	err := r.shard.QueryRowContext(ctx, "SELECT `AUTO_POTION_CONFIG` FROM `SRO_SHARD`.`CHAR` WHERE `id`=?", characterID).Scan(&payload)
	if err != nil {
		return settings, false, fmt.Errorf("load auto-potion settings: %w", err)
	}
	if len(payload) == 0 {
		return settings, false, nil
	}
	if len(payload) != AutoPotionSettingsSize {
		return settings, false, fmt.Errorf("load auto-potion settings: payload length %d, want %d", len(payload), AutoPotionSettingsSize)
	}
	copy(settings[:], payload)
	return settings, true, nil
}

func (r *SQLAutoPotionSettingsRepository) SaveAutoPotionSettings(ctx context.Context, characterID int, settings AutoPotionSettings) error {
	if r == nil || r.shard == nil {
		return ErrAutoPotionRepositoryUnavailable
	}
	if characterID <= 0 {
		return fmt.Errorf("%w: %d", ErrAutoPotionCharacterIDRequired, characterID)
	}
	if _, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `AUTO_POTION_CONFIG`=? WHERE `id`=?", settings[:], characterID); err != nil {
		return fmt.Errorf("save auto-potion settings: %w", err)
	}
	return nil
}
