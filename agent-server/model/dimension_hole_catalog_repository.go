package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var ErrDimensionHoleCatalogRepositoryUnavailable = errors.New("Dimension Hole catalog repository is unavailable")

const SelectDimensionHoleCatalog = `SELECT h.ITEM_REF_ID,i.CodeName,h.WORLD_ID,w.CODE_NAME,w.MAX_USER_COUNT,
 i.CanUse,i.TypeID_1,i.TypeID_2,i.TypeID_3,i.TypeID_4,
 i.Param_1,i.Param_2,i.Param_3,i.Param_4,i.Param_5,i.Param_6,i.Param_7,i.Param_8,i.Param_9,i.Param_10
FROM SRO_SHARD.REF_DIMENSION_HOLE h
JOIN SRO_SHARD.ITEMDATA i ON i.ItemID=h.ITEM_REF_ID
JOIN SRO_SHARD.REF_GAME_WORLD w ON w.WORLD_ID=h.WORLD_ID
WHERE h.SERVICE=1
ORDER BY h.WORLD_ID`

type DimensionHoleCatalogRepository interface {
	LoadDimensionHoleCatalog(context.Context) ([]DimensionHoleDefinition, error)
}

type SQLDimensionHoleCatalogRepository struct{ shard frameworkdb.Queryer }

func NewDimensionHoleCatalogRepository(shard frameworkdb.Queryer) (*SQLDimensionHoleCatalogRepository, error) {
	if shard == nil {
		return nil, ErrDimensionHoleCatalogRepositoryUnavailable
	}
	return &SQLDimensionHoleCatalogRepository{shard: shard}, nil
}

func (r *SQLDimensionHoleCatalogRepository) LoadDimensionHoleCatalog(ctx context.Context) ([]DimensionHoleDefinition, error) {
	if r == nil || r.shard == nil {
		return nil, ErrDimensionHoleCatalogRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.shard.QueryContext(ctx, SelectDimensionHoleCatalog)
	if err != nil {
		return nil, fmt.Errorf("query Dimension Hole catalog: %w", err)
	}
	defer rows.Close()

	definitions := make([]DimensionHoleDefinition, 0)
	for rows.Next() {
		var definition DimensionHoleDefinition
		var canUse, typeID1, typeID2, typeID3, typeID4 int
		var param1, param2, param3, param4, param5, param6, param7, param8, param9, param10 int
		if err := rows.Scan(
			&definition.ItemRefID, &definition.ItemCodeName, &definition.WorldID, &definition.WorldCodeName, &definition.Capacity,
			&canUse, &typeID1, &typeID2, &typeID3, &typeID4,
			&param1, &param2, &param3, &param4, &param5, &param6, &param7, &param8, &param9, &param10,
		); err != nil {
			return nil, fmt.Errorf("scan Dimension Hole catalog: %w", err)
		}
		if canUse == 0 || typeID1 != 3 || typeID2 != 3 || typeID3 != 12 || typeID4 != 7 ||
			param1 <= 0 || param1 > 255 || param2 != 0 || param3 <= 0 || param3 > 65535 ||
			param4 < param3 || param4 > 65535 || param5 <= 0 || param5 > 65535 ||
			param6 != 6 || param7 != 1 || param8 != -1 || param9 != -1 || param10 <= 0 ||
			int(definition.Capacity) != param5 {
			return nil, fmt.Errorf("Dimension Hole catalog item %d and world %d metadata mismatch", definition.ItemRefID, definition.WorldID)
		}
		definition.Difficulty = uint8(param1)
		definition.MinLevel = uint16(param3)
		definition.MaxLevel = uint16(param4)
		definition.Cooldown = time.Duration(param10) * time.Millisecond
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Dimension Hole catalog: %w", err)
	}
	if err := InstallDimensionHoleCatalog(definitions); err != nil {
		return nil, fmt.Errorf("install Dimension Hole catalog: %w", err)
	}
	return DimensionHoleCatalog(), nil
}
