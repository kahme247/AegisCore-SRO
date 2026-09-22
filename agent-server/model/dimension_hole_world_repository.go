package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const (
	DimensionHoleWorldType             uint8  = 1
	DimensionHoleWorldMaxInstanceCount uint16 = 50
	DimensionHoleFirstWorldID          uint16 = 63
	DimensionHoleLastWorldID           uint16 = 84
	DimensionHoleWorldEntryType        uint8  = 0
	DimensionHoleWorldEntranceType     uint8  = 0
	DimensionHoleWorldLeaveType        uint8  = 0
	DimensionHoleWorldDurationMS       uint32 = 7_200_000
	DimensionHoleWorldEmptyRemainMS    uint32 = 600_000

	SelectDimensionHoleGameWorlds = `SELECT w.WORLD_ID,w.CODE_NAME,w.WORLD_TYPE,w.MAX_INSTANCE_COUNT,w.MAX_USER_COUNT,
w.WORLD_ENTRY_TYPE,w.WORLD_ENTRANCE_TYPE,w.WORLD_LEAVE_TYPE,w.WORLD_DURATION_MS,w.WORLD_EMPTY_REMAIN_MS,w.CONFIG_GROUP_CODE_NAME
FROM SRO_SHARD.REF_DIMENSION_HOLE h
JOIN SRO_SHARD.REF_GAME_WORLD w ON w.WORLD_ID=h.WORLD_ID
WHERE h.SERVICE=1
ORDER BY w.WORLD_ID`
)

var (
	ErrDimensionHoleWorldRepositoryUnavailable = errors.New("Dimension Hole world repository is unavailable")
	ErrDimensionHoleWorldCatalogMismatch       = errors.New("Dimension Hole world catalog does not match the database definition catalog")
)

// DimensionHoleWorldReference is one validated REF_GAME_WORLD row. The
// repository requires one matching world for every enabled database definition.
type DimensionHoleWorldReference struct {
	WorldID             uint16
	CodeName            string
	WorldType           uint8
	MaxInstanceCount    uint16
	MaxUserCount        uint16
	WorldEntryType      uint8
	WorldEntranceType   uint8
	WorldLeaveType      uint8
	WorldDurationMS     uint32
	WorldEmptyRemainMS  uint32
	ConfigGroupCodeName string
}

// DimensionHoleWorldCatalog is the immutable result of the read-only startup
// preflight. It proves database metadata, not authored dungeon content.
type DimensionHoleWorldCatalog struct {
	worlds []DimensionHoleWorldReference
	byID   map[uint16]DimensionHoleWorldReference
}

func (c *DimensionHoleWorldCatalog) Worlds() []DimensionHoleWorldReference {
	if c == nil {
		return nil
	}
	worlds := make([]DimensionHoleWorldReference, len(c.worlds))
	copy(worlds, c.worlds)
	return worlds
}

func (c *DimensionHoleWorldCatalog) World(worldID uint16) (DimensionHoleWorldReference, bool) {
	if c == nil {
		return DimensionHoleWorldReference{}, false
	}
	world, ok := c.byID[worldID]
	return world, ok
}

type DimensionHoleWorldRepository interface {
	LoadDimensionHoleWorldCatalog(context.Context) (*DimensionHoleWorldCatalog, error)
}

type SQLDimensionHoleWorldRepository struct {
	shard frameworkdb.Queryer
}

var _ DimensionHoleWorldRepository = (*SQLDimensionHoleWorldRepository)(nil)

type DimensionHoleWorldRepositoryError struct {
	Operation string
	WorldID   uint16
	Err       error
}

func (e *DimensionHoleWorldRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.WorldID != 0 {
		return fmt.Sprintf("Dimension Hole world repository %s world %d: %v", e.Operation, e.WorldID, e.Err)
	}
	return fmt.Sprintf("Dimension Hole world repository %s: %v", e.Operation, e.Err)
}

func (e *DimensionHoleWorldRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewDimensionHoleWorldRepository(shard frameworkdb.Queryer) (*SQLDimensionHoleWorldRepository, error) {
	if shard == nil {
		return nil, &DimensionHoleWorldRepositoryError{
			Operation: "configure",
			Err:       ErrDimensionHoleWorldRepositoryUnavailable,
		}
	}
	return &SQLDimensionHoleWorldRepository{shard: shard}, nil
}

func (r *SQLDimensionHoleWorldRepository) LoadDimensionHoleWorldCatalog(ctx context.Context) (*DimensionHoleWorldCatalog, error) {
	if r == nil || r.shard == nil {
		return nil, &DimensionHoleWorldRepositoryError{Operation: "query", Err: ErrDimensionHoleWorldRepositoryUnavailable}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, &DimensionHoleWorldRepositoryError{Operation: "query", Err: err}
	}
	definitions := DimensionHoleCatalog()
	if len(definitions) == 0 {
		return nil, dimensionHoleWorldMismatch(0, "definition catalog is empty")
	}

	rows, err := r.shard.QueryContext(ctx, SelectDimensionHoleGameWorlds)
	if err != nil {
		return nil, &DimensionHoleWorldRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	worlds := make([]DimensionHoleWorldReference, 0, len(definitions))
	for rows.Next() {
		var world DimensionHoleWorldReference
		if err = rows.Scan(
			&world.WorldID,
			&world.CodeName,
			&world.WorldType,
			&world.MaxInstanceCount,
			&world.MaxUserCount,
			&world.WorldEntryType,
			&world.WorldEntranceType,
			&world.WorldLeaveType,
			&world.WorldDurationMS,
			&world.WorldEmptyRemainMS,
			&world.ConfigGroupCodeName,
		); err != nil {
			return nil, &DimensionHoleWorldRepositoryError{Operation: "scan", Err: err}
		}
		if len(worlds) >= len(definitions) {
			return nil, dimensionHoleWorldMismatch(world.WorldID, "unexpected extra row")
		}
		expected := definitions[len(worlds)]
		if world.WorldID != expected.WorldID {
			return nil, dimensionHoleWorldMismatch(expected.WorldID, fmt.Sprintf("expected world %d before world %d", expected.WorldID, world.WorldID))
		}
		if err = validateDimensionHoleWorldReference(world, expected); err != nil {
			return nil, err
		}
		worlds = append(worlds, world)
	}
	if err = rows.Err(); err != nil {
		return nil, &DimensionHoleWorldRepositoryError{Operation: "iterate", Err: err}
	}
	return NewDimensionHoleWorldCatalog(worlds)
}

// NewDimensionHoleWorldCatalog validates a complete immutable catalog before
// it can reach scope allocation.
func NewDimensionHoleWorldCatalog(worlds []DimensionHoleWorldReference) (*DimensionHoleWorldCatalog, error) {
	definitions := DimensionHoleCatalog()
	if len(definitions) == 0 {
		return nil, dimensionHoleWorldMismatch(0, "definition catalog is empty")
	}
	if len(worlds) < len(definitions) {
		missing := definitions[len(worlds)].WorldID
		return nil, dimensionHoleWorldMismatch(missing, "missing row")
	}
	if len(worlds) > len(definitions) {
		return nil, dimensionHoleWorldMismatch(worlds[len(definitions)].WorldID, "unexpected extra row")
	}
	for index, definition := range definitions {
		world := worlds[index]
		if world.WorldID != definition.WorldID {
			return nil, dimensionHoleWorldMismatch(definition.WorldID, fmt.Sprintf("expected world %d before world %d", definition.WorldID, world.WorldID))
		}
		if err := validateDimensionHoleWorldReference(world, definition); err != nil {
			return nil, err
		}
	}
	return newDimensionHoleWorldCatalog(worlds), nil
}

func newDimensionHoleWorldCatalog(worlds []DimensionHoleWorldReference) *DimensionHoleWorldCatalog {
	owned := make([]DimensionHoleWorldReference, len(worlds))
	copy(owned, worlds)
	byID := make(map[uint16]DimensionHoleWorldReference, len(owned))
	for _, world := range owned {
		byID[world.WorldID] = world
	}
	return &DimensionHoleWorldCatalog{worlds: owned, byID: byID}
}

func validateDimensionHoleWorldReference(world DimensionHoleWorldReference, definition DimensionHoleDefinition) error {
	wantGroup := "GROUP_" + strings.TrimPrefix(definition.WorldCodeName, "INS_")
	if world.CodeName != definition.WorldCodeName ||
		world.WorldType != DimensionHoleWorldType ||
		world.MaxInstanceCount != DimensionHoleWorldMaxInstanceCount ||
		world.MaxUserCount != definition.Capacity ||
		world.WorldEntryType != DimensionHoleWorldEntryType ||
		world.WorldEntranceType != DimensionHoleWorldEntranceType ||
		world.WorldLeaveType != DimensionHoleWorldLeaveType ||
		world.WorldDurationMS != DimensionHoleWorldDurationMS ||
		world.WorldEmptyRemainMS != DimensionHoleWorldEmptyRemainMS ||
		world.ConfigGroupCodeName != wantGroup {
		return dimensionHoleWorldMismatch(world.WorldID, fmt.Sprintf(
			"got code=%q type=%d instances=%d users=%d entry=%d entrance=%d leave=%d duration=%d empty=%d group=%q; want code=%q type=%d instances=%d users=%d entry=%d entrance=%d leave=%d duration=%d empty=%d group=%q",
			world.CodeName, world.WorldType, world.MaxInstanceCount, world.MaxUserCount, world.WorldEntryType, world.WorldEntranceType, world.WorldLeaveType, world.WorldDurationMS, world.WorldEmptyRemainMS, world.ConfigGroupCodeName,
			definition.WorldCodeName, DimensionHoleWorldType, DimensionHoleWorldMaxInstanceCount, definition.Capacity, DimensionHoleWorldEntryType, DimensionHoleWorldEntranceType, DimensionHoleWorldLeaveType, DimensionHoleWorldDurationMS, DimensionHoleWorldEmptyRemainMS, wantGroup,
		))
	}
	return nil
}

func dimensionHoleWorldMismatch(worldID uint16, detail string) error {
	return &DimensionHoleWorldRepositoryError{
		Operation: "validate",
		WorldID:   worldID,
		Err:       fmt.Errorf("%w: %s", ErrDimensionHoleWorldCatalogMismatch, detail),
	}
}
