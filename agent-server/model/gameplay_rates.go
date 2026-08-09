package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const SelectGameplayRates = "SELECT p.`EXPERIENCE_RATE_PERCENT`, p.`SKILL_EXPERIENCE_RATE_PERCENT`, m.`MOB_COUNT_RATE_PERCENT`, m.`MOB_RESPAWN_RATE_PERCENT` FROM `SRO_SHARD`.`GAMEPLAY_PROGRESSION_RATES` p JOIN `SRO_SHARD`.`GAMEPLAY_MOB_RATES` m ON m.`SETTING_ID`=p.`SETTING_ID` WHERE p.`SETTING_ID`=1"

var ErrGameplayRateRepositoryUnavailable = errors.New("gameplay rate repository is unavailable")
var ErrGameplayMobRatesInvalid = errors.New("gameplay mob rates must be positive")

type GameplayRates struct {
	ExperienceRatePercent      uint32
	SkillExperienceRatePercent uint32
	MobCountRatePercent        uint32
	MobRespawnRatePercent      uint32
}

type GameplayRateRepository interface {
	GameplayRates(context.Context) (GameplayRates, error)
}

type SQLGameplayRateRepository struct {
	shard frameworkdb.RowQueryer
}

func NewGameplayRateRepository(shard frameworkdb.RowQueryer) (*SQLGameplayRateRepository, error) {
	if shard == nil {
		return nil, ErrGameplayRateRepositoryUnavailable
	}
	return &SQLGameplayRateRepository{shard: shard}, nil
}

func (r *SQLGameplayRateRepository) GameplayRates(ctx context.Context) (GameplayRates, error) {
	if r == nil || r.shard == nil {
		return GameplayRates{}, ErrGameplayRateRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var rates GameplayRates
	if err := r.shard.QueryRowContext(ctx, SelectGameplayRates).Scan(
		&rates.ExperienceRatePercent,
		&rates.SkillExperienceRatePercent,
		&rates.MobCountRatePercent,
		&rates.MobRespawnRatePercent,
	); err != nil {
		return GameplayRates{}, fmt.Errorf("load gameplay progression rates: %w", err)
	}
	if rates.MobCountRatePercent == 0 || rates.MobRespawnRatePercent == 0 {
		return GameplayRates{}, ErrGameplayMobRatesInvalid
	}
	return rates, nil
}
