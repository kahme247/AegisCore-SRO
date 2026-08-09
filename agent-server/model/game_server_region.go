package model

import (
	"context"
	"errors"
	"fmt"
	"sort"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

type GameServerRegion struct {
	ContinentName string
	Regions       []int16
}

func (gsr *GameServerRegion) HasRegion(regionId int16) bool {
	for _, region := range gsr.Regions {
		if regionId == region {
			return true
		}
	}
	return false
}

const (
	SelectGameserverRegions string = "SELECT gr.Continent_Name, r.wRegionID FROM `SRO_SHARD`.`GAME_SERVER_REGION` gr INNER JOIN `SRO_SHARD`.`REGION_REFERENCE` AS r ON r.ContinentName = gr.Continent_Name WHERE gr.Game_Server_ID=? ORDER BY gr.Continent_Name ASC, r.wRegionID ASC"
)

// ErrGameServerRegionRepositoryUnavailable identifies a missing injected
// shard queryer. New startup paths must fail explicitly instead of opening a
// request-scoped database connection.
var ErrGameServerRegionRepositoryUnavailable = errors.New("game-server region repository is unavailable")

// GameServerRegionRepositoryError identifies the repository operation that
// failed while preserving the driver, context, or scan failure for callers.
type GameServerRegionRepositoryError struct {
	Operation string
	Err       error
}

func (e *GameServerRegionRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("game-server region repository %s: %v", e.Operation, e.Err)
}

func (e *GameServerRegionRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// GameServerRegionRepository is the context-aware persistence boundary for
// the game-server continent and region catalog. Implementations do not own or
// close the supplied queryer.
type GameServerRegionRepository interface {
	GetRegionsForGameServer(ctx context.Context, gameServerID int) ([]GameServerRegion, error)
}

// SQLGameServerRegionRepository queries the catalog through an injected
// long-lived pool or a transaction owned by a higher-level unit of work.
type SQLGameServerRegionRepository struct {
	shard frameworkdb.Queryer
}

var _ GameServerRegionRepository = (*SQLGameServerRegionRepository)(nil)

// NewGameServerRegionRepository constructs the production catalog repository
// from an injected shard pool or transaction.
func NewGameServerRegionRepository(shard frameworkdb.Queryer) (*SQLGameServerRegionRepository, error) {
	if shard == nil {
		return nil, &GameServerRegionRepositoryError{
			Operation: "configure",
			Err:       ErrGameServerRegionRepositoryUnavailable,
		}
	}
	return &SQLGameServerRegionRepository{shard: shard}, nil
}

// GetRegionsForGameServer returns a deterministic continent catalog. Region
// IDs and continents are both sorted after scanning so callers are never
// coupled to database row order or Go map iteration order.
func (r *SQLGameServerRegionRepository) GetRegionsForGameServer(ctx context.Context, gameServerID int) ([]GameServerRegion, error) {
	if r == nil || r.shard == nil {
		return nil, &GameServerRegionRepositoryError{
			Operation: "query",
			Err:       ErrGameServerRegionRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, &GameServerRegionRepositoryError{Operation: "query", Err: err}
	}

	rows, err := r.shard.QueryContext(ctx, SelectGameserverRegions, gameServerID)
	if err != nil {
		return nil, &GameServerRegionRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	regionsByContinent := make(map[string][]int16)
	for rows.Next() {
		var continent string
		var regionID int16
		if err := rows.Scan(&continent, &regionID); err != nil {
			return nil, &GameServerRegionRepositoryError{Operation: "scan", Err: err}
		}

		regionsByContinent[continent] = append(regionsByContinent[continent], regionID)
	}
	if err := rows.Err(); err != nil {
		return nil, &GameServerRegionRepositoryError{Operation: "iterate", Err: err}
	}

	continents := make([]string, 0, len(regionsByContinent))
	for continent := range regionsByContinent {
		continents = append(continents, continent)
	}
	sort.Strings(continents)

	regions := make([]GameServerRegion, 0, len(continents))
	for _, continent := range continents {
		regionIDs := regionsByContinent[continent]
		sort.Slice(regionIDs, func(i, j int) bool { return regionIDs[i] < regionIDs[j] })
		regions = append(regions, GameServerRegion{
			ContinentName: continent,
			Regions:       regionIDs,
		})
	}
	return regions, nil
}

// GetRegionsForGameServer is the compatibility wrapper for existing startup
// code. New code should inject SQLGameServerRegionRepository and propagate its
// context/error result instead.
func GetRegionsForGameServer(gameServerId int) []GameServerRegion {
	conn := frameworkdb.OpenConnShard()
	defer conn.Close()

	repository, err := NewGameServerRegionRepository(conn)
	if err != nil {
		return nil
	}
	regions, err := repository.GetRegionsForGameServer(context.Background(), gameServerId)
	if err != nil {
		return nil
	}
	return regions
}
