package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

type Spawn struct {
	NestID                int
	Position              navmeshv2.RtNavmeshPosition
	RefObjID              uint64
	NpcCodeName           string
	Radius                int
	GenerateRadius        int
	MaxTotalCount         int
	DelayTimeMin          int
	DelayTimeMax          int
	SightRange            int
	AggressType           byte
	ChampionGenPercentage int
}

type SpawnDB struct {
	NestID                int
	X                     float32
	Y                     float32
	Z                     float32
	Heading               int
	RegionID              int16
	RefObjID              uint64
	NpcCodeName           string
	Radius                int
	GenerateRadius        int
	MaxTotalCount         int
	DelayTimeMin          int
	DelayTimeMax          int
	SightRange            int
	AggressType           byte
	ChampionGenPercentage int
}

const SelectSpawnsForContinent string = `SELECT n.dwNestID, r.wRegionID, n.fLocalPosX, n.fLocalPosY, n.fLocalPosZ, n.wInitialDir, t.ObjID, c.CodeName, n.nRadius, n.nGenerateRadius, n.nChampionGenPercentage, n.dwMaxTotalCount, n.dwDelayTimeMin, n.dwDelayTimeMax, t.SightRange, t.AggressType
FROM SRO_SHARD.REGION_REFERENCE r, SRO_SHARD.SPAWN_REF_NESTS n, SRO_SHARD.SPAWN_REF_TACTICS t, SRO_SHARD.CHAR_REF_DATA c
WHERE r.ContinentName=?
AND r.wRegionID = n.nRegionDBID
AND n.dwTacticsID = t.TacticsID
AND t.ObjID = c.RefObjID
ORDER BY n.dwNestID ASC, r.wRegionID ASC, t.ObjID ASC, c.CodeName ASC`

// ErrContinentSpawnRepositoryUnavailable identifies a missing injected shard
// queryer. New startup paths must surface the failure instead of opening a
// connection per catalog load.
var ErrContinentSpawnRepositoryUnavailable = errors.New("continent spawn repository is unavailable")

// ContinentSpawnRepositoryError identifies the catalog operation that failed
// while preserving the underlying driver, context, scan, or row error.
type ContinentSpawnRepositoryError struct {
	Operation string
	Err       error
}

func (e *ContinentSpawnRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("continent spawn repository %s: %v", e.Operation, e.Err)
}

func (e *ContinentSpawnRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ContinentSpawnRepository is the context-aware persistence boundary for
// continent spawn catalogs. Implementations do not own or close the supplied
// queryer.
type ContinentSpawnRepository interface {
	GetSpawnsForContinent(ctx context.Context, continent string) ([]SpawnDB, error)
}

// SQLContinentSpawnRepository queries the spawn catalog through an injected
// long-lived pool or a transaction owned by a higher-level unit of work.
type SQLContinentSpawnRepository struct {
	shard frameworkdb.Queryer
}

var _ ContinentSpawnRepository = (*SQLContinentSpawnRepository)(nil)

// NewContinentSpawnRepository constructs the production catalog repository
// from an injected shard pool or transaction.
func NewContinentSpawnRepository(shard frameworkdb.Queryer) (*SQLContinentSpawnRepository, error) {
	if shard == nil {
		return nil, &ContinentSpawnRepositoryError{
			Operation: "configure",
			Err:       ErrContinentSpawnRepositoryUnavailable,
		}
	}
	return &SQLContinentSpawnRepository{shard: shard}, nil
}

// GetSpawnsForContinent returns a deterministic spawn catalog. The total sort
// is intentional: it prevents the world bootstrap from inheriting an
// unspecified database row order if a driver or query plan changes.
func (r *SQLContinentSpawnRepository) GetSpawnsForContinent(ctx context.Context, continent string) ([]SpawnDB, error) {
	if r == nil || r.shard == nil {
		return nil, &ContinentSpawnRepositoryError{
			Operation: "query",
			Err:       ErrContinentSpawnRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, &ContinentSpawnRepositoryError{Operation: "query", Err: err}
	}

	rows, err := r.shard.QueryContext(ctx, SelectSpawnsForContinent, continent)
	if err != nil {
		return nil, &ContinentSpawnRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	spawns := make([]SpawnDB, 0)
	for rows.Next() {
		var regionID int16
		var x, y, z float32
		var refObjID uint64
		var codeName string
		var nestID, heading, radius, generateRadius, championGenPercentage, maxTotalCount, delayTimeMin, delayTimeMax, sightRange int
		var aggressType byte

		if err := rows.Scan(&nestID, &regionID, &x, &y, &z, &heading, &refObjID, &codeName, &radius, &generateRadius, &championGenPercentage, &maxTotalCount, &delayTimeMin, &delayTimeMax, &sightRange, &aggressType); err != nil {
			return nil, &ContinentSpawnRepositoryError{Operation: "scan", Err: err}
		}

		spawns = append(spawns, SpawnDB{
			NestID:                nestID,
			RegionID:              regionID,
			X:                     x,
			Y:                     y,
			Z:                     z,
			Heading:               heading,
			RefObjID:              refObjID,
			NpcCodeName:           codeName,
			Radius:                radius,
			GenerateRadius:        generateRadius,
			ChampionGenPercentage: championGenPercentage,
			MaxTotalCount:         maxTotalCount,
			DelayTimeMin:          delayTimeMin,
			DelayTimeMax:          delayTimeMax,
			SightRange:            sightRange,
			AggressType:           aggressType,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, &ContinentSpawnRepositoryError{Operation: "iterate", Err: err}
	}

	sort.Slice(spawns, func(i, j int) bool { return lessSpawnDB(spawns[i], spawns[j]) })
	return spawns, nil
}

func lessSpawnDB(left, right SpawnDB) bool {
	if left.NestID != right.NestID {
		return left.NestID < right.NestID
	}
	if left.RegionID != right.RegionID {
		return left.RegionID < right.RegionID
	}
	if left.RefObjID != right.RefObjID {
		return left.RefObjID < right.RefObjID
	}
	if left.NpcCodeName != right.NpcCodeName {
		return left.NpcCodeName < right.NpcCodeName
	}
	if leftX, rightX := math.Float32bits(left.X), math.Float32bits(right.X); leftX != rightX {
		return leftX < rightX
	}
	if leftY, rightY := math.Float32bits(left.Y), math.Float32bits(right.Y); leftY != rightY {
		return leftY < rightY
	}
	if leftZ, rightZ := math.Float32bits(left.Z), math.Float32bits(right.Z); leftZ != rightZ {
		return leftZ < rightZ
	}
	if left.Heading != right.Heading {
		return left.Heading < right.Heading
	}
	if left.Radius != right.Radius {
		return left.Radius < right.Radius
	}
	if left.GenerateRadius != right.GenerateRadius {
		return left.GenerateRadius < right.GenerateRadius
	}
	if left.MaxTotalCount != right.MaxTotalCount {
		return left.MaxTotalCount < right.MaxTotalCount
	}
	if left.DelayTimeMin != right.DelayTimeMin {
		return left.DelayTimeMin < right.DelayTimeMin
	}
	return left.DelayTimeMax < right.DelayTimeMax
}

// GetSpawnsForContinent is the compatibility wrapper for existing world
// startup code. New code should inject SQLContinentSpawnRepository and
// propagate its context/error result instead.
func GetSpawnsForContinent(continent string) []SpawnDB {
	conn := frameworkdb.OpenConnShard()
	defer conn.Close()

	repository, err := NewContinentSpawnRepository(conn)
	if err != nil {
		return nil
	}
	spawns, err := repository.GetSpawnsForContinent(context.Background(), continent)
	if err != nil {
		return nil
	}
	return spawns
}
