package model

import (
	"context"
	"errors"
	"fmt"
	"slices"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const SelectDimensionHoleGameWorldRegions = `SELECT r.WORLD_ID,r.REGION_ID
FROM SRO_SHARD.REF_DIMENSION_HOLE h
JOIN SRO_SHARD.REF_GAME_WORLD_REGION r ON r.WORLD_ID=h.WORLD_ID
WHERE h.SERVICE=1
ORDER BY r.WORLD_ID,r.REGION_ID`

var (
	ErrDimensionHoleWorldRegionRepositoryUnavailable = errors.New("Dimension Hole world-region repository is unavailable")
	ErrDimensionHoleWorldRegionCatalogMismatch       = errors.New("Dimension Hole world-region catalog does not match the database definition catalog")
)

type DimensionHoleWorldRegionReference struct {
	WorldID  uint16
	RegionID int16
}

type DimensionHoleWorldRegionCatalog struct {
	byWorld map[uint16]map[int16]struct{}
}

func (c *DimensionHoleWorldRegionCatalog) Contains(worldID uint16, regionID int16) bool {
	if c == nil || c.byWorld == nil {
		return false
	}
	_, ok := c.byWorld[worldID][regionID]
	return ok
}

func (c *DimensionHoleWorldRegionCatalog) Regions(worldID uint16) []int16 {
	if c == nil || c.byWorld == nil {
		return nil
	}
	regions := make([]int16, 0, len(c.byWorld[worldID]))
	for regionID := range c.byWorld[worldID] {
		regions = append(regions, regionID)
	}
	slices.Sort(regions)
	return regions
}

type DimensionHoleWorldRegionRepository interface {
	LoadDimensionHoleWorldRegionCatalog(context.Context) (*DimensionHoleWorldRegionCatalog, error)
}

type SQLDimensionHoleWorldRegionRepository struct {
	shard frameworkdb.Queryer
}

var _ DimensionHoleWorldRegionRepository = (*SQLDimensionHoleWorldRegionRepository)(nil)

func NewDimensionHoleWorldRegionRepository(shard frameworkdb.Queryer) (*SQLDimensionHoleWorldRegionRepository, error) {
	if shard == nil {
		return nil, ErrDimensionHoleWorldRegionRepositoryUnavailable
	}
	return &SQLDimensionHoleWorldRegionRepository{shard: shard}, nil
}

func (r *SQLDimensionHoleWorldRegionRepository) LoadDimensionHoleWorldRegionCatalog(ctx context.Context) (*DimensionHoleWorldRegionCatalog, error) {
	if r == nil || r.shard == nil {
		return nil, ErrDimensionHoleWorldRegionRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := r.shard.QueryContext(ctx, SelectDimensionHoleGameWorldRegions)
	if err != nil {
		return nil, fmt.Errorf("query Dimension Hole world regions: %w", err)
	}
	defer rows.Close()
	references := make([]DimensionHoleWorldRegionReference, 0, 2088)
	for rows.Next() {
		var reference DimensionHoleWorldRegionReference
		if err = rows.Scan(&reference.WorldID, &reference.RegionID); err != nil {
			return nil, fmt.Errorf("scan Dimension Hole world region: %w", err)
		}
		references = append(references, reference)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Dimension Hole world regions: %w", err)
	}
	return NewDimensionHoleWorldRegionCatalog(references)
}

func NewDimensionHoleWorldRegionCatalog(references []DimensionHoleWorldRegionReference) (*DimensionHoleWorldRegionCatalog, error) {
	definitions := DimensionHoleCatalog()
	if len(definitions) == 0 {
		return nil, dimensionHoleWorldRegionMismatch(0, "definition catalog is empty")
	}
	byWorld := make(map[uint16]map[int16]struct{}, len(definitions))
	for _, reference := range references {
		if reference.RegionID == 0 {
			return nil, dimensionHoleWorldRegionMismatch(reference.WorldID, "zero region")
		}
		if _, ok := DimensionHoleDefinitionForWorld(reference.WorldID); !ok {
			return nil, dimensionHoleWorldRegionMismatch(reference.WorldID, "unknown world")
		}
		if byWorld[reference.WorldID] == nil {
			byWorld[reference.WorldID] = make(map[int16]struct{})
		}
		if _, duplicate := byWorld[reference.WorldID][reference.RegionID]; duplicate {
			return nil, dimensionHoleWorldRegionMismatch(reference.WorldID, fmt.Sprintf("duplicate region %d", reference.RegionID))
		}
		byWorld[reference.WorldID][reference.RegionID] = struct{}{}
	}
	for _, definition := range definitions {
		if len(byWorld[definition.WorldID]) == 0 {
			return nil, dimensionHoleWorldRegionMismatch(definition.WorldID, "missing regions")
		}
	}
	return &DimensionHoleWorldRegionCatalog{byWorld: byWorld}, nil
}

func dimensionHoleWorldRegionMismatch(worldID uint16, detail string) error {
	return fmt.Errorf("%w: world %d: %s", ErrDimensionHoleWorldRegionCatalogMismatch, worldID, detail)
}
