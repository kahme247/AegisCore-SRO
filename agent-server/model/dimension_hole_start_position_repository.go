package model

import (
	"context"
	"errors"
	"fmt"
	"slices"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const SelectDimensionHoleStartPositions = `SELECT p.WORLD_ID,p.START_INDEX,p.REGION_ID,p.POS_X,p.POS_Y,p.POS_Z,p.PARAM_VALUE
FROM SRO_SHARD.REF_DIMENSION_HOLE h
JOIN SRO_SHARD.REF_INSTANCE_WORLD_START_POSITION p ON p.WORLD_ID=h.WORLD_ID
WHERE h.SERVICE=1
ORDER BY p.WORLD_ID,p.START_INDEX`

var (
	ErrDimensionHoleStartPositionRepositoryUnavailable = errors.New("Dimension Hole start-position repository is unavailable")
	ErrDimensionHoleStartPositionCatalogMismatch       = errors.New("Dimension Hole start-position catalog does not match the database definition catalog")
)

// DimensionHoleStartPositionReference preserves the stock
// _RefInstance_World_Start_Pos integer field widths. START_INDEX is a stable
// local ordering key because the stock table permits multiple rows per world.
type DimensionHoleStartPositionReference struct {
	WorldID    uint16
	StartIndex uint16
	RegionID   int16
	X          int32
	Y          int32
	Z          int32
	Param      int32
}

type DimensionHoleStartPositionCatalog struct {
	byWorld map[uint16][]DimensionHoleStartPositionReference
}

func (c *DimensionHoleStartPositionCatalog) Positions(worldID uint16) []DimensionHoleStartPositionReference {
	if c == nil || c.byWorld == nil {
		return nil
	}
	return slices.Clone(c.byWorld[worldID])
}

type DimensionHoleStartPositionRepository interface {
	LoadDimensionHoleStartPositionCatalog(context.Context, *DimensionHoleWorldRegionCatalog) (*DimensionHoleStartPositionCatalog, error)
}

type SQLDimensionHoleStartPositionRepository struct {
	shard frameworkdb.Queryer
}

var _ DimensionHoleStartPositionRepository = (*SQLDimensionHoleStartPositionRepository)(nil)

func NewDimensionHoleStartPositionRepository(shard frameworkdb.Queryer) (*SQLDimensionHoleStartPositionRepository, error) {
	if shard == nil {
		return nil, ErrDimensionHoleStartPositionRepositoryUnavailable
	}
	return &SQLDimensionHoleStartPositionRepository{shard: shard}, nil
}

func (r *SQLDimensionHoleStartPositionRepository) LoadDimensionHoleStartPositionCatalog(ctx context.Context, regions *DimensionHoleWorldRegionCatalog) (*DimensionHoleStartPositionCatalog, error) {
	if r == nil || r.shard == nil {
		return nil, ErrDimensionHoleStartPositionRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	definitions := DimensionHoleCatalog()
	if len(definitions) == 0 {
		return nil, dimensionHoleStartPositionMismatch(0, "definition catalog is empty")
	}
	rows, err := r.shard.QueryContext(ctx, SelectDimensionHoleStartPositions)
	if err != nil {
		return nil, fmt.Errorf("query Dimension Hole start positions: %w", err)
	}
	defer rows.Close()
	references := make([]DimensionHoleStartPositionReference, 0, len(definitions))
	for rows.Next() {
		var reference DimensionHoleStartPositionReference
		if err = rows.Scan(&reference.WorldID, &reference.StartIndex, &reference.RegionID, &reference.X, &reference.Y, &reference.Z, &reference.Param); err != nil {
			return nil, fmt.Errorf("scan Dimension Hole start position: %w", err)
		}
		references = append(references, reference)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Dimension Hole start positions: %w", err)
	}
	return NewDimensionHoleStartPositionCatalog(regions, references)
}

func NewDimensionHoleStartPositionCatalog(regions *DimensionHoleWorldRegionCatalog, references []DimensionHoleStartPositionReference) (*DimensionHoleStartPositionCatalog, error) {
	if regions == nil {
		return nil, dimensionHoleStartPositionMismatch(0, "world-region catalog is unavailable")
	}
	definitions := DimensionHoleCatalog()
	if len(definitions) == 0 {
		return nil, dimensionHoleStartPositionMismatch(0, "definition catalog is empty")
	}
	byWorld := make(map[uint16][]DimensionHoleStartPositionReference, len(definitions))
	seen := make(map[[2]uint16]struct{}, len(references))
	for _, reference := range references {
		if _, ok := DimensionHoleDefinitionForWorld(reference.WorldID); !ok {
			return nil, dimensionHoleStartPositionMismatch(reference.WorldID, "unknown world")
		}
		if reference.RegionID == 0 || !regions.Contains(reference.WorldID, reference.RegionID) {
			return nil, dimensionHoleStartPositionMismatch(reference.WorldID, fmt.Sprintf("region %d is outside the world", reference.RegionID))
		}
		if reference.Param != 0 {
			return nil, dimensionHoleStartPositionMismatch(reference.WorldID, fmt.Sprintf("unsupported Param %d", reference.Param))
		}
		key := [2]uint16{reference.WorldID, reference.StartIndex}
		if _, duplicate := seen[key]; duplicate {
			return nil, dimensionHoleStartPositionMismatch(reference.WorldID, fmt.Sprintf("duplicate start index %d", reference.StartIndex))
		}
		seen[key] = struct{}{}
		byWorld[reference.WorldID] = append(byWorld[reference.WorldID], reference)
	}
	for _, definition := range definitions {
		if len(byWorld[definition.WorldID]) == 0 {
			return nil, dimensionHoleStartPositionMismatch(definition.WorldID, "missing start position")
		}
		slices.SortFunc(byWorld[definition.WorldID], func(left, right DimensionHoleStartPositionReference) int {
			return int(left.StartIndex) - int(right.StartIndex)
		})
	}
	return &DimensionHoleStartPositionCatalog{byWorld: byWorld}, nil
}

func dimensionHoleStartPositionMismatch(worldID uint16, detail string) error {
	return fmt.Errorf("%w: world %d: %s", ErrDimensionHoleStartPositionCatalogMismatch, worldID, detail)
}
