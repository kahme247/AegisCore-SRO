package database

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
	log "github.com/sirupsen/logrus"
)

type DropRatesGold struct {
	MobLvl int
	Prob   float64
	Min    int
	Max    int
}

const (
	select_drop_rates_gold string = "SELECT MOB_LVL, PROB, MIN, MAX FROM DROP_RATES_GOLD"
)

var ErrGoldDropRateRepositoryUnavailable = errors.New("gold drop rate repository is unavailable")

// GoldDropRateRepositoryError identifies the failed repository operation
// without forcing callers to parse a database-driver error string.
type GoldDropRateRepositoryError struct {
	Operation string
	Err       error
}

func (e *GoldDropRateRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("gold drop rate repository %s: %v", e.Operation, e.Err)
}

func (e *GoldDropRateRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// GoldDropRateRepository receives a long-lived pool or transaction from its
// caller. It never opens or closes a database connection itself.
type GoldDropRateRepository struct {
	queryer frameworkdb.Queryer
}

func NewGoldDropRateRepository(queryer frameworkdb.Queryer) (*GoldDropRateRepository, error) {
	if queryer == nil {
		return nil, &GoldDropRateRepositoryError{
			Operation: "configure",
			Err:       ErrGoldDropRateRepositoryUnavailable,
		}
	}
	return &GoldDropRateRepository{queryer: queryer}, nil
}

// List returns every configured gold drop rate. It handles query, row scan,
// and deferred row errors explicitly so malformed or unavailable reference
// data cannot turn into a process-wide panic.
func (r *GoldDropRateRepository) List(ctx context.Context) (map[int]DropRatesGold, error) {
	if r == nil || r.queryer == nil {
		return nil, &GoldDropRateRepositoryError{
			Operation: "query",
			Err:       ErrGoldDropRateRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.queryer.QueryContext(ctx, select_drop_rates_gold)
	if err != nil {
		return nil, &GoldDropRateRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	rates := make(map[int]DropRatesGold)
	for rows.Next() {
		rate := DropRatesGold{}
		if err := rows.Scan(&rate.MobLvl, &rate.Prob, &rate.Min, &rate.Max); err != nil {
			return nil, &GoldDropRateRepositoryError{Operation: "scan", Err: err}
		}
		rates[rate.MobLvl] = rate
	}
	if err := rows.Err(); err != nil {
		return nil, &GoldDropRateRepositoryError{Operation: "iterate", Err: err}
	}

	return rates, nil
}

// GetGoldDropRates is retained for compatibility with older callers. New
// code should construct a GoldDropRateRepository with an injected long-lived
// pool and handle List's error. The legacy wrapper is intentionally non-fatal:
// it logs the failure and returns an empty result rather than panicking.
func GetGoldDropRates() map[int]DropRatesGold {
	conn := frameworkdb.OpenConnShard()
	defer conn.Close()

	repository, err := NewGoldDropRateRepository(conn)
	if err != nil {
		log.WithError(err).Error("configure gold drop rate repository")
		return map[int]DropRatesGold{}
	}

	rates, err := repository.List(context.Background())
	if err != nil {
		log.WithError(err).Error("load gold drop rates")
		return map[int]DropRatesGold{}
	}
	return rates
}
