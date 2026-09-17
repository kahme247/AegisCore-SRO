package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

var ErrDimensionHoleEntryDestinationInvalid = errors.New("Dimension Hole entry destination is invalid")

// DimensionHoleEntryDestinationCatalog is an immutable, complete target
// catalog for the pinned 22-world vSRO 1.188 Dimension Hole family.
type DimensionHoleEntryDestinationCatalog struct {
	regions *model.DimensionHoleWorldRegionCatalog
	byWorld map[uint16][]DimensionHoleEntryDestination
	roll    func(int) int
}

func NewDimensionHoleEntryDestinationCatalog(regions *model.DimensionHoleWorldRegionCatalog, destinations []DimensionHoleEntryDestination, selectors ...func(int) int) (*DimensionHoleEntryDestinationCatalog, error) {
	definitions := model.DimensionHoleCatalog()
	if regions == nil || len(destinations) < len(definitions) || len(selectors) > 1 {
		return nil, fmt.Errorf("%w: got %d destinations, want at least %d", ErrDimensionHoleEntryDestinationInvalid, len(destinations), len(definitions))
	}
	roll := rand.Intn
	if len(selectors) == 1 {
		roll = selectors[0]
	}
	if roll == nil {
		return nil, ErrDimensionHoleEntryDestinationInvalid
	}
	byWorld := make(map[uint16][]DimensionHoleEntryDestination, len(definitions))
	for _, destination := range destinations {
		if _, ok := model.DimensionHoleDefinitionForWorld(destination.WorldID); !ok ||
			destination.Region == 0 ||
			!regions.Contains(destination.WorldID, destination.Region) ||
			!finiteDimensionHoleCoordinate(destination.X) ||
			!finiteDimensionHoleCoordinate(destination.Y) ||
			!finiteDimensionHoleCoordinate(destination.Z) {
			return nil, fmt.Errorf("%w: world %d", ErrDimensionHoleEntryDestinationInvalid, destination.WorldID)
		}
		byWorld[destination.WorldID] = append(byWorld[destination.WorldID], destination)
	}
	for _, definition := range definitions {
		if len(byWorld[definition.WorldID]) == 0 {
			return nil, fmt.Errorf("%w: missing world %d", ErrDimensionHoleEntryDestinationInvalid, definition.WorldID)
		}
	}
	return &DimensionHoleEntryDestinationCatalog{regions: regions, byWorld: byWorld, roll: roll}, nil
}

func NewDimensionHoleEntryDestinationCatalogFromStarts(regions *model.DimensionHoleWorldRegionCatalog, starts *model.DimensionHoleStartPositionCatalog, selectors ...func(int) int) (*DimensionHoleEntryDestinationCatalog, error) {
	if starts == nil {
		return nil, ErrDimensionHoleEntryDestinationInvalid
	}
	destinations := make([]DimensionHoleEntryDestination, 0, len(model.DimensionHoleCatalog()))
	for _, definition := range model.DimensionHoleCatalog() {
		for _, start := range starts.Positions(definition.WorldID) {
			destinations = append(destinations, DimensionHoleEntryDestination{
				WorldID: start.WorldID,
				Region:  start.RegionID,
				X:       float32(start.X),
				Y:       float32(start.Y),
				Z:       float32(start.Z),
			})
		}
	}
	return NewDimensionHoleEntryDestinationCatalog(regions, destinations, selectors...)
}

func (c *DimensionHoleEntryDestinationCatalog) ResolveDimensionHoleEntry(ctx context.Context, admission DimensionHoleAdmission) (DimensionHoleEntryDestination, error) {
	if c == nil || c.regions == nil || c.byWorld == nil || c.roll == nil {
		return DimensionHoleEntryDestination{}, ErrDimensionHoleEntryDestinationInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DimensionHoleEntryDestination{}, err
	}
	expected, ok := model.DimensionHoleDefinitionForWorld(admission.Definition.WorldID)
	if !ok || admission.Definition != expected || !admission.Scope.ValidInstance() ||
		admission.Scope.WorldID() != admission.Definition.WorldID ||
		admission.PortalUID == 0 || admission.PlayerUID == 0 {
		return DimensionHoleEntryDestination{}, ErrDimensionHoleEntryDestinationInvalid
	}
	destinations := c.byWorld[admission.Definition.WorldID]
	if len(destinations) == 0 {
		return DimensionHoleEntryDestination{}, ErrDimensionHoleEntryDestinationInvalid
	}
	index := c.roll(len(destinations))
	if index < 0 || index >= len(destinations) {
		return DimensionHoleEntryDestination{}, ErrDimensionHoleEntryDestinationInvalid
	}
	destination := destinations[index]
	if !c.regions.Contains(destination.WorldID, destination.Region) {
		return DimensionHoleEntryDestination{}, ErrDimensionHoleEntryDestinationInvalid
	}
	return destination, nil
}

func finiteDimensionHoleCoordinate(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}
