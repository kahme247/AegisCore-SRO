package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

const SelectMovementSettings = "SELECT `PLAYER_WALK_SPEED`, `PLAYER_RUN_SPEED` FROM `SRO_SHARD`.`GAMEPLAY_MOVEMENT_SETTINGS` WHERE `SETTING_ID`=1"

var (
	ErrMovementSettingsRepositoryUnavailable = errors.New("movement settings repository is unavailable")
	ErrMovementSettingsInvalid               = errors.New("movement settings are invalid")
)

// MovementSettings contains the server-authoritative player movement speeds.
// The values are serialized to the client during character bootstrap and are
// also retained on Player for server-side movement validation.
type MovementSettings struct {
	PlayerWalkSpeed float32
	PlayerRunSpeed  float32
}

// MovementSettingsRepository is the consumer-owned persistence boundary for
// the singleton shard movement configuration.
type MovementSettingsRepository interface {
	MovementSettings(ctx context.Context) (MovementSettings, error)
}

type MovementSettingsRepositoryError struct {
	Operation string
	Err       error
}

func (e *MovementSettingsRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("movement settings repository %s: %v", e.Operation, e.Err)
}

func (e *MovementSettingsRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SQLMovementSettingsRepository reads settings through the Agent-owned shard
// pool. It does not own or close the supplied queryer.
type SQLMovementSettingsRepository struct {
	shard frameworkdb.Queryer
}

var _ MovementSettingsRepository = (*SQLMovementSettingsRepository)(nil)

func NewMovementSettingsRepository(shard frameworkdb.Queryer) (*SQLMovementSettingsRepository, error) {
	if shard == nil {
		return nil, &MovementSettingsRepositoryError{
			Operation: "configure",
			Err:       ErrMovementSettingsRepositoryUnavailable,
		}
	}
	return &SQLMovementSettingsRepository{shard: shard}, nil
}

func (r *SQLMovementSettingsRepository) MovementSettings(ctx context.Context) (MovementSettings, error) {
	if r == nil || r.shard == nil {
		return MovementSettings{}, &MovementSettingsRepositoryError{
			Operation: "query",
			Err:       ErrMovementSettingsRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "query", Err: err}
	}

	rows, err := r.shard.QueryContext(ctx, SelectMovementSettings)
	if err != nil {
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "iterate", Err: err}
		}
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "scan", Err: sql.ErrNoRows}
	}

	var settings MovementSettings
	if err := rows.Scan(&settings.PlayerWalkSpeed, &settings.PlayerRunSpeed); err != nil {
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "scan", Err: err}
	}
	if rows.Next() {
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "validate", Err: ErrMovementSettingsInvalid}
	}
	if err := rows.Err(); err != nil {
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "iterate", Err: err}
	}
	if !validMovementSpeed(settings.PlayerWalkSpeed) || !validMovementSpeed(settings.PlayerRunSpeed) {
		return MovementSettings{}, &MovementSettingsRepositoryError{Operation: "validate", Err: ErrMovementSettingsInvalid}
	}
	return settings, nil
}

func validMovementSpeed(speed float32) bool {
	return speed > 0 && !math.IsNaN(float64(speed)) && !math.IsInf(float64(speed), 0)
}
